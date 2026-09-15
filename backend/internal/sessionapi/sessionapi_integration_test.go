//go:build integration

// The sessions API through the real /v1 chain, against real Postgres and Redis
// (P3-09).
//
// Every assertion that matters here is about a CONSEQUENCE: a revoked session
// is refused when its cookie is presented, a revoked token's next API call is
// refused, a refresh token's row is revoked. A test on a column would pass
// against a system that updates the row and keeps honouring the cache.
package sessionapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/account"
	"github.com/zed378/zed-auth/backend/internal/anomaly"
	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/authz"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/mfaapi"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/projectgrant"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/role"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/internal/user"
)

const issuer = "https://auth.example.test"

const (
	windowsChrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	sessionIP     = "198.51.100.23"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db       *postgres.DB
	factory  *testsupport.Factory
	handler  http.Handler
	signer   *signing.Signer
	sessions *session.Manager
	api      *Handler
	guard    *countingGuard

	orgA, orgB string
	clientID   string

	member, colleague, admin, outsider string
}

// countingGuard counts successful mutations that recorded nothing.
type countingGuard struct {
	mu     sync.Mutex
	missed []string
}

func (c *countingGuard) MutationNotAudited(route string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.missed = append(c.missed, route)
}

func (c *countingGuard) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.missed)
}

// placeLocator resolves the fixture's one address.
type placeLocator struct{}

func (placeLocator) Locate(ip string) anomaly.Location {
	if ip == sessionIP {
		return anomaly.Location{City: "Jakarta", Country: "ID", Latitude: -6.2, Longitude: 106.8}
	}
	return anomaly.Location{}
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	member := factory.User(orgA, "member@example.test")
	colleague := factory.User(orgA, "colleague@example.test")
	admin := factory.User(orgA, "admin@example.test")
	outsider := factory.User(orgB, "outsider@example.test")
	factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`, admin, orgA)
	factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`, outsider, orgB)

	var projectID, clientID string
	factory.QueryRow(&projectID, `INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgA)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'console', 'web') RETURNING id`,
		projectID, orgA)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keys := signingKeys(t, stack)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(db, discard(), nil)
	guard := &countingGuard{}
	sessions := session.NewManager(db, session.NewCache(rdb, nil), auditor, discard())

	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
			// The sid check, as production wires it: a token whose session
			// has ended is refused at the door.
			Sessions: sessions,
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 1000, Window: time.Minute}, ""),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard(), Observer: guard},
		BufferBody:  true,
	}

	api := &Handler{
		Audit:    auditor,
		Sessions: sessions,
		Refresh:  token.NewRefreshStore(),
		Members:  user.NewStore(),
		DB:       db,
		Policy:   session.DefaultPolicy,
		Log:      discard(),
	}

	srv := httpserver.New(config.HTTPConfig{
		Addr: "127.0.0.1:0", ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second,
	}, httpserver.Deps{
		Logger: discard(),
		Health: &httpserver.Health{},
		V1:     chain,
		Organizations: &organization.Handler{
			Store: organization.NewStore(), DB: db, Audit: auditor, Log: discard(),
		},
		ProjectAPI:      &project.Handler{Store: project.NewStore(), DB: db, Audit: auditor, Log: discard()},
		ApplicationAPI:  application.New(db, auditor, discard()),
		RoleAPI:         role.New(db, auditor, discard()),
		GrantAPI:        grant.New(db, auditor, discard()),
		AuthzAPI:        &authz.Handler{DB: db, Log: discard()},
		UserAPI:         &user.Handler{Store: user.NewStore(), DB: db, Audit: auditor, Log: discard()},
		SessionAPI:      api,
		MfaAPI:          &mfaapi.Handler{},       // not exercised here
		ProjectGrantAPI: &projectgrant.Handler{}, // not exercised here
		AccountAPI:      &account.Handler{},      // not exercised here
		AuditAPI:        &auditlog.Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, factory: factory, handler: srv.Handler(), signer: signing.NewSigner(keys),
		sessions: sessions, api: api, guard: guard,
		orgA: orgA, orgB: orgB, clientID: clientID,
		member: member, colleague: colleague, admin: admin, outsider: outsider,
	}
}

func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("inserting the key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("rotating: %v", err)
	}
	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

// signIn creates a real session for a user and returns it with its cookie
// value.
func (f *fixture) signIn(t *testing.T, orgID, userID string) (session.Session, string) {
	t.Helper()

	var (
		created session.Session
		tok     session.Token
	)
	if err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		created, tok, err = f.sessions.Create(context.Background(), tx, session.New{
			UserID: userID, OrgID: orgID, AuthMethods: []string{"pwd"},
			IP: sessionIP, UserAgent: windowsChrome,
		}, session.DefaultPolicy, time.Now())
		return err
	}); err != nil {
		t.Fatalf("creating a session: %v", err)
	}
	cookie := tok.Reveal()

	// Warm the cache, so every "refused afterwards" below has to come through
	// invalidation rather than a cold read.
	if _, err := f.sessions.Lookup(context.Background(), cookie, session.DefaultPolicy, time.Now()); err != nil {
		t.Fatalf("warming the session cache: %v", err)
	}
	return created, cookie
}

func (f *fixture) cookieWorks(cookie string) bool {
	_, err := f.sessions.Lookup(context.Background(), cookie, session.DefaultPolicy, time.Now())
	return err == nil
}

func (f *fixture) issueRefresh(t *testing.T, orgID, userID, sessionID string) {
	t.Helper()
	if err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		_, _, err := token.NewRefreshStore().Issue(context.Background(), tx, token.Refresh{
			UserID: userID, ClientID: f.clientID, OrgID: orgID, SessionID: sessionID,
			Scope: []string{"openid"}, ExpiresAt: time.Now().Add(token.RefreshTokenLifetime),
		}, "", time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing a refresh token: %v", err)
	}
}

func (f *fixture) liveRefresh(t *testing.T, sessionID string) int {
	t.Helper()
	var n int
	f.factory.QueryRow(&n, `SELECT count(*) FROM refresh_tokens WHERE session_id = $1 AND NOT revoked`, sessionID)
	return n
}

// bearer signs an access token for a user, bound to a session when one is given.
func (f *fixture) bearer(t *testing.T, orgID, userID, sessionID string) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: orgID, UserID: userID, SessionID: sessionID, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	signed, err := f.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func (f *fixture) call(t *testing.T, bearer, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

type listBody struct {
	Sessions []struct {
		ID       string  `json:"id"`
		Location *string `json:"location"`
		Device   struct {
			Browser *string `json:"browser"`
			OS      *string `json:"os"`
		} `json:"device"`
		AuthMethods  []string  `json:"auth_methods"`
		LastActiveAt time.Time `json:"last_active_at"`
		Current      bool      `json:"current"`
	} `json:"sessions"`
	PageInfo *struct {
		NextPageToken *string `json:"next_page_token"`
	} `json:"page_info"`
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) listBody {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", w.Code, w.Body.String())
	}
	var out listBody
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding: %v\n%s", err, w.Body.String())
	}
	return out
}

type revokedEvent struct {
	Actor   string         `json:"actor"`
	Payload map[string]any `json:"payload"`
}

func (f *fixture) revocations(t *testing.T) []revokedEvent {
	t.Helper()
	var raw string
	f.factory.QueryRow(&raw, `
		SELECT COALESCE(json_agg(json_build_object('actor', actor_user_id, 'payload', payload)
		       ORDER BY created_at, id)::text, '[]')
		  FROM events WHERE event_type = 'session.revoked'`)
	var out []revokedEvent
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decoding events: %v", err)
	}
	return out
}

// --- F-1 / A-3: the caller's own list ----------------------------------------------------------

func TestAUserListsOnlyTheirOwnSessions(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	other, _ := f.signIn(t, f.orgA, f.member)
	f.signIn(t, f.orgA, f.colleague)

	w := f.call(t, f.bearer(t, f.orgA, f.member, current.ID), http.MethodGet, "/v1/me/sessions")
	list := decodeList(t, w)

	if len(list.Sessions) != 2 {
		t.Fatalf("listed %d sessions, want the member's 2", len(list.Sessions))
	}
	seen := map[string]bool{}
	for _, s := range list.Sessions {
		seen[s.ID] = true
		if s.Current != (s.ID == current.ID) {
			t.Errorf("session %s has current=%v", s.ID, s.Current)
		}
	}
	if !seen[current.ID] || !seen[other.ID] {
		t.Errorf("the list is not the member's two sessions: %v", seen)
	}

	first := list.Sessions[0]
	if first.Device.Browser == nil || *first.Device.Browser != "Chrome" || first.Device.OS == nil || *first.Device.OS != "Windows" {
		t.Errorf("device = %+v, want Chrome on Windows", first.Device)
	}
	if first.Location != nil {
		t.Errorf("location = %q with no geolocation database configured", *first.Location)
	}

	// A-3: nothing a fingerprint or a precise location is made of.
	body := w.Body.String()
	if strings.Contains(body, sessionIP) {
		t.Error("the session list carries the IP address")
	}
	if strings.Contains(body, "AppleWebKit") || strings.Contains(body, "Mozilla/5.0") {
		t.Error("the session list carries the raw user agent")
	}
}

func TestTheLocationIsCoarse(t *testing.T) {
	f := setup(t)
	f.api.Locator = placeLocator{}
	current, _ := f.signIn(t, f.orgA, f.member)

	list := decodeList(t, f.call(t, f.bearer(t, f.orgA, f.member, current.ID), http.MethodGet, "/v1/me/sessions"))
	if len(list.Sessions) != 1 || list.Sessions[0].Location == nil || *list.Sessions[0].Location != "Jakarta, ID" {
		t.Fatalf("location = %+v, want \"Jakarta, ID\"", list.Sessions)
	}
}

func TestTheListPages(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	f.signIn(t, f.orgA, f.member)
	f.signIn(t, f.orgA, f.member)
	bearer := f.bearer(t, f.orgA, f.member, current.ID)

	seen := map[string]bool{}
	target := "/v1/me/sessions?page_size=2"
	for pages := 0; pages < 5; pages++ {
		list := decodeList(t, f.call(t, bearer, http.MethodGet, target))
		for _, s := range list.Sessions {
			if seen[s.ID] {
				t.Fatalf("session %s appeared on two pages", s.ID)
			}
			seen[s.ID] = true
		}
		if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
			break
		}
		target = "/v1/me/sessions?page_size=2&page_token=" + *list.PageInfo.NextPageToken
	}
	if len(seen) != 3 {
		t.Errorf("paging saw %d sessions, want 3", len(seen))
	}
}

// --- F-2 / A-1: ending one's own ----------------------------------------------------------------

func TestAUserCannotEndAnotherUsersSession(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	theirs, theirCookie := f.signIn(t, f.orgA, f.colleague)
	foreign, foreignCookie := f.signIn(t, f.orgB, f.outsider)

	for _, id := range []string{theirs.ID, foreign.ID} {
		w := f.call(t, f.bearer(t, f.orgA, f.member, current.ID), http.MethodDelete, "/v1/me/sessions/"+id)
		if w.Code != http.StatusNotFound {
			t.Errorf("ending someone else's session answered %d, want 404:\n%s", w.Code, w.Body.String())
		}
	}
	if !f.cookieWorks(theirCookie) || !f.cookieWorks(foreignCookie) {
		t.Fatal("a user ended a session that is not theirs")
	}
	if n := len(f.revocations(t)); n != 0 {
		t.Errorf("a refused revocation wrote %d events", n)
	}
}

func TestEndingASessionIsImmediateAndTakesItsRefreshTokens(t *testing.T) {
	f := setup(t)
	current, currentCookie := f.signIn(t, f.orgA, f.member)
	laptop, laptopCookie := f.signIn(t, f.orgA, f.member)
	f.issueRefresh(t, f.orgA, f.member, laptop.ID)
	f.issueRefresh(t, f.orgA, f.member, current.ID)

	bearer := f.bearer(t, f.orgA, f.member, current.ID)
	if w := f.call(t, bearer, http.MethodDelete, "/v1/me/sessions/"+laptop.ID); w.Code != http.StatusNoContent {
		t.Fatalf("ending the laptop's session answered %d:\n%s", w.Code, w.Body.String())
	}

	if f.cookieWorks(laptopCookie) {
		t.Error("the ended session still resolves through the cache")
	}
	if f.liveRefresh(t, laptop.ID) != 0 {
		t.Error("a refresh token issued through the ended session is still live")
	}
	if !f.cookieWorks(currentCookie) || f.liveRefresh(t, current.ID) != 1 {
		t.Error("ending one session touched another")
	}

	events := f.revocations(t)
	if len(events) != 1 || events[0].Actor != f.member ||
		events[0].Payload["user_id"] != f.member || events[0].Payload["reason"] != "self_service" {
		t.Fatalf("revocation events = %+v", events)
	}

	// Again: 204, and no second event.
	if w := f.call(t, bearer, http.MethodDelete, "/v1/me/sessions/"+laptop.ID); w.Code != http.StatusNoContent {
		t.Errorf("ending an ended session answered %d", w.Code)
	}
	if len(f.revocations(t)) != 1 {
		t.Error("ending an ended session wrote another event")
	}

	// Neither request is an unaudited mutation: the first recorded an event
	// and the second declared it changed nothing.
	if n := f.guard.count(); n != 0 {
		t.Errorf("the audit guard reported %d unaudited mutations: %v", n, f.guard.missed)
	}
}

// Ending the current session is a sign-out: the token that asked is refused on
// its next call.
func TestEndingTheCurrentSessionEndsTheTokenToo(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	bearer := f.bearer(t, f.orgA, f.member, current.ID)

	if w := f.call(t, bearer, http.MethodDelete, "/v1/me/sessions/"+current.ID); w.Code != http.StatusNoContent {
		t.Fatalf("answered %d", w.Code)
	}
	if w := f.call(t, bearer, http.MethodGet, "/v1/me/sessions"); w.Code != http.StatusUnauthorized {
		t.Errorf("a token whose session was just ended answered %d, want 401", w.Code)
	}
}

// --- F-3: everything but this ---------------------------------------------------------------------

func TestRevokeOthersKeepsTheCurrentSession(t *testing.T) {
	f := setup(t)
	current, currentCookie := f.signIn(t, f.orgA, f.member)
	phone, phoneCookie := f.signIn(t, f.orgA, f.member)
	_, tabletCookie := f.signIn(t, f.orgA, f.member)
	_, colleagueCookie := f.signIn(t, f.orgA, f.colleague)
	f.issueRefresh(t, f.orgA, f.member, phone.ID)
	f.issueRefresh(t, f.orgA, f.member, current.ID)

	w := f.call(t, f.bearer(t, f.orgA, f.member, current.ID), http.MethodPost, "/v1/me/sessions/revoke-others")
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d:\n%s", w.Code, w.Body.String())
	}
	var body struct {
		Revoked int `json:"revoked"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Revoked != 2 {
		t.Errorf("revoked = %d, want 2", body.Revoked)
	}

	if !f.cookieWorks(currentCookie) {
		t.Error("the current session was ended")
	}
	if f.cookieWorks(phoneCookie) || f.cookieWorks(tabletCookie) {
		t.Error("another session survived")
	}
	if !f.cookieWorks(colleagueCookie) {
		t.Error("a colleague's session was ended")
	}
	if f.liveRefresh(t, phone.ID) != 0 {
		t.Error("the phone's refresh token survived")
	}
	if f.liveRefresh(t, current.ID) != 1 {
		t.Error("the current session's refresh token was revoked")
	}

	events := f.revocations(t)
	if len(events) != 1 || events[0].Actor != f.member || events[0].Payload["reason"] != "logout_others" ||
		events[0].Payload["kept"] != current.ID {
		t.Errorf("revocation events = %+v, want one logout_others event keeping the current session", events)
	}

	// With nothing left to end: 200, revoked 0, no event, and not reported.
	again := f.call(t, f.bearer(t, f.orgA, f.member, current.ID), http.MethodPost, "/v1/me/sessions/revoke-others")
	if again.Code != http.StatusOK || !strings.Contains(again.Body.String(), `"revoked":0`) {
		t.Errorf("a second revoke-others answered %d: %s", again.Code, again.Body.String())
	}
	if len(f.revocations(t)) != 1 {
		t.Error("revoking no sessions wrote an event")
	}
	if n := f.guard.count(); n != 0 {
		t.Errorf("the audit guard reported %d unaudited mutations", n)
	}
}

func TestRevokeOthersWithoutASessionIsRefused(t *testing.T) {
	f := setup(t)
	_, cookie := f.signIn(t, f.orgA, f.member)

	w := f.call(t, f.bearer(t, f.orgA, f.member, ""), http.MethodPost, "/v1/me/sessions/revoke-others")
	if w.Code != http.StatusBadRequest {
		t.Errorf("a token with no session answered %d, want 400:\n%s", w.Code, w.Body.String())
	}
	if !f.cookieWorks(cookie) {
		t.Fatal("a token with no session ended every session")
	}
}

// --- F-4 / F-5: an administrator ------------------------------------------------------------------

func TestAnAdminListsAndEndsAMembersSession(t *testing.T) {
	f := setup(t)
	adminSession, _ := f.signIn(t, f.orgA, f.admin)
	target, targetCookie := f.signIn(t, f.orgA, f.member)
	f.signIn(t, f.orgA, f.member)
	f.issueRefresh(t, f.orgA, f.member, target.ID)
	bearer := f.bearer(t, f.orgA, f.admin, adminSession.ID)

	list := decodeList(t, f.call(t, bearer, http.MethodGet,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions"))
	if len(list.Sessions) != 2 {
		t.Fatalf("the admin sees %d of the member's sessions, want 2", len(list.Sessions))
	}
	for _, s := range list.Sessions {
		if s.Current {
			t.Error("a member's session is marked as the administrator's current one")
		}
	}

	w := f.call(t, bearer, http.MethodDelete,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions/"+target.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("answered %d:\n%s", w.Code, w.Body.String())
	}
	if f.cookieWorks(targetCookie) || f.liveRefresh(t, target.ID) != 0 {
		t.Error("the member's session or its refresh token survived")
	}

	events := f.revocations(t)
	if len(events) != 1 || events[0].Actor != f.admin ||
		events[0].Payload["user_id"] != f.member || events[0].Payload["reason"] != "admin" {
		t.Errorf("revocation events = %+v, want actor=admin, target=member, reason=admin", events)
	}
}

// A-1 on the organization route: the session must belong to the user in the
// path, not merely to the organization.
func TestASessionUnderTheWrongMemberIsNotFound(t *testing.T) {
	f := setup(t)
	adminSession, _ := f.signIn(t, f.orgA, f.admin)
	colleagueSession, colleagueCookie := f.signIn(t, f.orgA, f.colleague)

	w := f.call(t, f.bearer(t, f.orgA, f.admin, adminSession.ID), http.MethodDelete,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions/"+colleagueSession.ID)
	if w.Code != http.StatusNotFound {
		t.Errorf("answered %d, want 404", w.Code)
	}
	if !f.cookieWorks(colleagueCookie) {
		t.Error("a session was ended through a path naming a different member")
	}
}

func TestAnUnknownMemberIsNotFound(t *testing.T) {
	f := setup(t)
	adminSession, _ := f.signIn(t, f.orgA, f.admin)

	w := f.call(t, f.bearer(t, f.orgA, f.admin, adminSession.ID), http.MethodGet,
		"/v1/organizations/"+f.orgA+"/users/"+f.outsider+"/sessions")
	if w.Code != http.StatusNotFound {
		t.Errorf("listing another organization's user through this one answered %d, want 404", w.Code)
	}
}

// A-6: a member without a role cannot use the administrator's route.
func TestAMemberCannotReadAColleaguesSessions(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	_, colleagueCookie := f.signIn(t, f.orgA, f.colleague)
	bearer := f.bearer(t, f.orgA, f.member, current.ID)

	if w := f.call(t, bearer, http.MethodGet,
		"/v1/organizations/"+f.orgA+"/users/"+f.colleague+"/sessions"); w.Code == http.StatusOK {
		t.Errorf("a member read a colleague's sessions:\n%s", w.Body.String())
	}
	if !f.cookieWorks(colleagueCookie) {
		t.Fatal("setup: colleague session is dead")
	}
}

// A-2: an administrator of another organization reaches nothing.
func TestAnotherOrganizationsAdminReachesNothing(t *testing.T) {
	f := setup(t)
	outsiderSession, _ := f.signIn(t, f.orgB, f.outsider)
	target, targetCookie := f.signIn(t, f.orgA, f.member)
	bearer := f.bearer(t, f.orgB, f.outsider, outsiderSession.ID)

	if w := f.call(t, bearer, http.MethodGet,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions"); w.Code == http.StatusOK {
		t.Errorf("another organization's admin listed a member's sessions:\n%s", w.Body.String())
	}
	w := f.call(t, bearer, http.MethodDelete,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions/"+target.ID)
	if w.Code == http.StatusNoContent {
		t.Error("another organization's admin ended a member's session")
	}
	if !f.cookieWorks(targetCookie) {
		t.Fatal("the member's session was ended from another organization")
	}

	// And through their OWN organization's path, naming this member: still
	// nothing, because the tenant scope is theirs and the member is not in it.
	w = f.call(t, bearer, http.MethodDelete,
		"/v1/organizations/"+f.orgB+"/users/"+f.member+"/sessions/"+target.ID)
	if w.Code != http.StatusNotFound || !f.cookieWorks(targetCookie) {
		t.Errorf("naming a foreign member under one's own organization answered %d", w.Code)
	}
}

// --- edges ------------------------------------------------------------------------------------

// A page token that cannot be read is a 400, not a silent first page.
func TestAnUnreadablePageTokenIsRefused(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	adminSession, _ := f.signIn(t, f.orgA, f.admin)

	if w := f.call(t, f.bearer(t, f.orgA, f.member, current.ID), http.MethodGet,
		"/v1/me/sessions?page_token=not-a-token"); w.Code != http.StatusBadRequest {
		t.Errorf("the self route answered %d, want 400", w.Code)
	}
	if w := f.call(t, f.bearer(t, f.orgA, f.admin, adminSession.ID), http.MethodGet,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions?page_token=not-a-token"); w.Code != http.StatusBadRequest {
		t.Errorf("the organization route answered %d, want 400", w.Code)
	}
}

// A script's session is listed with a null device rather than guessed at.
func TestAnUnrecognisedDeviceIsNull(t *testing.T) {
	f := setup(t)

	var created session.Session
	if err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		var err error
		created, _, err = f.sessions.Create(context.Background(), tx, session.New{
			UserID: f.member, OrgID: f.orgA, AuthMethods: []string{"pwd"},
			IP: sessionIP, UserAgent: "curl/8.4.0",
		}, session.DefaultPolicy, time.Now())
		return err
	}); err != nil {
		t.Fatalf("creating a session: %v", err)
	}

	list := decodeList(t, f.call(t, f.bearer(t, f.orgA, f.member, created.ID), http.MethodGet, "/v1/me/sessions"))
	if len(list.Sessions) != 1 || list.Sessions[0].Device.Browser != nil || list.Sessions[0].Device.OS != nil {
		t.Errorf("an unrecognised agent produced device %+v, want nulls", list.Sessions)
	}
}

// A revocation that cannot be recorded does not happen.
//
// The audit write is inside the transaction, so its failure rolls the
// revocation back — the session and its refresh token are exactly as they were.
// An untraceable revocation would be worse than a failed one: it is how an
// intruder signs a user out of the device they would have noticed them on.
func TestARevocationThatCannotBeAuditedIsRolledBack(t *testing.T) {
	f := setup(t)
	current, _ := f.signIn(t, f.orgA, f.member)
	laptop, laptopCookie := f.signIn(t, f.orgA, f.member)
	f.issueRefresh(t, f.orgA, f.member, laptop.ID)
	f.api.Audit = nil

	bearer := f.bearer(t, f.orgA, f.member, current.ID)
	if w := f.call(t, bearer, http.MethodDelete, "/v1/me/sessions/"+laptop.ID); w.Code != http.StatusInternalServerError {
		t.Errorf("an unauditable revocation answered %d, want 500", w.Code)
	}
	if w := f.call(t, bearer, http.MethodPost, "/v1/me/sessions/revoke-others"); w.Code != http.StatusInternalServerError {
		t.Errorf("an unauditable revoke-others answered %d, want 500", w.Code)
	}

	if !f.cookieWorks(laptopCookie) || f.liveRefresh(t, laptop.ID) != 1 {
		t.Error("a revocation that could not be audited took effect anyway")
	}
}

// A-3 on the ADMINISTRATOR route (P3-14). The checks above run on the caller's
// own list. The organization route renders the same rows for somebody else,
// and an administrator is exactly who the card says must not receive more
// than is justified — no IP, no raw user agent, a coarse location only.
func TestAnAdminSeesNoFingerprintOfAMembersSession(t *testing.T) {
	f := setup(t)
	f.api.Locator = placeLocator{}
	adminSession, _ := f.signIn(t, f.orgA, f.admin)
	f.signIn(t, f.orgA, f.member)

	w := f.call(t, f.bearer(t, f.orgA, f.admin, adminSession.ID), http.MethodGet,
		"/v1/organizations/"+f.orgA+"/users/"+f.member+"/sessions")
	list := decodeList(t, w)
	if len(list.Sessions) != 1 {
		t.Fatalf("the admin sees %d sessions, want the member's 1 — the leak check would be vacuous", len(list.Sessions))
	}

	body := w.Body.String()
	if strings.Contains(body, sessionIP) {
		t.Error("an administrator's view of a member's sessions carries the IP address")
	}
	if strings.Contains(body, "AppleWebKit") || strings.Contains(body, "Mozilla/5.0") {
		t.Error("an administrator's view of a member's sessions carries the raw user agent")
	}
	if loc := list.Sessions[0].Location; loc == nil || *loc != "Jakarta, ID" {
		t.Errorf("location = %v, want the coarse \"Jakarta, ID\"", loc)
	}
}

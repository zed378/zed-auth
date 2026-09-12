//go:build integration

// The user endpoints and the two token flows, end to end (P1-19).
//
// The claims worth testing against a real database are the ones a reading of
// the code cannot settle: that a deactivated user's live session and live
// refresh token both stop working in the same request, that a token can be
// consumed exactly once under concurrency, and that the enumeration answers
// really are identical rather than merely intended to be.
package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

const issuer = "https://auth.example.test"

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// capturingMailer records what would have been sent.
//
// The link a test needs is in the message body and nowhere else — which is
// itself the property under test: no API response carries it.
type capturingMailer struct {
	mu       sync.Mutex
	sent     []mail.Message
	failNext bool
}

func (c *capturingMailer) Send(_ context.Context, msg mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext {
		c.failNext = false
		return fmt.Errorf("simulated delivery failure")
	}
	c.sent = append(c.sent, msg)
	return nil
}

func (c *capturingMailer) last() (mail.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return mail.Message{}, false
	}
	return c.sent[len(c.sent)-1], true
}

func (c *capturingMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

type fixture struct {
	db      *postgres.DB
	store   *Store
	factory *testsupport.Factory
	handler http.Handler
	signer  *signing.Signer
	mailer  *capturingMailer

	sessions *session.Manager
	refresh  *token.RefreshStore
	rdb      *redis.Client

	// api is the very handler the server routes to, so a test that changes a
	// field on it changes what the next request meets — not a copy that only
	// looks like it.
	api *Handler

	orgA, orgB string
	userID     string
	clientID   string
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)
	adminID := factory.User(orgA)

	var project, clientID string
	factory.QueryRow(&project,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgA)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, project, orgA)

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
	sessions := session.NewManager(db, session.NewCache(rdb, nil), auditor, discard())
	refresh := token.NewRefreshStore()
	mailer := &capturingMailer{}

	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 500, Window: time.Minute}),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}

	store := NewStore()
	users := &Handler{
		Store: store, DB: db, Audit: auditor, Log: discard(),
		Sessions: sessions, Refresh: refresh,
		Mailer: mailer, BaseURL: issuer,
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
		ProjectAPI:     stubProjects{},
		ApplicationAPI: stubApplications{},
		RoleAPI:        stubRoles{},
		GrantAPI:       stubGrants{},
		AuthzAPI:       stubAuthz{},
		UserAPI:        users,
		AuditAPI:       &auditlog.Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, store: store, factory: factory, handler: srv.Handler(),
		signer: signing.NewSigner(keys), mailer: mailer,
		sessions: sessions, refresh: refresh, rdb: rdb, api: users,
		orgA: orgA, orgB: orgB, userID: adminID, clientID: clientID,
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

func (f *fixture) token(t *testing.T) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: f.orgA, UserID: f.userID, Scope: []string{"openid"},
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

func (f *fixture) grant(role management.Role, scopeID string) {
	f.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, $2, $3)`,
		f.userID, string(role), scopeID)
}

func (f *fixture) call(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token(t))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) users(orgID string) string {
	return "/v1/organizations/" + orgID + "/users"
}

func mustStatus(t *testing.T, w *httptest.ResponseRecorder, want int) *httptest.ResponseRecorder {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
	}
	return w
}

func decodeUser(t *testing.T, w *httptest.ResponseRecorder) api.User {
	t.Helper()
	var u api.User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatalf("not a User: %s", w.Body.String())
	}
	return u
}

func envelope(t *testing.T, w *httptest.ResponseRecorder) (string, []api.ErrorDetail) {
	t.Helper()
	var e struct {
		Error struct {
			Code    string            `json:"code"`
			Details []api.ErrorDetail `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("not docs/PLAN/05's envelope: %s", w.Body.String())
	}
	return e.Error.Code, e.Error.Details
}

// invite creates a user and returns them with the link from their email.
func (f *fixture) invite(t *testing.T, email string) (api.UserCreated, string) {
	t.Helper()

	body := fmt.Sprintf(`{"email":%q,"display_name":"Invited Person"}`, email)
	w := mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA), body), http.StatusCreated)

	var created api.UserCreated
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("not a UserCreated: %s", w.Body.String())
	}

	msg, ok := f.mailer.last()
	if !ok {
		t.Fatal("no invitation was sent")
	}
	return created, linkFrom(t, msg.Body)
}

// linkFrom pulls the URL out of a message body.
func linkFrom(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, issuer+"/password/set?token=") {
			return line
		}
	}
	t.Fatalf("no link in the message:\n%s", body)
	return ""
}

func tokenFrom(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("bad link %q: %v", link, err)
	}
	value := u.Query().Get("token")
	if value == "" {
		t.Fatalf("no token in %q", link)
	}
	return value
}

// --- the create contract ----------------------------------------------------------------------

// **The card's first DoD item**: the create response matches docs/PLAN/05's
// worked example field for field.
func TestTheCreateResponseMatchesTheDocumentedExample(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	w := mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"budi@company.com","username":"budi","display_name":"Budi Santoso"}`),
		http.StatusCreated)

	// Against the raw JSON, not a decoded struct: the claim is about the
	// document a caller receives, and a struct would silently tolerate a
	// missing key by leaving a zero value.
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %s", w.Body.String())
	}

	for _, field := range []string{"id", "email", "status", "created_at"} {
		if _, present := body[field]; !present {
			t.Errorf("the documented field %q is missing: %s", field, w.Body.String())
		}
	}
	if body["email"] != "budi@company.com" {
		t.Errorf("email = %v", body["email"])
	}
	if body["status"] != "invited" {
		t.Errorf("status = %v, want invited — docs/PLAN/05's example", body["status"])
	}

	// And nothing that should never be there.
	for _, forbidden := range []string{"password", "password_hash", "invite_token", "invite_link"} {
		if _, present := body[forbidden]; present {
			t.Errorf("the response carries %q", forbidden)
		}
	}
}

func TestAnInvitedUserHasNoPasswordAndCannotSignIn(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "nopassword@example.test")

	var hash sql.NullString
	f.factory.QueryRow(&hash, `SELECT password_hash FROM users WHERE id = $1`, created.Id.String())
	if hash.Valid {
		t.Error("an invited user was created with a password hash")
	}
}

// A create that could not be mailed still returns 201, and says so (ADR-018).
func TestACreateWhoseEmailFailsStillReturnsTheUser(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	f.mailer.failNext = true
	w := mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"undeliverable@example.test"}`), http.StatusCreated)

	var created api.UserCreated
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("not a UserCreated: %s", w.Body.String())
	}
	if created.InviteEmailSent {
		t.Error("invite_email_sent is true after a delivery failure")
	}

	// The account and its token exist regardless. Rolling the user back
	// because a mail server was busy would be the worse outcome.
	var tokens int
	f.factory.QueryRow(&tokens,
		`SELECT count(*) FROM user_tokens WHERE user_id = $1 AND purpose = 'invite'`,
		created.Id.String())
	if tokens != 1 {
		t.Errorf("%d invite tokens, want 1", tokens)
	}
}

func TestSendInviteEmailFalseCreatesTheTokenAndSendsNothing(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	before := f.mailer.count()
	w := mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"quiet@example.test","send_invite_email":false}`), http.StatusCreated)

	if f.mailer.count() != before {
		t.Error("a message was sent despite send_invite_email: false")
	}

	var created api.UserCreated
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	var tokens int
	f.factory.QueryRow(&tokens,
		`SELECT count(*) FROM user_tokens WHERE user_id = $1`, created.Id.String())
	if tokens != 1 {
		t.Errorf("%d tokens, want 1 — the invitation still has to exist", tokens)
	}
}

// A duplicate address is a 409 that says so. **Deliberately not hidden**: the
// caller is an authenticated administrator who can already list every user in
// the organization, so concealing it would withhold nothing and leave them
// guessing.
func TestADuplicateEmailIsAConflict(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"taken@example.test"}`), http.StatusCreated)

	w := f.call(t, http.MethodPost, f.users(f.orgA), `{"email":"TAKEN@example.test"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	code, details := envelope(t, w)
	if code != "CONFLICT" {
		t.Errorf("code = %q", code)
	}
	if len(details) == 0 || details[0].Field != "email" {
		t.Errorf("the conflict does not name the field: %+v", details)
	}
}

func TestTheSameAddressInAnotherOrganizationIsFine(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)
	f.grant(management.OrgAdmin, f.orgB)

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"shared@example.test"}`), http.StatusCreated)
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgB),
		`{"email":"shared@example.test"}`), http.StatusCreated)
}

// --- privilege escalation (abuse case A-5) -------------------------------------------------------

func TestAnUpdateNamingAPrivilegeFieldIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "escalate@example.test")
	one := f.users(f.orgA) + "/" + created.Id.String()

	for _, body := range []string{
		`{"status":"active"}`,
		`{"email_verified":true}`,
		`{"password":"chosen-by-an-administrator"}`,
		`{"password_hash":"$argon2id$..."}`,
		`{"roles":["ORG_OWNER"]}`,
		`{"org_id":"00000000-0000-0000-0000-0000000000ff"}`,
		`{"mfa_enabled":true}`,
	} {
		w := f.call(t, http.MethodPatch, one, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", body, w.Code, w.Body.String())
			continue
		}
		if _, details := envelope(t, w); len(details) == 0 {
			t.Errorf("%s: the refusal names no field", body)
		}
	}

	// Nothing changed, which is the half that would still hold with no
	// refusal at all — so both are asserted.
	var status string
	var hash sql.NullString
	f.factory.QueryRow(&status, `SELECT status FROM users WHERE id = $1`, created.Id.String())
	f.factory.QueryRow(&hash, `SELECT password_hash FROM users WHERE id = $1`, created.Id.String())
	if status != StatusInvited {
		t.Errorf("status = %q — a refused update was applied", status)
	}
	if hash.Valid {
		t.Error("a password was set through the update endpoint")
	}
}

func TestAProfileUpdateWorks(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "profile@example.test")
	one := f.users(f.orgA) + "/" + created.Id.String()

	updated := decodeUser(t, mustStatus(t,
		f.call(t, http.MethodPatch, one, `{"display_name":"New Name"}`), http.StatusOK))

	name, err := updated.DisplayName.Get()
	if err != nil || name != "New Name" {
		t.Errorf("display_name = %v (%v)", name, err)
	}
}

// Changing the address clears the verification, because a verification is a
// statement about one address rather than about the user who holds it.
func TestChangingTheAddressClearsTheVerification(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "verify@example.test")
	f.acceptInvite(t, link, "Correct-Horse-Battery-Staple-1")

	before := decodeUser(t, mustStatus(t,
		f.call(t, http.MethodGet, f.users(f.orgA)+"/"+created.Id.String(), ""), http.StatusOK))
	if !before.EmailVerified {
		t.Fatal("accepting an invitation did not verify the address")
	}

	after := decodeUser(t, mustStatus(t, f.call(t, http.MethodPatch,
		f.users(f.orgA)+"/"+created.Id.String(),
		`{"email":"moved@example.test"}`), http.StatusOK))
	if after.EmailVerified {
		t.Error("the verification survived an address change")
	}
}

// --- deactivation (the card's fourth DoD item) ---------------------------------------------------

// **A deactivated user who can still act is not deactivated.**
//
// A live session and a live refresh token, both created before the
// deactivation and both checked through their real interfaces afterwards —
// not by reading a column, which would pass against a service that set the
// status and revoked nothing.
func TestDeactivationKillsSessionsAndRefreshTokensInOneRequest(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "deactivate@example.test")
	f.acceptInvite(t, link, "Correct-Horse-Battery-Staple-2")
	id := created.Id.String()

	// A real session, through the real manager.
	var sessionToken session.Token
	if err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, token, err := f.sessions.Create(context.Background(), tx, session.New{
			UserID: id, OrgID: f.orgA, AuthMethods: []string{"pwd"},
		}, session.DefaultPolicy, time.Now())
		sessionToken = token
		return err
	}); err != nil {
		t.Fatalf("creating a session: %v", err)
	}

	// A real refresh token.
	refreshPlaintext := f.issueRefresh(t, id)

	// Both work before.
	if _, err := f.sessions.Lookup(
		context.Background(), sessionToken.Reveal(), session.DefaultPolicy, time.Now()); err != nil {
		t.Fatalf("the session does not work before deactivation: %v", err)
	}
	if !f.refreshLive(t, refreshPlaintext) {
		t.Fatal("the refresh token does not work before deactivation")
	}

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/deactivate", ""),
		http.StatusNoContent)

	// Neither works after — in the same request, with no sweep in between.
	if _, err := f.sessions.Lookup(
		context.Background(), sessionToken.Reveal(), session.DefaultPolicy, time.Now()); err == nil {
		t.Error("the session still resolves after deactivation")
	}
	if f.refreshLive(t, refreshPlaintext) {
		t.Error("the refresh token still works after deactivation")
	}

	var status string
	f.factory.QueryRow(&status, `SELECT status FROM users WHERE id = $1`, id)
	if status != StatusDeactivated {
		t.Errorf("status = %q", status)
	}
}

// A live invitation or reset link for a deactivated user is a way back in.
func TestDeactivationRetiresLiveLinks(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "retire@example.test")
	id := created.Id.String()

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/deactivate", ""),
		http.StatusNoContent)

	if _, err := f.store.LookupToken(
		context.Background(), f.db, tokenFrom(t, link), time.Now()); err == nil {
		t.Error("the invitation still resolves after the account was deactivated")
	}
}

func TestReactivationReturnsAnInvitedUserToInvited(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "never-accepted@example.test")
	id := created.Id.String()

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/deactivate", ""),
		http.StatusNoContent)
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/reactivate", ""),
		http.StatusNoContent)

	var status string
	f.factory.QueryRow(&status, `SELECT status FROM users WHERE id = $1`, id)
	if status != StatusInvited {
		t.Errorf("status = %q, want invited — an active account with no password cannot sign in", status)
	}
}

func TestReactivatingAnActiveUserIsAConflict(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "already-active@example.test")
	w := f.call(t, http.MethodPost,
		f.users(f.orgA)+"/"+created.Id.String()+"/reactivate", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
}

// --- tokens ----------------------------------------------------------------------------------------

// **Abuse case A-6.** A sequential test passes against a read-then-write, so
// this one is concurrent: sixteen goroutines racing to consume one token, and
// exactly one may win.
func TestATokenIsConsumedExactlyOnceUnderConcurrency(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "race@example.test")
	plaintext := tokenFrom(t, link)

	const racers = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)
	start := make(chan struct{})

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
				_, err := f.store.ConsumeToken(
					context.Background(), tx, plaintext, PurposeInvite, time.Now())
				return err
			})
			if err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Errorf("%d of %d racers consumed the same token, want exactly 1", winners, racers)
	}

	var used int
	f.factory.QueryRow(&used,
		`SELECT count(*) FROM user_tokens WHERE user_id = $1 AND used_at IS NOT NULL`,
		created.Id.String())
	if used != 1 {
		t.Errorf("%d tokens marked used", used)
	}
}

func TestATokenIsStoredHashedAndNeverInPlaintext(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "hashed@example.test")
	plaintext := tokenFrom(t, link)

	var stored string
	f.factory.QueryRow(&stored,
		`SELECT token_hash FROM user_tokens WHERE user_id = $1`, created.Id.String())

	if stored == plaintext {
		t.Fatal("the token is stored in plaintext")
	}
	if stored != HashToken(plaintext) {
		t.Error("the stored value is not the hash of the token")
	}
	// And the plaintext appears nowhere in the row.
	var row string
	f.factory.QueryRow(&row,
		`SELECT coalesce(to_jsonb(t)::text, '') FROM user_tokens t WHERE user_id = $1`,
		created.Id.String())
	if strings.Contains(row, plaintext) {
		t.Error("the plaintext token appears in the stored row")
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "expired@example.test")
	plaintext := tokenFrom(t, link)

	// Asked from after the expiry rather than by backdating the row:
	// user_tokens_expires_after_creation refuses a row whose expiry precedes
	// its creation, and a test that fights a CHECK is testing the wrong thing.
	_ = created
	if _, err := f.store.LookupToken(
		context.Background(), f.db, plaintext, time.Now().Add(InviteLifetime+time.Minute)); err == nil {
		t.Error("an expired token still resolves")
	}

	// The control: the same token resolves before its expiry, so the refusal
	// above is about the clock rather than about the token being wrong.
	if _, err := f.store.LookupToken(context.Background(), f.db, plaintext, time.Now()); err != nil {
		t.Errorf("the token does not resolve before its expiry: %v", err)
	}
}

// Issuing a second token of the same purpose retires the first, so an account
// never has two live credentials at once.
func TestIssuingASecondTokenRetiresTheFirst(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, first := f.invite(t, "reissue@example.test")
	id := created.Id.String()

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/password-reset", ""),
		http.StatusAccepted)
	msg, _ := f.mailer.last()
	second := linkFrom(t, msg.Body)

	// Different tokens, and only the new one resolves.
	if tokenFrom(t, first) == tokenFrom(t, second) {
		t.Fatal("the same token was reissued")
	}
	if _, err := f.store.LookupToken(
		context.Background(), f.db, tokenFrom(t, second), time.Now()); err != nil {
		t.Errorf("the new token does not resolve: %v", err)
	}
}

// --- the administrator-triggered reset ---------------------------------------------------------

// The link is mailed to the user and is NOT in the response — an administrator
// who could read it could take over any account without leaving a trace saying
// so.
func TestAResetLinkIsNeverInTheResponse(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "reset@example.test")

	w := mustStatus(t, f.call(t, http.MethodPost,
		f.users(f.orgA)+"/"+created.Id.String()+"/password-reset", ""), http.StatusAccepted)

	msg, ok := f.mailer.last()
	if !ok {
		t.Fatal("no reset message was sent")
	}
	plaintext := tokenFrom(t, linkFrom(t, msg.Body))

	if strings.Contains(w.Body.String(), plaintext) {
		t.Error("the response carries the reset token")
	}
	if strings.Contains(w.Body.String(), "/password/set") {
		t.Error("the response carries the reset link")
	}
	// The message went to the USER, not to the administrator.
	if msg.To != "reset@example.test" {
		t.Errorf("the message went to %q", msg.To)
	}
}

func TestAResetForADeactivatedUserIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "gone@example.test")
	id := created.Id.String()
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/deactivate", ""),
		http.StatusNoContent)

	w := f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/password-reset", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
}

// --- tenant boundary ------------------------------------------------------------------------------

func TestAnotherTenantsUserIsUnreachable(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	theirs := f.factory.User(f.orgB)

	// Their id, our organization's path: the application layer allows it, so
	// only RLS is left.
	w := f.call(t, http.MethodGet, f.users(f.orgA)+"/"+theirs, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}

	// The control.
	ours, _ := f.invite(t, "ours@example.test")
	mustStatus(t, f.call(t, http.MethodGet,
		f.users(f.orgA)+"/"+ours.Id.String(), ""), http.StatusOK)
}

func TestAListNeverCrossesATenantBoundary(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	f.factory.QueryRow(new(string),
		`INSERT INTO users (org_id, email, status) VALUES ($1, 'theirs@example.test', 'active') RETURNING id`,
		f.orgB)

	w := mustStatus(t, f.call(t, http.MethodGet, f.users(f.orgA), ""), http.StatusOK)
	if strings.Contains(w.Body.String(), "theirs@example.test") {
		t.Error("the list crossed a tenant boundary")
	}
}

// --- search and pagination -------------------------------------------------------------------------

func TestSearchMatchesEmailUsernameAndDisplayName(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"findme@example.test","username":"needle","display_name":"Haystack Person"}`),
		http.StatusCreated)
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"other@example.test","username":"other","display_name":"Someone Else"}`),
		http.StatusCreated)

	for _, term := range []string{"findme", "needle", "Haystack", "aystac"} {
		w := mustStatus(t, f.call(t, http.MethodGet,
			f.users(f.orgA)+"?search="+url.QueryEscape(term), ""), http.StatusOK)
		if !strings.Contains(w.Body.String(), "findme@example.test") {
			t.Errorf("search %q did not find the user: %s", term, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "other@example.test") {
			t.Errorf("search %q also matched the other user", term)
		}
	}
}

// A wildcard in the search term is matched literally, not interpreted.
func TestSearchTreatsWildcardsLiterally(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"literal@example.test","display_name":"a_b"}`), http.StatusCreated)
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA),
		`{"email":"axb@example.test","display_name":"axb"}`), http.StatusCreated)

	w := mustStatus(t, f.call(t, http.MethodGet,
		f.users(f.orgA)+"?search="+url.QueryEscape("a_b"), ""), http.StatusOK)

	if !strings.Contains(w.Body.String(), "literal@example.test") {
		t.Error("the literal match was not found")
	}
	if strings.Contains(w.Body.String(), "axb@example.test") {
		t.Error("the underscore was treated as a wildcard")
	}

	// The percent sign too — as a wildcard it would match everybody.
	w = mustStatus(t, f.call(t, http.MethodGet,
		f.users(f.orgA)+"?search="+url.QueryEscape("%"), ""), http.StatusOK)
	if strings.Contains(w.Body.String(), "literal@example.test") {
		t.Error("a bare % matched every user")
	}
}

func TestWalkingTheUserListSeesEachOnce(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for i := range 9 {
		f.factory.Exec(
			`INSERT INTO users (org_id, email, status) VALUES ($1, $2, 'active')`,
			f.orgA, fmt.Sprintf("walk-%02d@example.test", i))
	}

	seen := map[string]int{}
	target := f.users(f.orgA) + "?page_size=4"

	for range 10 {
		w := mustStatus(t, f.call(t, http.MethodGet, target, ""), http.StatusOK)

		var list api.UserList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("not a list: %s", w.Body.String())
		}
		for _, u := range list.Users {
			seen[u.Id.String()]++
		}
		if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
			break
		}
		target = f.users(f.orgA) + "?page_size=4&page_token=" +
			url.QueryEscape(*list.PageInfo.NextPageToken)
	}

	// Nine plus the fixture's administrator.
	if len(seen) != 10 {
		t.Errorf("saw %d distinct users, want 10", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times", id, n)
		}
	}
}

// --- permissions -------------------------------------------------------------------------------------

func TestEveryUserRouteNeedsOrgAdmin(t *testing.T) {
	f := setup(t)
	// No grant at all.

	for _, probe := range []struct{ method, target, body string }{
		{http.MethodGet, f.users(f.orgA), ""},
		{http.MethodPost, f.users(f.orgA), `{"email":"nope@example.test"}`},
	} {
		w := f.call(t, probe.method, probe.target, probe.body)
		if w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 404 or 403: %s",
				probe.method, probe.target, w.Code, w.Body.String())
		}
	}

	// The control: with the role, the same list works.
	f.grant(management.OrgAdmin, f.orgA)
	mustStatus(t, f.call(t, http.MethodGet, f.users(f.orgA), ""), http.StatusOK)
}

// Nothing here is raised to ORG_OWNER — deactivation is the reversible
// operation the card asks for instead of deletion, and raising it would make
// the safe action harder than the unsafe one it replaced.
func TestAnOrgAdminCanDeactivate(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "admin-can@example.test")
	mustStatus(t, f.call(t, http.MethodPost,
		f.users(f.orgA)+"/"+created.Id.String()+"/deactivate", ""), http.StatusNoContent)
}

// --- audit ----------------------------------------------------------------------------------------------

func TestUserLifecycleEventsAreAudited(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, link := f.invite(t, "audited@example.test")
	id := created.Id.String()

	mustStatus(t, f.call(t, http.MethodPatch, f.users(f.orgA)+"/"+id,
		`{"display_name":"Audited Person"}`), http.StatusOK)
	f.acceptInvite(t, link, "Correct-Horse-Battery-Staple-3")
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/password-reset", ""),
		http.StatusAccepted)
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/deactivate", ""),
		http.StatusNoContent)
	mustStatus(t, f.call(t, http.MethodPost, f.users(f.orgA)+"/"+id+"/reactivate", ""),
		http.StatusNoContent)

	// user.invite_accepted is deliberately absent from this list. It is written
	// by the hosted set-password page, which lives in internal/login — and the
	// helper below consumes the invitation through the store rather than
	// through that page, so asserting it here would assert that the HELPER
	// wrote it. It is covered where it happens.
	for _, kind := range []audit.EventType{
		audit.EventUserCreated, audit.EventUserUpdated,
		audit.EventPasswordResetSent, audit.EventUserDeactivated, audit.EventUserReactivated,
	} {
		var count int
		f.factory.QueryRow(&count,
			`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
			f.orgA, string(kind))
		if count < 1 {
			t.Errorf("no %s event", kind)
		}
	}

	// No payload carries a token or a password.
	var all string
	f.factory.QueryRow(&all,
		`SELECT coalesce(string_agg(payload::text, ' '), '') FROM events WHERE org_id = $1`, f.orgA)
	if strings.Contains(all, tokenFrom(t, link)) {
		t.Error("an audit payload carries a token")
	}
	if strings.Contains(all, "Correct-Horse-Battery-Staple-3") {
		t.Error("an audit payload carries a password")
	}
}

// A no-op update writes no event.
func TestAnUpdateThatChangesNothingIsNotAudited(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.invite(t, "noop@example.test")
	one := f.users(f.orgA) + "/" + created.Id.String()

	mustStatus(t, f.call(t, http.MethodPatch, one, `{"display_name":"Invited Person"}`),
		http.StatusOK)

	var count int
	f.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		f.orgA, string(audit.EventUserUpdated))
	if count != 0 {
		t.Errorf("%d update events for an update that changed nothing", count)
	}
}

// --- helpers ---------------------------------------------------------------------------------------------

// acceptInvite consumes an invitation the way the hosted page does.
func (f *fixture) acceptInvite(t *testing.T, link, password string) {
	t.Helper()

	plaintext := tokenFrom(t, link)
	claim, err := f.store.LookupToken(context.Background(), f.db, plaintext, time.Now())
	if err != nil {
		t.Fatalf("resolving the invitation: %v", err)
	}

	if err := f.db.WithTenant(context.Background(), claim.OrgID, func(tx *postgres.Tx) error {
		consumed, err := f.store.ConsumeToken(
			context.Background(), tx, plaintext, PurposeInvite, time.Now())
		if err != nil {
			return err
		}
		return f.store.SetPassword(
			context.Background(), tx, consumed.UserID, password, true, time.Now())
	}); err != nil {
		t.Fatalf("accepting the invitation: %v", err)
	}
}

// issueRefresh mints a refresh token for a user and returns its plaintext.
func (f *fixture) issueRefresh(t *testing.T, userID string) string {
	t.Helper()

	var plaintext string
	if err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		issued, _, err := f.refresh.Issue(context.Background(), tx, token.Refresh{
			UserID: userID, OrgID: f.orgA, ClientID: f.clientID,
			Scope: []string{"openid"},
		}, "", time.Now())
		plaintext = issued.Reveal()
		return err
	}); err != nil {
		t.Fatalf("issuing a refresh token: %v", err)
	}
	return plaintext
}

// refreshLive reports whether a refresh token still resolves.
func (f *fixture) refreshLive(t *testing.T, plaintext string) bool {
	t.Helper()

	var live bool
	if err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM refresh_tokens WHERE token_hash = $1 AND NOT revoked)`,
			token.HashRefresh(plaintext)).Scan(&live)
	}); err != nil {
		t.Fatalf("checking the refresh token: %v", err)
	}
	return live
}

// Stubs for the halves of the Management API this package does not exercise.
//
// httpserver.New refuses a /v1 chain with any of them missing, and these
// return an honest error rather than panicking if a test ever reaches one.
type stubProjects struct{}

func (stubProjects) ListProjects(context.Context, api.ListProjectsRequestObject) (api.ListProjectsResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) CreateProject(context.Context, api.CreateProjectRequestObject) (api.CreateProjectResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) GetProject(context.Context, api.GetProjectRequestObject) (api.GetProjectResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) UpdateProject(context.Context, api.UpdateProjectRequestObject) (api.UpdateProjectResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) DeleteProject(context.Context, api.DeleteProjectRequestObject) (api.DeleteProjectResponseObject, error) {
	return nil, errNotWired
}

type stubApplications struct{}

func (stubApplications) ListApplications(context.Context, api.ListApplicationsRequestObject) (api.ListApplicationsResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) CreateApplication(context.Context, api.CreateApplicationRequestObject) (api.CreateApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) GetApplication(context.Context, api.GetApplicationRequestObject) (api.GetApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) UpdateApplication(context.Context, api.UpdateApplicationRequestObject) (api.UpdateApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) DeleteApplication(context.Context, api.DeleteApplicationRequestObject) (api.DeleteApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) RotateApplicationSecret(context.Context, api.RotateApplicationSecretRequestObject) (api.RotateApplicationSecretResponseObject, error) {
	return nil, errNotWired
}

var errNotWired = fmt.Errorf("this harness does not wire that half of the Management API")

// --- the email-amplification bound (card step 4, abuse case A-4) -----------------------------

// **Keyed on the RECIPIENT, not on the caller.**
//
// The abuse this bounds is flooding a third party: an administrator with a
// legitimate account sending twenty invitations to somebody who never asked
// for one. A per-caller bound does not touch that — the caller is entitled to
// be there, and it is the mailbox that suffers.
func TestMessagesToOneAddressAreBounded(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	// A bound of two, so the test does not depend on the deployment's number.
	f.limitMail(t, 2, time.Hour)

	created, _ := f.invite(t, "flooded@example.test")
	one := f.users(f.orgA) + "/" + created.Id.String()

	// The invitation was the first message. A reset is the second.
	mustStatus(t, f.call(t, http.MethodPost, one+"/password-reset", ""), http.StatusAccepted)
	sentAfterTwo := f.mailer.count()

	// The third is refused — and the request still succeeds, saying the
	// message was not sent. A 429 here would tell a caller which addresses are
	// near their bound, which is a signal about somebody else's mailbox.
	w := mustStatus(t, f.call(t, http.MethodPost, one+"/password-reset", ""), http.StatusAccepted)
	if strings.Contains(w.Body.String(), `"email_sent":true`) {
		t.Error("the third message reports as sent")
	}
	if f.mailer.count() != sentAfterTwo {
		t.Errorf("%d messages sent, want %d — the bound did not hold",
			f.mailer.count(), sentAfterTwo)
	}

	// The control: a DIFFERENT address is unaffected, so the bound is per
	// recipient rather than a global stop.
	before := f.mailer.count()
	f.invite(t, "unaffected@example.test")
	if f.mailer.count() != before+1 {
		t.Error("the bound on one address stopped a message to another")
	}
}

// limitMail installs a per-recipient bound on the running handler.
func (f *fixture) limitMail(t *testing.T, limit int, window time.Duration) {
	t.Helper()
	f.api.MailLimit = ratelimit.NewQuotas(f.rdb, nil, discard()).
		WithQuota(ratelimit.Quota{Limit: limit, Window: window})
}

// **The race, made deterministic.**
//
// The sixteen-goroutine test above turned out to be weaker than it looks: a
// mutation that replaced the conditional UPDATE with a read-then-write
// SURVIVED it. Under serial execution a read-then-write is correct — the
// second reader sees `used_at` already set — so the test only catches the bug
// when two transactions genuinely overlap, and goroutine scheduling plus
// connection acquisition kept them from doing so.
//
// This one forces the overlap instead of hoping for it. One transaction
// consumes the token and stays open; a second consumes the SAME token while
// the first is uncommitted, and only then does the first commit.
//
//   - the real implementation: the second's UPDATE blocks on the row lock,
//     re-evaluates `used_at IS NULL` after the commit, matches nothing, and is
//     refused.
//   - a read-then-write: the second's SELECT ran before the commit and saw
//     NULL, so its UPDATE proceeds and both succeed.
func TestATokenCannotBeConsumedByAnOverlappingTransaction(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	_, link := f.invite(t, "overlap@example.test")
	plaintext := tokenFrom(t, link)

	var (
		firstConsumed = make(chan struct{})
		secondIssued  = make(chan struct{})
		secondErr     = make(chan error, 1)
	)

	go func() {
		<-firstConsumed
		close(secondIssued)
		secondErr <- f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
			_, err := f.store.ConsumeToken(
				context.Background(), tx, plaintext, PurposeInvite, time.Now())
			return err
		})
	}()

	firstErr := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		if _, err := f.store.ConsumeToken(
			context.Background(), tx, plaintext, PurposeInvite, time.Now()); err != nil {
			return err
		}
		close(firstConsumed)
		<-secondIssued
		// Long enough for the second transaction to have issued its statement
		// and be waiting on the row lock. Without this the two might not
		// overlap at all, which is exactly the weakness this test exists to
		// remove.
		time.Sleep(400 * time.Millisecond)
		return nil
	})

	if firstErr != nil {
		t.Fatalf("the first consumption failed: %v", firstErr)
	}
	if err := <-secondErr; err == nil {
		t.Error("an overlapping transaction consumed the same token")
	}

	var used int
	f.factory.QueryRow(&used,
		`SELECT count(*) FROM user_tokens WHERE token_hash = $1 AND used_at IS NOT NULL`,
		HashToken(plaintext))
	if used != 1 {
		t.Errorf("%d rows marked used, want 1", used)
	}
}

// stubRoles is the role half of the Management API, which this suite does not
// exercise. Present because httpserver.New refuses a /v1 chain with any half
// missing — a nil handler behind a registered route is a panic on the first
// request rather than a boot failure (P1-18's reasoning, P2-02's addition).
type stubRoles struct{}

func (stubRoles) ListRoles(context.Context, api.ListRolesRequestObject) (api.ListRolesResponseObject, error) {
	return nil, errNotWired
}

func (stubRoles) CreateRole(context.Context, api.CreateRoleRequestObject) (api.CreateRoleResponseObject, error) {
	return nil, errNotWired
}

func (stubRoles) GetRole(context.Context, api.GetRoleRequestObject) (api.GetRoleResponseObject, error) {
	return nil, errNotWired
}

func (stubRoles) UpdateRole(context.Context, api.UpdateRoleRequestObject) (api.UpdateRoleResponseObject, error) {
	return nil, errNotWired
}

func (stubRoles) DeleteRole(context.Context, api.DeleteRoleRequestObject) (api.DeleteRoleResponseObject, error) {
	return nil, errNotWired
}

// stubGrants is the user-grant half of the Management API, which this suite
// does not exercise. Present because httpserver.New refuses a /v1 chain with
// any half missing (P2-03).
type stubGrants struct{}

func (stubGrants) ListUserGrants(context.Context, api.ListUserGrantsRequestObject) (api.ListUserGrantsResponseObject, error) {
	return nil, errNotWired
}

func (stubGrants) GrantRolesToUser(context.Context, api.GrantRolesToUserRequestObject) (api.GrantRolesToUserResponseObject, error) {
	return nil, errNotWired
}

func (stubGrants) ReplaceUserGrant(context.Context, api.ReplaceUserGrantRequestObject) (api.ReplaceUserGrantResponseObject, error) {
	return nil, errNotWired
}

func (stubGrants) RevokeUserGrant(context.Context, api.RevokeUserGrantRequestObject) (api.RevokeUserGrantResponseObject, error) {
	return nil, errNotWired
}

// stubAuthz is the authorization check, which this suite does not exercise.
// Present because httpserver.New refuses a /v1 chain with any half missing.
type stubAuthz struct{}

func (stubAuthz) CheckAuthorization(context.Context, api.CheckAuthorizationRequestObject) (api.CheckAuthorizationResponseObject, error) {
	return nil, errNotWired
}

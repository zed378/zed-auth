//go:build integration

// The caller's own account through the real /v1 chain, against real Postgres
// and Redis (P3-12).
package account

import (
	"bytes"
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
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/authn"
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
	"github.com/zed378/zed-auth/backend/internal/sessionapi"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/internal/user"
)

const (
	issuer          = "https://auth.example.test"
	currentPassword = "Correct Horse Battery Staple 1"
	newPassword     = "Another Horse Battery Staple 2"
	email           = "member@example.test"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// switchableCorpus lets a test declare every password breached.
type switchableCorpus struct{ breached bool }

func (c *switchableCorpus) Breached(context.Context, string) (bool, error) { return c.breached, nil }

type fixture struct {
	db       *postgres.DB
	factory  *testsupport.Factory
	handler  http.Handler
	signer   *signing.Signer
	sessions *session.Manager
	corpus   *switchableCorpus
	users    *authn.UserStore

	orgID, clientID, member string
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance(), "Acme")
	member := factory.User(orgID, email)
	hash, err := authn.Hash(currentPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	factory.Exec(`UPDATE users SET password_hash = $2, display_name = 'Member' WHERE id = $1`, member, hash)

	var projectID, clientID string
	factory.QueryRow(&projectID, `INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgID)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'console', 'web') RETURNING id`,
		projectID, orgID)

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
	corpus := &switchableCorpus{}
	users := authn.NewUserStore()

	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(), Sessions: sessions,
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 1000, Window: time.Minute}, ""),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}

	api := &Handler{
		DB: db, Audit: auditor, Credentials: users, Passwords: user.NewStore(),
		Validator: &authn.PasswordValidator{
			Policies: authn.NewPolicyStore(discard()), Breaches: corpus, Audit: auditor,
		},
		Policies: authn.NewPolicyStore(discard()),
		Limiter:  ratelimit.New(rdb, nil, discard()),
		Sessions: sessions, Refresh: token.NewRefreshStore(), BreachChecked: true, Log: discard(),
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
		SessionAPI:      &sessionapi.Handler{},
		MfaAPI:          &mfaapi.Handler{},
		ProjectGrantAPI: &projectgrant.Handler{}, // not exercised here
		AccountAPI:      api,
		AuditAPI:        &auditlog.Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, factory: factory, handler: srv.Handler(), signer: signing.NewSigner(keys),
		sessions: sessions, corpus: corpus, users: users,
		orgID: orgID, clientID: clientID, member: member,
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

// signIn creates a session and returns a bearer token issued through it, with
// the session's cookie value.
func (f *fixture) signIn(t *testing.T) (string, string, string) {
	t.Helper()
	var (
		created session.Session
		tok     session.Token
	)
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		created, tok, err = f.sessions.Create(context.Background(), tx, session.New{
			UserID: f.member, OrgID: f.orgID, AuthMethods: []string{"pwd"},
		}, session.DefaultPolicy, time.Now())
		return err
	}); err != nil {
		t.Fatalf("creating a session: %v", err)
	}
	if _, err := f.sessions.Lookup(context.Background(), tok.Reveal(), session.DefaultPolicy, time.Now()); err != nil {
		t.Fatalf("warming: %v", err)
	}
	return f.bearer(t, created.ID), tok.Reveal(), created.ID
}

func (f *fixture) bearer(t *testing.T, sessionID string) string {
	t.Helper()
	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: f.orgID, UserID: f.member, SessionID: sessionID, Scope: []string{"openid"},
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

func (f *fixture) call(t *testing.T, bearer, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	r := httptest.NewRequest(method, target, reader)
	r.Header.Set("Authorization", "Bearer "+bearer)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) change(t *testing.T, bearer, current, chosen string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"current_password": current, "new_password": chosen})
	return f.call(t, bearer, http.MethodPost, "/v1/me/password", string(body))
}

func (f *fixture) passwordWorks(t *testing.T, password string) bool {
	t.Helper()
	var ok bool
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		_, verified, err := f.users.Authenticate(context.Background(), tx, email, password)
		ok = verified
		return err
	}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return ok
}

func (f *fixture) cookieWorks(cookie string) bool {
	_, err := f.sessions.Lookup(context.Background(), cookie, session.DefaultPolicy, time.Now())
	return err == nil
}

// --- reading ------------------------------------------------------------------------------------

func TestMeShowsTheAccountAndThePolicyUpFront(t *testing.T) {
	f := setup(t)
	f.factory.Exec(`UPDATE organizations SET settings = '{"password_policy": {"min_length": 16, "require_uppercase": false, "max_age_days": 30}}' WHERE id = $1`, f.orgID)
	bearer, _, _ := f.signIn(t)

	w := f.call(t, bearer, http.MethodGet, "/v1/me", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/me answered %d:\n%s", w.Code, w.Body.String())
	}
	var me struct {
		Email        string `json:"email"`
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
		Policy struct {
			MinLength     int  `json:"min_length"`
			Uppercase     bool `json:"require_uppercase"`
			MaxAgeDays    int  `json:"max_age_days"`
			BreachChecked bool `json:"breach_checked"`
		} `json:"password_policy"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	if me.Email != email || me.Organization.Name != "Acme" {
		t.Errorf("account = %+v", me)
	}
	if me.Policy.MinLength != 16 || me.Policy.Uppercase || me.Policy.MaxAgeDays != 30 || !me.Policy.BreachChecked {
		t.Errorf("policy = %+v, want the organization's configured values", me.Policy)
	}
	if strings.Contains(w.Body.String(), "password_hash") || strings.Contains(w.Body.String(), "$argon2") {
		t.Error("the account read carries password material")
	}
}

// --- changing the password --------------------------------------------------------------------

func TestChangingThePasswordSignsEverybodyElseOut(t *testing.T) {
	f := setup(t)
	bearer, currentCookie, _ := f.signIn(t)
	_, otherCookie, otherSession := f.signIn(t)
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		_, _, err := token.NewRefreshStore().Issue(context.Background(), tx, token.Refresh{
			UserID: f.member, ClientID: f.clientID, OrgID: f.orgID, SessionID: otherSession,
			Scope: []string{"openid"}, ExpiresAt: time.Now().Add(token.RefreshTokenLifetime),
		}, "", time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing a refresh token: %v", err)
	}

	w := f.change(t, bearer, currentPassword, newPassword)
	if w.Code != http.StatusNoContent {
		t.Fatalf("the change answered %d:\n%s", w.Code, w.Body.String())
	}

	if f.passwordWorks(t, currentPassword) || !f.passwordWorks(t, newPassword) {
		t.Error("the password did not change")
	}
	if !f.cookieWorks(currentCookie) {
		t.Error("the session that changed the password was signed out")
	}
	if f.cookieWorks(otherCookie) {
		t.Error("another session survived the password change")
	}
	var live int
	f.factory.QueryRow(&live, `SELECT count(*) FROM refresh_tokens WHERE session_id = $1 AND NOT revoked`, otherSession)
	if live != 0 {
		t.Error("the other session's refresh token survived the password change")
	}
	var events int
	f.factory.QueryRow(&events, `SELECT count(*) FROM events WHERE event_type = 'user.password.changed'
		AND payload->>'via' = 'self-service' AND actor_user_id = $1`, f.member)
	if events != 1 {
		t.Errorf("found %d password-change events, want 1", events)
	}
}

func TestTheCurrentPasswordIsRequired(t *testing.T) {
	f := setup(t)
	bearer, _, _ := f.signIn(t)

	w := f.change(t, bearer, "not my password", newPassword)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"current_password"`) {
		t.Fatalf("a wrong current password answered %d:\n%s", w.Code, w.Body.String())
	}
	if !f.passwordWorks(t, currentPassword) {
		t.Error("a change with the wrong current password took effect")
	}
}

// The endpoint shares sign-in's bound: it is not a second place to guess.
func TestWrongGuessesShareTheSignInCooldown(t *testing.T) {
	f := setup(t)
	bearer, _, _ := f.signIn(t)

	for i := 0; i < ratelimit.PerAddress.Free+1; i++ {
		f.change(t, bearer, "guess", newPassword)
	}

	w := f.change(t, bearer, currentPassword, newPassword)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("the right password during a cooldown answered %d, want 429", w.Code)
	}
	if !f.passwordWorks(t, currentPassword) {
		t.Error("the password changed during a cooldown")
	}
}

func TestTheNewPasswordMustMeetThePolicyAndNotBeBreached(t *testing.T) {
	f := setup(t)
	bearer, _, _ := f.signIn(t)

	if w := f.change(t, bearer, currentPassword, "short"); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), `"new_password"`) {
		t.Errorf("a weak password answered %d:\n%s", w.Code, w.Body.String())
	}

	f.corpus.breached = true
	if w := f.change(t, bearer, currentPassword, newPassword); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "known data breach") {
		t.Errorf("a breached password answered %d:\n%s", w.Code, w.Body.String())
	}
	f.corpus.breached = false

	if w := f.change(t, bearer, currentPassword, currentPassword); w.Code != http.StatusBadRequest {
		t.Errorf("changing to the same password answered %d", w.Code)
	}
	if !f.passwordWorks(t, currentPassword) {
		t.Error("a refused new password took effect")
	}
}

func TestATokenWithoutASessionCannotChangeThePassword(t *testing.T) {
	f := setup(t)
	if w := f.change(t, f.bearer(t, ""), currentPassword, newPassword); w.Code != http.StatusBadRequest {
		t.Errorf("a sessionless token answered %d, want 400", w.Code)
	}
	if !f.passwordWorks(t, currentPassword) {
		t.Error("a sessionless token changed the password")
	}
}

func TestBothPasswordsAreRequired(t *testing.T) {
	f := setup(t)
	bearer, _, _ := f.signIn(t)

	for _, body := range []string{`{"current_password":"","new_password":"x"}`, `{"current_password":"x","new_password":""}`} {
		if w := f.call(t, bearer, http.MethodPost, "/v1/me/password", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", body, w.Code)
		}
	}
}

// A profile with no name and a recorded password change reads as null and a time.
func TestMeReportsAnAbsentNameAndTheLastPasswordChange(t *testing.T) {
	f := setup(t)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	f.factory.Exec(`UPDATE users SET display_name = NULL, password_changed_at = $2 WHERE id = $1`, f.member, at)
	bearer, _, _ := f.signIn(t)

	w := f.call(t, bearer, http.MethodGet, "/v1/me", "")
	var me struct {
		DisplayName       *string    `json:"display_name"`
		PasswordChangedAt *time.Time `json:"password_changed_at"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	if me.DisplayName != nil {
		t.Errorf("display_name = %q, want null", *me.DisplayName)
	}
	if me.PasswordChangedAt == nil || !me.PasswordChangedAt.Equal(at) {
		t.Errorf("password_changed_at = %v, want %v", me.PasswordChangedAt, at)
	}
}

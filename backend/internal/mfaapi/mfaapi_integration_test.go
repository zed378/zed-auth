//go:build integration

// The factor API through the real /v1 chain, against real Postgres and Redis
// (P3-10).
//
// Real sealing, real TOTP codes computed the way an authenticator app computes
// them, real recovery codes, and the same Redis attempt bound the sign-in
// challenge uses. The abuse cases are asserted on what is left in the database
// afterwards, not only on the status code.
package mfaapi

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
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/account"
	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/authz"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/mfa"
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

const issuer = "https://auth.example.test"

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

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

type fixture struct {
	db       *postgres.DB
	rdb      *redis.Client
	factory  *testsupport.Factory
	handler  http.Handler
	signer   *signing.Signer
	sessions *session.Manager
	api      *Handler
	guard    *countingGuard
	recovery *mfa.RecoveryStore

	orgA, orgB, clientID               string
	member, colleague, admin, outsider string
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

	sealer, err := mfa.NewSealer([]byte("an-integration-test-key-long-enough"))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	factorStore := mfa.NewStore()
	totp := &mfa.TOTP{
		Store: factorStore, DB: db, Sealer: sealer, Log: discard(), Issuer: issuer,
		OrgOf: func(ctx context.Context, factorID string) (string, error) {
			var orgID string
			err := db.SQL().QueryRowContext(ctx, `SELECT mfa_factor_org($1)`, factorID).Scan(&orgID)
			return orgID, err
		},
	}
	recovery := mfa.NewRecoveryStore()

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
		Audit:       &management.AuditGuard{Log: discard(), Observer: guard},
		BufferBody:  true,
	}

	api := &Handler{
		DB: db, Audit: auditor, Factors: factorStore, Recovery: recovery,
		Policies: authn.NewPolicyStore(discard()), Members: user.NewStore(),
		Enroller: totp, Attempts: &mfa.RedisAttempts{Client: rdb}, PasskeysAvailable: true,
		Log: discard(),
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
		MfaAPI:          api,
		ProjectGrantAPI: &projectgrant.Handler{}, // not exercised here
		AccountAPI:      &account.Handler{},      // not exercised here
		AuditAPI:        &auditlog.Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, rdb: rdb, factory: factory, handler: srv.Handler(), signer: signing.NewSigner(keys),
		sessions: sessions, api: api, guard: guard, recovery: recovery,
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

// signedIn creates a session that authenticated at `at` and returns a bearer
// token issued through it.
func (f *fixture) signedIn(t *testing.T, orgID, userID string, at time.Time) string {
	t.Helper()

	var created session.Session
	if err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		created, _, err = f.sessions.Create(context.Background(), tx, session.New{
			UserID: userID, OrgID: orgID, AuthMethods: []string{"pwd"},
		}, session.DefaultPolicy, at)
		return err
	}); err != nil {
		t.Fatalf("creating a session: %v", err)
	}
	return f.bearer(t, orgID, userID, created.ID)
}

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

type enrolment struct {
	FactorID        string `json:"factor_id"`
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
	Qr              struct {
		Size int      `json:"size"`
		Rows []string `json:"rows"`
	} `json:"qr"`
}

func (f *fixture) begin(t *testing.T, bearer string) enrolment {
	t.Helper()
	w := f.call(t, bearer, http.MethodPost, "/v1/me/mfa/totp", `{"label":"phone"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("beginning an enrolment answered %d:\n%s", w.Code, w.Body.String())
	}
	var out enrolment
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return out
}

func codeFor(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	raw, err := mfa.DecodeTOTPSecret(secret)
	if err != nil {
		t.Fatalf("decoding a secret: %v", err)
	}
	return mfa.TOTPCode(raw, mfa.TOTPCounter(at))
}

func (f *fixture) confirm(t *testing.T, bearer, factorID, code string) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, bearer, http.MethodPost, "/v1/me/mfa/totp/"+factorID+"/confirm", `{"code":"`+code+`"}`)
}

func (f *fixture) factorStatus(t *testing.T, factorID string) string {
	t.Helper()
	var status string
	f.factory.QueryRow(&status, `SELECT COALESCE((SELECT status FROM user_mfa_factors WHERE id = $1), 'gone')`, factorID)
	return status
}

func (f *fixture) events(t *testing.T, kind string) int {
	t.Helper()
	var n int
	f.factory.QueryRow(&n, `SELECT count(*) FROM events WHERE event_type = $1`, kind)
	return n
}

type myMfa struct {
	Factors []struct {
		ID    string  `json:"id"`
		Type  string  `json:"type"`
		Label *string `json:"label"`
	} `json:"factors"`
	RecoveryCodesRemaining int        `json:"recovery_codes_remaining"`
	MfaRequired            bool       `json:"mfa_required"`
	GraceEndsAt            *time.Time `json:"grace_ends_at"`
	AvailableTypes         []string   `json:"available_types"`
}

func (f *fixture) mine(t *testing.T, bearer string) myMfa {
	t.Helper()
	w := f.call(t, bearer, http.MethodGet, "/v1/me/mfa", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/me/mfa answered %d:\n%s", w.Code, w.Body.String())
	}
	var out myMfa
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return out
}

// enrolled runs a whole enrolment for a user and returns the factor id and the
// recovery codes it produced.
func (f *fixture) enrolled(t *testing.T, bearer string) (string, []string) {
	t.Helper()
	e := f.begin(t, bearer)
	w := f.confirm(t, bearer, e.FactorID, codeFor(t, e.Secret, time.Now()))
	if w.Code != http.StatusOK {
		t.Fatalf("confirming answered %d:\n%s", w.Code, w.Body.String())
	}
	var body struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return e.FactorID, body.RecoveryCodes
}

// --- the whole enrolment --------------------------------------------------------------------

func TestAUserEnrolsAnAuthenticatorApp(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())

	before := f.mine(t, bearer)
	if len(before.Factors) != 0 || len(before.AvailableTypes) != 2 {
		t.Fatalf("before enrolment: %+v", before)
	}

	e := f.begin(t, bearer)
	if e.Secret == "" || !strings.HasPrefix(e.ProvisioningURI, "otpauth://totp/") {
		t.Fatalf("the enrolment is missing its secret or URI: %+v", e)
	}
	if e.Qr.Size < 21 || len(e.Qr.Rows) != e.Qr.Size || len(e.Qr.Rows[0]) != e.Qr.Size {
		t.Errorf("the QR grid is malformed: size %d, %d rows", e.Qr.Size, len(e.Qr.Rows))
	}

	// Pending is invisible.
	if pending := f.mine(t, bearer); len(pending.Factors) != 0 {
		t.Errorf("a pending enrolment is listed as a factor: %+v", pending.Factors)
	}

	// A wrong code is a 400 on the field and activates nothing.
	if w := f.confirm(t, bearer, e.FactorID, "000000"); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), `"code"`) {
		t.Errorf("a wrong code answered %d:\n%s", w.Code, w.Body.String())
	}
	if f.factorStatus(t, e.FactorID) != "pending" {
		t.Fatal("a wrong code changed the factor")
	}

	w := f.confirm(t, bearer, e.FactorID, codeFor(t, e.Secret, time.Now()))
	if w.Code != http.StatusOK {
		t.Fatalf("confirming answered %d:\n%s", w.Code, w.Body.String())
	}
	var confirmed struct {
		Factor struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"factor"`
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &confirmed)
	if confirmed.Factor.ID != e.FactorID || confirmed.Factor.Type != "totp" {
		t.Errorf("confirmed factor = %+v", confirmed.Factor)
	}
	if len(confirmed.RecoveryCodes) != mfa.RecoveryCodeCount {
		t.Errorf("the first factor came with %d recovery codes, want %d", len(confirmed.RecoveryCodes), mfa.RecoveryCodeCount)
	}
	if f.factorStatus(t, e.FactorID) != "active" {
		t.Error("the factor is not active after a correct code")
	}

	after := f.mine(t, bearer)
	if len(after.Factors) != 1 || after.RecoveryCodesRemaining != mfa.RecoveryCodeCount {
		t.Errorf("after enrolment: %+v", after)
	}

	// The record, and nothing secret in it.
	if f.events(t, string(audit.EventMFAEnrolmentStarted)) != 1 || f.events(t, string(audit.EventMFAEnrolled)) != 1 ||
		f.events(t, string(audit.EventMFACodesIssued)) != 1 {
		t.Error("the enrolment was not fully audited")
	}
	var leaked int
	f.factory.QueryRow(&leaked, `SELECT count(*) FROM events WHERE payload::text LIKE '%' || $1 || '%'`, e.Secret)
	if leaked != 0 {
		t.Error("the secret appears in an audit payload")
	}
	if f.guard.count() != 0 {
		t.Errorf("the audit guard reported %v", f.guard.missed)
	}
}

// A second enrolment for a user who already has recovery codes does not replace
// them — that would silently invalidate codes somebody wrote down.
func TestConfirmingWithCodesAlreadyHeldIssuesNoNewOnes(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())
	factorID, codes := f.enrolled(t, bearer)

	// Remove it (no mandate), keeping the codes, and enrol again.
	if w := f.call(t, bearer, http.MethodDelete, "/v1/me/mfa/factors/"+factorID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("removing answered %d", w.Code)
	}
	e := f.begin(t, bearer)
	w := f.confirm(t, bearer, e.FactorID, codeFor(t, e.Secret, time.Now().Add(mfa.TOTPPeriod)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"recovery_codes":null`) {
		t.Fatalf("a second enrolment with codes held answered %d:\n%s", w.Code, w.Body.String())
	}
	if _, err := f.spend(t, f.orgA, f.member, codes[0]); err != nil {
		t.Errorf("an existing recovery code stopped working: %v", err)
	}
}

func (f *fixture) spend(t *testing.T, orgID, userID, code string) (int, error) {
	t.Helper()
	var remaining int
	err := f.db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		remaining, err = f.recovery.Consume(context.Background(), tx, userID, code, time.Now())
		return err
	})
	return remaining, err
}

// --- A-1: recent authentication -------------------------------------------------------------

func TestChangingFactorsNeedsARecentSignIn(t *testing.T) {
	f := setup(t)
	fresh := f.signedIn(t, f.orgA, f.member, time.Now())
	factorID, _ := f.enrolled(t, fresh)

	stale := f.signedIn(t, f.orgA, f.member, time.Now().Add(-RecentAuthentication-time.Minute))
	noSession := f.bearer(t, f.orgA, f.member, "")

	for _, bearer := range []string{stale, noSession} {
		for _, req := range []struct{ method, path, body string }{
			{http.MethodPost, "/v1/me/mfa/totp", "{}"},
			{http.MethodDelete, "/v1/me/mfa/factors/" + factorID, ""},
			{http.MethodPost, "/v1/me/mfa/recovery-codes", ""},
		} {
			w := f.call(t, bearer, req.method, req.path, req.body)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "REAUTHENTICATION_REQUIRED") {
				t.Errorf("%s %s with a stale or sessionless token answered %d:\n%s", req.method, req.path, w.Code, w.Body.String())
			}
		}
	}

	if f.factorStatus(t, factorID) != "active" {
		t.Error("a stale session removed a factor")
	}
	var total int
	f.factory.QueryRow(&total, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1`, f.member)
	if total != 1 {
		t.Errorf("a stale session changed the factor set: %d rows", total)
	}

	// Reading needs no recency.
	if got := f.mine(t, stale); len(got.Factors) != 1 {
		t.Errorf("a stale session could not read its own factors: %+v", got)
	}
}

// --- A-2: the attempt bound --------------------------------------------------------------------

func TestConfirmationIsBoundedPerUser(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())
	e := f.begin(t, bearer)

	for i := 0; i < mfa.MaxAttemptsPerWindow; i++ {
		f.confirm(t, bearer, e.FactorID, "000000")
	}

	w := f.confirm(t, bearer, e.FactorID, codeFor(t, e.Secret, time.Now()))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("a correct code after exhausting the bound answered %d, want 429", w.Code)
	}
	if f.factorStatus(t, e.FactorID) != "pending" {
		t.Error("the bound was exhausted and the factor was activated anyway")
	}
}

// --- A-3 / A-4: another user's factor ------------------------------------------------------------

func TestAUserCannotTouchAnotherUsersFactor(t *testing.T) {
	f := setup(t)
	member := f.signedIn(t, f.orgA, f.member, time.Now())
	colleague := f.signedIn(t, f.orgA, f.colleague, time.Now())

	theirs := f.begin(t, colleague)
	if w := f.confirm(t, member, theirs.FactorID, codeFor(t, theirs.Secret, time.Now())); w.Code != http.StatusNotFound {
		t.Errorf("confirming a colleague's pending factor answered %d, want 404", w.Code)
	}
	if f.factorStatus(t, theirs.FactorID) != "pending" {
		t.Fatal("a user activated a colleague's factor")
	}

	if w := f.confirm(t, colleague, theirs.FactorID, codeFor(t, theirs.Secret, time.Now())); w.Code != http.StatusOK {
		t.Fatalf("the colleague could not confirm their own: %d", w.Code)
	}
	if w := f.call(t, member, http.MethodDelete, "/v1/me/mfa/factors/"+theirs.FactorID, ""); w.Code != http.StatusNotFound {
		t.Errorf("removing a colleague's factor answered %d, want 404", w.Code)
	}
	if f.factorStatus(t, theirs.FactorID) != "active" {
		t.Error("a user removed a colleague's factor")
	}
}

// --- A-7: the mandate ------------------------------------------------------------------------------

func TestTheLastFactorCannotBeRemovedUnderAMandate(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())
	factorID, _ := f.enrolled(t, bearer)

	f.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true, "mfa_required_since": "2020-01-01T00:00:00Z"}' WHERE id = $1`, f.orgA)

	if got := f.mine(t, bearer); !got.MfaRequired {
		t.Error("the mandate is not reported")
	}
	w := f.call(t, bearer, http.MethodDelete, "/v1/me/mfa/factors/"+factorID, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("removing the last factor under a mandate answered %d, want 409:\n%s", w.Code, w.Body.String())
	}
	if f.factorStatus(t, factorID) != "active" {
		t.Fatal("the last factor was removed under a mandate")
	}

	f.factory.Exec(`UPDATE organizations SET settings = '{}' WHERE id = $1`, f.orgA)
	if w := f.call(t, bearer, http.MethodDelete, "/v1/me/mfa/factors/"+factorID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("removing without a mandate answered %d", w.Code)
	}
	if f.factorStatus(t, factorID) != "gone" || f.events(t, string(audit.EventMFARemoved)) != 1 {
		t.Error("the removal did not happen or was not audited")
	}
}

// The deadline reaches the caller (P3-13), so a person inside the grace can be
// told before the sign-in that stops them — and it is absent without a mandate.
func TestTheGraceDeadlineIsReportedToTheCaller(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())

	if got := f.mine(t, bearer); got.GraceEndsAt != nil {
		t.Errorf("a deadline was reported with no mandate: %s", got.GraceEndsAt)
	}

	f.factory.Exec(`UPDATE organizations SET settings = '{"mfa_required": true, "mfa_required_since": "2026-09-01T00:00:00Z"}' WHERE id = $1`, f.orgA)

	got := f.mine(t, bearer)
	if got.GraceEndsAt == nil {
		t.Fatal("no deadline reported under a mandate")
	}
	if want := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC); !got.GraceEndsAt.Equal(want) {
		t.Errorf("grace_ends_at = %s, want %s — fourteen days from activation", got.GraceEndsAt, want)
	}
}

// --- recovery codes ------------------------------------------------------------------------------

func TestRegeneratingReplacesEveryCode(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())

	// With no factor there is nothing to recover past.
	if w := f.call(t, bearer, http.MethodPost, "/v1/me/mfa/recovery-codes", ""); w.Code != http.StatusConflict {
		t.Errorf("regenerating with no factor answered %d, want 409", w.Code)
	}

	_, old := f.enrolled(t, bearer)
	w := f.call(t, bearer, http.MethodPost, "/v1/me/mfa/recovery-codes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("regenerating answered %d:\n%s", w.Code, w.Body.String())
	}
	var body struct {
		Codes []string `json:"codes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Codes) != mfa.RecoveryCodeCount {
		t.Fatalf("got %d codes", len(body.Codes))
	}

	if _, err := f.spend(t, f.orgA, f.member, old[0]); err == nil {
		t.Error("a code from the replaced batch still works")
	}
	if _, err := f.spend(t, f.orgA, f.member, body.Codes[0]); err != nil {
		t.Errorf("a new code does not work: %v", err)
	}
}

// One authenticator app per user; replacing it is a removal first.
func TestASecondAuthenticatorAppIsRefused(t *testing.T) {
	f := setup(t)
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())
	f.enrolled(t, bearer)

	if w := f.call(t, bearer, http.MethodPost, "/v1/me/mfa/totp", "{}"); w.Code != http.StatusConflict {
		t.Errorf("a second authenticator app answered %d, want 409", w.Code)
	}
}

// --- A-5 / A-6: the administrator ------------------------------------------------------------------

func TestAnAdministratorCanReadButNotWrite(t *testing.T) {
	f := setup(t)
	member := f.signedIn(t, f.orgA, f.member, time.Now())
	e := f.begin(t, member)
	if w := f.confirm(t, member, e.FactorID, codeFor(t, e.Secret, time.Now())); w.Code != http.StatusOK {
		t.Fatalf("setup: %d", w.Code)
	}
	admin := f.signedIn(t, f.orgA, f.admin, time.Now())

	w := f.call(t, admin, http.MethodGet, "/v1/organizations/"+f.orgA+"/users/"+f.member+"/mfa", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":"totp"`) {
		t.Fatalf("the admin view answered %d:\n%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), e.Secret) || strings.Contains(w.Body.String(), "otpauth") {
		t.Error("the admin view carries secret material")
	}

	// The admin's token on a self route acts on the ADMIN, never the member.
	f.begin(t, admin)
	var memberFactors, adminFactors int
	f.factory.QueryRow(&memberFactors, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1`, f.member)
	f.factory.QueryRow(&adminFactors, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1`, f.admin)
	if memberFactors != 1 || adminFactors != 1 {
		t.Errorf("after the admin began an enrolment: member has %d factors, admin %d", memberFactors, adminFactors)
	}

	// A member cannot use the admin view; another organization's admin reaches nothing.
	colleague := f.signedIn(t, f.orgA, f.colleague, time.Now())
	if w := f.call(t, colleague, http.MethodGet, "/v1/organizations/"+f.orgA+"/users/"+f.member+"/mfa", ""); w.Code == http.StatusOK {
		t.Error("a member read a colleague's factors")
	}
	outsider := f.signedIn(t, f.orgB, f.outsider, time.Now())
	if w := f.call(t, outsider, http.MethodGet, "/v1/organizations/"+f.orgA+"/users/"+f.member+"/mfa", ""); w.Code == http.StatusOK {
		t.Error("another organization's admin read a member's factors")
	}
	if w := f.call(t, outsider, http.MethodGet, "/v1/organizations/"+f.orgB+"/users/"+f.member+"/mfa", ""); w.Code != http.StatusNotFound {
		t.Errorf("naming a foreign member under one's own organization answered %d, want 404", w.Code)
	}
}

// Without the factor framework, reads say so and writes refuse.
func TestAnUnconfiguredDeploymentSaysSo(t *testing.T) {
	f := setup(t)
	f.api.Enroller, f.api.Attempts, f.api.PasskeysAvailable = nil, nil, false
	bearer := f.signedIn(t, f.orgA, f.member, time.Now())

	if got := f.mine(t, bearer); len(got.AvailableTypes) != 0 {
		t.Errorf("available types = %v on a deployment with no MFA", got.AvailableTypes)
	}
	if w := f.call(t, bearer, http.MethodPost, "/v1/me/mfa/totp", "{}"); w.Code != http.StatusConflict {
		t.Errorf("enrolling with no MFA configured answered %d, want 409", w.Code)
	}
}

// A token cannot borrow another user's fresh session.
//
// The recency check reads the session named by the token's `sid` TOGETHER with
// the token's user. Reading by `sid` alone would let a token for one user
// present a colleague's just-created session as proof of a recent sign-in.
func TestRecencyCannotBeBorrowedFromAnotherUsersSession(t *testing.T) {
	f := setup(t)

	var colleagueSession session.Session
	if err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		var err error
		colleagueSession, _, err = f.sessions.Create(context.Background(), tx, session.New{
			UserID: f.colleague, OrgID: f.orgA, AuthMethods: []string{"pwd"},
		}, session.DefaultPolicy, time.Now())
		return err
	}); err != nil {
		t.Fatalf("creating a session: %v", err)
	}

	borrowed := f.bearer(t, f.orgA, f.member, colleagueSession.ID)
	w := f.call(t, borrowed, http.MethodPost, "/v1/me/mfa/totp", "{}")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "REAUTHENTICATION_REQUIRED") {
		t.Errorf("a token carrying a colleague's session answered %d:\n%s", w.Code, w.Body.String())
	}
	var factors int
	f.factory.QueryRow(&factors, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1`, f.member)
	if factors != 0 {
		t.Error("an enrolment began on borrowed recency")
	}
}

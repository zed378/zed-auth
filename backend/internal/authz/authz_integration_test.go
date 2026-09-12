//go:build integration

// The authorization check, end to end (P2-06).
//
// Three properties here cannot be tested any other way: that the decision reads
// LIVE data rather than the caller's token, that a nonexistent subject and an
// unauthorized one are byte-identical, and that a dependency failure denies.
package authz

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/role"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/internal/user"
)

const issuer = "https://auth.example.test"

func TestMain(m *testing.M) { os.Exit(testsupport.RunTests(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- the reason the endpoint exists ----------------------------------------

// A revocation is honoured on the next check, without waiting for a token to
// expire.
//
// The caller's token is minted ONCE, before the grant is revoked, and reused
// for both checks. So the only way the second answer can differ is if the
// decision read live data — which is the entire premise of the endpoint
// (`docs/PLAN/08`: prefer this over trusting token claims for sensitive
// actions).
func TestARevocationIsHonouredOnTheNextCheck(t *testing.T) {
	f := setup(t)

	f.grantRole(t, f.subject, "approver")

	if got := f.check(t, f.subject, "approve", "purchase_request"); !got.Allowed {
		t.Fatalf("the granted permission was denied: %+v", got)
	}

	f.factory.Exec(`DELETE FROM user_grants WHERE user_id = $1`, f.subject)

	after := f.check(t, f.subject, "approve", "purchase_request")
	if after.Allowed {
		t.Error("a revoked role was still honoured — the decision is reading the token, not the grant")
	}
}

// --- the oracle ------------------------------------------------------------

// A subject who does not exist and one who exists with no matching role must
// produce the SAME response, byte for byte.
//
// Otherwise this endpoint answers "does user X exist in this organization?" for
// anybody holding a valid client token — `docs/SECURITY/02` §12's enumeration,
// wearing an authorization question's clothes. And it answers it cheaply,
// repeatedly, and without appearing in any audit log, because decisions are
// deliberately not audited.
func TestANonexistentSubjectIsIndistinguishableFromAnUnauthorizedOne(t *testing.T) {
	f := setup(t)

	// Exists, holds a role, and it is NOT the one being asked about.
	f.grantRole(t, f.subject, "reader", "purchase_request:read")

	real := f.raw(t, f.subject, "approve", "purchase_request")
	ghost := f.raw(t, "00000000-0000-0000-0000-000000000000", "approve", "purchase_request")

	if real.Code != ghost.Code {
		t.Errorf("status differs: %d for a real subject, %d for one that does not exist", real.Code, ghost.Code)
	}
	if real.Body.String() != ghost.Body.String() {
		t.Errorf("the responses differ:\n  real:  %s\n  ghost: %s", real.Body, ghost.Body)
	}
}

// The project comes from the caller's token, not from anywhere in the request —
// so a caller cannot ask about a project it does not own.
func TestTheProjectComesFromTheTokenNotTheBody(t *testing.T) {
	f := setup(t)

	// The subject holds `approver` in the OTHER project, which this caller's
	// client does not belong to.
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, 'approver', 'Approver', $3)`,
		f.orgA, f.otherProject, pq.Array([]string{"purchase_request:approve"}))
	f.factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		f.subject, f.otherProject, f.orgA, pq.Array([]string{"approver"}))

	got := f.check(t, f.subject, "approve", "purchase_request")
	if got.Allowed {
		t.Error("a role held in another project decided this project's check")
	}
}

// A caller cannot ask about a subject in another organization: the tenant comes
// from the token, and row-level security is what confines the read.
func TestACheckCannotCrossOrganizations(t *testing.T) {
	f := setup(t)

	// A user in organization B, granted everything there.
	otherUser := f.factory.User(f.orgB)
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, 'approver', 'Approver', $3)`,
		f.orgB, f.projectB, pq.Array([]string{"purchase_request:approve"}))
	f.factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		otherUser, f.projectB, f.orgB, pq.Array([]string{"approver"}))

	got := f.check(t, otherUser, "approve", "purchase_request")
	if got.Allowed {
		t.Error("a caller in organization A got an allow about a user in organization B")
	}
}

// --- failing closed --------------------------------------------------------

// Fault injection: the database is closed under the handler.
//
// `docs/PLAN/13`: "availability of a decision is never a reason to weaken
// security posture". Nothing is allowed — and the response is a 503 rather than
// `200 {"allowed": false}`, because the second claims a decision was reached.
// That distinction is not pedantry: a caller may cache a decision, and an
// operator watching the allow/deny ratio would see a policy change instead of
// an outage.
func TestADependencyFailureDenies(t *testing.T) {
	f := setup(t)
	f.grantRole(t, f.subject, "approver")

	// It works first, so the failure below is the only difference.
	if got := f.check(t, f.subject, "approve", "purchase_request"); !got.Allowed {
		t.Fatal("the check did not work before the fault was injected")
	}

	// Only the DECISION's pool is broken, not the whole request.
	//
	// Closing the shared pool would break authentication — the middleware
	// reads the caller's manager roles on every request — and the request
	// would fail with a 500 before reaching the code under test. The fault has
	// to land on the thing that must fail closed, so the handler the server
	// holds gets its own, already-closed pool.
	broken, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: f.dsn, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening a second pool: %v", err)
	}
	if err := broken.Close(); err != nil {
		t.Fatalf("closing it: %v", err)
	}
	f.authz.DB = broken

	rec := f.raw(t, f.subject, "approve", "purchase_request")

	if rec.Code == http.StatusOK {
		var out struct {
			Allowed bool `json:"allowed"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.Allowed {
			t.Fatal("a dependency failure produced an ALLOW")
		}
		t.Errorf("a failure answered 200 %s — that claims a decision was reached", rec.Body)
	}
	// 503, not 500: a 500 says a bug happened here. This says a dependency did
	// not answer, which is a different thing for a caller to act on.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a dependency failure answered %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "denied") {
		t.Errorf("the response does not tell the caller how to treat it: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "UNAVAILABLE") {
		t.Errorf("the error code is not UNAVAILABLE: %s", rec.Body)
	}
}

// --- attributes ------------------------------------------------------------

// `resource.attributes` are the consumer application's business data —
// `docs/PLAN/13` and `CLAUDE.md` both name them explicitly — and they must not
// reach any log sink.
//
// Asserted by capturing everything the handler logs during a request that
// carries deliberately distinctive attributes, and searching it.
func TestResourceAttributesNeverReachTheLog(t *testing.T) {
	f := setup(t)

	var captured bytes.Buffer
	f.handlerLog(&captured)

	body := fmt.Sprintf(`{
		"subject": {"user_id": %q},
		"action": "approve",
		"resource": {
			"type": "purchase_request",
			"id": "pr_9931",
			"attributes": {"department": "finance", "amount": 8000000, "note": "SECRET-CANARY-VALUE"}
		},
		"context": {"ip": "10.0.4.2"}
	}`, f.subject)

	rec := f.post(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("the check failed: %d %s", rec.Code, rec.Body)
	}

	logged := captured.String()
	if logged == "" {
		t.Fatal("nothing was logged at all, so this assertion proves nothing")
	}
	for _, secret := range []string{"SECRET-CANARY-VALUE", "finance", "8000000", "pr_9931"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log carries %q:\n%s", secret, logged)
		}
	}
	// And it does log something useful, or the test above is satisfied by
	// silence.
	if !strings.Contains(logged, "purchase_request:approve") {
		t.Errorf("the log does not record what was asked:\n%s", logged)
	}
}

// --- the documented example ------------------------------------------------

// The response shape is what `docs/PLAN/05` prints. Consumers build against it.
func TestTheResponseMatchesTheDocumentedShape(t *testing.T) {
	f := setup(t)
	f.grantRole(t, f.subject, "approver")

	rec := f.raw(t, f.subject, "approve", "purchase_request")

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	for _, field := range []string{"allowed", "matched_policy", "reasons"} {
		if _, present := out[field]; !present {
			t.Errorf("the response has no %q: %s", field, rec.Body)
		}
	}
	if len(out) != 3 {
		t.Errorf("the response carries unexpected fields: %s", rec.Body)
	}
}

// --- fixture ---------------------------------------------------------------

type decision struct {
	Allowed       bool     `json:"allowed"`
	MatchedPolicy string   `json:"matched_policy"`
	Reasons       []string `json:"reasons"`
}

type fixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	handler http.Handler
	authz   *Handler

	dsn   string
	redis *redis.Client
	cache *Cache

	// The two handlers that invalidate, so a cache test can wire them.
	grants *grant.Handler
	roles  *role.Handler

	adminToken string

	// Exposed so a test can mint a token this fixture did not: `P2-16`'s
	// claim-tampering suite needs a validly SIGNED token whose claims say
	// something the database does not.
	signer   *signing.Signer
	clientID string

	orgA, orgB   string
	projectA     string
	otherProject string
	projectB     string

	subject string
	token   string
}

func (f *fixture) handlerLog(into *bytes.Buffer) {
	f.authz.Log = slog.New(slog.NewTextHandler(into, nil))
}

// grantRole gives a user a role carrying exactly the permissions named.
//
// The permissions are a parameter rather than a constant, and that is not
// incidental: the first version granted `purchase_request:approve` to every
// role it created, so a test that meant to set up an UNAUTHORIZED subject
// created an authorized one, and the oracle test failed for a reason that had
// nothing to do with the oracle.
func (f *fixture) grantRole(t *testing.T, userID, roleKey string, permissions ...string) {
	t.Helper()
	if len(permissions) == 0 {
		permissions = []string{"purchase_request:approve"}
	}
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, $3, $3, $4)`,
		f.orgA, f.projectA, roleKey, pq.Array(permissions))
	f.factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		userID, f.projectA, f.orgA, pq.Array([]string{roleKey}))
}

func (f *fixture) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/authz/check", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// request is the general form; `post` and `raw` are the authorization-check
// shapes built on it.
func (f *fixture) request(t *testing.T, method, path, body, tok string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) raw(t *testing.T, subject, action, resourceType string) *httptest.ResponseRecorder {
	t.Helper()
	return f.post(t, fmt.Sprintf(
		`{"subject":{"user_id":%q},"action":%q,"resource":{"type":%q}}`,
		subject, action, resourceType))
}

func (f *fixture) check(t *testing.T, subject, action, resourceType string) decision {
	t.Helper()
	rec := f.raw(t, subject, action, resourceType)
	if rec.Code != http.StatusOK {
		t.Fatalf("check: %d %s", rec.Code, rec.Body)
	}
	var out decision
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	return out
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	newProject := func(org, name string) string {
		var id string
		factory.QueryRow(&id, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, name)
		return id
	}
	projectA := newProject(orgA, "alpha")
	otherProject := newProject(orgA, "other")
	projectB := newProject(orgB, "beta")

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keys := signingKeys(t, stack)
	signer := signing.NewSigner(keys)

	// The caller is an application IN projectA — which is what scopes every
	// decision it can ask for.
	var clientID string
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'checker', 'web') RETURNING id`, projectA, orgA)

	// A plain user with no manager role at all: `Member` is the requirement,
	// and a consumer service holding an administrative role would be the
	// inversion of least privilege this endpoint exists to avoid.
	callerUser := factory.User(orgA)
	subject := factory.User(orgA)

	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(db, discard(), nil)
	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 3000, Window: time.Minute}, ""),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}

	authzHandler := &Handler{DB: db, Log: discard()}
	grantHandler := grant.New(db, auditor, discard())
	roleHandler := role.New(db, auditor, discard())

	// An administrator, for the tests that change a grant or a role through
	// the API rather than through the factory — which is what invalidation
	// hangs off.
	adminUser := factory.User(orgA)
	factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`,
		adminUser, orgA)

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
		ProjectAPI:     &project.Handler{Store: project.NewStore(), DB: db, Audit: auditor, Log: discard()},
		ApplicationAPI: application.New(db, auditor, discard()),
		RoleAPI:        roleHandler,
		GrantAPI:       grantHandler,
		AuthzAPI:       authzHandler,
		UserAPI:        &user.Handler{Store: user.NewStore(), DB: db, Audit: auditor, Log: discard()},
		AuditAPI:       &auditlog.Handler{DB: db, Log: discard()},
	})

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: clientID,
		OrgID: orgA, UserID: callerUser, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	signed, err := signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	adminClaims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: clientID,
		OrgID: orgA, UserID: adminUser, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("admin claims: %v", err)
	}
	adminPayload, err := adminClaims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	adminSigned, err := signer.SignWithType(adminPayload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	return &fixture{
		db: db, dsn: stack.AppDSN, redis: rdb, factory: factory,
		handler: srv.Handler(), authz: authzHandler,
		grants: grantHandler, roles: roleHandler, adminToken: adminSigned,
		signer: signer, clientID: clientID,
		orgA: orgA, orgB: orgB,
		projectA: projectA, otherProject: otherProject, projectB: projectB,
		subject: subject, token: signed,
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

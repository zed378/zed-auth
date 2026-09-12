//go:build integration

// User grants, end to end (P2-03).
//
// This is the row that actually gives somebody access, so the tests that matter
// most are the ones asserting an ABSENCE: a user with no grant has nothing, a
// caller cannot grant to themselves, and a delegated grant is refused until
// Phase 4 implements the subset check it needs.
package grant

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
	"github.com/zed378/zed-auth/backend/internal/authz"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	project2 "github.com/zed378/zed-auth/backend/internal/project"
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

// --- least privilege, which is the whole point ------------------------------

// `docs/PLAN/17` Phase 2 asks for this literally: a user with no grant has zero
// access.
//
// Asserted as an absence, because the absence IS the control — nothing writes a
// grant except these endpoints, so a user with no row has no roles. There is no
// code path to test, which is exactly why it needs a test: a future "default
// role for new users" convenience would break it silently.
func TestAUserWithNoGrantHasNoRoles(t *testing.T) {
	f := setup(t)

	fresh := f.factory.User(f.orgA)

	var keys []string
	if err := f.in(t, func(tx *postgres.Tx) error {
		var err error
		keys, err = f.store.RoleKeysFor(context.Background(), tx, fresh, f.projectA)
		return err
	}); err != nil {
		t.Fatalf("reading roles: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("a brand-new user holds %v", keys)
	}

	// Through the API too: an empty list, not an error. Having no access is an
	// ordinary state for a new account.
	got := f.do(t, "GET", f.grantsPath(fresh), f.adminToken, "")
	if got.Code != http.StatusOK {
		t.Fatalf("listing a new user's grants: %d %s", got.Code, got.Body)
	}
	var page struct {
		Grants []apiGrant `json:"grants"`
	}
	decode(t, got, &page)
	if len(page.Grants) != 0 {
		t.Errorf("a brand-new user has %d grants", len(page.Grants))
	}
}

// --- the closed Phase 4 slot ------------------------------------------------

// `docs/PLAN/08` Part C requires a delegated grant's role keys to be a SUBSET of
// what was delegated, revalidated on every request. That is the most
// security-critical check in the system and it belongs to `P4-01`.
//
// **This test is written to be INVERTED in Phase 4, not deleted.** The refusal
// is a trigger whose body Phase 4 replaces with the subset validation, so the
// diff shows a rule changing rather than a guard disappearing.
func TestADelegatedGrantIsRefusedUntilPhaseFour(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)

	// Written directly, because no API accepts the column at all — which is
	// the first line of defence and the reason this has to reach past it.
	err := f.factory.TryExec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys, project_grant_id)
		VALUES ($1, $2, $3, $4, gen_random_uuid())`,
		subject, f.projectA, f.orgA, pq.Array([]string{"admin"}))
	if err == nil {
		t.Fatal("a delegated grant was written with no subset validation anywhere")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("refused, but not by the slot: %v", err)
	}
}

// --- the role must exist ----------------------------------------------------

// A grant naming a role that does not exist looks like access, carries a key
// nothing defines, and is denied by every consumer with nothing explaining why.
func TestAGrantCannotNameARoleThatDoesNotExist(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)

	got := f.do(t, "POST", f.grantsPath(subject), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["admin","ghost"]}`, f.projectA))
	if got.Code != http.StatusBadRequest {
		t.Fatalf("got %d %s", got.Code, got.Body)
	}
	// It names WHICH key. "one of your role keys is invalid" sends an
	// administrator to guess, and a script usually sent several.
	if !strings.Contains(got.Body.String(), "ghost") {
		t.Errorf("the error does not name the missing role: %s", got.Body)
	}

	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM user_grants WHERE user_id = $1`, subject)
	if count != 0 {
		t.Error("the grant was written despite the refusal")
	}

	// And the database refuses it too, for a writer that never came through
	// the API.
	direct := f.factory.TryExec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		subject, f.projectA, f.orgA, pq.Array([]string{"ghost"}))
	if direct == nil {
		t.Error("the database accepted a grant naming a role that does not exist")
	}
}

// A role that exists in ANOTHER project does not count. Roles are scoped per
// project, and this is where that scoping either holds or silently does not.
func TestARoleFromAnotherProjectCannotBeGranted(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)

	// `other-only` exists in projectA2 and nowhere else.
	f.mustCreateRole(t, f.projectA2, "other-only")

	got := f.do(t, "POST", f.grantsPath(subject), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["other-only"]}`, f.projectA))
	if got.Code != http.StatusBadRequest {
		t.Errorf("a role from another project was granted: %d %s", got.Code, got.Body)
	}
}

// --- self-service escalation ------------------------------------------------

// The permission model says yes — an ORG_ADMIN administers every user in the
// organization, and they are one of those users. The answer should be no.
func TestACallerCannotGrantToThemselves(t *testing.T) {
	f := setup(t)

	got := f.do(t, "POST", f.grantsPath(f.adminUser), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["admin"]}`, f.projectA))
	if got.Code != http.StatusForbidden {
		t.Fatalf("an administrator granted themselves a role: %d %s", got.Code, got.Body)
	}

	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM user_grants WHERE user_id = $1`, f.adminUser)
	if count != 0 {
		t.Error("the self-grant was written")
	}

	// But they may still REVOKE their own access: it can only reduce what they
	// hold, and refusing it would mean dropping a privilege requires somebody
	// else.
	subject := f.adminUser
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`, subject, f.projectA, f.orgA, pq.Array([]string{"admin"}))

	revoked := f.do(t, "DELETE", f.grantsPath(subject)+"/"+f.projectA, f.adminToken, "")
	if revoked.Code != http.StatusNoContent {
		t.Errorf("an administrator could not revoke their own access: %d %s", revoked.Code, revoked.Body)
	}
}

// --- tenancy ----------------------------------------------------------------

func TestAGrantCannotBeWrittenIntoAnotherOrganizationsProject(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)

	got := f.do(t, "POST", f.grantsPath(subject), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["admin"]}`, f.projectB))
	if got.Code != http.StatusNotFound {
		t.Errorf("got %d %s, want 404", got.Code, got.Body)
	}
}

// --- the ordinary path, and revocation --------------------------------------

func TestGrantingReplacingAndRevoking(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)
	f.mustCreateRole(t, f.projectA, "reader")

	created := f.do(t, "POST", f.grantsPath(subject), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["admin"]}`, f.projectA))
	if created.Code != http.StatusCreated {
		t.Fatalf("grant: %d %s", created.Code, created.Body)
	}

	// A second grant for the same project is a conflict, not a second row.
	again := f.do(t, "POST", f.grantsPath(subject), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["reader"]}`, f.projectA))
	if again.Code != http.StatusConflict {
		t.Errorf("a duplicate grant gave %d, want 409", again.Code)
	}

	replaced := f.do(t, "PATCH", f.grantsPath(subject)+"/"+f.projectA, f.adminToken,
		`{"role_keys":["reader"]}`)
	if replaced.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", replaced.Code, replaced.Body)
	}
	var after apiGrant
	decode(t, replaced, &after)
	if len(after.RoleKeys) != 1 || after.RoleKeys[0] != "reader" {
		t.Errorf("after replace: %v", after.RoleKeys)
	}

	// An empty set is refused: a grant with no roles grants nothing
	// (`docs/PLAN/08` § Least Privilege).
	//
	// Both layers refuse it — the application check and the table's own
	// CHECK — so the status alone cannot tell them apart, and a mutation
	// removing the application check left this green. What the application
	// layer adds is the sentence that tells the caller what to do instead,
	// and that is what is asserted.
	empty := f.do(t, "PATCH", f.grantsPath(subject)+"/"+f.projectA, f.adminToken, `{"role_keys":[]}`)
	if empty.Code != http.StatusBadRequest {
		t.Errorf("an empty grant gave %d, want 400", empty.Code)
	}
	if !strings.Contains(empty.Body.String(), "delete the grant") {
		t.Errorf("the refusal does not say what to do instead: %s", empty.Body)
	}

	revoked := f.do(t, "DELETE", f.grantsPath(subject)+"/"+f.projectA, f.adminToken, "")
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", revoked.Code, revoked.Body)
	}

	// Immediate: the row is gone, not flagged. Nothing can read it afterwards.
	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM user_grants WHERE user_id = $1`, subject)
	if count != 0 {
		t.Errorf("%d grant row(s) survive a revocation", count)
	}
}

// --- audit ------------------------------------------------------------------

func TestGrantChangesAreAudited(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)
	f.mustCreateRole(t, f.projectA, "reader")

	f.do(t, "POST", f.grantsPath(subject), f.adminToken, fmt.Sprintf(
		`{"project_id":%q,"role_keys":["admin"]}`, f.projectA))
	f.do(t, "PATCH", f.grantsPath(subject)+"/"+f.projectA, f.adminToken, `{"role_keys":["reader"]}`)
	f.do(t, "DELETE", f.grantsPath(subject)+"/"+f.projectA, f.adminToken, "")

	var assigned, revoked int
	f.factory.QueryRow(&assigned,
		`SELECT count(*) FROM events WHERE event_type = 'role.assigned' AND actor_user_id = $1`, f.adminUser)
	f.factory.QueryRow(&revoked,
		`SELECT count(*) FROM events WHERE event_type = 'role.revoked' AND actor_user_id = $1`, f.adminUser)
	if assigned != 2 {
		t.Errorf("role.assigned written %d times, want 2 (grant and replace)", assigned)
	}
	if revoked != 1 {
		t.Errorf("role.revoked written %d times, want 1", revoked)
	}

	// The subject, the project and the exact keys — the card's step 7. An
	// audit row that says "somebody changed a grant" answers nothing.
	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE event_type = 'role.revoked' LIMIT 1`)
	for _, want := range []string{subject, f.projectA, "reader"} {
		if !strings.Contains(payload, want) {
			t.Errorf("the revocation event does not record %q: %s", want, payload)
		}
	}
}

// --- fixture ----------------------------------------------------------------

type apiGrant struct {
	UserID    string   `json:"user_id"`
	ProjectID string   `json:"project_id"`
	RoleKeys  []string `json:"role_keys"`
}

type fixture struct {
	db      *postgres.DB
	store   *Store
	factory *testsupport.Factory
	handler http.Handler

	orgA, orgB          string
	projectA, projectA2 string
	projectB            string

	adminUser  string
	adminToken string
}

func (f *fixture) grantsPath(userID string) string {
	return fmt.Sprintf("/v1/organizations/%s/users/%s/grants", f.orgA, userID)
}

func (f *fixture) in(t *testing.T, fn func(tx *postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), f.orgA, fn)
}

func (f *fixture) mustCreateRole(t *testing.T, projectID, key string) {
	t.Helper()
	f.factory.Exec(`
		INSERT INTO roles (org_id, project_id, key, display_name)
		VALUES ((SELECT org_id FROM projects WHERE id = $1), $1, $2, $3)`,
		projectID, key, key)
}

func (f *fixture) do(t *testing.T, method, path, tok, body string) *httptest.ResponseRecorder {
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

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(into); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	project := func(org, name string) string {
		var id string
		factory.QueryRow(&id, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, name)
		return id
	}
	projectA := project(orgA, "alpha")
	projectA2 := project(orgA, "alpha-two")
	projectB := project(orgB, "beta")

	// `admin` exists in projectA — the role most of these tests grant.
	factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'admin', 'Admin')`,
		orgA, projectA)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keys := signingKeys(t, stack)
	signer := signing.NewSigner(keys)

	var consoleProject, clientID string
	factory.QueryRow(&consoleProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgA)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, consoleProject, orgA)

	adminUser := factory.User(orgA)
	factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`,
		adminUser, orgA)

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
				WithQuota(ratelimit.Quota{Limit: 300, Window: time.Minute}),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
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
		ProjectAPI:     &project2.Handler{Store: project2.NewStore(), DB: db, Audit: auditor, Log: discard()},
		ApplicationAPI: application.New(db, auditor, discard()),
		RoleAPI:        role.New(db, auditor, discard()),
		GrantAPI:       New(db, auditor, discard()),
		AuthzAPI:       &authz.Handler{DB: db, Log: discard()},
		UserAPI:        &user.Handler{Store: user.NewStore(), DB: db, Audit: auditor, Log: discard()},
		AuditAPI:       &auditlog.Handler{DB: db, Log: discard()},
	})

	mint := func(userID string) string {
		claims, err := token.AccessTokenClaims(token.Subject{
			Issuer: issuer, Audience: issuer, ClientID: clientID,
			OrgID: orgA, UserID: userID, Scope: []string{"openid"},
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
		return signed
	}

	return &fixture{
		db: db, store: NewStore(), factory: factory, handler: srv.Handler(),
		orgA: orgA, orgB: orgB,
		projectA: projectA, projectA2: projectA2, projectB: projectB,
		adminUser: adminUser, adminToken: mint(adminUser),
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

//go:build integration

// The project endpoints end to end (P1-17).
//
// The card asks for cross-organization access to be impossible "verified at
// both the RLS and application layers", and those really are two separate
// claims. The application layer is P1-15's Authorize, which refuses a caller
// with no role over the organization in the path. RLS is the database, which
// refuses the ROW even when the application layer has been satisfied.
//
// Testing only the first would pass against a service with RLS switched off.
// Testing only the second would pass against a service that lets anybody name
// any organization. So both are here, and each is arranged so the other cannot
// be what makes it pass.
package project

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
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/role"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/internal/user"
)

const issuer = "https://auth.example.test"

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db      *postgres.DB
	store   *Store
	factory *testsupport.Factory
	handler http.Handler
	signer  *signing.Signer

	instance   string
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
	userID := factory.User(orgA)

	var consoleProject, clientID string
	factory.QueryRow(&consoleProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgA)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, consoleProject, orgA)

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
		ProjectAPI: &Handler{Store: NewStore(), DB: db, Audit: auditor, Log: discard()},
		// P1-18 made this required too. Nothing here calls it; httpserver.New
		// refuses a /v1 chain with any half of the Management API missing,
		// because a nil handler behind a registered route is a panic on the
		// first request rather than a boot failure.
		ApplicationAPI: application.New(db, auditor, discard()),
		RoleAPI:        role.New(db, auditor, discard()),
		// Fourth of four. Nothing here calls it; httpserver.New refuses a /v1
		// chain with any half of the Management API missing, and the handler
		// refuses a deactivation it cannot make real rather than panicking on
		// a nil revoker.
		UserAPI:  &user.Handler{Store: user.NewStore(), DB: db, Audit: auditor, Log: discard()},
		AuditAPI: &auditlog.Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, store: NewStore(), factory: factory, handler: srv.Handler(),
		signer:   signing.NewSigner(keys),
		instance: instance, orgA: orgA, orgB: orgB, userID: userID, clientID: clientID,
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

func (f *fixture) projects(orgID string) string {
	return "/v1/organizations/" + orgID + "/projects"
}

func decodeProject(t *testing.T, w *httptest.ResponseRecorder) api.Project {
	t.Helper()
	var p api.Project
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("not a Project: %s", w.Body.String())
	}
	return p
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

// --- the ordinary path -------------------------------------------------------------------

func TestAnAdminCreatesReadsRenamesAndDeletesAProject(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)
	f.grant(management.OrgOwner, f.orgA) // delete needs it

	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Billing"}`), http.StatusCreated))
	if created.Name != "Billing" {
		t.Errorf("name = %q", created.Name)
	}

	one := f.projects(f.orgA) + "/" + created.Id.String()

	read := decodeProject(t, mustStatus(t, f.call(t, http.MethodGet, one, ""), http.StatusOK))
	if read.Id != created.Id {
		t.Errorf("read %s, created %s", read.Id, created.Id)
	}

	renamed := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPatch, one, `{"name":"Invoicing"}`), http.StatusOK))
	if renamed.Name != "Invoicing" {
		t.Errorf("renamed to %q", renamed.Name)
	}

	mustStatus(t, f.call(t, http.MethodDelete, one, ""), http.StatusNoContent)
	mustStatus(t, f.call(t, http.MethodGet, one, ""), http.StatusNotFound)
}

func mustStatus(t *testing.T, w *httptest.ResponseRecorder, want int) *httptest.ResponseRecorder {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
	}
	return w
}

// --- cross-organization: the APPLICATION layer ---------------------------------------------

// A caller with no role over the organization in the path is refused before any
// query runs.
//
// Arranged so RLS cannot be what makes it pass: the caller holds ORG_ADMIN over
// their OWN organization, and the project they are reaching for really exists.
// If the application layer were absent, the request would reach a transaction
// scoped to orgB and succeed.
func TestACallerWithNoRoleOverTheTargetIsRefusedBeforeAnyQuery(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	var theirs string
	f.factory.QueryRow(&theirs,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'Theirs') RETURNING id`, f.orgB)

	for _, target := range []string{
		f.projects(f.orgB),
		f.projects(f.orgB) + "/" + theirs,
	} {
		w := f.call(t, http.MethodGet, target, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404: %s", target, w.Code, w.Body.String())
		}
		if code, _ := envelope(t, w); code != "NOT_FOUND" {
			t.Errorf("code = %q", code)
		}
	}
}

// --- cross-organization: the RLS layer -----------------------------------------------------

// **The application layer is satisfied and the row is still unreachable.**
//
// The caller holds ORG_ADMIN over their own organization and asks for a project
// id that belongs to another one, through their OWN organization's path. So
// Authorize allows it — the organization in the path is theirs — and the only
// thing standing between the request and another tenant's row is RLS.
//
// This is the test that fails if the policy is dropped, and it is the reason
// the store's queries carry no org_id predicate: a predicate would make this
// pass for the wrong reason.
func TestAnotherOrganizationsProjectIsUnreachableEvenWithTheRightRole(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	var theirs string
	f.factory.QueryRow(&theirs,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'Theirs') RETURNING id`, f.orgB)

	// Their id, our organization's path.
	w := f.call(t, http.MethodGet, f.projects(f.orgA)+"/"+theirs, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — another tenant's project was readable: %s",
			w.Code, w.Body.String())
	}

	// The control: the same request for a project that IS ours succeeds, so
	// the 404 above is about the tenant rather than about the route.
	ours := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Ours"}`), http.StatusCreated))
	mustStatus(t, f.call(t, http.MethodGet, f.projects(f.orgA)+"/"+ours.Id.String(), ""), http.StatusOK)
}

// Directly at the store, with a correctly scoped transaction: RLS on its own,
// with no HTTP layer involved at all.
func TestTheStoreCannotReadAcrossTenants(t *testing.T) {
	f := setup(t)

	var theirs string
	f.factory.QueryRow(&theirs,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'Theirs') RETURNING id`, f.orgB)

	err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := f.store.Get(context.Background(), tx, theirs)
		return err
	})
	if err == nil {
		t.Fatal("a project in another organization was readable")
	}

	// The control: the same call from the owning tenant works.
	if err := f.db.WithTenant(context.Background(), f.orgB, func(tx *postgres.Tx) error {
		_, err := f.store.Get(context.Background(), tx, theirs)
		return err
	}); err != nil {
		t.Fatalf("the owning tenant could not read its own project: %v", err)
	}
}

// A list never contains another organization's projects, and the query that
// produces it carries no org_id predicate.
func TestAListNeverCrossesATenantBoundary(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for i := range 3 {
		f.factory.Exec(`INSERT INTO projects (org_id, name) VALUES ($1, $2)`,
			f.orgB, fmt.Sprintf("theirs-%d", i))
	}
	f.factory.Exec(`INSERT INTO projects (org_id, name) VALUES ($1, 'ours')`, f.orgA)

	w := mustStatus(t, f.call(t, http.MethodGet, f.projects(f.orgA), ""), http.StatusOK)

	var list api.ProjectList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("not a list: %s", w.Body.String())
	}
	// 'console' from the fixture plus 'ours'.
	if len(list.Projects) != 2 {
		t.Fatalf("%d projects, want 2 — the list crossed a tenant boundary", len(list.Projects))
	}
	for _, p := range list.Projects {
		if strings.HasPrefix(p.Name, "theirs") {
			t.Errorf("another tenant's project %q was listed", p.Name)
		}
	}
}

// --- mass assignment ------------------------------------------------------------------------

// **An org_id in the body has nowhere to land** (card step 2). The generated
// request type carries no such field, and the store reads the organization from
// the transaction's own scope rather than from anything the caller sent.
func TestAnOrgIdInTheBodyIsIgnored(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	body := fmt.Sprintf(`{"name":"Smuggled","org_id":%q,"id":"00000000-0000-0000-0000-0000000000ff"}`, f.orgB)
	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), body), http.StatusCreated))

	if created.Id.String() == "00000000-0000-0000-0000-0000000000ff" {
		t.Error("the body set the id")
	}

	var owner string
	f.factory.QueryRow(&owner, `SELECT org_id FROM projects WHERE id = $1`, created.Id.String())
	if owner != f.orgA {
		t.Errorf("the project landed in %s, want the path's organization %s", owner, f.orgA)
	}
}

// --- deletion -------------------------------------------------------------------------------

// **The card's second DoD item.** A project with dependents refuses, and the
// refusal lists what blocks it — "this project still has things in it" is not
// actionable; "4 applications and 2 roles" tells an administrator where to look.
func TestDeletingAProjectWithDependentsFailsAndSaysWhat(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Busy"}`), http.StatusCreated))
	id := created.Id.String()

	for i := range 2 {
		f.factory.Exec(
			`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, $3, 'web')`,
			id, f.orgA, fmt.Sprintf("app-%d", i))
	}
	f.factory.Exec(
		`INSERT INTO roles (project_id, org_id, key, display_name) VALUES ($1, $2, 'admin', 'Admin')`,
		id, f.orgA)

	w := f.call(t, http.MethodDelete, f.projects(f.orgA)+"/"+id, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}

	code, details := envelope(t, w)
	if code != "CONFLICT" {
		t.Errorf("code = %q", code)
	}

	found := map[string]string{}
	for _, d := range details {
		found[d.Field] = d.Issue
	}
	if !strings.Contains(found["applications"], "2") {
		t.Errorf("applications detail = %q, want the count", found["applications"])
	}
	if !strings.Contains(found["roles"], "1") {
		t.Errorf("roles detail = %q, want the count", found["roles"])
	}

	// And nothing was deleted — not the project, and not what was in it.
	var apps int
	f.factory.QueryRow(&apps, `SELECT count(*) FROM applications WHERE project_id = $1`, id)
	if apps != 2 {
		t.Errorf("%d applications survived, want 2", apps)
	}
	var exists int
	f.factory.QueryRow(&exists, `SELECT count(*) FROM projects WHERE id = $1`, id)
	if exists != 1 {
		t.Error("the project was deleted despite its dependents")
	}
}

// Removing the dependents makes the delete work, so the refusal is about them
// rather than about deletion being broken.
func TestOnceEmptiedAProjectDeletes(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Busy"}`), http.StatusCreated))
	id := created.Id.String()

	f.factory.Exec(
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'app', 'web')`,
		id, f.orgA)

	mustStatus(t, f.call(t, http.MethodDelete, f.projects(f.orgA)+"/"+id, ""), http.StatusConflict)

	f.factory.Exec(`DELETE FROM applications WHERE project_id = $1`, id)

	mustStatus(t, f.call(t, http.MethodDelete, f.projects(f.orgA)+"/"+id, ""), http.StatusNoContent)
}

// Deleting needs ORG_OWNER. An ORG_ADMIN can do everything else here and not
// this — and gets 403 rather than 404, because they can plainly see the project.
func TestAnOrgAdminCannotDeleteAProject(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Mine"}`), http.StatusCreated))

	w := f.call(t, http.MethodDelete, f.projects(f.orgA)+"/"+created.Id.String(), "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	if code, _ := envelope(t, w); code != "PERMISSION_DENIED" {
		t.Errorf("code = %q", code)
	}
}

// --- naming ------------------------------------------------------------------------------------

func TestADuplicateNameIsAConflict(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	mustStatus(t, f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Billing"}`), http.StatusCreated)

	// Case-insensitively: two projects called "Billing" and "billing" in one
	// tenant is a support ticket waiting to be filed.
	w := f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"billing"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
}

// The same name in a DIFFERENT organization is fine. Uniqueness is per tenant,
// and making it global would leak which names other customers use.
func TestTheSameNameInAnotherOrganizationIsFine(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)
	f.grant(management.OrgAdmin, f.orgB)

	mustStatus(t, f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Billing"}`), http.StatusCreated)
	mustStatus(t, f.call(t, http.MethodPost, f.projects(f.orgB), `{"name":"Billing"}`), http.StatusCreated)
}

func TestABlankNameIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for _, body := range []string{`{"name":""}`, `{"name":"   "}`} {
		w := f.call(t, http.MethodPost, f.projects(f.orgA), body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", body, w.Code, w.Body.String())
		}
	}
}

// --- PROJECT_OWNER is reserved and grants nothing ------------------------------------------------

// **Card step 4.** The role exists in the schema and in the permission model,
// and Phase 1 grants it to nobody. A caller who somehow holds one must get
// nothing from it — not an error that reveals it is unimplemented, and
// certainly not the access an ORG_ADMIN would have.
func TestHoldingProjectOwnerGrantsNothingYet(t *testing.T) {
	f := setup(t)
	f.grant(management.ProjectOwner, f.orgA)

	w := f.call(t, http.MethodGet, f.projects(f.orgA), "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — PROJECT_OWNER granted access it should not: %s",
			w.Code, w.Body.String())
	}

	// The control: the same caller with ORG_ADMIN gets through, so the refusal
	// is about the role rather than about the fixture.
	f.grant(management.OrgAdmin, f.orgA)
	mustStatus(t, f.call(t, http.MethodGet, f.projects(f.orgA), ""), http.StatusOK)
}

// --- audit ---------------------------------------------------------------------------------------

// **The card's third DoD item.**
func TestProjectLifecycleEventsAreAudited(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Audited"}`), http.StatusCreated))
	one := f.projects(f.orgA) + "/" + created.Id.String()

	mustStatus(t, f.call(t, http.MethodPatch, one, `{"name":"Renamed"}`), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodDelete, one, ""), http.StatusNoContent)

	for _, kind := range []audit.EventType{
		audit.EventProjectCreated, audit.EventProjectUpdated, audit.EventProjectDeleted,
	} {
		var count int
		f.factory.QueryRow(&count,
			`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
			f.orgA, string(kind))
		if count != 1 {
			t.Errorf("%d %s events, want 1", count, kind)
		}
	}

	// The rename event says what it was, or the log cannot answer "what was
	// this project called last week".
	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE org_id = $1 AND event_type = $2`,
		f.orgA, string(audit.EventProjectUpdated))
	if !strings.Contains(payload, "Audited") {
		t.Errorf("the rename event does not record the previous name: %s", payload)
	}
}

// A rename to the same name writes no event. An audit log full of "renamed
// Billing to Billing" is an audit log nobody reads.
func TestARenameToTheSameNameIsNotAudited(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created := decodeProject(t, mustStatus(t,
		f.call(t, http.MethodPost, f.projects(f.orgA), `{"name":"Same"}`), http.StatusCreated))

	mustStatus(t, f.call(t,
		http.MethodPatch, f.projects(f.orgA)+"/"+created.Id.String(), `{"name":"Same"}`), http.StatusOK)

	var count int
	f.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		f.orgA, string(audit.EventProjectUpdated))
	if count != 0 {
		t.Errorf("%d update events for a rename that changed nothing", count)
	}
}

// --- pagination ------------------------------------------------------------------------------------

func TestWalkingTheProjectListSeesEachProjectOnce(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for i := range 9 {
		f.factory.Exec(`INSERT INTO projects (org_id, name) VALUES ($1, $2)`,
			f.orgA, fmt.Sprintf("project-%02d", i))
	}

	seen := map[string]int{}
	target := f.projects(f.orgA) + "?page_size=4"

	for range 10 {
		w := mustStatus(t, f.call(t, http.MethodGet, target, ""), http.StatusOK)

		var list api.ProjectList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("not a list: %s", w.Body.String())
		}
		for _, p := range list.Projects {
			seen[p.Id.String()]++
		}
		if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
			break
		}
		target = f.projects(f.orgA) + "?page_size=4&page_token=" +
			url.QueryEscape(*list.PageInfo.NextPageToken)
	}

	// Nine plus the fixture's 'console'.
	if len(seen) != 10 {
		t.Errorf("saw %d distinct projects, want 10", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times", id, n)
		}
	}
}

//go:build integration

// Project Grants through the real /v1 chain, against real Postgres (P4-01).
//
// Three organizations, because a delegation involves two and the property that
// nobody else sees it needs a third (threat review T4-3): A grants, B receives,
// C must see nothing.
package projectgrant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

	"github.com/zed378/zed-auth/backend/internal/account"
	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/application"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/mfaapi"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/role"
	"github.com/zed378/zed-auth/backend/internal/sessionapi"
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
	factory *testsupport.Factory
	handler http.Handler

	orgA, orgB, orgC     string
	projectA, otherProjA string
	projectB             string
	adminA, ownerOfOther string
	tokens               map[string]string

	// mint signs an access token for an existing user and records it under a
	// name, for callers whose roles are created mid-test (P4-03).
	mint func(name, org, userID string)
}

func setup(t *testing.T) *fixture {
	t.Helper()
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)
	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	f := &fixture{factory: factory, tokens: map[string]string{}}
	f.orgA = factory.Organization(instance, "Acme Vendor")
	f.orgB = factory.Organization(instance, "Bravo Client")
	f.orgC = factory.Organization(instance, "Charlie Bystander")

	newProject := func(org, name string) string {
		var id string
		factory.QueryRow(&id, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, name)
		return id
	}
	f.projectA = newProject(f.orgA, "pos")
	f.otherProjA = newProject(f.orgA, "billing")
	f.projectB = newProject(f.orgB, "own")
	for _, key := range []string{"cashier", "manager"} {
		factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, $3, $3)`, f.orgA, f.projectA, key)
	}
	factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'auditor', 'auditor')`, f.orgA, f.otherProjA)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f.db = db

	keys := signingKeys(t, stack)
	signer := signing.NewSigner(keys)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	_ = rdb.FlushAll(context.Background()).Err()

	auditor := audit.NewWriter(db, discard(), nil)
	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).WithQuota(ratelimit.Quota{Limit: 1000, Window: time.Minute}, ""),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}
	srv := httpserver.New(config.HTTPConfig{
		Addr: "127.0.0.1:0", ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second,
	}, httpserver.Deps{
		Logger: discard(), Health: &httpserver.Health{}, V1: chain,
		Organizations:   &organization.Handler{Store: organization.NewStore(), DB: db, Audit: auditor, Log: discard()},
		ProjectAPI:      &project.Handler{Store: project.NewStore(), DB: db, Audit: auditor, Log: discard()},
		ApplicationAPI:  application.New(db, auditor, discard()),
		RoleAPI:         role.New(db, auditor, discard()),
		GrantAPI:        grant.New(db, auditor, discard()),
		AuthzAPI:        stubAuthz{},
		UserAPI:         &user.Handler{Store: user.NewStore(), DB: db, Audit: auditor, Log: discard()},
		SessionAPI:      &sessionapi.Handler{},
		MfaAPI:          &mfaapi.Handler{},
		ProjectGrantAPI: New(db, auditor, discard()),
		AccountAPI:      &account.Handler{},
		AuditAPI:        &auditlog.Handler{DB: db, Log: discard()},
	})
	f.handler = srv.Handler()

	// Callers, each with exactly the grant their name says.
	f.mint = func(name, org, id string) {
		var consoleProject, client string
		factory.QueryRow(&consoleProject, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, "console-"+name)
		factory.QueryRow(&client, `INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'console', 'web') RETURNING id`, consoleProject, org)
		claims, err := token.AccessTokenClaims(token.Subject{
			Issuer: issuer, Audience: issuer, ClientID: client, OrgID: org, UserID: id, Scope: []string{"openid"},
		}, time.Now())
		if err != nil {
			t.Fatalf("claims: %v", err)
		}
		payload, _ := claims.Encode()
		signed, err := signer.SignWithType(payload, signing.TypeAccessToken)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		f.tokens[name] = signed
	}
	caller := func(name, org, role, scope string) string {
		id := factory.User(org, name+"@example.test")
		if role != "" {
			factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, $2, $3)`, id, role, scope)
		}
		f.mint(name, org, id)
		return id
	}
	f.adminA = caller("admin-a", f.orgA, "ORG_ADMIN", f.orgA)
	caller("owner-pos", f.orgA, "PROJECT_OWNER", f.projectA)
	f.ownerOfOther = caller("owner-billing", f.orgA, "PROJECT_OWNER", f.otherProjA)
	caller("member-a", f.orgA, "", "")
	caller("admin-b", f.orgB, "ORG_ADMIN", f.orgB)
	return f
}

func (f *fixture) call(t *testing.T, who, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.tokens[who])
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) grantsPath(org, project string) string {
	return "/v1/organizations/" + org + "/projects/" + project + "/grants"
}

func (f *fixture) create(t *testing.T, who string, keys ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"granted_org_id": f.orgB, "role_keys": keys})
	return f.call(t, who, http.MethodPost, f.grantsPath(f.orgA, f.projectA), string(body))
}

func decode(t *testing.T, w *httptest.ResponseRecorder) api.ProjectGrant {
	t.Helper()
	var g api.ProjectGrant
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
	return g
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	f.factory.QueryRow(&n, query, args...)
	return n
}

// --- the lifecycle -----------------------------------------------------------------------

func TestAProjectOwnerDelegatesAProjectAndItIsAudited(t *testing.T) {
	f := setup(t)

	w := f.create(t, "owner-pos", "manager", "cashier")
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	g := decode(t, w)
	if g.GrantedOrgId.String() != f.orgB || g.GrantingOrgId.String() != f.orgA || g.ProjectId.String() != f.projectA {
		t.Errorf("parties = %+v", g)
	}
	if strings.Join(g.GrantedRoleKeys, ",") != "cashier,manager" || g.Status != api.ProjectGrantStatusActive {
		t.Errorf("keys %v status %s, want the sorted subset, active", g.GrantedRoleKeys, g.Status)
	}
	if g.GrantedOrgName != "Bravo Client" {
		t.Errorf("granted_org_name = %q — a person revoking cannot check a UUID", g.GrantedOrgName)
	}
	if n := f.count(t, `SELECT count(*) FROM events WHERE event_type = 'project_grant.created' AND org_id = $1 AND payload->>'grant_id' = $2`, f.orgA, g.Id.String()); n != 1 {
		t.Errorf("found %d creation events in the granting organization's log, want 1", n)
	}

	list := f.call(t, "owner-pos", http.MethodGet, f.grantsPath(f.orgA, f.projectA), "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), g.Id.String()) {
		t.Errorf("list = %d, missing the grant: %s", list.Code, list.Body.String())
	}
}

func TestRevocationIsAStatusChangeAuditedOnce(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "admin-a", "cashier"))
	path := f.grantsPath(f.orgA, f.projectA) + "/" + g.Id.String()

	for i := 0; i < 2; i++ {
		if w := f.call(t, "admin-a", http.MethodDelete, path, ""); w.Code != http.StatusNoContent {
			t.Fatalf("revoke %d = %d: %s", i+1, w.Code, w.Body.String())
		}
	}
	got := decode(t, f.call(t, "admin-a", http.MethodGet, path, ""))
	if got.Status != api.ProjectGrantStatusRevoked || !got.RevokedAt.IsSpecified() || got.RevokedAt.IsNull() {
		t.Errorf("after revocation: status %s, revoked_at %v — want a kept row marked revoked", got.Status, got.RevokedAt)
	}
	if n := f.count(t, `SELECT count(*) FROM events WHERE event_type = 'project_grant.revoked' AND payload->>'grant_id' = $1`, g.Id.String()); n != 1 {
		t.Errorf("two revocations wrote %d revoke events, want 1 — the second changed nothing", n)
	}

	// Revoked, a new grant to the same organization is allowed again.
	if w := f.create(t, "admin-a", "manager"); w.Code != http.StatusCreated {
		t.Errorf("re-granting after revocation = %d: %s", w.Code, w.Body.String())
	}
}

// --- validation --------------------------------------------------------------------------

func TestAGrantNamesOnlyThisProjectsRoles(t *testing.T) {
	f := setup(t)
	for name, keys := range map[string][]string{
		"a role that does not exist":  {"cashier", "superuser"},
		"a role from another project": {"auditor"},
	} {
		w := f.create(t, "owner-pos", keys...)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: create = %d, want 400: %s", name, w.Code, w.Body.String())
		}
	}
	if n := f.count(t, `SELECT count(*) FROM project_grants`); n != 0 {
		t.Errorf("%d grants were written by refused requests", n)
	}
}

func TestAGrantMustGoToAnotherLiveOrganization(t *testing.T) {
	f := setup(t)
	suspended := f.factory.Organization(f.factory.Instance(), "Suspended")
	f.factory.Exec(`UPDATE organizations SET status = 'suspended' WHERE id = $1`, suspended)

	var messages []string
	for name, org := range map[string]string{
		"itself":          f.orgA,
		"an unknown id":   "00000000-0000-4000-8000-000000000001",
		"a suspended one": suspended,
	} {
		body, _ := json.Marshal(map[string]any{"granted_org_id": org, "role_keys": []string{"cashier"}})
		w := f.call(t, "owner-pos", http.MethodPost, f.grantsPath(f.orgA, f.projectA), string(body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("granting to %s = %d, want 400", name, w.Code)
		}
		messages = append(messages, w.Body.String())
	}
	// A-5: one answer for all three, so the endpoint does not sort ids into
	// "real" and "not".
	if messages[0] != messages[1] || messages[1] != messages[2] {
		t.Errorf("the refusals differ, which says which ids are live organizations:\n%s", strings.Join(messages, "\n"))
	}
}

func TestOneActiveGrantPerReceivingOrganization(t *testing.T) {
	f := setup(t)
	if w := f.create(t, "owner-pos", "cashier"); w.Code != http.StatusCreated {
		t.Fatalf("first grant = %d", w.Code)
	}
	if w := f.create(t, "owner-pos", "cashier", "manager"); w.Code != http.StatusConflict {
		t.Errorf("a second active grant (a way to widen the first) = %d, want 409", w.Code)
	}
}

// --- authorization -----------------------------------------------------------------------

func TestOnlyTheGrantingProjectsOwnerMayActOnItsGrants(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "owner-pos", "cashier"))
	grantPath := f.grantsPath(f.orgA, f.projectA) + "/" + g.Id.String()
	body := `{"granted_org_id":"` + f.orgC + `","role_keys":["cashier"]}`

	for _, c := range []struct {
		who, method, path, body, why string
	}{
		{"owner-billing", http.MethodPost, f.grantsPath(f.orgA, f.projectA), body, "A-1: PROJECT_OWNER of a different project"},
		{"member-a", http.MethodPost, f.grantsPath(f.orgA, f.projectA), body, "no manager role"},
		{"owner-billing", http.MethodDelete, grantPath, "", "revoking another project's grant"},
		// A-4: the receiving organization, which RLS lets SEE the row.
		{"admin-b", http.MethodDelete, grantPath, "", "the receiving organization revoking through the granting route"},
		{"admin-b", http.MethodGet, f.grantsPath(f.orgB, f.projectA) + "/" + g.Id.String(), "", "the receiving organization naming itself in the path"},
		{"admin-b", http.MethodDelete, f.grantsPath(f.orgB, f.projectA) + "/" + g.Id.String(), "", "the receiving organization revoking under its own path"},
		// The case only the store's granting_org_id filter stops: a project the
		// receiving organization DOES own, so the project check passes, and a
		// grant id RLS lets it see. Without the filter this reads — and revokes —
		// another organization's delegation.
		{"admin-b", http.MethodGet, f.grantsPath(f.orgB, f.projectB) + "/" + g.Id.String(), "", "the receiving organization reading the grant under its own project"},
		{"admin-b", http.MethodDelete, f.grantsPath(f.orgB, f.projectB) + "/" + g.Id.String(), "", "the receiving organization revoking the grant under its own project"},
	} {
		w := f.call(t, c.who, c.method, c.path, c.body)
		if w.Code < 400 {
			t.Errorf("%s: answered %d, want a refusal", c.why, w.Code)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM project_grants WHERE status = 'active'`); n != 1 {
		t.Errorf("active grants = %d after refused requests, want the original 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM project_grants WHERE granted_org_id = $1`, f.orgC); n != 0 {
		t.Error("a refused caller created a grant")
	}
}

// --- the data enforces the lifecycle -------------------------------------------------------

// A-3 and A-6, from the OWNER connection: no path widens a grant, changes its
// parties or brings it back, whoever is writing.
func TestAGrantOnlyEverNarrowsEvenForTheOwnerConnection(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "owner-pos", "cashier"))
	id := g.Id.String()

	for name, stmt := range map[string]string{
		"widening the role keys": `UPDATE project_grants SET granted_role_keys = '{cashier,manager}' WHERE id = $1`,
		"re-pointing the grant":  `UPDATE project_grants SET granted_org_id = '` + f.orgC + `' WHERE id = $1`,
	} {
		if err := f.factory.TryExec(stmt, id); err == nil {
			t.Errorf("%s succeeded", name)
		}
	}
	f.factory.Exec(`UPDATE project_grants SET status = 'revoked', revoked_at = now() WHERE id = $1`, id)
	if err := f.factory.TryExec(`UPDATE project_grants SET status = 'active', revoked_at = NULL WHERE id = $1`, id); err == nil {
		t.Error("a revoked grant was reactivated")
	}
}

// T4-3's third organization, and A-4 at the database: the receiving
// organization can see the row and cannot write it; a bystander sees nothing,
// and cannot read the receiving organization's name.
func TestTheReceivingSideReadsAndABystanderSeesNothing(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "owner-pos", "cashier"))
	ctx := context.Background()

	var seenByB, seenByC int
	_ = f.db.WithTenant(ctx, f.orgB, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_grants WHERE id = $1`, g.Id.String()).Scan(&seenByB)
	})
	_ = f.db.WithTenant(ctx, f.orgC, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_grants WHERE id = $1`, g.Id.String()).Scan(&seenByC)
	})
	if seenByB != 1 || seenByC != 0 {
		t.Errorf("visible to receiving org: %d (want 1), to a bystander: %d (want 0)", seenByB, seenByC)
	}

	writeErr := f.db.WithTenant(ctx, f.orgB, func(tx *postgres.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE project_grants SET status = 'revoked', revoked_at = now() WHERE id = $1`, g.Id.String())
		return err
	})
	if writeErr == nil {
		t.Error("the receiving organization wrote to the grant it can see")
	}

	var names int
	_ = f.db.WithTenant(ctx, f.orgC, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM granted_organization_names($1::uuid[])`, "{"+f.orgB+"}").Scan(&names)
	})
	if names != 0 {
		t.Error("an organization that granted nothing read the receiving organization's name")
	}
}

// --- role deletion (F-10, A-7) -----------------------------------------------------------

func TestADelegatedRoleCannotBeDeletedUntilTheGrantIsRevoked(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "owner-pos", "cashier"))

	var roleID string
	f.factory.QueryRow(&roleID, `SELECT id FROM roles WHERE project_id = $1 AND key = 'cashier'`, f.projectA)
	rolePath := "/v1/organizations/" + f.orgA + "/projects/" + f.projectA + "/roles/" + roleID

	if w := f.call(t, "owner-pos", http.MethodDelete, rolePath, ""); w.Code != http.StatusConflict {
		t.Fatalf("deleting a delegated role = %d, want 409: %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "project grant") {
		t.Errorf("the refusal does not say a grant delegates it: %s", w.Body.String())
	}

	if w := f.call(t, "owner-pos", http.MethodDelete, f.grantsPath(f.orgA, f.projectA)+"/"+g.Id.String(), ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoking = %d", w.Code)
	}
	if w := f.call(t, "owner-pos", http.MethodDelete, rolePath, ""); w.Code != http.StatusNoContent {
		t.Errorf("deleting after revocation = %d, want 204 — a revoked grant is history, not a lock: %s", w.Code, w.Body.String())
	}
}

// --- plumbing ----------------------------------------------------------------------------

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
	return signing.NewCache(func() (*signing.KeySet, error) { return store.Load(context.Background()) }, time.Minute)
}

type stubAuthz struct{}

func (stubAuthz) CheckAuthorization(context.Context, api.CheckAuthorizationRequestObject) (api.CheckAuthorizationResponseObject, error) {
	return nil, errors.New("project grant tests do not wire the authorization check")
}

// The revoke dialog's blast radius (P4-05): the granting organization sees how
// many users hold a role through a grant, and nobody else sees even that.
//
// The assignment is inserted from the owner connection because P4-02, which
// creates delegated assignments through the API, is not built. What this pins
// is the function the count comes through, not who may write the row.
func TestTheHolderCountIsTheGrantersAndOnlyTheirs(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "owner-pos", "cashier"))
	if g.HolderCount != 0 {
		t.Fatalf("a new grant reports %d holders", g.HolderCount)
	}

	// A real delegated assignment, by the receiving organization (P4-02).
	holder := f.factory.User(f.orgB, "holder@bravo.test")
	body := `{"user_id":"` + holder + `","role_keys":["cashier"]}`
	if w := f.call(t, "admin-b", http.MethodPost, "/v1/organizations/"+f.orgB+"/project-grants/"+g.Id.String()+"/user-grants", body); w.Code != http.StatusCreated {
		t.Fatalf("assigning through the grant = %d: %s", w.Code, w.Body.String())
	}

	got := decode(t, f.call(t, "owner-pos", http.MethodGet, f.grantsPath(f.orgA, f.projectA)+"/"+g.Id.String(), ""))
	if got.HolderCount != 1 {
		t.Errorf("holder_count = %d, want 1 — the revoke dialog would understate what it removes", got.HolderCount)
	}

	ctx := context.Background()
	rowsFor := func(org string) int {
		var rows int
		if err := f.db.WithTenant(ctx, org, func(tx *postgres.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM project_grant_holder_counts($1::uuid[])`, "{"+g.Id.String()+"}").Scan(&rows)
		}); err != nil {
			t.Fatalf("reading holder counts as %s: %v", org, err)
		}
		return rows
	}
	// The granting organization reads its own count through the same query, so
	// the zero from organization C is the bound and not a broken query.
	if n := rowsFor(f.orgA); n != 1 {
		t.Fatalf("the granting organization read %d rows of its own holder count, want 1", n)
	}
	if rowsFor(f.orgC) != 0 {
		t.Error("an organization that granted nothing read the grant's holder count")
	}
}

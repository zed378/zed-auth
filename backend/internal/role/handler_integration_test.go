//go:build integration

// The role endpoints end to end (P2-02).
//
// `role_integration_test.go` covers the store and the schema. This covers what
// only exists at the HTTP boundary: the project in the path, the permission
// requirement, the audit events, and the error shapes a console has to render.
//
// The authorization tests here are the point of the file. `P2-05` proved the
// hierarchy as a pure function; these prove that the hierarchy is what these
// routes actually consult — a distinction that matters, because a route whose
// policy entry is missing gets the zero Requirement and is unreachable, and a
// route whose policy entry names the wrong scope is reachable by the wrong
// people. Neither shows up in a unit test of `Authorize`.
package role

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// --- the happy path, once, so the rest can be about what goes wrong ---------

func TestARoleCanBeDefinedReadUpdatedAndDeleted(t *testing.T) {
	f := setupAPI(t)

	created := f.do(t, "POST", f.rolesPath(), f.adminToken, `{
		"key": "billing-admin",
		"display_name": "Billing Administrator",
		"permission_keys": ["billing:read", "billing.invoice:write"]
	}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}

	var role apiRole
	decode(t, created, &role)
	if role.Key != "billing-admin" || len(role.PermissionKeys) != 2 {
		t.Fatalf("created %+v", role)
	}
	if role.GrantCount != 0 {
		t.Errorf("a role nothing can reference reports %d grants", role.GrantCount)
	}
	if role.IsBuiltin {
		t.Error("a role created through the API is marked built-in")
	}

	read := f.do(t, "GET", f.rolesPath()+"/"+role.ID, f.adminToken, "")
	if read.Code != http.StatusOK {
		t.Fatalf("read: %d %s", read.Code, read.Body)
	}

	updated := f.do(t, "PATCH", f.rolesPath()+"/"+role.ID, f.adminToken, `{
		"display_name": "Billing Admin",
		"permission_keys": ["billing:read"]
	}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	var after apiRole
	decode(t, updated, &after)
	if len(after.PermissionKeys) != 1 || after.PermissionKeys[0] != "billing:read" {
		t.Errorf("permissions after update: %v", after.PermissionKeys)
	}
	if after.Key != "billing-admin" {
		t.Errorf("the key changed to %q", after.Key)
	}

	deleted := f.do(t, "DELETE", f.rolesPath()+"/"+role.ID, f.adminToken, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body)
	}

	gone := f.do(t, "GET", f.rolesPath()+"/"+role.ID, f.adminToken, "")
	if gone.Code != http.StatusNotFound {
		t.Errorf("after delete, read gave %d", gone.Code)
	}
}

// --- authorization, which is what this file is for -------------------------

// A PROJECT_OWNER reaches their own project's roles and nothing else. The
// second half is the assertion that matters: the first would pass against a
// route with no scope check at all.
func TestAProjectOwnerReachesOnlyTheirOwnProjectsRoles(t *testing.T) {
	f := setupAPI(t)

	mine := f.do(t, "GET", f.rolesPath(), f.projectOwnerToken, "")
	if mine.Code != http.StatusOK {
		t.Fatalf("a PROJECT_OWNER was refused on their own project: %d %s", mine.Code, mine.Body)
	}

	theirs := f.do(t, "GET", f.rolesPathIn(f.otherProject), f.projectOwnerToken, "")
	if theirs.Code == http.StatusOK {
		t.Fatal("a PROJECT_OWNER listed another project's roles")
	}
	// 404, not 403: they hold nothing over that project, so its existence is
	// not theirs to learn.
	if theirs.Code != http.StatusNotFound {
		t.Errorf("reaching another project gave %d, want 404", theirs.Code)
	}
}

// An ORG_ADMIN administers every project in their own organization — `PG-32`,
// and the reason `P1-17`'s endpoints were not wrong.
func TestAnOrgAdminAdministersRolesInEveryProjectOfTheirOrganization(t *testing.T) {
	f := setupAPI(t)

	for _, path := range []string{f.rolesPath(), f.rolesPathIn(f.otherProject)} {
		got := f.do(t, "GET", path, f.adminToken, "")
		if got.Code != http.StatusOK {
			t.Errorf("an ORG_ADMIN was refused at %s: %d %s", path, got.Code, got.Body)
		}
	}
}

// A caller with a valid token and no manager role at all.
func TestAnOrdinaryUserCannotDefineRoles(t *testing.T) {
	f := setupAPI(t)

	got := f.do(t, "POST", f.rolesPath(), f.memberToken, `{"key":"sneaky","display_name":"Sneaky"}`)
	if got.Code == http.StatusCreated {
		t.Fatal("a user with no manager role defined a role")
	}
	if got.Code != http.StatusForbidden && got.Code != http.StatusNotFound {
		t.Errorf("got %d, want 403 or 404", got.Code)
	}

	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM roles WHERE key = 'sneaky'`)
	if count != 0 {
		t.Errorf("%d sneaky role(s) exist", count)
	}
}

// The route is reached with another organization's project id in the path. The
// caller holds ORG_ADMIN — over the wrong organization.
func TestAnAdminCannotReachAnotherOrganizationsProject(t *testing.T) {
	f := setupAPI(t)

	path := fmt.Sprintf("/v1/organizations/%s/projects/%s/roles", f.orgB, f.projectB)
	got := f.do(t, "GET", path, f.adminToken, "")
	if got.Code == http.StatusOK {
		t.Fatal("an ORG_ADMIN of organization A listed organization B's roles")
	}
	if got.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 — a 403 confirms the organization exists", got.Code)
	}
}

// A project id that does not exist in this organization is 404, not an empty
// list.
//
// This is what `requireProject` is for, and the distinction is easy to lose:
// without it, listing the roles of a project that does not exist returns `200`
// with `[]`, which tells a console the project is real and simply has no
// roles. The permission layer does not catch it — an ORG_ADMIN legitimately
// holds the organization, so `Authorize` allows the request and the handler is
// what has to notice the path is wrong.
//
// A mutation test caught the absence of this: removing `requireProject` left
// every other test in this file green.
func TestRolesOfANonexistentProjectAreNotFound(t *testing.T) {
	f := setupAPI(t)

	missing := "00000000-0000-0000-0000-000000000000"
	got := f.do(t, "GET", f.rolesPathIn(missing), f.adminToken, "")
	if got.Code != http.StatusNotFound {
		t.Errorf("listing a nonexistent project's roles gave %d %s, want 404", got.Code, got.Body)
	}

	created := f.do(t, "POST", f.rolesPathIn(missing), f.adminToken,
		`{"key":"ghost","display_name":"Ghost"}`)
	if created.Code != http.StatusNotFound {
		t.Errorf("creating a role in a nonexistent project gave %d %s, want 404", created.Code, created.Body)
	}

	// The same answer for a project in ANOTHER organization, which RLS makes
	// invisible — so this cannot be used to ask which project ids are real.
	other := f.do(t, "GET", fmt.Sprintf("/v1/organizations/%s/projects/%s/roles", f.orgA, f.projectB), f.adminToken, "")
	if other.Code != http.StatusNotFound {
		t.Errorf("another organization's project gave %d, want 404", other.Code)
	}
}

// --- validation surfaces ---------------------------------------------------

// A field-level error names the field, so a form can point at it. And for a
// permission key it names the INDEX, so a form can point at the row.
func TestAMalformedPermissionKeyIsRefusedByIndex(t *testing.T) {
	f := setupAPI(t)

	got := f.do(t, "POST", f.rolesPath(), f.adminToken, `{
		"key": "reader",
		"display_name": "Reader",
		"permission_keys": ["user:read", "not a permission key"]
	}`)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("got %d %s", got.Code, got.Body)
	}
	if !strings.Contains(got.Body.String(), "permission_keys[1]") {
		t.Errorf("the error does not name the offending index: %s", got.Body)
	}
}

func TestAWildcardPermissionIsRefused(t *testing.T) {
	f := setupAPI(t)

	got := f.do(t, "POST", f.rolesPath(), f.adminToken, `{
		"key": "everything", "display_name": "Everything",
		"permission_keys": ["billing:*"]
	}`)
	if got.Code != http.StatusBadRequest {
		t.Errorf("a wildcard permission was accepted: %d %s", got.Code, got.Body)
	}
}

func TestAReservedRoleKeyIsRefused(t *testing.T) {
	f := setupAPI(t)

	got := f.do(t, "POST", f.rolesPath(), f.adminToken, `{"key":"org_admin","display_name":"Org Admin"}`)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("a manager role name was accepted as a project role: %d %s", got.Code, got.Body)
	}
	if !strings.Contains(got.Body.String(), "reserved") {
		t.Errorf("the refusal does not say why: %s", got.Body)
	}
}

func TestADuplicateKeyInOneProjectIsAConflict(t *testing.T) {
	f := setupAPI(t)

	body := `{"key":"admin","display_name":"Admin"}`
	if first := f.do(t, "POST", f.rolesPath(), f.adminToken, body); first.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", first.Code, first.Body)
	}
	second := f.do(t, "POST", f.rolesPath(), f.adminToken, body)
	if second.Code != http.StatusConflict {
		t.Errorf("a duplicate key gave %d, want 409", second.Code)
	}

	// And the same key in a DIFFERENT project is fine, which is the whole
	// point of scoping roles per project.
	other := f.do(t, "POST", f.rolesPathIn(f.otherProject), f.adminToken, body)
	if other.Code != http.StatusCreated {
		t.Errorf("the same key in another project gave %d %s", other.Code, other.Body)
	}
}

// --- the delete refusal, through HTTP --------------------------------------

func TestDeletingAnAssignedRoleIsA409ThatSaysHowMany(t *testing.T) {
	f := setupAPI(t)

	created := f.do(t, "POST", f.rolesPath(), f.adminToken, `{"key":"cashier","display_name":"Cashier"}`)
	var role apiRole
	decode(t, created, &role)

	userID := f.factory.User(f.orgA)
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		userID, f.projectA, f.orgA, pq.Array([]string{"cashier"}))

	got := f.do(t, "DELETE", f.rolesPath()+"/"+role.ID, f.adminToken, "")
	if got.Code != http.StatusConflict {
		t.Fatalf("got %d %s", got.Code, got.Body)
	}
	if !strings.Contains(got.Body.String(), "1 user grant") {
		t.Errorf("the refusal does not say how many: %s", got.Body)
	}

	// And the list says so in advance, which is what lets a console warn
	// before the operator clicks.
	list := f.do(t, "GET", f.rolesPath(), f.adminToken, "")
	var page struct {
		Roles []apiRole `json:"roles"`
	}
	decode(t, list, &page)
	for _, r := range page.Roles {
		if r.Key == "cashier" && r.GrantCount != 1 {
			t.Errorf("grant_count is %d, want 1", r.GrantCount)
		}
	}
}

// --- pagination -------------------------------------------------------------

// Roles are the only list in this API ordered by something other than creation
// time, so the cursor carries a key rather than a timestamp. That is a shape
// nothing else exercises, and a page token that round-trips wrongly shows up as
// a list that repeats or skips rather than as an error.
func TestTheRoleListPagesByKeyWithoutRepeatingOrSkipping(t *testing.T) {
	f := setupAPI(t)

	const total = 7
	for i := 0; i < total; i++ {
		body := fmt.Sprintf(`{"key":"role-%d","display_name":"Role %d"}`, i, i)
		if got := f.do(t, "POST", f.rolesPath(), f.adminToken, body); got.Code != http.StatusCreated {
			t.Fatalf("seeding role %d: %d %s", i, got.Code, got.Body)
		}
	}

	seen := map[string]int{}
	pageToken := ""
	for pages := 0; pages < 10; pages++ {
		path := f.rolesPath() + "?page_size=3"
		if pageToken != "" {
			path += "&page_token=" + pageToken
		}
		got := f.do(t, "GET", path, f.adminToken, "")
		if got.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", pages, got.Code, got.Body)
		}

		var page struct {
			Roles    []apiRole `json:"roles"`
			PageInfo *struct {
				NextPageToken *string `json:"next_page_token"`
			} `json:"page_info"`
		}
		decode(t, got, &page)

		for _, r := range page.Roles {
			seen[r.Key]++
		}
		if page.PageInfo == nil || page.PageInfo.NextPageToken == nil {
			break
		}
		pageToken = *page.PageInfo.NextPageToken
	}

	if len(seen) != total {
		t.Errorf("saw %d distinct roles across the pages, want %d", len(seen), total)
	}
	for key, times := range seen {
		if times != 1 {
			t.Errorf("%s appeared %d times across the pages", key, times)
		}
	}
}

// --- audit -----------------------------------------------------------------

// Every mutation writes an event with the actor, against a real database.
// `P1-14` found a lockout event that was never written because a DoD item had
// been ticked against a fake auditor, so these read the table.
func TestRoleChangesAreAudited(t *testing.T) {
	f := setupAPI(t)

	created := f.do(t, "POST", f.rolesPath(), f.adminToken,
		`{"key":"auditable","display_name":"Auditable","permission_keys":["user:read"]}`)
	var role apiRole
	decode(t, created, &role)

	f.do(t, "PATCH", f.rolesPath()+"/"+role.ID, f.adminToken,
		`{"display_name":"Auditable","permission_keys":["user:read","billing:read"]}`)
	f.do(t, "DELETE", f.rolesPath()+"/"+role.ID, f.adminToken, "")

	for _, want := range []string{"role.created", "role.updated", "role.deleted"} {
		var count int
		f.factory.QueryRow(&count,
			`SELECT count(*) FROM events WHERE event_type = $1 AND actor_user_id = $2`,
			want, f.adminUser)
		if count != 1 {
			t.Errorf("%s was written %d times with the right actor", want, count)
		}
	}

	// The update event records the DIFFERENCE, not a snapshot: "who gave this
	// role billing access" should be one row to read.
	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE event_type = 'role.updated' LIMIT 1`)
	if !strings.Contains(payload, "billing:read") || !strings.Contains(payload, "permissions_added") {
		t.Errorf("the update event does not record what changed: %s", payload)
	}

	// And the delete event keeps what the role could do. After the row is
	// gone, this is the only record of it.
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE event_type = 'role.deleted' LIMIT 1`)
	if !strings.Contains(payload, "user:read") {
		t.Errorf("the delete event does not record the permissions it carried: %s", payload)
	}
}

// --- fixture ---------------------------------------------------------------

type apiRole struct {
	ID             string   `json:"id"`
	ProjectID      string   `json:"project_id"`
	Key            string   `json:"key"`
	DisplayName    string   `json:"display_name"`
	PermissionKeys []string `json:"permission_keys"`
	IsBuiltin      bool     `json:"is_builtin"`
	GrantCount     int      `json:"grant_count"`
}

type apiFixture struct {
	*fixture
	handler http.Handler

	adminUser string

	adminToken        string
	projectOwnerToken string
	memberToken       string

	otherProject string
	projectB     string
}

func (f *apiFixture) rolesPath() string { return f.rolesPathIn(f.projectA) }

func (f *apiFixture) rolesPathIn(projectID string) string {
	return fmt.Sprintf("/v1/organizations/%s/projects/%s/roles", f.orgA, projectID)
}

func (f *apiFixture) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
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

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return string(out)
}

var _ = context.Background
var _ = management.PageSize
var _ = testsupport.Start

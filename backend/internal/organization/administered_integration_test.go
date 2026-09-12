//go:build integration

// `GET /v1/me/organizations` (P2-13).
//
// Through the real generated router, the real /v1 chain, real `manager_roles`
// and a real signing key — so what is asserted here is what a caller meets.
//
// The question every test answers is the same one: **does the switcher's list
// come from server-side truth, and can it show an organization the caller
// cannot act in?** The second half is what makes it a security test rather
// than a rendering test.
package organization

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
)

// decodeAdministered insists on a 200 before it reads the body.
//
// Four tests here passed while the endpoint answered 500, because a failure
// renders as an empty list — which is indistinguishable from "this caller
// administers nothing", the very thing several of them assert. Checking the
// status is what makes the negative assertions mean anything.
func decodeAdministered(t *testing.T, w *httptest.ResponseRecorder) api.AdministeredOrganizationList {
	t.Helper()

	if w.Code != http.StatusOK {
		t.Fatalf("the switcher list answered %d, so anything read from it proves nothing:\n%s",
			w.Code, w.Body.String())
	}

	var out api.AdministeredOrganizationList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not an AdministeredOrganizationList: %s", w.Body.String())
	}
	return out
}

func names(list api.AdministeredOrganizationList) []string {
	out := make([]string, 0, len(list.Organizations))
	for _, o := range list.Organizations {
		out = append(out, o.Name)
	}
	return out
}

// A caller who administers nothing gets an empty list, not a refusal.
//
// This is the normal state for an ordinary user, and it is why the route needs
// no permission: an endpoint that required a role in order to report which
// roles you hold is one nobody can bootstrap from.
func TestACallerWhoAdministersNothingGetsAnEmptyList(t *testing.T) {
	e := setupEndpoints(t)

	w := e.call(t, http.MethodGet, "/v1/me/organizations", "")

	if w.Code != http.StatusOK {
		t.Fatalf("a caller with no manager roles got %d:\n%s", w.Code, w.Body.String())
	}
	if got := decodeAdministered(t, w); len(got.Organizations) != 0 {
		t.Errorf("a caller who administers nothing was offered %v", names(got))
	}
}

// The list names the organizations the caller holds a role over, and no others.
//
// **The half that matters is the second one.** A list that included every
// organization would satisfy any test that only checked the expected one is
// present — and it would be a switcher offering a tenant the caller cannot act
// in, which is a cross-tenant disclosure of the tenant's existence and name.
func TestTheListIsExactlyWhatTheCallerAdministers(t *testing.T) {
	e := setupEndpoints(t)

	mine := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'Mine' WHERE id = $1`, mine)

	other := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'Somebody Else' WHERE id = $1`, other)

	e.grant(management.OrgAdmin, mine)

	got := decodeAdministered(t, e.call(t, http.MethodGet, "/v1/me/organizations", ""))

	var sawMine, sawOther bool
	for _, o := range got.Organizations {
		switch o.Name {
		case "Mine":
			sawMine = true
		case "Somebody Else":
			sawOther = true
		}
	}
	if !sawMine {
		t.Errorf("an ORG_ADMIN was not offered the organization they administer: %v", names(got))
	}
	if sawOther {
		t.Errorf("the caller was offered an organization they hold no role over: %v", names(got))
	}
}

// Two organizations, two grants, both offered.
//
// The case the switcher exists for. A single-organization test cannot tell a
// working query from one that returns the caller's home organization and stops.
func TestAnAdministratorOfTwoOrganizationsIsOfferedBoth(t *testing.T) {
	e := setupEndpoints(t)

	first := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'First' WHERE id = $1`, first)
	second := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'Second' WHERE id = $1`, second)

	e.grant(management.OrgAdmin, first)
	e.grant(management.OrgOwner, second)

	got := decodeAdministered(t, e.call(t, http.MethodGet, "/v1/me/organizations", ""))

	found := map[string][]api.AdministeredOrganizationRoles{}
	for _, o := range got.Organizations {
		found[o.Name] = o.Roles
	}

	if _, ok := found["First"]; !ok {
		t.Errorf("the first organization is missing: %v", names(got))
	}
	if _, ok := found["Second"]; !ok {
		t.Errorf("the second organization is missing: %v", names(got))
	}
	// And the role travels with it, so the console can say what the caller is
	// there — not merely that they may switch.
	if roles := found["Second"]; len(roles) != 1 || roles[0] != api.ORGOWNER {
		t.Errorf("the ORG_OWNER grant came back as %v", roles)
	}
}

// An INSTANCE_OWNER administers every organization, and the list says so.
//
// Its scope is the instance, so it has no `manager_roles` row naming any
// organization — a join would drop it entirely, which is the bug this asserts
// against.
func TestAnInstanceOwnerIsOfferedEveryOrganization(t *testing.T) {
	e := setupEndpoints(t)

	one := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'One' WHERE id = $1`, one)
	two := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'Two' WHERE id = $1`, two)

	e.grant(management.InstanceOwner, e.instance)

	got := decodeAdministered(t, e.call(t, http.MethodGet, "/v1/me/organizations", ""))

	seen := map[string]bool{}
	for _, o := range got.Organizations {
		seen[o.Name] = true
		// INSTANCE_OWNER against every one of them.
		var carries bool
		for _, r := range o.Roles {
			if r == api.INSTANCEOWNER {
				carries = true
			}
		}
		if !carries {
			t.Errorf("%q came back without INSTANCE_OWNER: %v", o.Name, o.Roles)
		}
	}
	if !seen["One"] || !seen["Two"] {
		t.Errorf("an INSTANCE_OWNER was not offered every organization: %v", names(got))
	}
}

// Roles come back strongest first, which is the hierarchy and not the alphabet.
//
// "ORG_ADMIN, ORG_OWNER" sorts alphabetically and reads as though the first is
// the stronger — so a console showing `roles[0]` would label an owner an admin.
func TestRolesComeBackStrongestFirst(t *testing.T) {
	e := setupEndpoints(t)

	org := e.factory.Organization(e.instance)
	e.grant(management.OrgAdmin, org)
	e.grant(management.OrgOwner, org)

	got := decodeAdministered(t, e.call(t, http.MethodGet, "/v1/me/organizations", ""))

	for _, o := range got.Organizations {
		if o.Id.String() != org {
			continue
		}
		if len(o.Roles) != 2 {
			t.Fatalf("expected both roles, got %v", o.Roles)
		}
		if o.Roles[0] != api.ORGOWNER {
			t.Errorf("roles are not strongest-first: %v", o.Roles)
		}
		return
	}
	t.Fatalf("the organization is missing from the list: %v", names(got))
}

// A deleted organization is not offered.
//
// It is soft-deleted (`P1-16`), so a query that forgot `deleted_at IS NULL`
// would still find it — and offering a switch into a deleted tenant produces
// screens that all fail for reasons nobody can see.
func TestADeletedOrganizationIsNotOffered(t *testing.T) {
	e := setupEndpoints(t)

	org := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'Gone' WHERE id = $1`, org)
	e.grant(management.OrgAdmin, org)

	e.factory.Exec(`UPDATE organizations SET deleted_at = now() WHERE id = $1`, org)

	got := decodeAdministered(t, e.call(t, http.MethodGet, "/v1/me/organizations", ""))
	for _, o := range got.Organizations {
		if o.Name == "Gone" {
			t.Error("a deleted organization is still offered by the switcher")
		}
	}
}

// A PROJECT_OWNER's organization is not offered.
//
// Deliberate, and documented in the contract: their `scope_id` is a project,
// and `management.Policy` requires ORG_ADMIN for the organization endpoints —
// so switching there would land on screens that all answer 403. A switcher
// whose entries lead to refusals is worse than one that omits them.
func TestAProjectOwnerIsNotOfferedTheContainingOrganization(t *testing.T) {
	e := setupEndpoints(t)

	org := e.factory.Organization(e.instance)
	e.factory.Exec(`UPDATE organizations SET name = 'Contains A Project' WHERE id = $1`, org)

	var projectID string
	e.factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'theirs') RETURNING id`, org)
	e.grant(management.ProjectOwner, projectID)

	got := decodeAdministered(t, e.call(t, http.MethodGet, "/v1/me/organizations", ""))
	for _, o := range got.Organizations {
		if o.Name == "Contains A Project" {
			t.Error("a PROJECT_OWNER was offered a switch into an organization whose endpoints refuse them")
		}
	}
}

// No token, no list. The route needs no ROLE; it still needs authentication.
func TestTheSwitcherListStillRequiresAToken(t *testing.T) {
	e := setupEndpoints(t)

	r := httptest.NewRequest(http.MethodGet, "/v1/me/organizations", nil)
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated request got %d, not 401:\n%s", w.Code, w.Body.String())
	}
}

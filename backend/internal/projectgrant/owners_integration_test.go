//go:build integration

// PROJECT_GRANT_OWNER through the real /v1 chain (P4-03).
//
// The narrowest role in the hierarchy: the delegated roles of one grant, for
// the users of the organization it was granted to, while that grant is active.
// These tests are mostly about what it CANNOT reach.
package projectgrant

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (f *fixture) ownersPath(org, grantID string) string {
	return "/v1/organizations/" + org + "/project-grants/" + grantID + "/owners"
}

// appointLead makes a B user the owner of grant g through the API and mints
// their token as "lead".
func (f *fixture) appointLead(t *testing.T, g string) string {
	t.Helper()
	lead := f.factory.User(f.orgB, "lead@bravo.test")
	body, _ := json.Marshal(map[string]string{"user_id": lead})
	if w := f.call(t, "admin-b", http.MethodPost, f.ownersPath(f.orgB, g), string(body)); w.Code != http.StatusCreated {
		t.Fatalf("appointing the owner = %d: %s", w.Code, w.Body.String())
	}
	f.mint("lead", f.orgB, lead)
	return lead
}

func TestAGrantOwnerIsAppointedByTheReceivingOrganizationAndAssignsWithinTheGrant(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	lead := f.appointLead(t, g)
	staff := f.factory.User(f.orgB, "staff@bravo.test")

	if n := f.count(t, `
		SELECT count(*) FROM events
		 WHERE event_type = 'manager_role.assigned' AND org_id = $1::uuid
		   AND payload->>'role' = 'PROJECT_GRANT_OWNER' AND payload->>'grant_id' = $2
		   AND payload->>'subject_user_id' = $3 AND payload->>'granting_org_id' = $4`,
		f.orgB, g, lead, f.orgA); n != 1 {
		t.Errorf("found %d appointment events in the receiving organization's log, want 1", n)
	}
	list := f.call(t, "admin-b", http.MethodGet, f.ownersPath(f.orgB, g), "")
	if list.Code != http.StatusOK || !json.Valid(list.Body.Bytes()) || !strings.Contains(list.Body.String(), lead) {
		t.Errorf("owners list = %d: %s", list.Code, list.Body.String())
	}

	// The whole capability, and nothing more, as the grant owner.
	path := f.delegatedPath(f.orgB, g)
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, path, `{"user_id":"` + staff + `","role_keys":["cashier"]}`, http.StatusCreated},
		{http.MethodGet, path, "", http.StatusOK},
		{http.MethodPatch, path + "/" + staff, `{"role_keys":["cashier"]}`, http.StatusOK},
		{http.MethodDelete, path + "/" + staff, "", http.StatusNoContent},
	} {
		if w := f.call(t, "lead", c.method, c.path, c.body); w.Code != c.want {
			t.Errorf("grant owner %s %s = %d, want %d: %s", c.method, c.path, w.Code, c.want, w.Body.String())
		}
	}
	// The subset rule applies to a grant owner exactly as to an administrator.
	if w := f.call(t, "lead", http.MethodPost, path, `{"user_id":"`+staff+`","role_keys":["manager"]}`); w.Code != http.StatusBadRequest {
		t.Errorf("grant owner assigning an undelegated role = %d: %s", w.Code, w.Body.String())
	}
}

// A-1, A-2, A-3, A-6: everything else is out of reach.
func TestAGrantOwnerReachesNothingBeyondItsGrant(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	f.appointLead(t, g)
	staff := f.factory.User(f.orgB, "staff@bravo.test")

	// A second grant to the same organization, over another project.
	otherBody, _ := json.Marshal(map[string]any{"granted_org_id": f.orgB, "role_keys": []string{"auditor"}})
	w := f.call(t, "admin-a", http.MethodPost, f.grantsPath(f.orgA, f.otherProjA), string(otherBody))
	if w.Code != http.StatusCreated {
		t.Fatalf("second grant = %d: %s", w.Code, w.Body.String())
	}
	other := decode(t, w).Id.String()

	for _, c := range []struct{ method, path, body, why string }{
		{http.MethodPost, f.delegatedPath(f.orgB, other), `{"user_id":"` + staff + `","role_keys":["auditor"]}`, "A-3: another grant in the same organization"},
		{http.MethodGet, f.delegatedPath(f.orgB, other), "", "A-3: listing another grant"},
		{http.MethodPost, f.ownersPath(f.orgB, g), `{"user_id":"` + staff + `"}`, "A-6: appointing another owner"},
		{http.MethodGet, f.ownersPath(f.orgB, g), "", "listing owners"},
		{http.MethodGet, f.grantsPath(f.orgA, f.projectA), "", "A-1: the granting project's grants"},
		{http.MethodDelete, f.grantsPath(f.orgA, f.projectA) + "/" + g, "", "A-2: revoking or reshaping the grant"},
		{http.MethodGet, "/v1/organizations/" + f.orgA + "/projects/" + f.projectA + "/roles", "", "A-1: the granting project's roles"},
		{http.MethodGet, "/v1/organizations/" + f.orgB + "/users", "", "its own organization's users"},
		{http.MethodGet, "/v1/organizations/" + f.orgB + "/users/" + staff + "/grants", "", "direct grants"},
		{http.MethodGet, "/v1/organizations/" + f.orgB, "", "its organization's settings"},
		{http.MethodGet, "/v1/organizations/" + f.orgB + "/events", "", "the audit log"},
	} {
		if w := f.call(t, "lead", c.method, c.path, c.body); w.Code < 400 {
			t.Errorf("%s: answered %d, want a refusal", c.why, w.Code)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, other); n != 0 {
		t.Error("a grant owner wrote through a grant it does not own")
	}
	if n := f.count(t, `SELECT count(*) FROM manager_roles WHERE user_id = $1`, staff); n != 0 {
		t.Error("a grant owner appointed a manager role")
	}
}

// A-4 and T4-4: the role works while the grant does, and a re-grant under a new
// id does not revive it.
func TestAGrantOwnerStopsWhenTheGrantIsRevokedAndIsNotRevivedByARegrant(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	f.appointLead(t, g)
	staff := f.factory.User(f.orgB, "staff@bravo.test")
	if w := f.call(t, "lead", http.MethodPost, f.delegatedPath(f.orgB, g), `{"user_id":"`+staff+`","role_keys":["cashier"]}`); w.Code != http.StatusCreated {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	f.revoke(t, g)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, f.delegatedPath(f.orgB, g)},
		{http.MethodDelete, f.delegatedPath(f.orgB, g) + "/" + staff},
	} {
		if w := f.call(t, "lead", c.method, c.path, ""); w.Code != http.StatusNotFound {
			t.Errorf("grant owner %s under a revoked grant = %d: %s", c.method, w.Code, w.Body.String())
		}
	}
	// The organization's administrator keeps history and cleanup.
	if w := f.call(t, "admin-b", http.MethodGet, f.delegatedPath(f.orgB, g), ""); w.Code != http.StatusOK {
		t.Errorf("administrator listing under a revoked grant = %d", w.Code)
	}

	regrant := f.activeGrant(t, "cashier")
	fresh := f.factory.User(f.orgB, "fresh@bravo.test")
	if w := f.call(t, "lead", http.MethodPost, f.delegatedPath(f.orgB, regrant), `{"user_id":"`+fresh+`","role_keys":["cashier"]}`); w.Code < 400 {
		t.Errorf("the old grant's owner assigning through the re-grant = %d: %s", w.Code, w.Body.String())
	}
}

// A-5: only the receiving organization appoints, and only its own members,
// under an active grant.
func TestOnlyTheReceivingOrganizationAppointsOwnersFromItsOwnMembers(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	member := f.factory.User(f.orgB, "member@bravo.test")
	outsider := f.factory.User(f.orgC, "outsider@charlie.test")
	vendorStaff := f.factory.User(f.orgA, "vendor@acme.test")
	body := func(user string) string { return `{"user_id":"` + user + `"}` }

	for _, c := range []struct{ who, org string }{
		{"admin-a", f.orgA}, {"admin-a", f.orgB}, {"owner-pos", f.orgA}, {"owner-pos", f.orgB},
	} {
		if w := f.call(t, c.who, http.MethodPost, f.ownersPath(c.org, g), body(member)); w.Code < 400 {
			t.Errorf("%s appointing through %s's path = %d", c.who, c.org, w.Code)
		}
	}
	for name, user := range map[string]string{"a third organization's user": outsider, "the granting organization's user": vendorStaff} {
		if w := f.call(t, "admin-b", http.MethodPost, f.ownersPath(f.orgB, g), body(user)); w.Code != http.StatusNotFound {
			t.Errorf("appointing %s = %d: %s", name, w.Code, w.Body.String())
		}
	}
	var self string
	f.factory.QueryRow(&self, `SELECT id FROM users WHERE email = 'admin-b@example.test'`)
	if w := f.call(t, "admin-b", http.MethodPost, f.ownersPath(f.orgB, g), body(self)); w.Code != http.StatusForbidden {
		t.Errorf("self-appointment = %d: %s", w.Code, w.Body.String())
	}
	if n := f.count(t, `SELECT count(*) FROM manager_roles WHERE role = 'PROJECT_GRANT_OWNER'`); n != 0 {
		t.Fatalf("%d appointments were written by refused requests", n)
	}

	if w := f.call(t, "admin-b", http.MethodPost, f.ownersPath(f.orgB, g), body(member)); w.Code != http.StatusCreated {
		t.Fatalf("appointing a member = %d: %s", w.Code, w.Body.String())
	}
	if w := f.call(t, "admin-b", http.MethodPost, f.ownersPath(f.orgB, g), body(member)); w.Code != http.StatusConflict {
		t.Errorf("a duplicate appointment = %d", w.Code)
	}
	if w := f.call(t, "admin-b", http.MethodDelete, f.ownersPath(f.orgB, g)+"/"+member, ""); w.Code != http.StatusNoContent {
		t.Errorf("removing an owner = %d: %s", w.Code, w.Body.String())
	}
	if n := f.count(t, `SELECT count(*) FROM events WHERE event_type = 'manager_role.revoked' AND payload->>'subject_user_id' = $1`, member); n != 1 {
		t.Errorf("found %d removal events, want 1", n)
	}

	f.revoke(t, g)
	if w := f.call(t, "admin-b", http.MethodPost, f.ownersPath(f.orgB, g), body(member)); w.Code != http.StatusConflict {
		t.Errorf("appointing under a revoked grant = %d: %s", w.Code, w.Body.String())
	}
}

// A-7: a PROJECT_GRANT_OWNER row held by a user of ANOTHER organization buys
// nothing, whichever organization's path it uses.
func TestAGrantOwnerRowHeldOutsideTheReceivingOrganizationIsInert(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	outsider := f.factory.User(f.orgC, "outsider@charlie.test")
	f.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'PROJECT_GRANT_OWNER', $2)`, outsider, g)
	f.mint("outsider", f.orgC, outsider)
	staff := f.factory.User(f.orgB, "staff@bravo.test")

	for _, org := range []string{f.orgB, f.orgC} {
		if w := f.call(t, "outsider", http.MethodPost, f.delegatedPath(org, g), `{"user_id":"`+staff+`","role_keys":["cashier"]}`); w.Code < 400 {
			t.Errorf("an outside grant owner assigning through %s's path = %d", org, w.Code)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, g); n != 0 {
		t.Error("an outside grant owner wrote a delegated role")
	}
}

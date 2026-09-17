//go:build integration

// Delegated user grants through the real /v1 chain (P4-02).
//
// The rule under test is the one every plan document states on its own: the
// roles a receiving organization assigns through a Project Grant are a subset of
// what it delegates, checked against the grant as it stands at that moment, on
// every request. A grants, B receives and assigns, C must see nothing.
package projectgrant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

func (f *fixture) delegatedPath(org, grantID string) string {
	return "/v1/organizations/" + org + "/project-grants/" + grantID + "/user-grants"
}

func (f *fixture) assign(t *testing.T, who, org, grantID, userID string, keys ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"user_id": userID, "role_keys": keys})
	return f.call(t, who, http.MethodPost, f.delegatedPath(org, grantID), string(body))
}

func decodeGrant(t *testing.T, body []byte) api.Grant {
	t.Helper()
	var g api.Grant
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return g
}

// activeGrant creates a grant from A to B over projectA with these keys.
func (f *fixture) activeGrant(t *testing.T, keys ...string) string {
	t.Helper()
	w := f.create(t, "admin-a", keys...)
	if w.Code != http.StatusCreated {
		t.Fatalf("creating the grant = %d: %s", w.Code, w.Body.String())
	}
	return decode(t, w).Id.String()
}

func (f *fixture) revoke(t *testing.T, grantID string) {
	t.Helper()
	if w := f.call(t, "admin-a", http.MethodDelete, f.grantsPath(f.orgA, f.projectA)+"/"+grantID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoking = %d: %s", w.Code, w.Body.String())
	}
}

// --- the write path -----------------------------------------------------------------------

func TestAReceivingAdministratorAssignsADelegatedRoleAndItIsAudited(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier", "manager")
	staff := f.factory.User(f.orgB, "staff@bravo.test")

	w := f.assign(t, "admin-b", f.orgB, g, staff, "cashier")
	if w.Code != http.StatusCreated {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}
	got := decodeGrant(t, w.Body.Bytes())
	if got.ProjectId.String() != f.projectA || got.ProjectGrantId.IsNull() || got.ProjectGrantId.MustGet().String() != g {
		t.Errorf("rendered %+v — want the granting project and this grant's id", got)
	}

	// The row is the receiving organization's, for the granting project.
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE user_id = $1 AND org_id = $2 AND project_id = $3 AND project_grant_id = $4`,
		staff, f.orgB, f.projectA, g); n != 1 {
		t.Errorf("found %d delegated rows filed under the receiving organization, want 1", n)
	}

	// Both organizations, the grant, the user and the exact roles, in the
	// receiving organization's log.
	if n := f.count(t, `
		SELECT count(*) FROM events
		 WHERE event_type = 'delegated_role.assigned' AND org_id = $1::uuid
		   AND payload->>'grant_id' = $2 AND payload->>'granting_org_id' = $3
		   AND payload->>'granted_org_id' = $1::text AND payload->>'subject_user_id' = $4
		   AND payload->'roles_added' = '["cashier"]'::jsonb`,
		f.orgB, g, f.orgA, staff); n != 1 {
		t.Errorf("found %d complete assignment events, want 1", n)
	}

	list := f.call(t, "admin-b", http.MethodGet, f.delegatedPath(f.orgB, g), "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), staff) {
		t.Errorf("list = %d, missing the assignment: %s", list.Code, list.Body.String())
	}

	// The receiving organization's own view of the user carries the grant id,
	// which is what the role-source badge reads.
	direct := f.call(t, "admin-b", http.MethodGet, "/v1/organizations/"+f.orgB+"/users/"+staff+"/grants", "")
	if direct.Code != http.StatusOK || !strings.Contains(direct.Body.String(), `"project_grant_id":"`+g+`"`) {
		t.Errorf("the user's grants = %d, without the project grant id: %s", direct.Code, direct.Body.String())
	}
}

func TestARoleTheGrantDoesNotDelegateIsRefusedByName(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	staff := f.factory.User(f.orgB, "staff@bravo.test")

	w := f.assign(t, "admin-b", f.orgB, g, staff, "cashier", "manager")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("assigning an undelegated role = %d: %s", w.Code, w.Body.String())
	}
	// Named, and with what IS delegated: the clear, actionable error
	// docs/PLAN/17 Phase 4 asks for.
	if body := w.Body.String(); !strings.Contains(body, "manager") || !strings.Contains(body, "It delegates: cashier") {
		t.Errorf("the refusal does not name the key and the delegated set: %s", body)
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE user_id = $1`, staff); n != 0 {
		t.Errorf("a refused assignment left %d rows", n)
	}

	// Widening afterwards is refused on every path that can write the row.
	if w := f.assign(t, "admin-b", f.orgB, g, staff, "cashier"); w.Code != http.StatusCreated {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}
	patch := f.call(t, "admin-b", http.MethodPatch, f.delegatedPath(f.orgB, g)+"/"+staff, `{"role_keys":["cashier","manager"]}`)
	if patch.Code != http.StatusBadRequest {
		t.Errorf("widening through the delegated PATCH = %d: %s", patch.Code, patch.Body.String())
	}
	// The direct-grant PATCH never checks delegation itself. The trigger does.
	direct := f.call(t, "admin-b", http.MethodPatch,
		"/v1/organizations/"+f.orgB+"/users/"+staff+"/grants/"+f.projectA, `{"role_keys":["cashier","manager"]}`)
	if direct.Code != http.StatusBadRequest || !strings.Contains(direct.Body.String(), "not delegated") {
		t.Errorf("widening through the direct-grant PATCH = %d: %s", direct.Code, direct.Body.String())
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE user_id = $1 AND role_keys = '{cashier}'`, staff); n != 1 {
		t.Error("the delegated row was widened")
	}
}

func TestARevokedGrantRefusesEveryAssignmentAndAllowsCleanup(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier", "manager")
	first := f.factory.User(f.orgB, "first@bravo.test")
	second := f.factory.User(f.orgB, "second@bravo.test")
	if w := f.assign(t, "admin-b", f.orgB, g, first, "cashier"); w.Code != http.StatusCreated {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	f.revoke(t, g)

	if w := f.assign(t, "admin-b", f.orgB, g, second, "cashier"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "revoked") {
		t.Errorf("assigning through a revoked grant = %d: %s", w.Code, w.Body.String())
	}
	if w := f.call(t, "admin-b", http.MethodPatch, f.delegatedPath(f.orgB, g)+"/"+first, `{"role_keys":["manager"]}`); w.Code != http.StatusConflict {
		t.Errorf("replacing through a revoked grant = %d: %s", w.Code, w.Body.String())
	}
	if w := f.call(t, "admin-b", http.MethodGet, f.delegatedPath(f.orgB, g), ""); w.Code != http.StatusOK {
		t.Errorf("listing under a revoked grant = %d: %s", w.Code, w.Body.String())
	}
	if w := f.call(t, "admin-b", http.MethodDelete, f.delegatedPath(f.orgB, g)+"/"+first, ""); w.Code != http.StatusNoContent {
		t.Errorf("removing under a revoked grant = %d: %s", w.Code, w.Body.String())
	}
	if n := f.count(t, `SELECT count(*) FROM events WHERE event_type = 'delegated_role.removed' AND payload->'roles_removed' = '["cashier"]'::jsonb`); n != 1 {
		t.Errorf("found %d removal events carrying the removed roles, want 1", n)
	}
}

// A-8. The task card asks for a grant narrowed after assignments exist. Since
// P4-01 a grant's keys cannot change; narrowing is revoke and re-grant, and the
// property is the same: the dropped role is refused from then on, through
// either grant.
func TestNarrowingByRegrantRefusesTheDroppedRoleThroughEitherGrant(t *testing.T) {
	f := setup(t)
	wide := f.activeGrant(t, "cashier", "manager")
	staff := f.factory.User(f.orgB, "staff@bravo.test")
	if w := f.assign(t, "admin-b", f.orgB, wide, staff, "manager"); w.Code != http.StatusCreated {
		t.Fatalf("assign under the wide grant = %d: %s", w.Code, w.Body.String())
	}

	f.revoke(t, wide)
	narrow := f.activeGrant(t, "cashier")

	if w := f.assign(t, "admin-b", f.orgB, narrow, staff, "manager"); w.Code != http.StatusBadRequest {
		t.Errorf("the dropped role through the narrow grant = %d: %s", w.Code, w.Body.String())
	}
	if w := f.assign(t, "admin-b", f.orgB, wide, staff, "manager"); w.Code != http.StatusConflict {
		t.Errorf("the dropped role through the revoked grant = %d: %s", w.Code, w.Body.String())
	}

	// A permitted role through the new grant replaces the stale row, and the
	// event says which grant it superseded.
	if w := f.assign(t, "admin-b", f.orgB, narrow, staff, "cashier"); w.Code != http.StatusCreated {
		t.Fatalf("re-assigning under the narrow grant = %d: %s", w.Code, w.Body.String())
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE user_id = $1 AND project_id = $2 AND project_grant_id = $3 AND role_keys = '{cashier}'`,
		staff, f.projectA, narrow); n != 1 {
		t.Error("the user does not hold exactly cashier through the narrow grant")
	}
	if n := f.count(t, `SELECT count(*) FROM events WHERE event_type = 'delegated_role.assigned' AND payload->>'superseded_grant_id' = $1`, wide); n != 1 {
		t.Errorf("found %d events recording the superseded grant, want 1", n)
	}
}

// --- whose people -------------------------------------------------------------------------

func TestOnlyTheReceivingOrganizationsOwnUsersReceiveDelegatedRoles(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	outsider := f.factory.User(f.orgC, "outsider@charlie.test")
	vendorStaff := f.factory.User(f.orgA, "vendor@acme.test")

	for name, userID := range map[string]string{"a third organization's user": outsider, "the granting organization's user": vendorStaff} {
		if w := f.assign(t, "admin-b", f.orgB, g, userID, "cashier"); w.Code != http.StatusNotFound {
			t.Errorf("assigning to %s = %d: %s", name, w.Code, w.Body.String())
		}
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, g); n != 0 {
		t.Errorf("%d delegated rows were written for users outside the receiving organization", n)
	}

	// Self-assignment: the same vertical escalation P2-03 refuses.
	var self string
	f.factory.QueryRow(&self, `SELECT id FROM users WHERE email = 'admin-b@example.test'`)
	if w := f.assign(t, "admin-b", f.orgB, g, self, "cashier"); w.Code != http.StatusForbidden {
		t.Errorf("self-assignment = %d: %s", w.Code, w.Body.String())
	}
}

// T4-4: the granting organization lends the project; it does not administer the
// partner's people.
func TestTheGrantingOrganizationCannotActOnThePartnersPeople(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	staff := f.factory.User(f.orgB, "staff@bravo.test")

	// With the partner's organization in the path: no role there, and the
	// chain answers an organization the caller cannot reach as not found.
	for _, who := range []string{"admin-a", "owner-pos"} {
		if w := f.assign(t, who, f.orgB, g, staff, "cashier"); w.Code != http.StatusNotFound {
			t.Errorf("%s assigning in the partner's organization = %d: %s", who, w.Code, w.Body.String())
		}
	}
	// With its own organization in the path, the grant is not one it received.
	vendorStaff := f.factory.User(f.orgA, "vendor@acme.test")
	for _, userID := range []string{staff, vendorStaff} {
		if w := f.assign(t, "admin-a", f.orgA, g, userID, "cashier"); w.Code != http.StatusNotFound {
			t.Errorf("admin-a assigning through its own path = %d: %s", w.Code, w.Body.String())
		}
	}
	if w := f.call(t, "admin-a", http.MethodGet, f.delegatedPath(f.orgA, g), ""); w.Code != http.StatusNotFound {
		t.Errorf("admin-a listing a grant it made = %d: %s", w.Code, w.Body.String())
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, g); n != 0 {
		t.Errorf("the granting organization wrote %d delegated rows", n)
	}
}

// T4-3. The row is the receiving organization's. Until P4-04 adds the reader
// join, the granting organization must not see it either — see the spec §6.
func TestADelegatedRowIsVisibleOnlyToTheReceivingOrganization(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	staff := f.factory.User(f.orgB, "staff@bravo.test")
	if w := f.assign(t, "admin-b", f.orgB, g, staff, "cashier"); w.Code != http.StatusCreated {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	for org, want := range map[string]int{f.orgB: 1, f.orgA: 0, f.orgC: 0} {
		var n int
		if err := f.db.WithTenant(ctx, org, func(tx *postgres.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, g).Scan(&n)
		}); err != nil {
			t.Fatalf("reading as %s: %v", org, err)
		}
		if n != want {
			t.Errorf("organization %s sees %d delegated rows, want %d", org, n, want)
		}
	}
}

// --- every writer --------------------------------------------------------------------------

// The trigger holds for a writer that is not the handler, including the owner
// connection, which bypasses RLS.
func TestTheDatabaseRefusesAnInvalidDelegatedRowFromAnyWriter(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	staff := f.factory.User(f.orgB, "staff@bravo.test")
	outsider := f.factory.User(f.orgC, "outsider@charlie.test")
	vendorStaff := f.factory.User(f.orgA, "vendor@acme.test")

	insert := `INSERT INTO user_grants (user_id, project_id, org_id, role_keys, project_grant_id) VALUES ($1, $2, $3, $4, $5)`
	for name, c := range map[string]struct {
		user, project, org string
		keys               []string
		grantID, want      string
	}{
		"a key it does not delegate":   {staff, f.projectA, f.orgB, []string{"manager"}, g, "is not delegated"},
		"filed under the granting org": {vendorStaff, f.projectA, f.orgA, []string{"cashier"}, g, "is not the organization"},
		"another project":              {staff, f.otherProjA, f.orgB, []string{"cashier"}, g, "is not the project"},
		"a third organization's user":  {outsider, f.projectA, f.orgB, []string{"cashier"}, g, "is not a member"},
		"a grant that does not exist":  {staff, f.projectA, f.orgB, []string{"cashier"}, "00000000-0000-4000-8000-000000000000", "is not visible"},
	} {
		err := f.factory.TryExec(insert, c.user, c.project, c.org, pq.Array(c.keys), c.grantID)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}

	// A valid row, then every way of changing it that the rule forbids.
	f.factory.Exec(insert, staff, f.projectA, f.orgB, pq.Array([]string{"cashier"}), g)
	for name, c := range map[string]struct{ statement, want string }{
		"widening role_keys":    {`UPDATE user_grants SET role_keys = '{cashier,manager}' WHERE user_id = $1`, "is not delegated"},
		"shedding the grant":    {`UPDATE user_grants SET project_grant_id = NULL WHERE user_id = $1`, "cannot change"},
		"moving to another org": {`UPDATE user_grants SET org_id = '` + f.orgC + `' WHERE user_id = $1`, "is not the organization"},
	} {
		if err := f.factory.TryExec(c.statement, staff); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}

	// A-5: a direct grant cannot borrow a delegation's scope.
	projectB := f.projectB
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'member', 'member')`, f.orgB, projectB)
	other := f.factory.User(f.orgB, "other@bravo.test")
	f.factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys) VALUES ($1, $2, $3, '{member}')`, other, projectB, f.orgB)
	if err := f.factory.TryExec(`UPDATE user_grants SET project_grant_id = $1 WHERE user_id = $2`, g, other); err == nil || !strings.Contains(err.Error(), "cannot change") {
		t.Errorf("attaching a project grant to a direct grant: err = %v", err)
	}

	// And a revoked grant refuses a new row even from the owner connection.
	f.revoke(t, g)
	late := f.factory.User(f.orgB, "late@bravo.test")
	if err := f.factory.TryExec(insert, late, f.projectA, f.orgB, pq.Array([]string{"cashier"}), g); err == nil || !strings.Contains(err.Error(), "is revoked") {
		t.Errorf("a row under a revoked grant: err = %v", err)
	}
}

// A-4: a revocation and an assignment cannot interleave. The assignment that
// waits behind a revocation sees it.
func TestAnAssignmentThatWaitsBehindARevocationSeesIt(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	staff := f.factory.User(f.orgB, "staff@bravo.test")
	ctx := context.Background()

	locked := make(chan struct{})
	release := make(chan struct{})
	revokeErr := make(chan error, 1)
	go func() {
		revokeErr <- f.db.WithTenant(ctx, f.orgA, func(tx *postgres.Tx) error {
			if _, _, err := NewStore().Revoke(ctx, tx, f.orgA, f.projectA, g, time.Now()); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	var (
		mu   sync.Mutex
		code int
		body string
	)
	done := make(chan struct{})
	go func() {
		w := f.assign(t, "admin-b", f.orgB, g, staff, "cashier")
		mu.Lock()
		code, body = w.Code, w.Body.String()
		mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		t.Fatalf("the assignment finished while the revocation held the grant: %d %s", code, body)
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	if err := <-revokeErr; err != nil {
		t.Fatalf("revoking: %v", err)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if code != http.StatusConflict {
		t.Errorf("the assignment that waited = %d: %s — it must see the revocation", code, body)
	}
	if n := f.count(t, `SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, g); n != 0 {
		t.Errorf("%d rows were written under a grant revoked before they committed", n)
	}
}

func TestTheDelegatedListIsPaginated(t *testing.T) {
	f := setup(t)
	g := f.activeGrant(t, "cashier")
	for i := 0; i < 3; i++ {
		u := f.factory.User(f.orgB, fmt.Sprintf("staff%d@bravo.test", i))
		if w := f.assign(t, "admin-b", f.orgB, g, u, "cashier"); w.Code != http.StatusCreated {
			t.Fatalf("assign %d = %d: %s", i, w.Code, w.Body.String())
		}
	}

	seen := map[string]bool{}
	path := f.delegatedPath(f.orgB, g) + "?page_size=2"
	for pages := 0; path != ""; pages++ {
		if pages > 3 {
			t.Fatal("pagination did not end")
		}
		w := f.call(t, "admin-b", http.MethodGet, path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("list = %d: %s", w.Code, w.Body.String())
		}
		var page api.DelegatedGrantList
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Grants {
			seen[row.UserId.String()] = true
		}
		path = ""
		if page.PageInfo != nil && page.PageInfo.NextPageToken != nil {
			path = f.delegatedPath(f.orgB, g) + "?page_size=2&page_token=" + *page.PageInfo.NextPageToken
		}
	}
	if len(seen) != 3 {
		t.Errorf("paging saw %d distinct users, want 3", len(seen))
	}
}

//go:build integration

// The receiving organization's own list of what it was given (P4-06).
//
// Until this route there was no way for B to discover a grant A made to it: it
// could assign delegated roles only if it already knew the grant id. The list
// has to answer that without answering anything else — a grant row is visible
// to both parties, so "which grants can I see" and "which grants was I given"
// have different answers, and only the second belongs here.
package projectgrant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

func (f *fixture) receivedPath(org string) string {
	return "/v1/organizations/" + org + "/project-grants"
}

func (f *fixture) received(t *testing.T, who, org, query string) api.ReceivedGrantList {
	t.Helper()
	w := f.call(t, who, http.MethodGet, f.receivedPath(org)+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("listing received grants = %d: %s", w.Code, w.Body.String())
	}
	return decodeReceived(t, w)
}

func decodeReceived(t *testing.T, w *httptest.ResponseRecorder) api.ReceivedGrantList {
	t.Helper()
	var out api.ReceivedGrantList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
	return out
}

// seedGrant writes a grant directly, for the rows a test needs to EXIST rather
// than to be created through the API — another organization's delegation, or
// the several rows a pagination test walks.
func (f *fixture) seedGrant(t *testing.T, project, from, to string, keys []string, ageSeconds int) string {
	t.Helper()
	var id string
	f.factory.QueryRow(&id, `
		INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys, created_at)
		VALUES ($1, $2, $3, $4, now() - make_interval(secs => $5))
		RETURNING id`, project, from, to, pq.Array(keys), ageSeconds)
	return id
}

func receivedIDs(list api.ReceivedGrantList) []string {
	out := make([]string, 0, len(list.Grants))
	for _, g := range list.Grants {
		out = append(out, g.Id.String())
	}
	return out
}

func TestTheReceivingOrganizationFindsWhatItWasGivenAndCanNameIt(t *testing.T) {
	f := setup(t)
	granted := decode(t, f.create(t, "admin-a", "cashier", "manager"))

	list := f.received(t, "admin-b", f.orgB, "")
	if len(list.Grants) != 1 {
		t.Fatalf("B sees %d grants, want the 1 it was given: %+v", len(list.Grants), list.Grants)
	}
	got := list.Grants[0]
	if got.Id != granted.Id || got.ProjectId.String() != f.projectA || got.GrantingOrgId.String() != f.orgA {
		t.Errorf("row = %+v, want the grant A made over its pos project", got)
	}
	// The point of the migration: the project and the organization behind it
	// are the granting side's rows, which B's own RLS hides.
	if got.ProjectName != "pos" || got.GrantingOrgName != "Acme Vendor" {
		t.Errorf("project %q from %q — a partner administrator cannot act on two UUIDs",
			got.ProjectName, got.GrantingOrgName)
	}
	if strings.Join(got.GrantedRoleKeys, ",") != "cashier,manager" {
		t.Errorf("granted_role_keys = %v, want exactly what the grant delegates", got.GrantedRoleKeys)
	}
	if got.Status != api.ReceivedGrantStatus(StatusActive) {
		t.Errorf("status = %s, want active", got.Status)
	}
	if got.RevokedAt.IsSpecified() && !got.RevokedAt.IsNull() {
		t.Errorf("revoked_at = %v on a live grant", got.RevokedAt)
	}
	if got.HolderCount != 0 {
		t.Errorf("holder_count = %d before anyone is assigned, want 0", got.HolderCount)
	}

	// Assigned, the count is B's own users holding a role through the grant.
	member := f.factory.User(f.orgB, "clerk@bravo.test")
	if w := f.assign(t, "admin-b", f.orgB, granted.Id.String(), member, "cashier"); w.Code != http.StatusCreated {
		t.Fatalf("assigning = %d: %s", w.Code, w.Body.String())
	}
	if n := f.received(t, "admin-b", f.orgB, "").Grants[0].HolderCount; n != 1 {
		t.Errorf("holder_count = %d after one assignment, want 1", n)
	}
}

// A-3: the route lists what this organization RECEIVED, never what it gave.
// Both are visible to it under the two-sided policy, which is exactly why the
// query filters rather than trusting what RLS returns.
func TestTheReceivingListLeavesOutTheGrantsThisOrganizationMade(t *testing.T) {
	f := setup(t)
	given := decode(t, f.create(t, "admin-a", "cashier")).Id.String()

	// B lends one of ITS projects to C.
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'clerk', 'clerk')`, f.orgB, f.projectB)
	made := f.seedGrant(t, f.projectB, f.orgB, f.orgC, []string{"clerk"}, 0)

	list := f.received(t, "admin-b", f.orgB, "")
	if got := receivedIDs(list); len(got) != 1 || got[0] != given {
		t.Fatalf("B's received list = %v, want only %s — %s is a grant B MADE", got, given, made)
	}
	// And the row it DID leave out is one B can genuinely see: the filter is
	// doing the work, not RLS.
	if n := f.count(t, `SELECT count(*) FROM project_grants WHERE id = $1 AND granting_org_id = $2`, made, f.orgB); n != 1 {
		t.Fatalf("the grant B made is not in the table at all — the test proves nothing")
	}
}

// A-4: a third organization is not a party to either side.
func TestNoThirdOrganizationSeesADelegationBetweenTwoOthers(t *testing.T) {
	f := setup(t)
	f.create(t, "admin-a", "cashier")

	adminC := f.factory.User(f.orgC, "admin-c@charlie.test")
	f.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`, adminC, f.orgC)
	f.mint("admin-c", f.orgC, adminC)

	if list := f.received(t, "admin-c", f.orgC, ""); len(list.Grants) != 0 {
		t.Errorf("C sees %d grants of a delegation it is not part of: %+v", len(list.Grants), list.Grants)
	}
	// Nor by asking under another organization's path, which is the scope the
	// policy table binds the route to. 404 rather than 403: an organization the
	// caller has no role in is not confirmed to exist.
	for _, org := range []string{f.orgA, f.orgB} {
		if w := f.call(t, "admin-c", http.MethodGet, f.receivedPath(org), ""); w.Code != http.StatusNotFound {
			t.Errorf("C listing %s's received grants = %d, want 404: %s", org, w.Code, w.Body.String())
		}
	}
}

// The name lookup runs as SECURITY DEFINER, so RLS does not bound it and its
// own WHERE clause is the whole control. Asked for a grant between two other
// organizations, it must answer nothing — the route only ever passes ids it has
// already listed, but a function that bypasses RLS is not allowed to depend on
// its caller being careful.
func TestTheNameLookupIsBoundedToGrantsTheCallerWasGiven(t *testing.T) {
	f := setup(t)
	between := decode(t, f.create(t, "admin-a", "cashier")).Id.String()

	names := func(org string) int {
		t.Helper()
		var n int
		if err := f.db.WithTenant(context.Background(), org, func(tx *postgres.Tx) error {
			return tx.QueryRow(context.Background(),
				`SELECT count(*) FROM received_grant_context($1::uuid[])`, pq.Array([]string{between})).Scan(&n)
		}); err != nil {
			t.Fatalf("reading the names as %s: %v", org, err)
		}
		return n
	}
	if got := names(f.orgB); got != 1 {
		t.Errorf("the organization that was GIVEN the grant resolves %d names, want 1", got)
	}
	for label, org := range map[string]string{"the granting side": f.orgA, "a bystander": f.orgC} {
		if got := names(org); got != 0 {
			t.Errorf("%s resolves %d names for a grant it was not given, want 0", label, got)
		}
	}
}

// F-3: access ends, the record does not. A partner that finds the roles simply
// gone learns nothing; a partner that sees "Ended" knows what happened.
func TestARevokedGrantStaysListedAndMarked(t *testing.T) {
	f := setup(t)
	g := decode(t, f.create(t, "admin-a", "cashier"))
	f.revoke(t, g.Id.String())

	list := f.received(t, "admin-b", f.orgB, "")
	if len(list.Grants) != 1 {
		t.Fatalf("after revocation B sees %d grants, want the revoked one still listed", len(list.Grants))
	}
	got := list.Grants[0]
	if got.Status != api.ReceivedGrantStatus(StatusRevoked) || !got.RevokedAt.IsSpecified() || got.RevokedAt.IsNull() {
		t.Errorf("revoked grant: status %s revoked_at %v, want revoked and dated", got.Status, got.RevokedAt)
	}
	if got.ProjectName != "pos" {
		t.Errorf("project_name = %q on a revoked grant — the record is only useful if it still names what ended", got.ProjectName)
	}
}

// F-1: newest first, and a page token that walks the whole set exactly once.
func TestTheReceivedListPagesNewestFirst(t *testing.T) {
	f := setup(t)

	want := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		var project string
		f.factory.QueryRow(&project, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`,
			f.orgA, fmt.Sprintf("lent-%d", i))
		f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'cashier', 'cashier')`, f.orgA, project)
		// Seeded oldest-first: the largest age goes in first.
		want = append(want, f.seedGrant(t, project, f.orgA, f.orgB, []string{"cashier"}, (5-i)*60))
	}
	// want is oldest-to-newest; the list is the reverse.
	for i, j := 0, len(want)-1; i < j; i, j = i+1, j-1 {
		want[i], want[j] = want[j], want[i]
	}

	var seen []string
	token := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatalf("paging did not terminate after %d pages: %v", pages, seen)
		}
		query := "?page_size=2"
		if token != "" {
			query += "&page_token=" + token
		}
		page := f.received(t, "admin-b", f.orgB, query)
		if len(page.Grants) > 2 {
			t.Fatalf("page_size=2 returned %d rows", len(page.Grants))
		}
		seen = append(seen, receivedIDs(page)...)
		if page.PageInfo == nil || page.PageInfo.NextPageToken == nil {
			break
		}
		token = *page.PageInfo.NextPageToken
	}

	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("paged order\n got %v\nwant %v (newest first, each row once)", seen, want)
	}
	unique := append([]string{}, seen...)
	sort.Strings(unique)
	for i := 1; i < len(unique); i++ {
		if unique[i] == unique[i-1] {
			t.Fatalf("row %s came back on two pages", unique[i])
		}
	}
}

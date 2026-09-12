//go:build integration

package auditlog

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/management"
)

// The audit log is org-scoped under EVERY filter combination (P2-08 step 6).
//
// A filter is a `WHERE` clause, and a `WHERE` clause is where tenant scoping
// gets lost — one query path built by string concatenation that forgets the
// organization, and an administrator can read another tenant's history by
// choosing the right filter.
//
// Row-level security is what actually prevents it, so this exercises every
// filter the contract offers and asserts that none of them widens what is
// visible. The combinations are generated rather than listed, because a hand-
// written list covers the ones somebody thought of.
func TestNoFilterCombinationCrossesTenants(t *testing.T) {
	f := setup(t)

	// The fixture already builds two organizations and a second user; this
	// test only needs them to differ in every filterable dimension.
	otherOrg := f.orgB
	otherUser := f.otherUser

	// ORG_ADMIN over the caller's own organization, and nothing over the other.
	f.grant(management.OrgAdmin, f.orgA)

	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-time.Minute)

	f.factory.Exec(`
		INSERT INTO events (org_id, actor_user_id, event_type, payload, created_at)
		VALUES ($1, $2, 'user.login.success', '{"canary":"OTHER-ORG"}'::jsonb, $3)`,
		otherOrg, otherUser, old)
	f.factory.Exec(`
		INSERT INTO events (org_id, actor_user_id, event_type, payload, created_at)
		VALUES ($1, $2, 'user.login.failed', '{"canary":"OTHER-ORG"}'::jsonb, $3)`,
		otherOrg, otherUser, recent)

	// One event in the caller's own organization, so a passing test is not
	// satisfied by an endpoint that returns nothing at all.
	f.factory.Exec(`
		INSERT INTO events (org_id, actor_user_id, event_type, payload, created_at)
		VALUES ($1, $2, 'user.login.success', '{"canary":"OWN-ORG"}'::jsonb, $3)`,
		f.orgA, f.userID, recent)

	// Every filter the contract offers, and the combinations of them. Built
	// from a list of clauses rather than written out, so adding a filter to
	// the API without adding it here is visible as a gap rather than invisible.
	clauses := []string{
		"",
		"event_type=user.login.success",
		"event_type=user.login.failed",
		"event_type=user.login.success&event_type=user.login.failed",
		"actor_id=" + otherUser,
		"actor_id=" + f.userID,
		"from=" + queryTime(old.Add(-time.Hour)),
		"to=" + queryTime(time.Now().Add(time.Hour)),
		"from=" + queryTime(old.Add(-time.Hour)) + "&to=" + queryTime(time.Now().Add(time.Hour)),
		"page_size=100",
	}

	// Only DISTINCT parameters are combined. Repeating a non-repeatable one —
	// two `actor_id` values — is correctly a 400, and folding that into this
	// test would mean tolerating a status that might one day hide a real
	// error. `event_type` is repeatable and appears pre-combined above.
	nameOf := func(clause string) string {
		if i := strings.Index(clause, "="); i >= 0 {
			return clause[:i]
		}
		return clause
	}

	combinations := 0
	for _, a := range clauses {
		for _, b := range clauses {
			query := a
			if b != "" && b != a {
				if nameOf(strings.Split(b, "&")[0]) == nameOf(strings.Split(a, "&")[0]) {
					continue
				}
				query = strings.TrimPrefix(a+"&"+b, "&")
			}
			combinations++

			path := f.events(f.orgA)
			if query != "" {
				path += "?" + query
			}

			rec := f.call(t, "GET", path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "OTHER-ORG") {
				t.Fatalf("%s leaked another organization's events:\n%s", path, rec.Body)
			}
		}
	}

	if combinations < 30 {
		t.Fatalf("only %d filter combinations were tried, too few to have covered the surface", combinations)
	}

	// And the caller can still read their OWN history — otherwise every
	// assertion above is satisfied by an endpoint that returns nothing.
	own := f.call(t, "GET", f.events(f.orgA), "")
	if !strings.Contains(own.Body.String(), "OWN-ORG") {
		t.Errorf("the caller cannot see their own events, so the isolation above proves nothing:\n%s", own.Body)
	}
}

// queryTime renders a timestamp for a query string.
//
// Named for what it does rather than `url`, which collides with net/url in a
// package that imports it.
func queryTime(at time.Time) string {
	return at.UTC().Format(time.RFC3339)
}

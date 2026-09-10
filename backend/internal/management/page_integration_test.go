//go:build integration

// Pagination against a real PostgreSQL (P1-15).
//
// The DoD item is "pagination is consistent across every list endpoint and
// stable under concurrent inserts", and the second half of that cannot be shown
// against a slice. It is a property of the QUERY: a keyset predicate is stable
// under inserts and an OFFSET is not, and the difference only appears when rows
// arrive between two pages.
package management

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

type pagedUser struct {
	id        string
	email     string
	createdAt time.Time
}

func userPosition(u pagedUser) Cursor { return Cursor{After: u.createdAt, ID: u.id} }

type pager struct {
	db    *postgres.DB
	orgID string
}

// page reads one page with the KEYSET predicate a list endpoint uses.
//
// `(created_at, id) > (after, id)` as a row comparison, not two ORs: the row
// form is what PostgreSQL can drive from a composite index, and writing it out
// as `created_at > x OR (created_at = x AND id > y)` is the version that
// silently sequential-scans.
//
// Note what is NOT here: no org_id predicate. RLS supplies it, which is the
// whole point of docs/PLAN/08 Part B — a page query that forgot to filter still
// cannot cross a tenant boundary.
func (p pager) page(t *testing.T, c Cursor, size int) Page[pagedUser] {
	t.Helper()

	var rows []pagedUser
	err := p.db.WithTenant(context.Background(), p.orgID, func(tx *postgres.Tx) error {
		result, err := tx.Query(context.Background(), `
			SELECT id, email, created_at
			  FROM users
			 WHERE ($1::timestamptz IS NULL) OR ((created_at, id) > ($1, $2::uuid))
			 ORDER BY created_at, id
			 LIMIT $3`,
			nullableTime(c), nullableID(c), size+1)
		if err != nil {
			return err
		}
		defer func() { _ = result.Close() }()

		for result.Next() {
			var u pagedUser
			if err := result.Scan(&u.id, &u.email, &u.createdAt); err != nil {
				return err
			}
			rows = append(rows, u)
		}
		return result.Err()
	})
	if err != nil {
		t.Fatalf("reading a page: %v", err)
	}

	page, err := Paginate(rows, size, userPosition)
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	return page
}

func nullableTime(c Cursor) any {
	if c.ID == "" {
		return nil
	}
	return c.After
}

func nullableID(c Cursor) any {
	if c.ID == "" {
		return nil
	}
	return c.ID
}

func setupPaging(t *testing.T) (pager, *v1) {
	t.Helper()
	s := setupV1(t)
	return pager{db: s.db, orgID: s.orgA}, s
}

// seed creates n users with strictly increasing created_at, so the ordering
// under test is deterministic rather than dependent on clock resolution.
func seed(s *v1, prefix string, n int, base time.Time) []string {
	ids := make([]string, 0, n)
	for i := range n {
		var id string
		s.factory.QueryRow(&id,
			`INSERT INTO users (org_id, email, status, created_at)
			 VALUES ($1, $2, 'active', $3) RETURNING id`,
			s.orgA, fmt.Sprintf("%s-%03d@example.test", prefix, i), base.Add(time.Duration(i)*time.Second))
		ids = append(ids, id)
	}
	return ids
}

// --- the walk -------------------------------------------------------------------------

// Every row, exactly once, with no inserts happening. The baseline that makes
// the next test mean something.
func TestWalkingAListReturnsEveryRowOnce(t *testing.T) {
	p, s := setupPaging(t)
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	seed(s, "seed", 47, base)

	seen := walk(t, p, 10)

	// 47 seeded, plus the one setupV1 creates for the caller.
	if len(seen) != 48 {
		t.Fatalf("saw %d distinct rows, want 48", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %s appeared %d times", id, n)
		}
	}
}

// **The DoD item.**
//
// Rows are inserted between every page, and deliberately in the two positions
// that break an OFFSET: BEFORE the cursor, which shifts every later row back by
// one and makes an offset walk return a row twice; and AFTER it, which an
// offset walk may skip.
//
// A keyset walk is unaffected in the way that matters: every row that existed
// when the walk started is returned exactly once. Rows inserted mid-walk may or
// may not appear — that is inherent to any paged read of a moving table, and
// promising otherwise would require a snapshot held across HTTP requests.
func TestConcurrentInsertsDoNotDuplicateOrSkipExistingRows(t *testing.T) {
	p, s := setupPaging(t)
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

	original := seed(s, "original", 30, base)
	wanted := map[string]bool{}
	for _, id := range original {
		wanted[id] = true
	}

	seen := map[string]int{}
	cursor := Cursor{}
	inserted := 0

	for round := range 20 {
		page := p.page(t, cursor, 5)
		for _, u := range page.Items {
			seen[u.id]++
		}
		if page.NextPageToken == "" {
			break
		}

		// The disruption, on every page boundary.
		//
		// One row lands BEFORE the cursor (an offset walk would now repeat a
		// row) and one AFTER it (an offset walk could skip one).
		s.factory.Exec(
			`INSERT INTO users (org_id, email, status, created_at) VALUES ($1, $2, 'active', $3)`,
			s.orgA, fmt.Sprintf("early-%03d@example.test", round),
			base.Add(-time.Duration(round+1)*time.Minute))
		s.factory.Exec(
			`INSERT INTO users (org_id, email, status, created_at) VALUES ($1, $2, 'active', $3)`,
			s.orgA, fmt.Sprintf("late-%03d@example.test", round),
			base.Add(time.Hour).Add(time.Duration(round)*time.Second))
		inserted += 2

		var err error
		if cursor, err = DecodeCursor(page.NextPageToken); err != nil {
			t.Fatalf("DecodeCursor: %v", err)
		}
	}

	if inserted == 0 {
		t.Fatal("no rows were inserted during the walk, so the test proves nothing")
	}

	for id := range wanted {
		switch seen[id] {
		case 1:
			// Correct.
		case 0:
			t.Errorf("row %s existed when the walk began and was never returned", id)
		default:
			t.Errorf("row %s was returned %d times", id, seen[id])
		}
	}
}

// The control that makes the test above meaningful: the same disruption applied
// to an OFFSET walk really does duplicate rows.
//
// Without this, "keyset pagination is stable" is a claim about a property
// nothing has been shown to lack — and a test that passes against both designs
// is a test that is not about the design.
func TestAnOffsetWalkIsNotStableUnderTheSameInserts(t *testing.T) {
	p, s := setupPaging(t)
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	seed(s, "original", 30, base)

	seen := map[string]int{}
	offset := 0

	for round := range 10 {
		var rows []pagedUser
		err := p.db.WithTenant(context.Background(), p.orgID, func(tx *postgres.Tx) error {
			result, err := tx.Query(context.Background(), `
				SELECT id, email, created_at FROM users
				 ORDER BY created_at, id LIMIT 5 OFFSET $1`, offset)
			if err != nil {
				return err
			}
			defer func() { _ = result.Close() }()
			for result.Next() {
				var u pagedUser
				if err := result.Scan(&u.id, &u.email, &u.createdAt); err != nil {
					return err
				}
				rows = append(rows, u)
			}
			return result.Err()
		})
		if err != nil {
			t.Fatalf("offset page: %v", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, u := range rows {
			seen[u.id]++
		}
		offset += 5

		// The same insert-before-the-cursor that the keyset walk shrugs off.
		s.factory.Exec(
			`INSERT INTO users (org_id, email, status, created_at) VALUES ($1, $2, 'active', $3)`,
			s.orgA, fmt.Sprintf("offset-early-%03d@example.test", round),
			base.Add(-time.Duration(round+1)*time.Minute))
	}

	duplicated := 0
	for _, n := range seen {
		if n > 1 {
			duplicated++
		}
	}
	if duplicated == 0 {
		t.Fatal("an OFFSET walk saw no duplicates under inserts before the cursor — " +
			"the disruption is not disrupting anything, so the keyset test proves nothing either")
	}
}

// --- isolation ---------------------------------------------------------------------------

// The page query carries no org_id predicate; RLS supplies it. A walk in one
// organization must therefore return nothing belonging to another, and this
// asserts it from the side that matters: rows exist and are not returned.
func TestAWalkNeverCrossesATenantBoundary(t *testing.T) {
	p, s := setupPaging(t)
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	seed(s, "mine", 10, base)

	// The other tenant's rows, interleaved in time so an unfiltered ORDER BY
	// would return them in the middle of the walk rather than after it.
	var theirs []string
	for i := range 10 {
		var id string
		s.factory.QueryRow(&id,
			`INSERT INTO users (org_id, email, status, created_at)
			 VALUES ($1, $2, 'active', $3) RETURNING id`,
			s.orgB, fmt.Sprintf("theirs-%03d@example.test", i),
			base.Add(time.Duration(i)*time.Second).Add(500*time.Millisecond))
		theirs = append(theirs, id)
	}

	seen := walk(t, p, 3)

	for _, id := range theirs {
		if seen[id] > 0 {
			t.Errorf("another tenant's row %s was returned", id)
		}
	}
	// The control: this walk did return something, so "saw none of theirs" is
	// not the vacuous result of seeing nothing at all.
	if len(seen) == 0 {
		t.Fatal("the walk returned no rows, so it proves nothing about isolation")
	}
}

// walk pages all the way through and counts what came back.
func walk(t *testing.T, p pager, size int) map[string]int {
	t.Helper()

	seen := map[string]int{}
	cursor := Cursor{}

	for range 100 { // bounded, so a broken cursor cannot loop forever
		page := p.page(t, cursor, size)
		for _, u := range page.Items {
			seen[u.id]++
		}
		if page.NextPageToken == "" {
			return seen
		}
		var err error
		if cursor, err = DecodeCursor(page.NextPageToken); err != nil {
			t.Fatalf("DecodeCursor: %v", err)
		}
	}

	t.Fatal("the walk did not terminate in 100 pages")
	return nil
}

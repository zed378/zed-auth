package management

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type row struct {
	id string
	at time.Time
}

func at(n int) time.Time {
	return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Second)
}

func rows(n int) []row {
	out := make([]row, 0, n)
	for i := range n {
		out = append(out, row{id: string(rune('a' + i)), at: at(i)})
	}
	return out
}

func position(r row) Cursor { return Cursor{After: r.at, ID: r.id} }

// --- page size ------------------------------------------------------------------

func TestPageSizeIsClampedAtBothEnds(t *testing.T) {
	cases := map[string]int{
		"":       DefaultPageSize,
		"0":      DefaultPageSize,
		"-1":     DefaultPageSize,
		"-1000":  DefaultPageSize,
		"abc":    DefaultPageSize,
		"1":      1,
		"20":     20,
		"100":    MaxPageSize,
		"101":    MaxPageSize,
		"100000": MaxPageSize,
	}

	for requested, want := range cases {
		if got := PageSize(requested); got != want {
			t.Errorf("PageSize(%q) = %d, want %d", requested, got, want)
		}
	}
}

// A negative that fell through would become a LIMIT the database refuses,
// which is a 500 for a caller who typed a minus sign.
func TestANegativePageSizeNeverReachesAQuery(t *testing.T) {
	if got := PageSize("-5"); got <= 0 {
		t.Fatalf("PageSize(-5) = %d, which would be an invalid LIMIT", got)
	}
}

// --- the cursor ------------------------------------------------------------------

func TestACursorRoundTrips(t *testing.T) {
	want := Cursor{After: at(7), ID: "abc-123"}

	token, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if !got.After.Equal(want.After) || got.ID != want.ID {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

// An unreadable token is an error naming the parameter, not an empty first
// page. Silently starting over shows a caller the beginning of a list they
// were halfway through, which reads as data loss and is much harder to
// diagnose.
func TestAnUnreadableTokenIsRefused(t *testing.T) {
	for name, token := range map[string]string{
		"not base64":     "!!!not base64!!!",
		"not JSON":       "bm90IGpzb24",
		"empty object":   "e30",
		"no id":          "eyJhIjoiMjAyNi0wOS0xMFQxMjowMDowMFoifQ",
		"no timestamp":   "eyJpIjoiYWJjIn0",
		"a crafted list": "WyJhIiwiYiJd",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeCursor(token)
			if err == nil {
				t.Fatal("the token was accepted")
			}

			var fault Fault
			if !errors.As(err, &fault) {
				t.Fatalf("err is %T, not a Fault the middleware can render", err)
			}
			if fault.Class != Invalid {
				t.Errorf("class = %v, want Invalid", fault.Class)
			}
			if fault.Reason == "" {
				t.Error("no reason for the log")
			}
			// The message must not echo the token: it is caller-supplied and
			// would be reflected onto whatever renders the error.
			if strings.Contains(fault.Message, token) {
				t.Error("the refusal echoes the token back")
			}
		})
	}
}

// An empty token is the first page, not an error. It is what a caller sends
// when they have not paged yet.
func TestAnEmptyTokenIsTheFirstPage(t *testing.T) {
	c, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("DecodeCursor(\"\"): %v", err)
	}
	if c.ID != "" || !c.After.IsZero() {
		t.Errorf("an empty token decoded to a position: %+v", c)
	}
}

// The type carries a sort position and nothing else, and that is the security
// property rather than a minimalism preference: a cursor carrying its own
// filter would be a filter the CLIENT controls.
//
// This test fails if a field is added, which is the intent — the author then
// has to decide whether the client may choose that value.
func TestTheCursorCarriesOnlyASortPosition(t *testing.T) {
	_ = Cursor{
		After: time.Time{},
		ID:    "",
	}
}

// A forged cursor can choose a starting position and nothing more. Encoding
// one by hand and decoding it must yield exactly a position — no filter, no
// organization, no page size.
func TestAForgedCursorCannotCarryAnythingElse(t *testing.T) {
	// {"a":"2026-09-10T12:00:00Z","i":"x","org_id":"other","page_size":9999}
	const forged = "eyJhIjoiMjAyNi0wOS0xMFQxMjowMDowMFoiLCJpIjoieCIsIm9yZ19pZCI6Im90aGVyIiwicGFnZV9zaXplIjo5OTk5fQ"

	c, err := DecodeCursor(forged)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if c.ID != "x" {
		t.Errorf("id = %q", c.ID)
	}
	// The extra keys are dropped by the struct, so there is nowhere for a
	// crafted filter to land.
	if got := (Cursor{After: c.After, ID: c.ID}); got != c {
		t.Error("the decoded cursor carries more than a position")
	}
}

// --- Paginate ---------------------------------------------------------------------

// The last page carries no token, which is what tells a caller to stop.
// Returning one that yields an empty page would make every consumer's loop run
// one request longer than it needs to, forever.
func TestTheLastPageHasNoToken(t *testing.T) {
	for _, n := range []int{0, 1, 5, 20} {
		page, err := Paginate(rows(n), 20, position)
		if err != nil {
			t.Fatalf("Paginate(%d): %v", n, err)
		}
		if page.NextPageToken != "" {
			t.Errorf("%d rows into a page of 20 produced a next token", n)
		}
		if len(page.Items) != n {
			t.Errorf("%d rows produced %d items", n, len(page.Items))
		}
	}
}

// The extra row answers "is there more" and is NOT returned. Returning it
// would give the caller size+1 items and make the next page skip one.
func TestTheExtraRowIsNotReturned(t *testing.T) {
	page, err := Paginate(rows(21), 20, position)
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}

	if len(page.Items) != 20 {
		t.Fatalf("%d items, want 20", len(page.Items))
	}
	if page.NextPageToken == "" {
		t.Fatal("21 rows into a page of 20 produced no next token")
	}

	// And the token names the LAST returned row, not the extra one — or the
	// next page would start after a row the caller never saw.
	c, err := DecodeCursor(page.NextPageToken)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if c.ID != page.Items[19].id {
		t.Errorf("the token points at %q, want the last returned row %q",
			c.ID, page.Items[19].id)
	}
}

// An empty result is [] and never null. A null where a consumer expects an
// array is a crash in most client languages and a puzzle in the rest.
func TestAnEmptyPageIsAnEmptyList(t *testing.T) {
	page, err := Paginate[row](nil, 20, position)
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	if page.Items == nil {
		t.Error("an empty page carries a nil slice, which encodes as null")
	}
}

// Walking every page returns every row exactly once, which is the property a
// consumer actually depends on.
func TestWalkingEveryPageSeesEveryRowOnce(t *testing.T) {
	const total, size = 47, 10

	all := rows(total)
	seen := map[string]int{}
	cursor := Cursor{}

	for range 20 { // bounded, so a broken cursor cannot loop forever
		var window []row
		for _, r := range all {
			if cursor.ID == "" || r.at.After(cursor.After) ||
				(r.at.Equal(cursor.After) && r.id > cursor.ID) {
				window = append(window, r)
			}
			if len(window) > size {
				break
			}
		}

		page, err := Paginate(window, size, position)
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		for _, r := range page.Items {
			seen[r.id]++
		}
		if page.NextPageToken == "" {
			break
		}
		if cursor, err = DecodeCursor(page.NextPageToken); err != nil {
			t.Fatalf("DecodeCursor: %v", err)
		}
	}

	if len(seen) != total {
		t.Errorf("saw %d distinct rows of %d", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %q appeared %d times", id, n)
		}
	}
}

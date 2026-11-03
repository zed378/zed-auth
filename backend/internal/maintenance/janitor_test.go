package maintenance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// The janitor's own behaviour needs no database.
//
// Whether it isolates a failing task, reports every table, and logs a success
// line when it removed nothing are all properties of this file and not of
// PostgreSQL. They live here rather than behind the `integration` build tag so
// they run on a machine with no Docker — which is what this package's whole
// subject matter is about: housekeeping that only appears to work.
//
// The SQL-level proofs — that a bare DELETE under instance scope silently
// removes nothing, and that the bound is real — genuinely need a database and
// stay in janitor_integration_test.go.

// One failing task does not stop the others.
//
// They are independent tables. A janitor that gave up on the first error would
// let one broken function stop every other table from being bounded, which is
// the same silent accumulation this package exists to prevent.
func TestOneFailingTaskDoesNotStopTheOthers(t *testing.T) {
	var ran []string

	janitor := &Janitor{
		Tasks: []Task{
			{
				Name:   "first",
				Before: func(now time.Time) time.Time { return now },
				Sweep: func(context.Context, time.Time, int) (int64, error) {
					ran = append(ran, "first")
					return 3, nil
				},
			},
			{
				Name:   "broken",
				Before: func(now time.Time) time.Time { return now },
				Sweep: func(context.Context, time.Time, int) (int64, error) {
					ran = append(ran, "broken")
					return 0, errors.New("this table's sweeper is broken")
				},
			},
			{
				Name:   "last",
				Before: func(now time.Time) time.Time { return now },
				Sweep: func(context.Context, time.Time, int) (int64, error) {
					ran = append(ran, "last")
					return 7, nil
				},
			},
		},
	}

	removed := janitor.Once(context.Background())

	if strings.Join(ran, ",") != "first,broken,last" {
		t.Errorf("tasks ran %v — a failure stopped the ones after it", ran)
	}
	if removed["first"] != 3 || removed["last"] != 7 {
		t.Errorf("counts %v, want first=3 last=7", removed)
	}
	// The failing table is reported as zero rather than omitted, so a caller
	// emitting one metric per table does not silently lose a series.
	if count, named := removed["broken"]; !named || count != 0 {
		t.Errorf("the failing table is reported as %v (named=%t), want 0 and named", count, named)
	}
}

// Every pass logs a success line, even one that removed nothing.
//
// BL-01's lesson, and the reason the session sweeper this janitor replaced was
// not good enough: it logged only when it deleted something, which made it
// silent when idle and silent when stopped. Those are the two states an
// operator most needs to tell apart.
func TestASweepThatRemovedNothingStillSaysSo(t *testing.T) {
	var out bytes.Buffer

	janitor := &Janitor{
		Log: slog.New(slog.NewTextHandler(&out, nil)),
		Tasks: []Task{{
			Name:   "quiet_table",
			Before: func(now time.Time) time.Time { return now },
			Sweep: func(context.Context, time.Time, int) (int64, error) {
				return 0, nil
			},
		}},
	}

	janitor.Once(context.Background())

	logged := out.String()
	if !strings.Contains(logged, "housekeeping swept") {
		t.Errorf("no success line was logged for a pass that removed nothing:\n%s", logged)
	}
	if !strings.Contains(logged, "quiet_table=0") {
		t.Errorf("the table's count is missing from the line:\n%s", logged)
	}
}

// A failing task is logged as an error, naming the table.
func TestAFailingSweepIsLoggedWithItsTable(t *testing.T) {
	var out bytes.Buffer

	janitor := &Janitor{
		Log: slog.New(slog.NewTextHandler(&out, nil)),
		Tasks: []Task{{
			Name:   "broken_table",
			Before: func(now time.Time) time.Time { return now },
			Sweep: func(context.Context, time.Time, int) (int64, error) {
				return 0, errors.New("nope")
			},
		}},
	}

	janitor.Once(context.Background())

	logged := out.String()
	if !strings.Contains(logged, "sweep failed") || !strings.Contains(logged, "broken_table") {
		t.Errorf("the failure does not name the table:\n%s", logged)
	}
	// And the pass still reports, so a run where one table failed is not
	// mistaken for a run that did not happen.
	if !strings.Contains(logged, "housekeeping swept") {
		t.Errorf("no success line after a partial failure:\n%s", logged)
	}
}

// Each task receives its own cutoff, not one shared clock reading.
//
// The whole point of Before being a function per task: an assertion id is
// deleted at expiry and a refresh token thirty days after it. A janitor that
// passed one cutoff to everything would either keep the SAML tables far too
// long or delete the token lineage far too early.
func TestEachTaskGetsItsOwnCutoff(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	seen := map[string]time.Time{}

	record := func(name string) func(context.Context, time.Time, int) (int64, error) {
		return func(_ context.Context, before time.Time, _ int) (int64, error) {
			seen[name] = before
			return 0, nil
		}
	}

	janitor := &Janitor{
		Now: func() time.Time { return now },
		Tasks: []Task{
			{Name: "at_expiry", Before: func(n time.Time) time.Time { return n }, Sweep: record("at_expiry")},
			{Name: "week", Before: func(n time.Time) time.Time { return n.Add(-7 * 24 * time.Hour) }, Sweep: record("week")},
			{Name: "month", Before: func(n time.Time) time.Time { return n.Add(-30 * 24 * time.Hour) }, Sweep: record("month")},
		},
	}

	janitor.Once(context.Background())

	if !seen["at_expiry"].Equal(now) {
		t.Errorf("at_expiry got %v, want %v", seen["at_expiry"], now)
	}
	if want := now.Add(-7 * 24 * time.Hour); !seen["week"].Equal(want) {
		t.Errorf("week got %v, want %v", seen["week"], want)
	}
	if want := now.Add(-30 * 24 * time.Hour); !seen["month"].Equal(want) {
		t.Errorf("month got %v, want %v", seen["month"], want)
	}

	// One reading of the clock for the whole pass, so two tasks with the same
	// retention cannot disagree about where the window ends.
	if !seen["at_expiry"].Equal(now) || seen["week"].Add(7*24*time.Hour) != seen["at_expiry"] {
		t.Error("the tasks were measured against different clock readings")
	}
}

// Every task is bounded by the same batch limit.
func TestEveryTaskIsBounded(t *testing.T) {
	var limits []int

	janitor := &Janitor{
		Tasks: []Task{
			{Name: "a", Before: func(n time.Time) time.Time { return n },
				Sweep: func(_ context.Context, _ time.Time, limit int) (int64, error) {
					limits = append(limits, limit)
					return 0, nil
				}},
			{Name: "b", Before: func(n time.Time) time.Time { return n },
				Sweep: func(_ context.Context, _ time.Time, limit int) (int64, error) {
					limits = append(limits, limit)
					return 0, nil
				}},
		},
	}

	janitor.Once(context.Background())

	for _, limit := range limits {
		if limit != Batch {
			t.Errorf("a task was given limit %d, want the bound %d — an unbounded sweep "+
				"takes a lock worth noticing on a table that grew for a month", limit, Batch)
		}
	}
	if len(limits) != 2 {
		t.Errorf("%d task(s) ran, want 2", len(limits))
	}
}

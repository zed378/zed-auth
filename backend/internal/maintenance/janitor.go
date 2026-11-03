// Package maintenance runs the housekeeping the database has no timer for.
//
// PostgreSQL has no TTL. Every `expires_at` in this schema is enforced on read
// — a session, a refresh token, a pending SAML request and an assertion id are
// each refused once expired — so deleting the row is not a control. It is the
// thing that stops the table growing without bound, which is a slow outage
// scheduled for whenever it stops fitting in memory.
//
// # Why this package exists rather than four goroutines in main
//
// Four tables had a sweeper nobody could call and one had none at all (BL-08).
// The failure was not that somebody forgot a line in main: it was that there
// was nowhere that owned the question "is everything with an expiry actually
// being swept?". A table added next year is added to a list here, in one place,
// next to the answer for every other table.
//
// # Two rules learned the hard way
//
// **It reports what it removed, and logs a success.** P0-20 found a backup
// timer that had been failing silently for days; the lesson recorded in
// BL-01 is that absence of success is the signal, not presence of failure. A
// sweeper that only logs when it errors is indistinguishable from one that
// stopped running.
//
// **Every statement goes through a bounded SECURITY DEFINER function.** These
// tables are under row-level security, a sweeper has no tenant to be, and
// under instance scope `current_org_id()` is NULL — so a plain DELETE reports
// success having removed nothing. That mistake is already in this repository's
// history twice, in reads, and it was written into two of these Prunes before
// anything called them.
package maintenance

import (
	"context"
	"log/slog"
	"time"
)

// Interval is how often the janitor runs.
//
// Hourly, matching the session sweep it replaces. Expiry is enforced on every
// read, so this is housekeeping and an hour is far more often than bounding a
// table requires.
const Interval = time.Hour

// Batch bounds one pass over one table.
//
// A thousand rows. The point of a bound is that a janitor waking up to a table
// that grew for a month does not take a lock worth noticing; the next tick
// takes the next thousand, and a table that is behind catches up over hours
// rather than blocking anything for minutes.
const Batch = 1000

// A Task is one table's housekeeping.
//
// `Before` is a function rather than a duration so each task can state its own
// retention against the same clock: an assertion id is deleted at expiry, a
// refresh token long after it, and the difference is a decision each task owns
// rather than a parameter the janitor guesses.
type Task struct {
	// Name appears in the log line, so it is the table rather than the
	// function: an operator reading "swept" wants to know what shrank.
	Name string

	// Before is the cutoff. Rows that expired after it are kept.
	Before func(now time.Time) time.Time

	// Sweep removes at most limit rows that expired before the cutoff, and
	// returns how many it removed.
	Sweep func(ctx context.Context, before time.Time, limit int) (int64, error)
}

// Janitor runs every task on a ticker.
type Janitor struct {
	Tasks []Task
	Log   *slog.Logger

	// Now is injectable so a test can stand at a chosen moment rather than
	// creating rows that expired in the past and hoping.
	Now func() time.Time
}

func (j *Janitor) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// Run sweeps on every tick until the context ends.
//
// It sweeps once immediately. A service restarted every few minutes by a
// crash loop would otherwise never reach the first tick, and a service
// restarted deliberately would leave an hour of accumulated rows for no
// reason.
func (j *Janitor) Run(ctx context.Context) {
	j.Once(ctx)

	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.Once(ctx)
		}
	}
}

// Once runs every task a single time and returns what each removed.
//
// One task failing does not stop the others. They are independent tables, and
// a sweeper that gave up on the first error would let one broken function stop
// every other table from being bounded — which is the same class of silent
// accumulation this package exists to prevent.
func (j *Janitor) Once(ctx context.Context) map[string]int64 {
	now := j.now()
	removed := make(map[string]int64, len(j.Tasks))

	for _, task := range j.Tasks {
		count, err := task.Sweep(ctx, task.Before(now), Batch)
		removed[task.Name] = count
		if err != nil {
			if j.Log != nil {
				j.Log.LogAttrs(ctx, slog.LevelError, "sweep failed",
					slog.String("table", task.Name),
					slog.String("error", err.Error()))
			}
			continue
		}
	}

	// One success line per pass, with every count, whether or not anything was
	// removed. A line that appeared only when rows were deleted would be
	// absent both when there is nothing to do and when the janitor has
	// stopped, and those are the two states an operator most needs to tell
	// apart (BL-01).
	if j.Log != nil {
		attrs := make([]slog.Attr, 0, len(removed)+1)
		attrs = append(attrs, slog.Int("tables", len(j.Tasks)))
		for _, task := range j.Tasks {
			attrs = append(attrs, slog.Int64(task.Name, removed[task.Name]))
		}
		j.Log.LogAttrs(ctx, slog.LevelInfo, "housekeeping swept", attrs...)
	}

	return removed
}

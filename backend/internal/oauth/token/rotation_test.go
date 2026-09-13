package token

import (
	"testing"
	"time"
)

// The retry test's truth table (P3-06).
//
// This is where the subtlety in the whole task lives. Reuse detection is easy;
// telling reuse apart from a client that retried after a lost response is not,
// and getting it wrong in the strict direction logs real users out until an
// operations team disables the protection — at which point the control is gone
// AND everybody believes it is there.
//
// So the conditions are enumerated rather than described.

func TestTheRetryWindowAdmitsOnlyAnUntouchedSuccessor(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	rotated := now.Add(-5 * time.Second)

	for _, tc := range []struct {
		name    string
		lineage Lineage
		want    bool
		why     string
	}{
		{
			name:    "a retry seconds after rotation, successor untouched",
			lineage: Lineage{ReplacedBy: "b", ReplacedAt: rotated},
			want:    true,
			why:     "the response never arrived; this is the only token the client holds",
		},
		{
			name:    "the successor has already been used",
			lineage: Lineage{ReplacedBy: "b", ReplacedAt: rotated, SuccessorSpent: true},
			want:    false,
			why:     "the client DID receive the successor, so this presentation is somebody else's copy",
		},
		{
			name:    "outside the grace window",
			lineage: Lineage{ReplacedBy: "b", ReplacedAt: now.Add(-2 * time.Minute)},
			want:    false,
			why:     "a retry follows its failure immediately; two minutes is a human or an attacker",
		},
		{
			name:    "exactly at the boundary",
			lineage: Lineage{ReplacedBy: "b", ReplacedAt: now.Add(-RotationGrace)},
			want:    true,
			why:     "the boundary is inclusive, so a clock that lands exactly on it is not a logout",
		},
		{
			name:    "one nanosecond past the boundary",
			lineage: Lineage{ReplacedBy: "b", ReplacedAt: now.Add(-RotationGrace - time.Nanosecond)},
			want:    false,
			why:     "the window has to end somewhere and this is where",
		},
		{
			name:    "the token was never rotated",
			lineage: Lineage{},
			want:    false,
			why:     "there is nothing to retry; this path is not reached for a live token",
		},
		{
			name:    "the family is already revoked",
			lineage: Lineage{ReplacedBy: "b", ReplacedAt: rotated, Revoked: true},
			want:    false,
			why:     "the kill already happened; nothing in a dead family is a legitimate retry",
		},
		{
			name:    "rotated, but no replacement timestamp",
			lineage: Lineage{ReplacedBy: "b"},
			want:    false,
			why:     "without a time there is no window, and defaulting to open would be a permanent hole",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.lineage.LegitimateRetry(now, RotationGrace)
			if got != tc.want {
				t.Errorf("LegitimateRetry = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// The grace window is short enough to be worth having.
//
// A bound nobody can state is a bound nobody maintains. This asserts the number
// rather than only using it, so widening it is a visible decision.
func TestTheGraceWindowIsShort(t *testing.T) {
	if RotationGrace > time.Minute {
		t.Errorf("RotationGrace is %s; a stolen token works for that long after rotation", RotationGrace)
	}
	if RotationGrace < 5*time.Second {
		t.Errorf("RotationGrace is %s, which is shorter than a retry after a timeout — "+
			"clients on slow networks would be logged out", RotationGrace)
	}
}

// Rotated is about the link, not about liveness.
func TestRotatedReportsTheLink(t *testing.T) {
	if (Lineage{}).Rotated() {
		t.Error("a token with no successor reports as rotated")
	}
	if !(Lineage{ReplacedBy: "b"}).Rotated() {
		t.Error("a token with a successor does not report as rotated")
	}
	// A revoked token that was never rotated is not reuse — it is a token that
	// was killed by something else, and answering "reuse" would raise an alert
	// with no theft behind it.
	if (Lineage{Revoked: true}).Rotated() {
		t.Error("a revoked but never-rotated token reports as rotated")
	}
}

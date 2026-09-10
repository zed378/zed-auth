package ratelimit

import (
	"strings"
	"testing"
	"time"
)

func noon() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

// --- the arithmetic -------------------------------------------------------------------

// The first request of a window spends one and reports the rest.
func TestTheFirstRequestSpendsOne(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}

	v := q.Decide(1, noon())
	if !v.Allowed {
		t.Error("the first request of a window was refused")
	}
	if v.Limit != 10 || v.Remaining != 9 {
		t.Errorf("limit %d remaining %d, want 10 and 9", v.Limit, v.Remaining)
	}
}

// The boundary is the whole point of a limit, and off-by-one here is the
// difference between honouring the documented number and being one short.
func TestTheLastAllowedRequestIsAllowed(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}

	if v := q.Decide(10, noon()); !v.Allowed {
		t.Error("the 10th request of a limit of 10 was refused")
	} else if v.Remaining != 0 {
		t.Errorf("remaining = %d after spending the last one, want 0", v.Remaining)
	}

	if v := q.Decide(11, noon()); v.Allowed {
		t.Error("the 11th request of a limit of 10 was allowed")
	}
}

// Remaining is clamped at zero. A client's own retries push the counter well
// past the limit, and reporting "-73 remaining" tells them how hard they have
// been hitting a wall rather than what they asked.
func TestRemainingIsNeverNegative(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}

	for _, count := range []int{11, 50, 1000} {
		if v := q.Decide(count, noon()); v.Remaining != 0 {
			t.Errorf("count %d reported %d remaining", count, v.Remaining)
		}
	}
}

// Retry-After is the time to the window's end, and only on a refusal.
func TestRetryAfterIsTheTimeLeftInTheWindow(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}

	// Twenty seconds into the minute: forty left.
	at := noon().Add(20 * time.Second)
	v := q.Decide(11, at)

	if v.RetryAfter != 40*time.Second {
		t.Errorf("RetryAfter = %v, want 40s", v.RetryAfter)
	}
	if !v.Reset.Equal(noon().Add(time.Minute)) {
		t.Errorf("Reset = %v, want the top of the next minute", v.Reset)
	}

	// And an allowed request carries none: a client told to wait when it was
	// not refused would pace itself into a limit it never hit.
	if allowed := q.Decide(1, at); allowed.RetryAfter != 0 {
		t.Errorf("an allowed request reported RetryAfter = %v", allowed.RetryAfter)
	}
}

// Reset names the same moment for every request in a window, which is what
// makes it something a client can act on.
func TestResetIsStableAcrossAWindow(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}
	want := noon().Add(time.Minute)

	for _, offset := range []time.Duration{0, time.Second, 30 * time.Second, 59 * time.Second} {
		if got := q.Decide(1, noon().Add(offset)).Reset; !got.Equal(want) {
			t.Errorf("at +%v, Reset = %v, want %v", offset, got, want)
		}
	}

	// And it moves on at the boundary, or the window never turns over.
	if got := q.Decide(1, noon().Add(time.Minute)).Reset; !got.Equal(noon().Add(2 * time.Minute)) {
		t.Errorf("Reset at the boundary = %v", got)
	}
}

// --- the key --------------------------------------------------------------------------

// A new window is a new key, which is what makes expiry self-correcting: there
// is no reset step to race with and no counter left holding a stale total.
func TestEachWindowGetsItsOwnKey(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}

	first := ClientKey("client-a", q.WindowStart(noon()))
	same := ClientKey("client-a", q.WindowStart(noon().Add(59*time.Second)))
	next := ClientKey("client-a", q.WindowStart(noon().Add(time.Minute)))

	if first != same {
		t.Errorf("two requests in one window used different keys:\n  %s\n  %s", first, same)
	}
	if first == next {
		t.Error("the next window reuses the previous window's counter")
	}
}

// Two clients never share a counter. Sharing one would let any client exhaust
// every other client's allowance, which turns a rate limit into a denial of
// service anybody can trigger.
func TestClientsDoNotShareACounter(t *testing.T) {
	q := Quota{Limit: 10, Window: time.Minute}
	start := q.WindowStart(noon())

	if ClientKey("client-a", start) == ClientKey("client-b", start) {
		t.Fatal("two clients share a counter")
	}
}

// The key must name the client, or every "per-client" claim in the spec is
// false. Asserting the shape rather than only inequality is what catches a key
// that is distinct for the wrong reason.
func TestTheKeyNamesTheClient(t *testing.T) {
	key := ClientKey("client-a", Quota{Window: time.Minute}.WindowStart(noon()))

	if !strings.Contains(key, "client-a") {
		t.Errorf("key = %q, which does not identify the client", key)
	}
	if !strings.HasPrefix(key, "ratelimit:") {
		t.Errorf("key = %q, outside the limiter's namespace", key)
	}
}

// No client, no key. The caller decides what an unidentified request means;
// this package must not answer it by inventing a shared bucket that every such
// request would then contend on.
func TestNoClientMeansNoKey(t *testing.T) {
	if got := ClientKey("", noon()); got != "" {
		t.Errorf("ClientKey(\"\") = %q", got)
	}
}

// --- failing open --------------------------------------------------------------------

// The unavailable verdict reports the FULL allowance rather than a fabricated
// remainder. A client pacing itself against a number this service made up is
// pacing against fiction.
func TestTheUnavailableVerdictDoesNotInventARemainder(t *testing.T) {
	q := Quota{Limit: 600, Window: time.Minute}

	v := q.Unlimited(noon())
	if !v.Allowed {
		t.Error("the fail-open verdict refuses — that is failing closed")
	}
	if v.Remaining != v.Limit {
		t.Errorf("remaining %d of %d: a count that never happened was reported as spent",
			v.Remaining, v.Limit)
	}
	if v.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v on an allowed request", v.RetryAfter)
	}
}

// --- the configured bound --------------------------------------------------------------

// The shipped policy is a real bound, not a placeholder that allows everything.
func TestThePerClientPolicyIsABound(t *testing.T) {
	if PerClient.Limit <= 0 {
		t.Fatalf("PerClient.Limit = %d, which bounds nothing", PerClient.Limit)
	}
	if PerClient.Window <= 0 {
		t.Fatalf("PerClient.Window = %v", PerClient.Window)
	}
	if v := PerClient.Decide(PerClient.Limit+1, noon()); v.Allowed {
		t.Error("a request past PerClient.Limit is allowed")
	}
}

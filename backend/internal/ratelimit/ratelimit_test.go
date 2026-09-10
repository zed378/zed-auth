package ratelimit

import (
	"testing"
	"time"
)

// Evaluate and Next are pure, so the whole progression can be walked
// exhaustively without Redis, a clock, or a request. Everything that decides
// whether somebody is locked out is here; the store only remembers State.

func at(minutes int) time.Time {
	return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

// walk runs n consecutive failures and returns the state after each.
func walk(p Policy, n int, now time.Time) []State {
	var (
		s   State
		out []State
	)
	for range n {
		if Evaluate(s, p, now).Allowed {
			s = Next(s, p, now)
		}
		out = append(out, s)
	}
	return out
}

// FR-3 and the number that matters most: a person who mistypes their password
// twice must notice NOTHING.
func TestTheFirstFailuresAreFree(t *testing.T) {
	now := at(0)
	var s State

	for attempt := 1; attempt <= PerAddress.Free; attempt++ {
		if d := Evaluate(s, PerAddress, now); !d.Allowed {
			t.Fatalf("attempt %d was refused; %d are meant to be free", attempt, PerAddress.Free)
		}
		s = Next(s, PerAddress, now)

		if !s.Until.IsZero() {
			t.Fatalf("a cooldown started after %d failures, within the free allowance", attempt)
		}
	}
}

// The allowance is exactly Free, not Free plus or minus one. Off-by-one here
// is the difference between "five free attempts" being true and being a claim
// nobody checked.
func TestTheAllowanceIsExactlyFree(t *testing.T) {
	now := at(0)
	states := walk(PerAddress, PerAddress.Free+1, now)

	if !states[PerAddress.Free-1].Until.IsZero() {
		t.Errorf("a cooldown started at failure %d, inside the allowance", PerAddress.Free)
	}
	if states[PerAddress.Free].Until.IsZero() {
		t.Errorf("no cooldown after %d failures, which is one past the allowance",
			PerAddress.Free+1)
	}
}

// FR-1: a cooldown, never a permanent lockout. Whatever an attacker does, the
// state clears on its own — that is what stops a brute-force attempt being
// convertible into a denial of service against the victim.
func TestTheCooldownAlwaysClearsOnItsOwn(t *testing.T) {
	now := at(0)
	var s State

	// Twenty rounds of relentless failure.
	for range 20 {
		if Evaluate(s, PerAddress, now).Allowed {
			s = Next(s, PerAddress, now)
		}
		if !s.Until.IsZero() {
			now = s.Until // wait exactly as long as told
		}
	}

	if d := Evaluate(s, PerAddress, now); !d.Allowed {
		t.Fatalf("after waiting out every cooldown the key is still refused for %s", d.RetryAfter)
	}
	if s.Until.Sub(now) > PerAddress.Cap {
		t.Errorf("a cooldown of %s exceeds the cap of %s", s.Until.Sub(now), PerAddress.Cap)
	}
}

// FR-3: increasing, and capped. Uncapped doubling reaches days within a dozen
// rounds, which is a permanent lockout with extra steps.
func TestTheCooldownGrowsAndIsCapped(t *testing.T) {
	var (
		now       = at(0)
		s         State
		durations []time.Duration
	)

	for range 8 {
		for Evaluate(s, PerAddress, now).Allowed {
			s = Next(s, PerAddress, now)
			if !s.Until.IsZero() {
				break
			}
		}
		d := s.Until.Sub(now)
		durations = append(durations, d)
		now = s.Until
	}

	for i := 1; i < len(durations); i++ {
		if durations[i] < durations[i-1] {
			t.Errorf("cooldown %d (%s) is shorter than %d (%s)",
				i+1, durations[i], i, durations[i-1])
		}
		if durations[i] > PerAddress.Cap {
			t.Errorf("cooldown %d is %s, above the cap of %s", i+1, durations[i], PerAddress.Cap)
		}
	}
	if durations[0] != PerAddress.Base {
		t.Errorf("the first cooldown is %s, want the base of %s", durations[0], PerAddress.Base)
	}
	if durations[len(durations)-1] != PerAddress.Cap {
		t.Errorf("the cooldown never reached the cap: %s", durations[len(durations)-1])
	}
}

// A refused attempt must NOT advance the counter. If it did, somebody who kept
// knocking during a cooldown would extend it indefinitely — a permanent
// lockout arrived at by accident, which is exactly what docs/PLAN/05 forbids
// and what an attacker targeting a victim would do on purpose.
func TestKnockingDuringACooldownDoesNotExtendIt(t *testing.T) {
	now := at(0)
	var s State

	for Evaluate(s, PerAddress, now).Allowed {
		s = Next(s, PerAddress, now)
	}
	until := s.Until
	if until.IsZero() {
		t.Fatal("no cooldown was reached")
	}

	// A hundred more attempts while it runs.
	for range 100 {
		if d := Evaluate(s, PerAddress, now); d.Allowed {
			t.Fatal("an attempt was allowed during a cooldown")
		}
		s = Next(s, PerAddress, now)
	}

	if !s.Until.Equal(until) {
		t.Errorf("the cooldown moved from %s to %s while being knocked on", until, s.Until)
	}
}

// The refusal reports how long is left, which is what the login page tells the
// user. Without it the message would be "try later" with no idea how much
// later, which is the kind of message people retry through.
func TestARefusalSaysHowLongIsLeft(t *testing.T) {
	now := at(0)
	var s State
	for Evaluate(s, PerAddress, now).Allowed {
		s = Next(s, PerAddress, now)
	}

	d := Evaluate(s, PerAddress, now)
	if d.Allowed {
		t.Fatal("no cooldown")
	}
	if d.RetryAfter <= 0 || d.RetryAfter > PerAddress.Cap {
		t.Errorf("RetryAfter = %s", d.RetryAfter)
	}
}

// Waiting out a cooldown returns a fresh allowance, so a user who gets it
// right on the next try is not immediately refused again.
func TestAFreshAllowanceFollowsACooldown(t *testing.T) {
	now := at(0)
	var s State
	for Evaluate(s, PerAddress, now).Allowed {
		s = Next(s, PerAddress, now)
	}

	now = s.Until.Add(time.Second)

	if d := Evaluate(s, PerAddress, now); !d.Allowed {
		t.Fatalf("still refused after the cooldown expired: %s", d.RetryAfter)
	}
	// And the next failure does not immediately re-trigger, because the
	// counter restarted with the cooldown.
	next := Next(s, PerAddress, now)
	if !next.Until.IsZero() && next.Until.After(now) {
		t.Error("one failure after a cooldown started another immediately")
	}
}

// The two bounds differ only in their numbers, and the IP one is looser
// because one address legitimately serves many people.
func TestTheIPBoundIsLooserThanTheAddressBound(t *testing.T) {
	if PerIP.Free <= PerAddress.Free {
		t.Errorf("PerIP.Free = %d and PerAddress.Free = %d; one IP serves many "+
			"users behind a NAT and must not be the tighter bound",
			PerIP.Free, PerAddress.Free)
	}
}

// TTL must outlive whatever is being remembered, or a cooldown would vanish
// with its key and the attacker would get a fresh allowance by waiting less
// than they were told.
func TestTheKeyOutlivesWhatItRemembers(t *testing.T) {
	now := at(0)
	var s State
	for Evaluate(s, PerAddress, now).Allowed {
		s = Next(s, PerAddress, now)
	}

	if ttl := TTL(s, PerAddress, now); ttl < s.Until.Sub(now) {
		t.Errorf("TTL is %s but the cooldown runs for %s; the key would expire first",
			ttl, s.Until.Sub(now))
	}
	if ttl := TTL(State{}, PerAddress, now); ttl != PerAddress.Window {
		t.Errorf("TTL with no cooldown is %s, want the window %s", ttl, PerAddress.Window)
	}
}

// --- the key ------------------------------------------------------------------------

// The choice this whole feature rests on. A counter keyed on a user id would
// only exist for addresses that have accounts, so a "too many attempts"
// refusal would confirm the account exists — and P1-12's enumeration defence
// would be undone by its own rate limiter.
func TestTheAddressKeyDoesNotDependOnAnAccountExisting(t *testing.T) {
	real := AddressKey("alice@example.test")
	invented := AddressKey("nobody-at-all@example.test")

	if real == invented {
		t.Fatal("two different addresses share a key")
	}
	// Both are well-formed keys of the same shape. Nothing about either says
	// whether an account is behind it, because nothing here has looked.
	for _, key := range []string{real, invented} {
		if len(key) <= len("ratelimit:login:address:") {
			t.Errorf("key %q is empty", key)
		}
	}
}

// Normalised the same way authn compares addresses, or "Alice@..." and
// "alice@..." would be two allowances for one account.
func TestTheAddressKeyIsNormalised(t *testing.T) {
	if AddressKey("  Alice@Example.TEST ") != AddressKey("alice@example.test") {
		t.Error("case and whitespace produce different keys, which doubles the allowance")
	}
}

func TestTheKeysAreDistinctNamespaces(t *testing.T) {
	if AddressKey("1.2.3.4") == IPKey("1.2.3.4") {
		t.Error("an address and an IP with the same text share a counter")
	}
}

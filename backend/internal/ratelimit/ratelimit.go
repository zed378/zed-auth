// Package ratelimit bounds how often a credential may be guessed.
//
// docs/PLAN/17 § Phase 1 makes this an acceptance criterion — "a simulated
// brute-force attempt is demonstrably blocked" — and docs/PLAN/05 constrains
// the shape of the answer: a COOLDOWN, never a permanent lockout. A permanent
// lockout converts a brute-force attempt into a denial of service against the
// victim, which is a cheaper win than the one the attacker was going for.
//
// The decision that makes the rest of this safe is what gets counted. See Key.
//
// Specification: MEMORY/specs/P1-13-rate-limiting.md.
package ratelimit

import (
	"strings"
	"time"
)

// Policy is one bound.
//
// Two are configured: a tight one per submitted address, and a loose one per
// client IP. They are the same mechanism with different numbers, because the
// difference between them is entirely a question of how many legitimate people
// share the key.
type Policy struct {
	// Free is how many failures cost nothing. A person who mistypes their
	// password twice must notice nothing at all.
	Free int

	// Window is how long failures are remembered. Counting forever would turn
	// a bad morning six months ago into a lockout today.
	Window time.Duration

	// Base is the first cooldown, and Cap the longest. The cooldown doubles
	// per consecutive round, so a determined attacker reaches the cap quickly
	// while somebody who eventually remembers their password pays Base once.
	Base time.Duration
	Cap  time.Duration
}

// PerAddress is the tight bound, keyed on the submitted address.
//
// Five is chosen against a person rather than against an attacker: three
// attempts is a normal bad morning, five is unusual, and the cost of being
// wrong is a one-minute wait rather than a lockout.
var PerAddress = Policy{
	Free:   5,
	Window: 15 * time.Minute,
	Base:   time.Minute,
	Cap:    15 * time.Minute,
}

// PerIP is the loose bound.
//
// Ten times the per-address allowance, because one IP legitimately serves many
// users behind a corporate NAT and punishing them for sharing an office is not
// a security control. It is deliberately not the thing that stops a targeted
// attack — PerAddress is — its job is to make a wide credential-stuffing run
// expensive.
var PerIP = Policy{
	Free:   50,
	Window: 15 * time.Minute,
	Base:   time.Minute,
	Cap:    15 * time.Minute,
}

// State is what the store remembers about one key.
type State struct {
	// Failures within the window.
	Failures int

	// Rounds is how many cooldowns this key has already served. It is what
	// makes the delay progressive, and it is separate from Failures because
	// Failures is reset by each cooldown and Rounds is not.
	Rounds int

	// Until is when the current cooldown ends. Zero means none is running.
	Until time.Time
}

// Decision is what the caller should do.
type Decision struct {
	// Allowed is whether the attempt may proceed to a password check.
	Allowed bool

	// RetryAfter is how long is left of the cooldown. Zero when allowed.
	RetryAfter time.Duration

	// StartsCooldown is true on the attempt that BEGINS a cooldown, and false
	// for every refusal during it. It is what stops one audit entry per
	// request: an attacker who could write to an append-only table as fast as
	// they can send requests has been handed a different attack.
	StartsCooldown bool
}

// Evaluate decides, from state alone.
//
// A pure function of (state, policy, now), so the progression can be tested
// exhaustively without Redis, a clock, or a request. The store's job is to
// hold State; every rule about what State means is here.
func Evaluate(s State, p Policy, now time.Time) Decision {
	if !s.Until.IsZero() && now.Before(s.Until) {
		return Decision{Allowed: false, RetryAfter: s.Until.Sub(now)}
	}

	// A running cooldown is the ONLY thing that refuses an attempt.
	//
	// The free allowance is not checked here, and an earlier version of this
	// function did check it — pointlessly, because both branches returned
	// Allowed and a mutation flipping the comparison changed nothing. That
	// dead branch read like a control and was not one.
	//
	// The allowance belongs in Next, and the reason is worth keeping: the
	// attempt that SPENDS the last free failure must still be tried. Refusing
	// it here would mean the fifth of five free attempts is never actually
	// made, which turns "five free attempts" into a claim that is off by one.
	return Decision{Allowed: true}
}

// Next returns the state after a failed attempt.
//
// Separate from Evaluate because they answer different questions and are
// called at different moments: Evaluate runs before the password check, Next
// after it fails. Folding them together would mean an attempt that is refused
// still advances the counter, so a cooldown could never expire while somebody
// kept knocking — a permanent lockout arrived at by accident, which is exactly
// what docs/PLAN/05 forbids.
func Next(s State, p Policy, now time.Time) State {
	if !s.Until.IsZero() && now.Before(s.Until) {
		// Already cooling down. The attempt was refused, so it does not count.
		return s
	}

	next := State{Failures: s.Failures + 1, Rounds: s.Rounds}

	if next.Failures > p.Free {
		next.Rounds = s.Rounds + 1
		next.Until = now.Add(backoff(p, next.Rounds))
		// The counter restarts with the cooldown: the next round's allowance
		// is fresh, and it is Rounds that makes each one longer.
		next.Failures = 0
	}

	return next
}

// backoff doubles per round, up to the cap.
func backoff(p Policy, rounds int) time.Duration {
	d := p.Base
	for i := 1; i < rounds; i++ {
		d *= 2
		if d >= p.Cap {
			return p.Cap
		}
	}
	if d > p.Cap {
		return p.Cap
	}
	return d
}

// TTL is how long a key is worth remembering.
//
// The longer of the window and any running cooldown, so a key expires on its
// own and no cleanup job can fail silently.
func TTL(s State, p Policy, now time.Time) time.Duration {
	ttl := p.Window
	if !s.Until.IsZero() {
		if remaining := s.Until.Sub(now); remaining > ttl {
			ttl = remaining
		}
	}
	return ttl
}

// AddressKey is the per-address counter's key.
//
// **Keyed on the SUBMITTED address, never on a resolved user id**, and that
// single choice is what makes this feature safe to be honest about.
//
// A counter keyed on a user id would only exist for addresses that have
// accounts, so "too many attempts" would confirm the account exists — and the
// enumeration defence P1-12 spends its entire design on would be undone by its
// own rate limiter.
//
// Keyed on the submitted address, the counter exists for nobody@example.test
// exactly as it does for a real user. A refusal then tells an attacker only
// about their own behaviour, which they already know, because they produced
// it. That is what lets the login page say something true and useful instead
// of forcing the uniform credential message onto somebody who has done nothing
// wrong and would otherwise keep retrying.
func AddressKey(address string) string {
	return "ratelimit:login:address:" + strings.ToLower(strings.TrimSpace(address))
}

// IPKey is the per-IP counter's key.
func IPKey(ip string) string {
	return "ratelimit:login:ip:" + ip
}

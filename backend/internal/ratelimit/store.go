package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// The Redis side.
//
// docs/PLAN/04 § What Is Deliberately Not Stored Here puts these counters in
// Redis: they are short-lived, they must expire on their own, and a cleanup
// job that fails silently is worse than a TTL. Nothing here is durable and
// nothing here should be — a durable record of which addresses somebody tried
// is the artefact an attacker who reaches the database most wants.

// ErrUnavailable means Redis could not answer.
//
// Named rather than wrapped generically because the caller's response to it is
// a policy decision, not an error path: see the Limiter's doc comment.
var ErrUnavailable = errors.New("ratelimit: the counter store is unavailable")

// Observer reports what the limiter did.
type Observer interface {
	// Refused counts a refused attempt, by which bound refused it.
	Refused(bound string)

	// Unavailable counts a decision made without the store.
	//
	// This is the metric that makes fail-open safe to choose: an outage that
	// silently removed the control is not possible while somebody is looking
	// at this number.
	Unavailable()
}

// Limiter reads and writes counters.
//
// **It fails open, loudly** (ADR-017). Failing closed would mean a Redis
// outage stops every login in the estate — and Redis already backs the session
// cache, so an outage is already degrading. Turning it into "nobody can
// authenticate at all" converts a cache failure into a total authentication
// outage whose blast radius is every user of every application.
//
// What makes failing open tolerable is a floor that does not depend on Redis:
// Argon2id at the current parameters costs 50-100ms of CPU per attempt
// (P1-01), so the service's own CPU bounds an attacker far below what an
// offline attack achieves. This limiter raises that floor; it is not the only
// thing holding it.
type Limiter struct {
	client   redis.UniversalClient
	observer Observer
	log      *slog.Logger
}

func New(client redis.UniversalClient, observer Observer, log *slog.Logger) *Limiter {
	return &Limiter{client: client, observer: observer, log: log}
}

// Bound names which policy refused an attempt, for the metric and the log.
const (
	BoundAddress = "address"
	BoundIP      = "ip"
)

// Check decides whether an attempt may proceed.
//
// Both bounds are consulted and the ADDRESS is checked first, deliberately: it
// is the tight one, it is the one that stops a targeted attack, and reporting
// it rather than the IP bound gives the operator the more specific fact.
func (l *Limiter) Check(ctx context.Context, address, ip string, now time.Time) (Decision, string) {
	for _, bound := range []struct {
		name   string
		key    string
		policy Policy
	}{
		{BoundAddress, AddressKey(address), PerAddress},
		{BoundIP, IPKey(ip), PerIP},
	} {
		if bound.key == "" || ip == "" && bound.name == BoundIP {
			continue
		}

		state, err := l.load(ctx, bound.key)
		if err != nil {
			l.unavailable("checking", err)
			return Decision{Allowed: true}, ""
		}

		if d := Evaluate(state, bound.policy, now); !d.Allowed {
			if l.observer != nil {
				l.observer.Refused(bound.name)
			}
			return d, bound.name
		}
	}

	return Decision{Allowed: true}, ""
}

// Fail records a failed attempt and reports whether it started a cooldown.
//
// The returned bool is what the caller audits on. One entry per cooldown, not
// one per attempt: auditing every refusal would let an attacker write to an
// append-only table as fast as they can send requests.
func (l *Limiter) Fail(ctx context.Context, address, ip string, now time.Time) (bool, string) {
	started := false
	which := ""

	for _, bound := range []struct {
		name   string
		key    string
		policy Policy
	}{
		{BoundAddress, AddressKey(address), PerAddress},
		{BoundIP, IPKey(ip), PerIP},
	} {
		if bound.key == "" || ip == "" && bound.name == BoundIP {
			continue
		}

		state, err := l.load(ctx, bound.key)
		if err != nil {
			l.unavailable("recording a failure", err)
			continue
		}

		next := Next(state, bound.policy, now)
		if err := l.save(ctx, bound.key, next, TTL(next, bound.policy, now)); err != nil {
			l.unavailable("recording a failure", err)
			continue
		}

		// A cooldown that was not running before and is now.
		if state.Until.Before(now) && !next.Until.IsZero() && next.Until.After(now) && !started {
			started = true
			which = bound.name
		}
	}

	return started, which
}

// Succeed clears the counters for a successful authentication.
//
// FR-9. The address counter is cleared because the person proved they are who
// they said; the IP counter is NOT, because one success from an office does
// not vouch for the other four hundred attempts coming from it, and clearing
// it would hand an attacker a reset button: one valid credential of their own
// would clear the bound for everybody sharing that address.
func (l *Limiter) Succeed(ctx context.Context, address string) {
	if address == "" {
		return
	}
	if err := l.client.Del(ctx, AddressKey(address)).Err(); err != nil {
		l.unavailable("clearing a counter", err)
	}
}

func (l *Limiter) load(ctx context.Context, key string) (State, error) {
	raw, err := l.client.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return State{}, nil
	case err != nil:
		return State{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		// A value we cannot read is treated as no value. It expires shortly
		// anyway, and refusing logins over a corrupt counter would be the
		// fail-closed behaviour this design rejected.
		return State{}, nil
	}
	return s, nil
}

func (l *Limiter) save(ctx context.Context, key string, s State, ttl time.Duration) error {
	encoded, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("ratelimit: encoding state: %w", err)
	}
	if err := l.client.Set(ctx, key, encoded, ttl).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// unavailable is the loud half of "fail open, loudly".
//
// A WARN per occurrence and a metric, so an outage that removed the control
// cannot pass unnoticed. The key is never logged: it contains the address
// somebody submitted.
func (l *Limiter) unavailable(doing string, err error) {
	if l.observer != nil {
		l.observer.Unavailable()
	}
	if l.log != nil {
		l.log.Warn("the rate limiter could not reach its store; logins are proceeding unlimited",
			"doing", doing, "error", err.Error())
	}
}

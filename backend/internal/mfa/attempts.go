package mfa

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Bounding code guesses across challenges, not just within one (P3-03 step 2).
//
// `MaxAttempts` bounds one challenge to five guesses. On its own that is not a
// bound at all for the attacker this control is for: somebody holding the
// password can spend five guesses, restart the login — they have the password,
// so restarting is free — and spend five more, for as long as they like. The
// per-challenge limit costs them a round trip and nothing else.
//
// The keyspace is the reason it matters. Six digits is 10^6, and the accepted
// window is three steps wide (`TOTPSkew` each way), so a single guess lands
// with probability 3/10^6. That is small per guess and not small per million.
//
// So the bound has to be on the USER, across every challenge, and it has to
// outlive the challenge that produced it.

// AttemptWindow and MaxAttemptsPerWindow are the per-user bound.
//
// Ten guesses per fifteen minutes. The arithmetic, stated rather than left for
// somebody to work out later:
//
//	10 guesses / 15 min  =  960 / day
//	960 × 3/10^6         ≈  0.3% chance per day of one guess landing
//
// That residual is real and it is not zero, which is why this is a bound and
// not a proof. It is also the bound acting on an attacker who ALREADY HAS THE
// PASSWORD — without one they are guessing both, and with one every failure
// here writes `user.mfa.failed`, which is the log line that says a credential
// is already lost.
//
// Ten rather than five, because a legitimate user gets five per challenge: one
// full challenge plus one restart is the honest failure case — an authenticator
// app on a phone whose clock had drifted, corrected, and tried again. Five
// would make a single restart the limit.
//
// **Cooldown, not lockout**, which `MaxAttempts` argues for at the challenge
// level and which applies with more force here: a bound that locked the account
// would let anybody holding a password deny its owner their own account, and
// the population that has the strongest reason to enrol a factor is exactly the
// population whose password somebody might have.
const (
	AttemptWindow        = 15 * time.Minute
	MaxAttemptsPerWindow = 10
)

// AttemptBound counts a user's failed factor guesses.
//
// An interface so the framework can be tested without Redis, and so this is a
// seam a future per-organization policy can widen (`P3-07`) without the
// framework knowing.
type AttemptBound interface {
	// Fail records one failed guess and reports whether the user may make
	// another.
	//
	// Only failures are counted. A correct code spends nothing, so a user who
	// signs in successfully every day never approaches the bound, and the
	// counter measures only what it is for.
	Fail(ctx context.Context, userID string, now time.Time) (allowed bool, err error)

	// Allowed reports whether a guess may be made at all, without counting
	// one. Read before the HMAC, so an exhausted user costs no verification
	// work.
	Allowed(ctx context.Context, userID string, now time.Time) (bool, error)
}

// RedisAttempts is the AttemptBound backed by the same Redis the challenge
// store uses.
//
// **Deliberately the same store**, and it is worth saying why rather than
// leaving it as an accident of wiring: this bound FAILS CLOSED, which is the
// opposite of `ratelimit.Quotas` and would normally be a hard call, because
// failing closed means a Redis outage stops people signing in.
//
// Here it costs nothing to decide. The challenge itself lives in Redis, so a
// Redis that cannot answer this cannot produce or read a challenge either —
// the login has already failed by the time this refuses. Failing closed simply
// makes the refusal say what it is, rather than letting an outage quietly
// remove the only bound on guessing a six-digit number.
type RedisAttempts struct {
	Client redis.UniversalClient
}

// attemptKey is one user's counter for one window.
//
// The window start is in the key, the same shape `ratelimit.ClientKey` uses
// and for the same reason: each window is a new key that expires on its own,
// so there is no reset step to race with and no counter that can be left
// holding a stale total.
//
// The USER ID, never the submitted email address. An address is attacker-
// supplied and an attacker can spell one many ways; by this point in the flow a
// password has been proven, so the user is resolved and there is no reason to
// key on anything weaker.
func attemptKey(userID string, windowStart time.Time) string {
	if userID == "" {
		return ""
	}
	return "mfa:attempts:" + userID + ":" + strconv.FormatInt(windowStart.Unix(), 10)
}

// countAndExpire increments and sets the expiry on the increment that creates
// the key, in one call.
//
// Two round trips would leave a window where a process death produces a counter
// with no expiry — a user bounded forever by a total that never resets. The
// same script `ratelimit` uses, duplicated rather than imported because
// importing it would make this package depend on the Management API's rate
// limiter for a control that has nothing to do with it.
var countAndExpire = redis.NewScript(`
	local count = redis.call("INCR", KEYS[1])
	if count == 1 then
		redis.call("PEXPIRE", KEYS[1], ARGV[1])
	end
	return count
`)

func (a *RedisAttempts) Fail(ctx context.Context, userID string, now time.Time) (bool, error) {
	key := attemptKey(userID, now.Truncate(AttemptWindow))
	if key == "" || a == nil || a.Client == nil {
		return false, fmt.Errorf("mfa: the attempt bound is not configured")
	}

	ttl := now.Truncate(AttemptWindow).Add(AttemptWindow + time.Second).Sub(now)

	count, err := countAndExpire.Run(ctx, a.Client, []string{key}, ttl.Milliseconds()).Int()
	if err != nil {
		return false, fmt.Errorf("mfa: counting a failed factor attempt: %w", err)
	}
	return count < MaxAttemptsPerWindow, nil
}

func (a *RedisAttempts) Allowed(ctx context.Context, userID string, now time.Time) (bool, error) {
	key := attemptKey(userID, now.Truncate(AttemptWindow))
	if key == "" || a == nil || a.Client == nil {
		return false, fmt.Errorf("mfa: the attempt bound is not configured")
	}

	count, err := a.Client.Get(ctx, key).Int()
	switch {
	case err == redis.Nil:
		// No failures in this window.
		return true, nil
	case err != nil:
		return false, fmt.Errorf("mfa: reading the factor attempt count: %w", err)
	}
	return count < MaxAttemptsPerWindow, nil
}

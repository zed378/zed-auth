//go:build integration

// The per-user attempt bound against real Redis (P3-03 step 2).
//
// It cannot be tested against a fake and mean anything: the whole bound is an
// atomic INCR with the expiry set on the increment that creates the key, and
// what makes it a bound rather than a suggestion is that two concurrent
// failures cannot both read the same count. A map with a mutex would pass every
// assertion below while proving nothing about the thing that ships.
package mfa

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func redisAttempts(t *testing.T) (*RedisAttempts, redis.UniversalClient) {
	t.Helper()

	stack := testsupport.Start(t)
	client := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = client.Close() })

	if err := client.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}
	return &RedisAttempts{Client: client}, client
}

// The bound survives the challenge that produced it.
//
// This is the whole reason it exists. MaxAttempts caps one challenge at five;
// an attacker who holds the password abandons it and starts another for free,
// so a bound that reset with the challenge would cap nothing at all.
func TestTheAttemptBoundSurvivesTheChallengeThatProducedIt(t *testing.T) {
	attempts, _ := redisAttempts(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	const user = "44444444-4444-4444-4444-444444444444"

	// Five failures — one challenge's worth. The user may still guess.
	for i := 0; i < 5; i++ {
		allowed, err := attempts.Fail(ctx, user, now)
		if err != nil {
			t.Fatalf("Fail %d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("refused after %d failures; the per-challenge limit is 5 and this bound is wider", i+1)
		}
	}

	// A second challenge starts. The count does NOT reset — there is nothing
	// to reset it, which is the point.
	for i := 5; i < MaxAttemptsPerWindow; i++ {
		if _, err := attempts.Fail(ctx, user, now); err != nil {
			t.Fatalf("Fail %d: %v", i, err)
		}
	}

	allowed, err := attempts.Allowed(ctx, user, now)
	if err != nil {
		t.Fatalf("Allowed: %v", err)
	}
	if allowed {
		t.Errorf("a user may still guess after %d failures across two challenges", MaxAttemptsPerWindow)
	}
}

// The bound is per user, so one person's attacker cannot exhaust everybody.
func TestTheAttemptBoundIsPerUser(t *testing.T) {
	attempts, _ := redisAttempts(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	const victim = "44444444-4444-4444-4444-444444444444"
	const bystander = "55555555-5555-5555-5555-555555555555"

	for i := 0; i < MaxAttemptsPerWindow; i++ {
		if _, err := attempts.Fail(ctx, victim, now); err != nil {
			t.Fatalf("Fail: %v", err)
		}
	}

	if allowed, _ := attempts.Allowed(ctx, victim, now); allowed {
		t.Error("the exhausted user may still guess")
	}
	allowed, err := attempts.Allowed(ctx, bystander, now)
	if err != nil {
		t.Fatalf("Allowed: %v", err)
	}
	if !allowed {
		t.Error("exhausting one user's bound also bounded another; the key is not per user")
	}
}

// It is a cooldown, not a lockout: the next window is clean.
//
// The alternative would hand anybody who knows a password a denial-of-service
// against its owner — aimed at exactly the population this control protects.
func TestTheAttemptBoundIsACooldownNotALockout(t *testing.T) {
	attempts, _ := redisAttempts(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	for i := 0; i < MaxAttemptsPerWindow; i++ {
		if _, err := attempts.Fail(ctx, "44444444-4444-4444-4444-444444444444", now); err != nil {
			t.Fatalf("Fail: %v", err)
		}
	}

	later := now.Add(AttemptWindow)
	allowed, err := attempts.Allowed(ctx, "44444444-4444-4444-4444-444444444444", later)
	if err != nil {
		t.Fatalf("Allowed: %v", err)
	}
	if !allowed {
		t.Error("the bound did not lift in the next window; it is a lockout, not a cooldown")
	}
}

// The counter carries an expiry from the increment that created it.
//
// A key with no TTL is a user bounded forever by a total nothing resets — the
// failure a separate INCR and EXPIRE produces when a process dies between them.
func TestTheCounterCannotOutliveItsWindow(t *testing.T) {
	attempts, client := redisAttempts(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	if _, err := attempts.Fail(ctx, "44444444-4444-4444-4444-444444444444", now); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	key := attemptKey("44444444-4444-4444-4444-444444444444", now.Truncate(AttemptWindow))
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("the counter has no expiry (TTL %v); a user would be bounded forever", ttl)
	}
	if ttl > AttemptWindow+2*time.Second {
		t.Errorf("TTL is %v, longer than the window it bounds", ttl)
	}
}

// Concurrent failures each count. A read-then-write would lose some, and a
// bound that undercounts under load is a bound that lifts exactly when it is
// being attacked.
func TestConcurrentFailuresAllCount(t *testing.T) {
	attempts, client := redisAttempts(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	const user = "44444444-4444-4444-4444-444444444444"
	const n = 25

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = attempts.Fail(ctx, user, now)
		}()
	}
	wg.Wait()

	count, err := client.Get(ctx, attemptKey(user, now.Truncate(AttemptWindow))).Int()
	if err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	if count != n {
		t.Errorf("counter = %d after %d concurrent failures; some were lost", count, n)
	}
}

// A missing store REFUSES rather than allowing.
//
// The opposite of ratelimit.Quotas, deliberately: a Redis that cannot count
// cannot hold a challenge either, so failing closed costs no availability that
// was not already gone — and failing open would let an outage remove the only
// bound on guessing a six-digit number.
func TestAnUnreachableStoreRefusesRatherThanAllows(t *testing.T) {
	attempts := &RedisAttempts{Client: nil}
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	allowed, err := attempts.Allowed(ctx, "44444444-4444-4444-4444-444444444444", now)
	if err == nil {
		t.Error("an unconfigured bound reported no error")
	}
	if allowed {
		t.Error("an unconfigured bound ALLOWED a guess; it must fail closed")
	}

	allowed, err = attempts.Fail(ctx, "44444444-4444-4444-4444-444444444444", now)
	if err == nil {
		t.Error("an unconfigured bound reported no error on Fail")
	}
	if allowed {
		t.Error("an unconfigured bound allowed a further guess")
	}
}

// The framework refuses to verify once the bound is spent, and does so BEFORE
// the verifier runs — so an exhausted caller cannot buy HMAC computations.
func TestTheFrameworkStopsVerifyingOnceTheBoundIsSpent(t *testing.T) {
	attempts, _ := redisAttempts(t)
	ctx := context.Background()
	at := time.Unix(1_700_000_000, 0).UTC()

	const user = "44444444-4444-4444-4444-444444444444"

	counter := &countingVerifier{}
	challenges := newMemoryChallenges()

	framework := &Framework{
		Registry:   NewRegistry(counter),
		Store:      &staticFactors{factors: []Factor{{ID: "f1", UserID: user, Type: TypeTOTP, Status: StatusActive}}},
		Challenges: challenges,
		Attempts:   attempts,
		Now:        func() time.Time { return at },
	}

	// Spend the bound outside any challenge, which is what a previous
	// challenge would have done.
	for i := 0; i < MaxAttemptsPerWindow; i++ {
		if _, err := attempts.Fail(ctx, user, at); err != nil {
			t.Fatalf("Fail: %v", err)
		}
	}

	handle, err := challenges.Put(ctx, Challenge{
		UserID: user, OrgID: "org", PendingID: "p1",
		FactorIDs: []string{"f1"}, CreatedAt: at,
	}, ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, err = framework.Answer(ctx, handle, "f1", "000000")
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("Answer gave %v, want ErrTooManyAttempts", err)
	}
	if counter.verifies != 0 {
		t.Errorf("the verifier ran %d time(s) for an exhausted caller; the bound must be read first",
			counter.verifies)
	}
}

// --- doubles for the last test -------------------------------------------------

type countingVerifier struct{ verifies int }

func (c *countingVerifier) Type() Type { return TypeTOTP }
func (c *countingVerifier) Begin(context.Context, string, string, string) (Enrolment, error) {
	return Enrolment{}, nil
}
func (c *countingVerifier) Confirm(context.Context, string, string) error { return nil }
func (c *countingVerifier) Verify(context.Context, string, string) error {
	c.verifies++
	return ErrWrongCode
}
func (c *countingVerifier) Remove(context.Context, string) error { return nil }

type staticFactors struct{ factors []Factor }

func (s *staticFactors) Confirmed(context.Context, string, string) ([]Factor, error) {
	return s.factors, nil
}

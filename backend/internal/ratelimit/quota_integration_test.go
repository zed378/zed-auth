//go:build integration

package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The Redis half of the request quota (P1-15, PG-19).
//
// Everything about what a count MEANS is in quota.go and tested as pure
// arithmetic. What is here is what only a real Redis can show: that the counter
// is atomic under concurrency, that a key carries a TTL from the moment it
// exists, that windows do not bleed into one another, and that an unreachable
// store does not stop the Management API.

func quotas(t *testing.T, q Quota) (*Quotas, *redis.Client, *counting) {
	t.Helper()

	stack := testsupport.Start(t)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	observer := &counting{}
	return NewQuotas(rdb, observer, discard()).WithQuota(q), rdb, observer
}

func quotaNoon() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

// --- the count ---------------------------------------------------------------------------

// Spend the allowance, then be refused. The whole cycle against real Redis.
func TestTheAllowanceIsSpentAndThenRefused(t *testing.T) {
	q, _, observer := quotas(t, Quota{Limit: 3, Window: time.Minute})
	ctx := context.Background()

	for i, wantRemaining := range []int{2, 1, 0} {
		v := q.Consume(ctx, "client-a", quotaNoon())
		if !v.Allowed {
			t.Fatalf("request %d of 3 was refused", i+1)
		}
		if v.Remaining != wantRemaining {
			t.Errorf("request %d reported %d remaining, want %d", i+1, v.Remaining, wantRemaining)
		}
	}

	v := q.Consume(ctx, "client-a", quotaNoon())
	if v.Allowed {
		t.Fatal("the 4th request of a limit of 3 was allowed")
	}
	if v.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v on a refusal", v.RetryAfter)
	}
	if len(observer.refused) != 1 || observer.refused[0] != BoundClient {
		t.Errorf("the refusal was reported as %v, want one %q", observer.refused, BoundClient)
	}
}

// Two clients have their own allowances. Sharing one would let any client
// exhaust every other client's, turning the limit into a denial of service
// anybody with a credential can trigger.
func TestOneClientCannotSpendAnothersAllowance(t *testing.T) {
	q, _, _ := quotas(t, Quota{Limit: 2, Window: time.Minute})
	ctx := context.Background()

	for range 3 {
		q.Consume(ctx, "client-a", quotaNoon())
	}
	if v := q.Consume(ctx, "client-a", quotaNoon()); v.Allowed {
		t.Fatal("client-a is not over its bound; the test proves nothing")
	}

	v := q.Consume(ctx, "client-b", quotaNoon())
	if !v.Allowed {
		t.Fatal("client-b was refused because client-a exhausted a shared counter")
	}
	if v.Remaining != 1 {
		t.Errorf("client-b has %d remaining of 2, so it is sharing a counter", v.Remaining)
	}
}

// --- the race ------------------------------------------------------------------------------

// Under concurrency, exactly Limit requests are allowed. A read-then-write
// counter passes every sequential test above and lets far more through here,
// which is the reason the increment is a script rather than a GET and a SET.
func TestConcurrentRequestsCannotExceedTheLimit(t *testing.T) {
	const limit, callers = 20, 60

	q, _, _ := quotas(t, Quota{Limit: limit, Window: time.Minute})
	ctx := context.Background()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
	)

	start := make(chan struct{})
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if q.Consume(ctx, "client-a", quotaNoon()).Allowed {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if granted != limit {
		t.Errorf("%d of %d concurrent requests were allowed, want exactly %d", granted, callers, limit)
	}
}

// --- the window --------------------------------------------------------------------------

// A new window is a fresh allowance. Without this the bound is a permanent
// lockout, which is the failure docs/PLAN/05 rules out for the login limiter and
// which would be no better here.
func TestTheNextWindowStartsFresh(t *testing.T) {
	q, _, _ := quotas(t, Quota{Limit: 2, Window: time.Minute})
	ctx := context.Background()

	for range 3 {
		q.Consume(ctx, "client-a", quotaNoon())
	}
	if v := q.Consume(ctx, "client-a", quotaNoon()); v.Allowed {
		t.Fatal("the client is not over its bound; the test proves nothing")
	}

	v := q.Consume(ctx, "client-a", quotaNoon().Add(time.Minute))
	if !v.Allowed {
		t.Fatal("the next window did not reset the allowance")
	}
	if v.Remaining != 1 {
		t.Errorf("the new window began with %d remaining of 2", v.Remaining)
	}
}

// The counter carries a TTL from the moment it exists.
//
// A counter with no expiry is a client rate-limited forever by a total that
// never resets, and it is exactly what an INCR followed by a separate EXPIRE
// leaves behind when a process dies between the two. Asserted on the FIRST
// request, because that is the increment that creates the key.
func TestTheCounterExpiresOnItsOwn(t *testing.T) {
	q, rdb, _ := quotas(t, Quota{Limit: 10, Window: time.Minute})
	ctx := context.Background()

	q.Consume(ctx, "client-a", quotaNoon())

	key := ClientKey("client-a", q.Quota().WindowStart(quotaNoon()))
	ttl, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("the counter has no expiry (PTTL %v) — this client is bounded forever", ttl)
	}
	// Past the window's end, so a counter cannot vanish fractionally before the
	// Reset the client was just told to wait for.
	if ttl <= time.Minute {
		t.Errorf("TTL %v does not outlast the window it counts", ttl)
	}
}

// A busy client must not slide its own window forward. Re-setting the expiry on
// every request would make the counter never reset while it is being hit, which
// turns a fixed window into a permanent lockout for the busiest callers.
func TestABusyClientDoesNotSlideItsWindow(t *testing.T) {
	q, rdb, _ := quotas(t, Quota{Limit: 100, Window: time.Minute})
	ctx := context.Background()
	key := ClientKey("client-a", q.Quota().WindowStart(quotaNoon()))

	q.Consume(ctx, "client-a", quotaNoon())
	first, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	q.Consume(ctx, "client-a", quotaNoon())

	second, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if second >= first {
		t.Errorf("the expiry moved forward on a second request (%v then %v)", first, second)
	}
}

// --- failing open -------------------------------------------------------------------------

// An unreachable store does not stop the Management API (ADR-017). Redis backs
// the session cache too, so an outage is already degrading; turning it into
// "nobody can administer anything" converts a cache failure into a total
// management outage.
func TestAnUnreachableStoreDoesNotStopTheAPI(t *testing.T) {
	q, rdb, observer := quotas(t, Quota{Limit: 1, Window: time.Minute})
	ctx := context.Background()

	_ = rdb.Close() // the outage

	v := q.Consume(ctx, "client-a", quotaNoon())
	if !v.Allowed {
		t.Fatal("a Redis outage refused a management request — that is failing closed")
	}
	if v.Remaining != v.Limit {
		t.Errorf("remaining %d of %d: a count that never happened was reported as spent",
			v.Remaining, v.Limit)
	}

	// Loudly. An outage that silently removed the control is what this metric
	// exists to prevent.
	if observer.unavailable != 1 {
		t.Errorf("the unavailable metric was incremented %d times, want 1", observer.unavailable)
	}
}

// A request with no client id is not counted against a shared empty key. Every
// such request contending on one bucket would be a limit anybody could exhaust
// for everybody.
func TestARequestWithNoClientIsNotCounted(t *testing.T) {
	q, rdb, _ := quotas(t, Quota{Limit: 1, Window: time.Minute})
	ctx := context.Background()

	for range 5 {
		if v := q.Consume(ctx, "", quotaNoon()); !v.Allowed {
			t.Fatal("an unidentified request was refused by a shared counter")
		}
	}

	keys, err := rdb.Keys(ctx, "ratelimit:client:*").Result()
	if err != nil {
		t.Fatalf("KEYS: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("an unidentified request created counters: %v", keys)
	}
}

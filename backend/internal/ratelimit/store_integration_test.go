//go:build integration

package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The Redis half. Everything about WHAT a cooldown means is in ratelimit.go
// and tested as a pure function; what is here is that the state survives a
// round trip, that keys expire on their own, and that an unreachable store
// does not stop a login (ADR-017).

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type counting struct {
	refused     []string
	unavailable int
}

func (c *counting) Refused(bound string) { c.refused = append(c.refused, bound) }
func (c *counting) Unavailable()         { c.unavailable++ }

func limiter(t *testing.T) (*Limiter, *redis.Client, *counting) {
	t.Helper()

	stack := testsupport.Start(t)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	observer := &counting{}
	return New(rdb, observer, discard()), rdb, observer
}

const (
	address = "alice@example.test"
	ip      = "203.0.113.7"
)

// The whole cycle against real Redis: free attempts, a cooldown, a refusal.
func TestTheCycleSurvivesARoundTrip(t *testing.T) {
	l, _, observer := limiter(t)
	ctx := context.Background()
	now := time.Now()

	for i := 1; i <= PerAddress.Free; i++ {
		if d, _ := l.Check(ctx, address, ip, now); !d.Allowed {
			t.Fatalf("attempt %d was refused inside the free allowance", i)
		}
		l.Fail(ctx, address, ip, now)
	}

	// The attempt that spends the last free failure is still made, and it is
	// the one that starts the cooldown.
	if d, _ := l.Check(ctx, address, ip, now); !d.Allowed {
		t.Fatal("the attempt that spends the allowance was refused rather than tried")
	}
	started, bound := l.Fail(ctx, address, ip, now)
	if !started {
		t.Fatal("no cooldown began after the allowance was spent")
	}
	if bound != BoundAddress {
		t.Errorf("bound = %q, want %q", bound, BoundAddress)
	}

	d, which := l.Check(ctx, address, ip, now)
	if d.Allowed {
		t.Fatal("the next attempt was allowed during a cooldown")
	}
	if which != BoundAddress || d.RetryAfter <= 0 {
		t.Errorf("refusal = %q after %s", which, d.RetryAfter)
	}
	if len(observer.refused) == 0 {
		t.Error("the refusal was not counted")
	}
}

// One entry per cooldown, not one per attempt — the property the login handler
// audits on.
func TestOnlyTheFirstRefusalReportsAStart(t *testing.T) {
	l, _, _ := limiter(t)
	ctx := context.Background()
	now := time.Now()

	var starts int
	for range PerAddress.Free + 10 {
		if d, _ := l.Check(ctx, address, ip, now); !d.Allowed {
			// A refused attempt still calls Fail, exactly as the handler does
			// not — but calling it must not start a second cooldown.
			if started, _ := l.Fail(ctx, address, ip, now); started {
				starts++
			}
			continue
		}
		if started, _ := l.Fail(ctx, address, ip, now); started {
			starts++
		}
	}

	if starts != 1 {
		t.Errorf("%d cooldowns began; one round of failures is one cooldown", starts)
	}
}

// FR-9, at the store: the address counter clears and the IP counter does not.
func TestSucceedClearsTheAddressAndNotTheIP(t *testing.T) {
	l, rdb, _ := limiter(t)
	ctx := context.Background()
	now := time.Now()

	l.Fail(ctx, address, ip, now)

	for _, key := range []string{AddressKey(address), IPKey(ip)} {
		if n, _ := rdb.Exists(ctx, key).Result(); n != 1 {
			t.Fatalf("no counter at %s", key)
		}
	}

	l.Succeed(ctx, address)

	if n, _ := rdb.Exists(ctx, AddressKey(address)).Result(); n != 0 {
		t.Error("the address counter survived a success")
	}
	if n, _ := rdb.Exists(ctx, IPKey(ip)).Result(); n != 1 {
		t.Error("a success cleared the per-IP counter, which would hand an attacker " +
			"a reset button for everybody sharing that address")
	}
}

// FR-4: keys expire on their own. A cleanup job that fails silently is worse
// than a TTL, and a key with no TTL is a permanent record of an address
// somebody tried.
func TestEveryKeyCarriesATTL(t *testing.T) {
	l, rdb, _ := limiter(t)
	ctx := context.Background()

	l.Fail(ctx, address, ip, time.Now())

	for _, key := range []string{AddressKey(address), IPKey(ip)} {
		ttl, err := rdb.TTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("reading the TTL of %s: %v", key, err)
		}
		if ttl <= 0 {
			t.Errorf("%s has no expiry (%s); it would outlive what it remembers", key, ttl)
		}
		if ttl > PerAddress.Window+PerAddress.Cap {
			t.Errorf("%s expires in %s, longer than anything it can be remembering", key, ttl)
		}
	}
}

// The TTL outlives a running cooldown, or the cooldown would vanish with its
// key and an attacker would get a fresh allowance by waiting less than they
// were told.
func TestTheKeyOutlivesARunningCooldown(t *testing.T) {
	l, rdb, _ := limiter(t)
	ctx := context.Background()
	now := time.Now()

	for range PerAddress.Free + 1 {
		l.Fail(ctx, address, ip, now)
	}

	d, _ := l.Check(ctx, address, ip, now)
	if d.Allowed {
		t.Fatal("no cooldown was reached")
	}

	ttl, err := rdb.TTL(ctx, AddressKey(address)).Result()
	if err != nil {
		t.Fatalf("reading the TTL: %v", err)
	}
	if ttl < d.RetryAfter {
		t.Errorf("the key expires in %s but the cooldown runs for %s", ttl, d.RetryAfter)
	}
}

// The two bounds are independent: exhausting one address does not refuse a
// different one from the same IP until the looser IP bound is reached.
func TestTheBoundsAreIndependent(t *testing.T) {
	l, _, _ := limiter(t)
	ctx := context.Background()
	now := time.Now()

	for range PerAddress.Free + 1 {
		l.Fail(ctx, address, ip, now)
	}
	if d, _ := l.Check(ctx, address, ip, now); d.Allowed {
		t.Fatal("the exhausted address was not refused")
	}

	if d, which := l.Check(ctx, "someone-else@example.test", ip, now); !d.Allowed {
		t.Errorf("a different address from the same IP was refused by %q after only "+
			"%d failures; the IP bound is %d", which, PerAddress.Free+1, PerIP.Free)
	}
}

// ADR-017. An unreachable store must not stop a login — and must be counted,
// because that number is what makes failing open safe to have chosen.
func TestAnUnreachableStoreFailsOpenAndIsCounted(t *testing.T) {
	broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = broken.Close() })

	observer := &counting{}
	l := New(broken, observer, discard())
	ctx := context.Background()

	d, which := l.Check(ctx, address, ip, time.Now())

	if !d.Allowed {
		t.Fatal("a login was refused because the counter store was unreachable; " +
			"ADR-017 chose the opposite, because failing closed converts a cache " +
			"outage into a total authentication outage")
	}
	if which != "" {
		t.Errorf("a bound was named for a decision no bound made: %q", which)
	}
	if observer.unavailable == 0 {
		t.Error("the outage was not counted")
	}

	// And recording a failure does not panic or block either.
	if started, _ := l.Fail(ctx, address, ip, time.Now()); started {
		t.Error("a cooldown was reported as started with no store to record it in")
	}
	l.Succeed(ctx, address)
}

// A corrupt value is treated as no value rather than as a refusal. Refusing
// logins over an unreadable counter would be the fail-closed behaviour ADR-017
// rejected, arriving through a different door.
func TestACorruptCounterDoesNotRefuse(t *testing.T) {
	l, rdb, _ := limiter(t)
	ctx := context.Background()

	if err := rdb.Set(ctx, AddressKey(address), "{{{not json", time.Minute).Err(); err != nil {
		t.Fatalf("seeding a corrupt value: %v", err)
	}

	if d, _ := l.Check(ctx, address, ip, time.Now()); !d.Allowed {
		t.Error("a corrupt counter refused a login")
	}
}

// An empty IP means no per-IP bound rather than a counter keyed on "". One
// shared empty key would be a global limit that any single caller could
// exhaust for everybody.
func TestAnEmptyIPIsNotCounted(t *testing.T) {
	l, rdb, _ := limiter(t)
	ctx := context.Background()

	l.Fail(ctx, address, "", time.Now())

	if n, _ := rdb.Exists(ctx, IPKey("")).Result(); n != 0 {
		t.Error("a counter was created for an empty IP; every caller without a " +
			"resolvable address would share it")
	}
	if n, _ := rdb.Exists(ctx, AddressKey(address)).Result(); n != 1 {
		t.Error("the address counter was not recorded")
	}
}

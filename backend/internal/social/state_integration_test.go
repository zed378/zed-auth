//go:build integration

package social

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func redisStateStore(t *testing.T) (*StateStore, *redis.Client) {
	t.Helper()
	stack := testsupport.Start(t)
	client := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = client.Close() })
	return NewStateStore(client), client
}

func TestRedisStateConcurrentRedemptionHasExactlyOneWinner(t *testing.T) {
	store, _ := redisStateStore(t)
	assertSingleStateConsumer(t, store)
}

func TestRedisStateExpiresAndIsStoredWithBoundedTTL(t *testing.T) {
	store, client := redisStateStore(t)
	binding := testStateBinding()
	ctx := context.Background()
	issued, err := store.Issue(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	ttl, err := client.PTTL(ctx, stateKey(issued.State)).Result()
	if err != nil || ttl <= 0 || ttl > StateLifetime {
		t.Fatalf("state TTL %v: %v", ttl, err)
	}
	if err := client.PExpire(ctx, stateKey(issued.State), time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := store.Consume(ctx, issued.State, binding); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expired Redis state accepted: %v", err)
	}
}

func TestRedisStateRejectsAnotherTenantBeforeRedemption(t *testing.T) {
	store, _ := redisStateStore(t)
	binding := testStateBinding()
	issued, err := store.Issue(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	binding.OrgID = "176f9d11-a695-4aa6-be2a-90bcda0cb915"
	if _, err := store.Consume(context.Background(), issued.State, binding); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("foreign tenant accepted: %v", err)
	}
}

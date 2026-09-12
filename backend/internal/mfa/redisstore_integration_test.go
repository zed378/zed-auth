//go:build integration

// The partially-authenticated state against real Redis (P3-01).
//
// Three properties here cannot be tested against a map: that the state
// **expires** without anybody comparing a timestamp, that a failed attempt does
// not extend that expiry, and that a consumed challenge is gone rather than
// merely marked.
package mfa

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// openAppPool connects as the RUNTIME role, never the owner — `P1-14`'s lesson:
// a tenant-scoped read as the owner returns nothing under RLS and every "no
// rows leaked" assertion passes against nothing.
func openAppPool(t *testing.T, stack *testsupport.Stack) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening the application pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMain(m *testing.M) { os.Exit(testsupport.RunTests(m)) }

func redisChallenges(t *testing.T) *RedisChallenges {
	t.Helper()

	stack := testsupport.Start(t)
	client := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = client.Close() })

	if err := client.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}
	return NewRedisChallenges(client)
}

func aChallenge() Challenge {
	return Challenge{
		UserID:    "11111111-1111-1111-1111-111111111111",
		OrgID:     "22222222-2222-2222-2222-222222222222",
		PendingID: "pending-1",
		FactorIDs: []string{"f1"},
		CreatedAt: time.Now(),
	}
}

// A stored challenge comes back, and a handle nobody issued does not.
func TestAStoredChallengeIsReadableAndAnInventedOneIsNot(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	handle, err := store.Put(ctx, aChallenge(), ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, handle)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != aChallenge().UserID {
		t.Errorf("the stored challenge names user %q", got.UserID)
	}

	if _, err := store.Get(ctx, "a-handle-nobody-issued"); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("an invented handle gave %v, want ErrNoChallenge", err)
	}
}

// The expiry is the store's, not a field somebody has to remember to compare.
//
// A row with an `expires_at` column keeps working if a query omits the
// predicate. A key with a TTL is gone.
func TestAChallengeExpiresWithoutAnybodyCheckingAClock(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	handle, err := store.Put(ctx, aChallenge(), 300*time.Millisecond)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := store.Get(ctx, handle); err != nil {
		t.Fatalf("the challenge was not readable immediately after storing it: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	if _, err := store.Get(ctx, handle); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("the challenge outlived its TTL: %v", err)
	}
}

// **A failed attempt does not extend the window.**
//
// The property the whole attempt counter rests on: an attacker who could
// refresh the clock by guessing wrong would have removed the time bound by
// using the thing it bounds.
func TestRecordingAFailedAttemptDoesNotExtendTheExpiry(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	challenge := aChallenge()
	handle, err := store.Put(ctx, challenge, 2*time.Second)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	before, err := store.Client.TTL(ctx, HandleKey(handle)).Result()
	if err != nil {
		t.Fatalf("reading the TTL: %v", err)
	}

	time.Sleep(700 * time.Millisecond)

	challenge.Attempts = 1
	if err := store.Replace(ctx, handle, challenge); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	after, err := store.Client.TTL(ctx, HandleKey(handle)).Result()
	if err != nil {
		t.Fatalf("reading the TTL back: %v", err)
	}

	if after >= before {
		t.Errorf("the TTL went from %s to %s across a failed attempt — the window is "+
			"being refreshed by the guessing it is supposed to bound", before, after)
	}

	// And the attempt was actually recorded, or the assertion above is about
	// a write that did nothing.
	got, err := store.Get(ctx, handle)
	if err != nil {
		t.Fatalf("Get after Replace: %v", err)
	}
	if got.Attempts != 1 {
		t.Errorf("the failed attempt was not recorded (attempts=%d)", got.Attempts)
	}
}

// A challenge that expired between the read and the write is not resurrected.
func TestReplacingAnExpiredChallengeDoesNotRecreateIt(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	handle, err := store.Put(ctx, aChallenge(), 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	challenge := aChallenge()
	challenge.Attempts = 1
	if err := store.Replace(ctx, handle, challenge); err != nil {
		t.Fatalf("Replace on an expired challenge should be a no-op, got: %v", err)
	}

	if _, err := store.Get(ctx, handle); !errors.Is(err, ErrNoChallenge) {
		t.Error("a failed attempt resurrected a challenge that had already expired")
	}
}

// A consumed challenge is gone, so a completed login cannot be replayed.
func TestAConsumedChallengeIsGone(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	handle, err := store.Put(ctx, aChallenge(), ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(ctx, handle); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := store.Get(ctx, handle); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("a consumed challenge is still readable: %v", err)
	}
}

// The stored key is a hash, so a dump of Redis hands out nothing usable.
//
// Checked against the real keyspace rather than against `HandleKey`, because
// the claim is about what an operator with `KEYS *` would see.
func TestRedisHoldsNoUsableHandle(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	handle, err := store.Put(ctx, aChallenge(), ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	keys, err := store.Client.Keys(ctx, "*").Result()
	if err != nil {
		t.Fatalf("listing keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("no keys at all, so this test is looking at an empty store")
	}

	for _, key := range keys {
		if key == handle || contains([]string{key}, handle) {
			t.Errorf("the key %q is the handle itself — a backup, a KEYS, or a "+
				"slow-command log hands out working challenge handles", key)
		}
	}
}

// The user id lives server-side, never in what the client holds.
//
// Abuse case A-1: a client that could edit the user id in its own state would
// complete somebody else's login.
func TestTheUserIdIsInTheStoreAndNotInTheHandle(t *testing.T) {
	store := redisChallenges(t)
	ctx := context.Background()

	challenge := aChallenge()
	handle, err := store.Put(ctx, challenge, ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if contains([]string{handle}, challenge.UserID) {
		t.Fatal("the handle contains the user id, so a client holds an editable subject")
	}

	got, err := store.Get(ctx, handle)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != challenge.UserID {
		t.Errorf("the store lost the user id: %q", got.UserID)
	}
}

// A challenge handle is not a session token (P3-01 FR-3).
//
// The Definition of Done asks that "a partially-authenticated session cannot
// access any resource or obtain a token". The strongest form of that is
// structural rather than procedural: the handle is not the kind of thing any
// credential path accepts, so there is no check to forget.
//
// This presents one to the session manager — the single place a browser
// credential is turned into an identity — and requires a refusal. Everything
// downstream of a session (the token endpoint, every /v1 route, the OIDC
// resume) reaches identity through here, so a handle that cannot become a
// session cannot become any of them.
func TestAChallengeHandleIsNotASessionToken(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	db := openAppPool(t, stack)
	manager := session.NewManager(db, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	handle, err := NewHandle()
	if err != nil {
		t.Fatalf("NewHandle: %v", err)
	}

	_, err = manager.Lookup(context.Background(), handle, session.Policy{
		AbsoluteLifetime: time.Hour,
		IdleTimeout:      time.Hour,
	}, time.Now())

	if err == nil {
		t.Fatal("a partially-authenticated challenge handle was accepted as a session — " +
			"which would make the state that exists precisely because a login is NOT " +
			"finished into a credential for everything a finished one gets")
	}
}

// Deleting nothing is not an error.
//
// The completion path calls Delete after a successful verification, and a
// handle that expired between the two is not a failure worth reporting to
// somebody who has just authenticated correctly.
//
// Here rather than in the unit tests because it needs a configured store: with
// a nil client the "no store configured" refusal fires first, which is the
// right order and makes the empty-handle branch unreachable.
func TestDeletingAnEmptyHandleIsANoOp(t *testing.T) {
	store := redisChallenges(t)

	if err := store.Delete(context.Background(), ""); err != nil {
		t.Errorf("deleting an empty handle gave %v", err)
	}
}

// An empty handle reads as "no such challenge", not as an error.
func TestAnEmptyHandleReadsAsNoChallenge(t *testing.T) {
	store := redisChallenges(t)

	if _, err := store.Get(context.Background(), ""); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("an empty handle gave %v, want ErrNoChallenge", err)
	}
}

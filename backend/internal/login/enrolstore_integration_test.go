//go:build integration

// The forced-enrolment store against real Redis (P3-07).
//
// It carries the same properties the challenge store does, and each is one a
// fake cannot show: the TTL is Redis's rather than a timestamp somebody compares,
// a failed attempt is written with KEEPTTL so guessing wrong cannot buy time, a
// Replace cannot resurrect an expired enrolment, and the key is a hash so a dump
// of Redis holds no usable handle.
package login

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func sampleEnrolment() EnrolState {
	return EnrolState{
		UserID:    "11111111-1111-1111-1111-111111111111",
		OrgID:     "22222222-2222-2222-2222-222222222222",
		PendingID: "pending-1",
		FactorID:  "33333333-3333-3333-3333-333333333333",
	}
}

func TestAnEnrolmentRoundTrips(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	handle, err := store.Put(context.Background(), sampleEnrolment(), 60)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(context.Background(), handle)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != sampleEnrolment() {
		t.Errorf("round trip = %+v, want %+v", got, sampleEnrolment())
	}
}

// The key is a HASH of the handle, so a Redis dump yields nothing presentable.
func TestTheHandleIsNotStoredAsAKey(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	handle, err := store.Put(context.Background(), sampleEnrolment(), 60)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	keys, err := s.rdb.Keys(context.Background(), "*").Result()
	if err != nil {
		t.Fatalf("listing keys: %v", err)
	}
	for _, k := range keys {
		if strings.Contains(k, handle) {
			t.Fatalf("the handle appears in a Redis key %q; a dump would hand it over", k)
		}
	}
	if len(keys) == 0 {
		t.Fatal("no key was written, so the check above proves nothing")
	}
}

// A failed attempt does not extend the window.
//
// If Replace reset the TTL, somebody guessing codes would refresh the clock by
// using the thing that bounds them.
func TestReplacingKeepsTheRemainingTTL(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	handle, err := store.Put(context.Background(), sampleEnrolment(), 60)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Shorten the key's life directly, as if most of the window has passed.
	if err := s.rdb.Expire(context.Background(), enrolKey(handle), 5*time.Second).Err(); err != nil {
		t.Fatalf("expire: %v", err)
	}

	state := sampleEnrolment()
	state.Attempts = 3
	if err := store.Replace(context.Background(), handle, state); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	ttl, err := s.rdb.TTL(context.Background(), enrolKey(handle)).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl > 5*time.Second {
		t.Errorf("Replace reset the TTL to %s; a wrong guess bought more time", ttl)
	}
	if ttl <= 0 {
		t.Errorf("Replace removed the expiry entirely (TTL %s); the enrolment would never end", ttl)
	}

	got, err := store.Get(context.Background(), handle)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Attempts != 3 {
		t.Errorf("Attempts = %d after Replace, want 3", got.Attempts)
	}
}

// Replace cannot resurrect an enrolment that has already expired.
func TestReplacingAnExpiredEnrolmentDoesNotRecreateIt(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	handle, err := store.Put(context.Background(), sampleEnrolment(), 60)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(context.Background(), handle); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_ = store.Replace(context.Background(), handle, sampleEnrolment())

	if _, err := store.Get(context.Background(), handle); !errors.Is(err, ErrNoEnrolment) {
		t.Errorf("a Replace after expiry brought the enrolment back: %v", err)
	}
}

// An unknown handle is ErrNoEnrolment, not a store error.
func TestAnUnknownHandleIsNoEnrolment(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	if _, err := store.Get(context.Background(), "nothing-here"); !errors.Is(err, ErrNoEnrolment) {
		t.Errorf("Get(unknown) = %v, want ErrNoEnrolment", err)
	}
	if _, err := store.Get(context.Background(), ""); !errors.Is(err, ErrNoEnrolment) {
		t.Errorf("Get(empty) = %v, want ErrNoEnrolment", err)
	}
}

// A stored value missing its identity is refused rather than completed against.
func TestACorruptEnrolmentIsRefused(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	handle, err := store.Put(context.Background(), EnrolState{PendingID: "p"}, 60)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := store.Get(context.Background(), handle); !errors.Is(err, ErrNoEnrolment) {
		t.Errorf("an enrolment with no user or factor was returned: %v", err)
	}
}

// The TTL is enforced by Redis, not by a comparison somebody has to remember.
func TestAnEnrolmentExpires(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	handle, err := store.Put(context.Background(), sampleEnrolment(), 1)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.Get(context.Background(), handle); errors.Is(err, ErrNoEnrolment) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Error("an enrolment with a one-second TTL was still readable after five seconds")
}

// Two Puts produce two distinct handles; neither overwrites the other.
func TestHandlesAreDistinct(t *testing.T) {
	s := setup(t)
	store := NewRedisEnrolments(s.rdb)

	a, err := store.Put(context.Background(), sampleEnrolment(), 60)
	if err != nil {
		t.Fatalf("Put a: %v", err)
	}
	other := sampleEnrolment()
	other.PendingID = "pending-2"
	b, err := store.Put(context.Background(), other, 60)
	if err != nil {
		t.Fatalf("Put b: %v", err)
	}

	if a == b {
		t.Fatal("two enrolments got the same handle")
	}
	got, _ := store.Get(context.Background(), a)
	if got.PendingID != "pending-1" {
		t.Errorf("the first enrolment was overwritten: %+v", got)
	}
}

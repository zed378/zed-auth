package mfa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// The partially-authenticated state, in Redis (P3-01).
//
// Redis rather than Postgres, for the reason `P1-06`'s pending authorization
// is: this is a thing that must **expire**, and an expiry enforced by the
// store is one nobody can forget to compare against. A row in a table with an
// `expires_at` column is a row that keeps working if a query omits the
// predicate; a key with a TTL is gone.
//
// It also means a challenge does not survive a Redis restart, which is the
// correct trade: the cost is a user re-entering their password, and the
// alternative is half-finished logins outliving the incident that interrupted
// them.

// RedisChallenges stores challenges under hashed handles.
type RedisChallenges struct {
	Client redis.UniversalClient
}

func NewRedisChallenges(client redis.UniversalClient) *RedisChallenges {
	return &RedisChallenges{Client: client}
}

// Put stores a challenge and returns its handle.
//
// The handle is generated here rather than taken from the caller, so there is
// no path by which a caller-chosen value becomes a lookup key — which would
// make the handle guessable by whoever chose it.
func (r *RedisChallenges) Put(ctx context.Context, c Challenge, ttl time.Duration) (string, error) {
	if r == nil || r.Client == nil {
		return "", fmt.Errorf("mfa: no challenge store configured")
	}

	handle, err := NewHandle()
	if err != nil {
		return "", err
	}

	raw, err := c.Encode()
	if err != nil {
		return "", err
	}

	// SetNX rather than Set: a handle collision would silently overwrite
	// somebody else's half-finished login. 32 bytes of CSPRNG makes that
	// impossible in practice, and "impossible in practice" is not a reason to
	// write the version that would do the wrong thing.
	ok, err := r.Client.SetNX(ctx, HandleKey(handle), raw, ttl).Result()
	if err != nil {
		return "", fmt.Errorf("mfa: storing a challenge: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("mfa: a challenge handle collided")
	}
	return handle, nil
}

// Get reads a challenge. ErrNoChallenge covers expired, consumed and never-was.
func (r *RedisChallenges) Get(ctx context.Context, handle string) (Challenge, error) {
	if r == nil || r.Client == nil {
		return Challenge{}, fmt.Errorf("mfa: no challenge store configured")
	}
	if handle == "" {
		return Challenge{}, ErrNoChallenge
	}

	raw, err := r.Client.Get(ctx, HandleKey(handle)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Challenge{}, ErrNoChallenge
	}
	if err != nil {
		// A store failure is NOT ErrNoChallenge. The caller must be able to
		// tell "there is no such challenge" from "we could not look", because
		// the first is a refusal the user can act on and the second is an
		// outage they cannot.
		return Challenge{}, fmt.Errorf("mfa: reading a challenge: %w", err)
	}

	return DecodeChallenge(raw)
}

// Replace overwrites a challenge, **keeping its remaining TTL**.
//
// `KEEPTTL` is the whole point. Recording a failed attempt must not extend the
// window: an attacker who could refresh the clock by guessing wrong would have
// removed the time bound by using the thing it bounds.
func (r *RedisChallenges) Replace(ctx context.Context, handle string, c Challenge) error {
	if r == nil || r.Client == nil {
		return fmt.Errorf("mfa: no challenge store configured")
	}

	raw, err := c.Encode()
	if err != nil {
		return err
	}

	// KeepTTL, and XX so a challenge that expired between the read and the
	// write is not resurrected by the attempt that failed against it.
	set, err := r.Client.SetArgs(ctx, HandleKey(handle), raw, redis.SetArgs{
		Mode:    "XX",
		KeepTTL: true,
	}).Result()
	if errors.Is(err, redis.Nil) {
		// XX found nothing: it expired underneath us. Not an error — the
		// challenge is gone, which is what a failed attempt was heading
		// towards anyway.
		return nil
	}
	if err != nil {
		return fmt.Errorf("mfa: recording a challenge attempt: %w", err)
	}
	_ = set
	return nil
}

// Delete consumes a challenge, so a completed one cannot be replayed.
func (r *RedisChallenges) Delete(ctx context.Context, handle string) error {
	if r == nil || r.Client == nil {
		return fmt.Errorf("mfa: no challenge store configured")
	}
	if handle == "" {
		return nil
	}
	if err := r.Client.Del(ctx, HandleKey(handle)).Err(); err != nil {
		return fmt.Errorf("mfa: deleting a challenge: %w", err)
	}
	return nil
}

package login

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// Forced-enrolment state in Redis (P3-07).
//
// The same shape as `mfa.RedisChallenges`, deliberately and not by accident:
// both hold a half-finished login, and every property that makes one safe makes
// the other safe. The handle is hashed before it becomes a key, the expiry is
// the store's TTL rather than a timestamp somebody remembers to compare, and a
// failed attempt is written with `KEEPTTL` so guessing wrong cannot buy time.
//
// It is a separate type rather than a reuse of `Challenge` because the two mean
// opposite things — one says "this user has a factor", the other "this user has
// none" — and a single store would let a stale value of either kind be read as
// the other.

// RedisEnrolments stores forced enrolments.
type RedisEnrolments struct {
	Client redis.UniversalClient
}

func NewRedisEnrolments(client redis.UniversalClient) *RedisEnrolments {
	return &RedisEnrolments{Client: client}
}

// enrolKey hashes a handle into its storage key.
//
// SHA-256 first, for the reason a session token is hashed (`PG-14`): the store
// then holds no value that could be presented back as a credential, so a Redis
// dump — a `KEYS`, a backup, a slow-command log — hands an attacker nothing
// they can use.
func enrolKey(handle string) string {
	if handle == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(handle))
	return "login:enrol:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func (r *RedisEnrolments) Put(ctx context.Context, state EnrolState, ttlSeconds int) (string, error) {
	if r == nil || r.Client == nil {
		return "", errors.New("login: no enrolment store configured")
	}

	handle, err := mfa.NewHandle()
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("login: encoding an enrolment: %w", err)
	}

	// SetNX rather than Set, matching the challenge store: a handle collision
	// would silently overwrite somebody else's half-finished enrolment. 32
	// bytes of CSPRNG makes that impossible in practice, and "impossible in
	// practice" is not a reason to write the version that would do the wrong
	// thing.
	ok, err := r.Client.SetNX(ctx, enrolKey(handle),
		raw, time.Duration(ttlSeconds)*time.Second).Result()
	if err != nil {
		return "", fmt.Errorf("login: storing an enrolment: %w", err)
	}
	if !ok {
		return "", errors.New("login: an enrolment handle collided")
	}
	return handle, nil
}

func (r *RedisEnrolments) Get(ctx context.Context, handle string) (EnrolState, error) {
	if r == nil || r.Client == nil {
		return EnrolState{}, errors.New("login: no enrolment store configured")
	}
	key := enrolKey(handle)
	if key == "" {
		return EnrolState{}, ErrNoEnrolment
	}

	raw, err := r.Client.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return EnrolState{}, ErrNoEnrolment
	case err != nil:
		// A store failure is NOT ErrNoEnrolment. The caller must be able to
		// tell "there is no such enrolment" from "I could not find out",
		// because the first ends a flow and the second is an outage.
		return EnrolState{}, fmt.Errorf("login: reading an enrolment: %w", err)
	}

	var state EnrolState
	if err := json.Unmarshal(raw, &state); err != nil {
		return EnrolState{}, fmt.Errorf("login: decoding an enrolment: %w", err)
	}
	if state.UserID == "" || state.OrgID == "" || state.FactorID == "" {
		// A stored value missing its identity is corrupt rather than empty.
		// Refused, because completing an enrolment against a blank user id
		// would be the worst possible interpretation of it.
		return EnrolState{}, ErrNoEnrolment
	}
	return state, nil
}

// Replace overwrites an enrolment in place, KEEPING its remaining TTL.
//
// Used to record a failed attempt without extending the window: somebody who
// could refresh the clock by guessing wrong would have removed the bound by
// using the thing it bounds. `XX` so a Replace cannot resurrect an enrolment
// that has already expired.
func (r *RedisEnrolments) Replace(ctx context.Context, handle string, state EnrolState) error {
	if r == nil || r.Client == nil {
		return errors.New("login: no enrolment store configured")
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("login: encoding an enrolment: %w", err)
	}

	return r.Client.SetArgs(ctx, enrolKey(handle), raw, redis.SetArgs{
		Mode:    "XX",
		KeepTTL: true,
	}).Err()
}

func (r *RedisEnrolments) Delete(ctx context.Context, handle string) error {
	if r == nil || r.Client == nil {
		return errors.New("login: no enrolment store configured")
	}
	return r.Client.Del(ctx, enrolKey(handle)).Err()
}

package authorize

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/redis/go-redis/v9"
)

// Authorization codes and pending requests live in Redis.
//
// PLAN/04 § What Is Deliberately Not Stored Here says why: both are single-use
// and very short-lived, so durability across a restart is not required, and an
// automatic expiry is stronger than a cleanup job that can fail silently.
//
// The requirement that shapes the implementation is in the same paragraph:
// "Redemption must be atomic — two concurrent redemptions of one code must
// yield exactly one success." That is one GETDEL, not a GET followed by a DEL.
// The two-call version passes every sequential test and loses a race under
// concurrency, which is the shape of bug that ships.

// ErrCodeNotFound means the code is unknown, already redeemed, or expired.
//
// All three collapse into one answer deliberately. Telling a caller which
// would say whether a code ever existed, which is a probing oracle on a value
// an attacker may have glimpsed in a URL.
var ErrCodeNotFound = errors.New("authorize: authorization code not found")

// ErrPendingNotFound means a login flow cannot be resumed.
var ErrPendingNotFound = errors.New("authorize: pending authorization request not found")

// CodeTTL is how long an authorization code lives.
//
// PLAN/04 requires under 60 seconds. Sixty is the ceiling, not the target: the
// code travels from a redirect to the consumer's backend, which is a round trip
// measured in milliseconds. Thirty seconds is generous for that and halves the
// window in which a code glimpsed in a URL bar or a proxy log is still worth
// anything.
const CodeTTL = 30 * time.Second

// PendingTTL is how long an interrupted authorization request can be resumed.
//
// Ten minutes: long enough to read a password manager, find a second factor, or
// reset a forgotten password; short enough that an abandoned flow does not sit
// in Redis for an afternoon.
const PendingTTL = 10 * time.Minute

// codeBytes is the entropy in an authorization code.
//
// 256 bits, matching client secrets and session tokens (ADR-016). A code is a
// bearer credential for its 30 seconds, and a guessable one would be redeemable
// by anyone who guessed.
const codeBytes = 32

// Code is what an authorization code binds.
//
// Every field here is checked at redemption by P1-07. A code that bound only
// the user would be redeemable by any client, against any redirect URI, with
// any verifier — each of those is a separate published attack.
type Code struct {
	ClientID    string   `json:"client_id"`
	RedirectURI string   `json:"redirect_uri"`
	UserID      string   `json:"user_id"`
	OrgID       string   `json:"org_id"`
	SessionID   string   `json:"session_id"`
	Scope       []string `json:"scope"`
	Nonce       string   `json:"nonce,omitempty"`

	// CodeChallenge is the PKCE challenge. P1-07 verifies the presented
	// verifier against it, which is what makes an intercepted code useless.
	CodeChallenge string `json:"code_challenge"`

	// AuthMethods and AuthTime come from the session, for the `amr` and
	// `auth_time` claims P1-07 builds.
	AuthMethods []string  `json:"auth_methods"`
	AuthTime    time.Time `json:"auth_time"`

	IssuedAt time.Time `json:"issued_at"`
}

// Store holds codes and pending requests.
type Store struct {
	client redis.UniversalClient
}

func NewStore(client redis.UniversalClient) *Store { return &Store{client: client} }

func codeKey(code string) string  { return "oauth:code:" + code }
func pendingKey(id string) string { return "oauth:pending:" + id }

// IssueCode stores a code and returns its opaque value.
func (s *Store) IssueCode(ctx context.Context, c Code, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > time.Minute {
		// PLAN/04's bound, enforced rather than trusted to the caller: a code
		// that outlives a minute is a credential sitting in a URL.
		return "", fmt.Errorf("authorize: code TTL must be positive and under a minute, got %s", ttl)
	}

	code, err := opaque()
	if err != nil {
		return "", err
	}

	encoded, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("authorize: encoding code: %w", err)
	}

	// SetNX rather than Set: a collision would silently overwrite somebody
	// else's live code, and at 256 bits a collision means the random source is
	// broken — which is worth failing loudly rather than papering over.
	ok, err := s.client.SetNX(ctx, codeKey(code), encoded, ttl).Result()
	if err != nil {
		return "", fmt.Errorf("authorize: storing code: %w", err)
	}
	if !ok {
		return "", errors.New("authorize: generated code already exists; the random source may be broken")
	}

	return code, nil
}

// RedeemCode consumes a code, returning what it bound.
//
// GETDEL, which is atomic: two concurrent redemptions yield exactly one
// success, as PLAN/04 requires. The obvious alternative — GET, check, DEL —
// has a window between the read and the delete in which a second caller reads
// the same code, and it passes every sequential test.
func (s *Store) RedeemCode(ctx context.Context, code string) (Code, error) {
	if code == "" {
		return Code{}, ErrCodeNotFound
	}

	raw, err := s.client.GetDel(ctx, codeKey(code)).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return Code{}, ErrCodeNotFound
	case err != nil:
		return Code{}, fmt.Errorf("authorize: redeeming code: %w", err)
	}

	var c Code
	if err := json.Unmarshal(raw, &c); err != nil {
		// The code existed and is now consumed, which is correct: a value we
		// cannot parse must not be retryable.
		return Code{}, fmt.Errorf("authorize: decoding code: %w", err)
	}
	return c, nil
}

// SavePending stores an authorization request that is waiting for a login.
//
// The returned id goes to the login page in a URL. It carries no authority of
// its own — resuming still requires authenticating — so it is a lookup key
// rather than a credential, and it is random and single-use anyway.
func (s *Store) SavePending(ctx context.Context, r Request, ttl time.Duration) (string, error) {
	id, err := opaque()
	if err != nil {
		return "", err
	}

	encoded, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("authorize: encoding pending request: %w", err)
	}

	if err := s.client.Set(ctx, pendingKey(id), encoded, ttl).Err(); err != nil {
		return "", fmt.Errorf("authorize: storing pending request: %w", err)
	}
	return id, nil
}

// LoadPending consumes a pending request.
//
// Single-use, by the same GETDEL. A resumable-twice request would let one
// login satisfy two authorization flows, which is a code issued for a flow
// nobody re-authorised.
func (s *Store) LoadPending(ctx context.Context, id string) (Request, error) {
	if id == "" {
		return Request{}, ErrPendingNotFound
	}

	raw, err := s.client.GetDel(ctx, pendingKey(id)).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return Request{}, ErrPendingNotFound
	case err != nil:
		return Request{}, fmt.Errorf("authorize: loading pending request: %w", err)
	}

	var r Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return Request{}, fmt.Errorf("authorize: decoding pending request: %w", err)
	}
	return r, nil
}

// opaque returns a 256-bit random identifier.
func opaque() (string, error) {
	buf := make([]byte, codeBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("authorize: generating identifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

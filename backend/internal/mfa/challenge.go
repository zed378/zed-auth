package mfa

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The partially-authenticated state (P3-01 step 2).
//
// **This is the highest-risk object in the system**, and the spec says so: it
// is by construction a thing that is almost a session. Every property that
// makes a session useful is one this must not have.
//
// So, explicitly, what it is not:
//
//   - It is not a credential for anything. It authorizes exactly one operation
//     — answering its own challenge — and no code path accepts it anywhere
//     else. `session.Manager` never sees it; the token endpoint never sees it.
//   - It carries no claims. The user id lives server-side, keyed by an opaque
//     handle, so a client cannot name a different user by editing what it
//     holds. That is abuse case A-1 and it is closed by the shape rather than
//     by a check.
//   - It does not survive. Five minutes, and the store's TTL is the enforcement
//     rather than a field somebody has to remember to compare.
//
// It follows the pending-authorization state `P1-06` already uses for the same
// reason — a login held mid-flight in Redis with a short expiry — rather than
// being a second mechanism invented beside it.

// ChallengeTTL bounds how long a half-finished login stays answerable.
//
// Five minutes: long enough to open an authenticator app, find the entry and
// type six digits, including on a phone that had to be unlocked first. Short
// enough that a state left behind on a shared machine is useless by the time
// anybody finds it.
const ChallengeTTL = 5 * time.Minute

// MaxAttempts bounds guesses against one challenge.
//
// Six digits is a million possibilities and TOTP's window is 30 seconds, so an
// unbounded challenge is brute-forceable by anybody who already has the
// password. Five attempts, then the challenge is spent and the login restarts.
//
// **Cooldown, not lockout** (`P1-13`'s shape). Exhausting a challenge costs the
// user one restart; it does not lock the account, because an attacker who could
// lock an account by failing its challenges would have a denial-of-service
// aimed at any user whose password they knew — which is the same population
// this control is protecting.
const MaxAttempts = 5

// Challenge is a login that has proven a password and not yet proven a factor.
type Challenge struct {
	// UserID and OrgID are here and NOT in anything the client holds.
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`

	// PendingID ties this to the authorization request that started it, so a
	// challenge answered in one login cannot complete a different one.
	PendingID string `json:"pending_id"`

	// FactorIDs are the confirmed factors this user may answer with. Captured
	// at challenge time, so a factor enrolled mid-challenge does not become
	// answerable and one removed mid-challenge does not keep working.
	FactorIDs []string `json:"factor_ids"`

	// Attempts counts failures against this challenge.
	Attempts int `json:"attempts"`

	// Methods are the factors already proven in this authentication. The
	// password is recorded separately because it was proven before the
	// challenge existed.
	Methods []Type `json:"methods"`

	CreatedAt time.Time `json:"created_at"`
}

// Spent reports whether this challenge has been guessed at too many times.
func (c Challenge) Spent() bool { return c.Attempts >= MaxAttempts }

// Errors the challenge store distinguishes.
var (
	// ErrNoChallenge is a handle that names nothing: expired, spent, already
	// completed, or never issued. One error for all of them, because the
	// difference is a fact about somebody else's login.
	ErrNoChallenge = errors.New("mfa: no such challenge")

	// ErrChallengeSpent is returned when the attempt bound is reached. The
	// caller tells the user to start again — which is a real thing they can
	// do, unlike a lockout.
	ErrChallengeSpent = errors.New("mfa: too many attempts")
)

// ChallengeStore holds partially-authenticated state.
//
// An interface because the backing store is Redis in production and a map in
// tests, and because the property being tested here — that the state is
// opaque, server-side and short-lived — is about this seam rather than about
// Redis.
type ChallengeStore interface {
	// Put stores a challenge and returns the opaque handle for it.
	Put(ctx context.Context, c Challenge, ttl time.Duration) (handle string, err error)

	// Get reads a challenge by handle. ErrNoChallenge when there is none.
	Get(ctx context.Context, handle string) (Challenge, error)

	// Replace overwrites a challenge in place, keeping its remaining TTL.
	// Used to record a failed attempt without extending the window: an
	// attacker who could refresh the clock by guessing wrong would have
	// removed the time bound by using the thing it bounds.
	Replace(ctx context.Context, handle string, c Challenge) error

	// Delete consumes a challenge. Called on success and on exhaustion, so a
	// completed challenge cannot be replayed.
	Delete(ctx context.Context, handle string) error
}

// NewHandle mints an opaque challenge handle.
//
// 32 bytes of CSPRNG, URL-safe. Unguessable, and — the part that matters —
// carrying nothing: it is a lookup key, not a container, so there is no field
// in it for a client to edit.
func NewHandle() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mfa: generating a challenge handle: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HandleKey is the storage key for a handle.
//
// The handle is HASHED before it becomes a key, for the reason a session token
// is (`PG-14`): the store then holds no value that could be presented back as
// a credential, so a dump of it — a Redis `KEYS`, a backup, a log of slow
// commands — hands an attacker nothing they can use.
func HandleKey(handle string) string {
	if handle == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(handle))
	return "mfa:challenge:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// Encode serialises a challenge for storage.
func (c Challenge) Encode() ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("mfa: encoding a challenge: %w", err)
	}
	return raw, nil
}

// DecodeChallenge reads a stored challenge.
func DecodeChallenge(raw []byte) (Challenge, error) {
	var c Challenge
	if err := json.Unmarshal(raw, &c); err != nil {
		return Challenge{}, fmt.Errorf("mfa: decoding a challenge: %w", err)
	}
	if c.UserID == "" || c.OrgID == "" {
		// A stored challenge with no subject is not a challenge. Refused
		// rather than returned, because the one thing downstream does with it
		// is trust the user id.
		return Challenge{}, fmt.Errorf("mfa: a stored challenge names no user")
	}
	return c, nil
}

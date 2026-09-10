package client

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

// Client secret generation, storage and verification.
//
// **On SHA-256, and why this is not the mistake it looks like.**
//
// internal/authn hashes passwords with Argon2id and this file hashes client
// secrets with a bare SHA-256. Seeing both in one codebase should prompt
// exactly the question you are asking, so here is the answer.
//
// A password is chosen by a human and carries perhaps 30 bits of entropy. A
// slow KDF is what stands between a leaked hash and a leaked password. A
// client secret here is 256 bits from crypto/rand: brute-forcing it behind an
// infinitely fast hash takes on the order of 10^52 years. The hash's speed is
// not what protects it — the entropy already did, by a margin no amount of
// key stretching can meaningfully extend.
//
// What a slow KDF would buy instead is a denial-of-service vector pointed at
// ourselves. Client secrets are verified on every client_credentials token
// request. At P1-01's measured Argon2id parameters — 90ms and 64 MiB on the
// staging VM — fifty token requests per second is 4.5 cores and 288 MiB
// resident, and an attacker sending deliberately WRONG secrets pays nothing
// while we pay all of it. That is unauthenticated amplification bought for no
// security benefit (ADR-016).
//
// **This holds only while the secret is high-entropy and generated here.** If
// a caller could supply one, an administrator would eventually choose
// "hunter2" and SHA-256 would become the wrong answer that afternoon. So it is
// not left to convention: Generate is the only way to produce a secret,
// nothing in this package accepts a caller-supplied one, and TestSecretEntropy
// fails if the length or alphabet is ever weakened.

// secretBytes is the entropy behind every client secret.
//
// 32 bytes, and the specific number is load-bearing rather than a round
// figure: the argument above depends on the secret being far beyond brute
// force, and it stops being true if this shrinks. Nothing else in the package
// gets to choose a length.
const secretBytes = 32

// ErrSecretNotSet means the application has no current secret — either it is a
// public client, or it has never been issued one.
var ErrSecretNotSet = errors.New("client: application has no client secret")

// Secret is a plaintext client secret on its way to the caller who created it.
//
// A struct rather than a string, and that is the entire point of the type. A
// string return value ends up in a log line, an error message, or a struct
// field that later grows a JSON tag — each an accident nobody makes
// deliberately and everybody makes eventually. This type cannot be printed by
// accident, and getting the value out requires writing the word Reveal, which
// is greppable and conspicuous in review.
type Secret struct {
	plaintext string
}

// Format redacts under EVERY verb.
//
// fmt.Stringer is not enough and the difference is not academic: fmt consults
// String only for %v, %s, %q, %x and %X. Under %d — or any other verb that
// does not apply to a struct — it falls back to printing the fields, and
// TestSecretDoesNotPrintItself caught exactly that, emitting the plaintext
// inside a `%!d(string=...)` marker.
//
// Nobody writes %d on a secret deliberately. Somebody writes it on a struct
// that contains one, or passes a value to a printf-style logger whose format
// string drifts from its arguments. fmt.Formatter takes precedence over both
// Stringer and GoStringer for every verb, so implementing it is the only way
// to close the whole surface rather than the part that was thought of.
func (s Secret) Format(f fmt.State, verb rune) {
	_, _ = io.WriteString(f, "[REDACTED]")
}

// String satisfies fmt.Stringer, for callers that invoke it directly or pass
// the value somewhere typed as one.
func (s Secret) String() string { return "[REDACTED]" }

// GoString covers a direct %#v path for callers that bypass Format.
func (s Secret) GoString() string { return "client.Secret{[REDACTED]}" }

// MarshalJSON refuses rather than redacting.
//
// A redacted string in a JSON response would be a field named client_secret
// containing "[REDACTED]", which a consumer would reasonably store. An error
// makes the mistake visible at the point it is made.
func (s Secret) MarshalJSON() ([]byte, error) {
	return nil, errors.New(
		"client: a Secret must not be marshalled directly; call Reveal() at the one " +
			"response that returns it, so the disclosure is a visible line of code")
}

// Reveal returns the plaintext. Call it once, at the response that hands the
// secret to its owner, and nowhere else.
func (s Secret) Reveal() string { return s.plaintext }

// IsZero reports whether this is the empty Secret.
func (s Secret) IsZero() bool { return s.plaintext == "" }

// Generate returns a new client secret and the hash to store.
//
// The plaintext is returned exactly once, here. Nothing persists it, and no
// read path can reconstruct it — which is what makes "shown once at creation
// only, never retrievable again" (docs/UI-UX/08) a property of the system rather
// than a promise made by a screen.
func Generate() (Secret, string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return Secret{}, "", fmt.Errorf("client: generating secret: %w", err)
	}

	// base64url without padding: safe in an Authorization header, a form body
	// and a URL, and free of the characters that make an operator quote it
	// wrongly in a shell.
	plaintext := base64.RawURLEncoding.EncodeToString(buf)

	return Secret{plaintext: plaintext}, Hash(plaintext), nil
}

// Hash returns the stored form of a secret.
//
// Unsalted, deliberately. A salt defends against precomputation across many
// low-entropy inputs, and there is no precomputing a table over 2^256. Adding
// one would imply the entropy assumption is not being relied on, which is
// worse than useless: it would make the next reader think a weaker secret
// would be safe here.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether the presented secret matches the stored hash.
//
// Constant-time over the digests. The comparison is on fixed-length hex
// output, so it leaks neither the secret nor its length.
func Verify(storedHash, presented string) bool {
	if storedHash == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(Hash(presented)), []byte(storedHash)) == 1
}

// --- rotation ---------------------------------------------------------------

// Credentials is the secret state of one application, as stored.
//
// Only hashes. This type is what a verification path holds, and it is
// incapable of carrying a plaintext.
type Credentials struct {
	Hash string

	// PreviousHash and PreviousExpiresAt are the rotation overlap: a newly
	// issued secret works immediately while the old one keeps working until it
	// expires, so rotating does not require redeploying the consumer
	// application at the same moment.
	//
	// P0-07's schema enforces that these are set together — a rotation window
	// with no expiry never closes.
	PreviousHash      string
	PreviousExpiresAt time.Time
}

// DefaultRotationOverlap is how long a replaced secret keeps working.
//
// Twenty-four hours: long enough for an operator to update a deployment during
// working hours in any timezone without racing a clock, short enough that a
// secret rotated *because it leaked* stops being useful to whoever has it
// within a day. An overlap is a window in which a compromised credential still
// works, so it is a trade rather than a convenience, and RotateNow exists for
// the case where there is nothing to trade.
const DefaultRotationOverlap = 24 * time.Hour

// Verify reports whether the presented secret matches the current secret, or
// the previous one while its overlap is still open.
//
// Both are checked on every call rather than short-circuiting on the current
// one, so the work done does not reveal which secret matched. `now` is a
// parameter because expiry is a decision about time and a function that reads
// the clock cannot be tested at a boundary.
func (c Credentials) Verify(presented string, now time.Time) bool {
	current := Verify(c.Hash, presented)

	previous := false
	if c.PreviousHash != "" && now.Before(c.PreviousExpiresAt) {
		previous = Verify(c.PreviousHash, presented)
	}

	return current || previous
}

// HasSecret reports whether a current secret exists.
func (c Credentials) HasSecret() bool { return c.Hash != "" }

// Rotate issues a new secret and moves the current one into the overlap
// window.
//
// Rotating while a previous secret is still live is allowed and restarts the
// window. Refusing would leave an operator who is rotating *because of a leak*
// unable to act until an unrelated window closed, which is precisely backwards.
func (c Credentials) Rotate(overlap time.Duration, now time.Time) (Secret, Credentials, error) {
	if !c.HasSecret() {
		return Secret{}, Credentials{}, ErrSecretNotSet
	}

	secret, hash, err := Generate()
	if err != nil {
		return Secret{}, Credentials{}, err
	}

	return secret, Credentials{
		Hash:              hash,
		PreviousHash:      c.Hash,
		PreviousExpiresAt: now.Add(overlap),
	}, nil
}

// RotateNow issues a new secret and retires the old one immediately, with no
// overlap. For a secret believed to be compromised.
func (c Credentials) RotateNow() (Secret, Credentials, error) {
	if !c.HasSecret() {
		return Secret{}, Credentials{}, ErrSecretNotSet
	}

	secret, hash, err := Generate()
	if err != nil {
		return Secret{}, Credentials{}, err
	}
	return secret, Credentials{Hash: hash}, nil
}

package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- HMAC-SHA1 per RFC 6238; see the note below.
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238 TOTP (P3-02).
//
// The algorithm is small and worth having in the repository rather than in a
// dependency: it is thirty lines, it is frozen — RFC 6238 is from 2011 and will
// not change — and a supply-chain compromise of a library that computes second
// factors is a compromise of every second factor at once. `PG-27` (no SBOM) is
// open, which makes a new dependency in the authentication path exactly the
// wrong place to spend.
//
// SHA-1 is not a mistake here. RFC 6238 names HMAC-SHA1 as the default and
// every authenticator app implements it; SHA-256 is permitted and is not
// interoperable in practice. HMAC-SHA1's security does not rest on SHA-1's
// collision resistance, which is the property that is broken.

const (
	// TOTPDigits is six, which is what every authenticator app shows.
	TOTPDigits = 6

	// TOTPPeriod is thirty seconds, RFC 6238's default.
	TOTPPeriod = 30 * time.Second

	// TOTPSkew is how many steps either side of now are accepted.
	//
	// **Exactly one** (`P3-02` step 5). One step each way tolerates a 30-second
	// clock difference, which covers a phone that has not synchronised
	// recently. Two steps would make the accepted window 150 seconds, and the
	// window is the attack surface: every extra step is another 30 seconds in
	// which a shoulder-surfed or phished code still works.
	TOTPSkew = 1

	// TOTPSecretBytes is the length of a generated secret.
	//
	// 20 bytes — 160 bits — which is RFC 4226's recommendation and the length
	// every authenticator app handles. It base32-encodes to 32 characters,
	// which is short enough to type by hand when a camera is not available.
	TOTPSecretBytes = 20
)

// NewTOTPSecret generates a shared secret.
func NewTOTPSecret() ([]byte, error) {
	secret := make([]byte, TOTPSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("mfa: generating a TOTP secret: %w", err)
	}
	return secret, nil
}

// EncodeTOTPSecret renders a secret for an authenticator app.
//
// Base32 without padding, upper-case: what every app expects and what a person
// can retype from a screen. The padding is dropped because several popular
// apps reject a secret containing `=`.
func EncodeTOTPSecret(secret []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// DecodeTOTPSecret reads a secret back.
//
// Tolerates the padding and the lower case that a person retyping one produces,
// because refusing a secret over its case is a support ticket rather than a
// security control.
func DecodeTOTPSecret(encoded string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(encoded, " ", "")))
	cleaned = strings.TrimRight(cleaned, "=")

	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("mfa: decoding a TOTP secret: %w", err)
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("mfa: the TOTP secret is empty")
	}
	return secret, nil
}

// TOTPCode computes the code for one counter step.
func TOTPCode(secret []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// RFC 4226 § 5.3's dynamic truncation.
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	return fmt.Sprintf("%0*d", TOTPDigits, value%pow10(TOTPDigits))
}

// TOTPCounter is the step number for an instant.
//
// The conversion is safe for every instant this can be called with. `at.Unix()`
// is negative only before 1970, and the counter is a step number for a code
// being presented now — a clock that far wrong produces no valid code by any
// route. Above, an int64 of seconds cannot exceed uint64's range at all.
func TOTPCounter(at time.Time) uint64 {
	// #nosec G115 -- see above: seconds since the epoch, for a live verification.
	return uint64(at.Unix()) / uint64(TOTPPeriod.Seconds())
}

// VerifyTOTP checks a code against a secret, and reports WHICH step matched.
//
// The counter is returned so the caller can refuse a replay: a code is valid
// for its whole 30-second step, and within that window it would otherwise be
// accepted as many times as it is presented. `P3-02` step 6 asks for exactly
// this, and the counter is what makes it enforceable — remembering the code
// itself would mean storing a live credential.
//
// Constant-time comparison, because a timing difference across the digits of a
// six-digit code is a meaningful oracle when an attacker may present a million
// of them.
func VerifyTOTP(secret []byte, code string, at time.Time) (counter uint64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != TOTPDigits {
		// An early-out, not the control. `hmac.Equal` below is length-
		// sensitive and would refuse a short code anyway — the mutation run
		// confirmed that removing this changes no behaviour.
		//
		// It stays because a candidate of the wrong length is not a candidate,
		// and computing three HMACs to reach an answer already available is
		// work an unauthenticated caller can ask for a million times.
		return 0, false
	}

	now := TOTPCounter(at)

	// The whole accepted window is scanned even after a match, so the time
	// taken does not depend on WHICH step matched. A version that returned
	// early would tell an attacker whether their clock was ahead or behind.
	var matched uint64
	var found bool

	for step := -TOTPSkew; step <= TOTPSkew; step++ {
		candidate := now + uint64(step) // wraps correctly for negative steps
		if hmac.Equal([]byte(TOTPCode(secret, candidate)), []byte(code)) {
			matched = candidate
			found = true
		}
	}
	return matched, found
}

// TOTPProvisioningURI is the `otpauth://` URI an authenticator app scans.
//
// The issuer appears twice — as a path prefix and as a parameter — which looks
// like a mistake and is what the de-facto standard requires: apps that predate
// the parameter read the prefix, and apps that postdate it read the parameter.
// Omitting either makes the entry appear under the wrong name in somebody's
// app, which is how a user with three accounts deletes the wrong one.
func TOTPProvisioningURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)

	query := url.Values{}
	query.Set("secret", EncodeTOTPSecret(secret))
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(TOTPDigits))
	query.Set("period", fmt.Sprint(int(TOTPPeriod.Seconds())))

	return "otpauth://totp/" + label + "?" + query.Encode()
}

func pow10(n int) uint32 {
	out := uint32(1)
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}

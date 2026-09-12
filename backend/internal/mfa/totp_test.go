package mfa

import (
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238 TOTP (P3-02).
//
// The first test is the one that matters most: the algorithm is checked against
// the RFC's own published vectors. An implementation that is self-consistent
// but wrong produces codes no authenticator app agrees with, and the symptom is
// every user's enrolment failing for a reason nobody can see from this side.

// RFC 6238 Appendix B, the SHA-1 rows.
//
// The RFC's test secret is the ASCII string "12345678901234567890" — 20 bytes,
// which is why TOTPSecretBytes is 20. The published values are 8 digits; this
// service emits 6, so each expectation is the last six of the RFC's value,
// which is what truncating to six digits produces.
func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	secret := []byte("12345678901234567890")

	for _, c := range []struct {
		unix int64
		want string // the last six digits of RFC 6238 Appendix B's SHA-1 value
	}{
		{59, "287082"},          // RFC: 94287082
		{1111111109, "081804"},  // RFC: 07081804
		{1111111111, "050471"},  // RFC: 14050471
		{1234567890, "005924"},  // RFC: 89005924
		{2000000000, "279037"},  // RFC: 69279037
		{20000000000, "353130"}, // RFC: 65353130
	} {
		at := time.Unix(c.unix, 0).UTC()
		got := TOTPCode(secret, TOTPCounter(at))
		if got != c.want {
			t.Errorf("at %d the code is %s, want %s — this implementation disagrees with "+
				"RFC 6238, so no authenticator app will agree with it either", c.unix, got, c.want)
		}
	}
}

// A code is six digits, always.
func TestACodeIsAlwaysSixDigits(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}

	for counter := uint64(0); counter < 2000; counter++ {
		code := TOTPCode(secret, counter)
		if len(code) != TOTPDigits {
			t.Fatalf("counter %d produced %q, which is %d digits", counter, code, len(code))
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("counter %d produced %q, which is not all digits", counter, code)
			}
		}
	}
}

// **Clock skew is exactly one step each way** (`P3-02` step 5, and its DoD).
//
// One step tolerates a 30-second clock difference, which covers a phone that
// has not synchronised recently. Every extra step is another 30 seconds in
// which a shoulder-surfed code still works, so the bound is asserted from both
// directions: the steps inside it are accepted, and the first step outside it
// is not.
func TestClockSkewIsExactlyOneStepEachWay(t *testing.T) {
	secret, _ := NewTOTPSecret()
	at := time.Unix(1_700_000_000, 0).UTC()
	now := TOTPCounter(at)

	for _, c := range []struct {
		offset int
		accept bool
		what   string
	}{
		{-2, false, "two steps behind"},
		{-1, true, "one step behind"},
		{0, true, "the current step"},
		{+1, true, "one step ahead"},
		{+2, false, "two steps ahead"},
	} {
		code := TOTPCode(secret, now+uint64(c.offset))
		matched, ok := VerifyTOTP(secret, code, at)

		if ok != c.accept {
			t.Errorf("a code from %s was accepted=%v, want %v", c.what, ok, c.accept)
		}
		if ok && matched != now+uint64(c.offset) {
			t.Errorf("a code from %s matched step %d, want %d", c.what, matched, now+uint64(c.offset))
		}
	}
}

// The counter a verification matched is returned, which is what makes the
// replay bound enforceable.
func TestVerifyReportsWhichStepMatched(t *testing.T) {
	secret, _ := NewTOTPSecret()
	at := time.Unix(1_700_000_000, 0).UTC()

	counter, ok := VerifyTOTP(secret, TOTPCode(secret, TOTPCounter(at)), at)
	if !ok {
		t.Fatal("the current step's own code did not verify")
	}
	if counter != TOTPCounter(at) {
		t.Errorf("the matched counter is %d, want %d", counter, TOTPCounter(at))
	}
}

// A code of the wrong length is refused without pretending to check it.
func TestACodeOfTheWrongLengthIsRefused(t *testing.T) {
	secret, _ := NewTOTPSecret()
	at := time.Now()

	for _, code := range []string{"", "1", "12345", "1234567", "abcdef "} {
		if _, ok := VerifyTOTP(secret, code, at); ok {
			t.Errorf("%q was accepted as a code", code)
		}
	}
}

// Surrounding whitespace is tolerated.
//
// People paste codes, and a space either side is not a wrong code — it is a
// clipboard. Refusing it is a support ticket that teaches nobody anything.
func TestSurroundingWhitespaceIsTolerated(t *testing.T) {
	secret, _ := NewTOTPSecret()
	at := time.Unix(1_700_000_000, 0).UTC()
	code := TOTPCode(secret, TOTPCounter(at))

	if _, ok := VerifyTOTP(secret, " "+code+" ", at); !ok {
		t.Error("a correct code with spaces around it was refused")
	}
}

// A different secret does not verify.
func TestADifferentSecretDoesNotVerify(t *testing.T) {
	mine, _ := NewTOTPSecret()
	theirs, _ := NewTOTPSecret()
	at := time.Unix(1_700_000_000, 0).UTC()

	if _, ok := VerifyTOTP(mine, TOTPCode(theirs, TOTPCounter(at)), at); ok {
		t.Error("a code generated from a different secret verified")
	}
}

// --- the secret ---------------------------------------------------------------

// A generated secret is 160 bits of randomness, and two are never the same.
func TestGeneratedSecretsAreLongAndUnique(t *testing.T) {
	seen := map[string]bool{}

	for i := 0; i < 500; i++ {
		secret, err := NewTOTPSecret()
		if err != nil {
			t.Fatalf("NewTOTPSecret: %v", err)
		}
		if len(secret) != TOTPSecretBytes {
			t.Fatalf("a secret is %d bytes, want %d", len(secret), TOTPSecretBytes)
		}
		encoded := EncodeTOTPSecret(secret)
		if seen[encoded] {
			t.Fatal("two generated secrets collided in five hundred draws")
		}
		seen[encoded] = true
	}
}

// The encoding is what an authenticator app accepts: base32, no padding.
func TestTheSecretEncodingIsUnpaddedBase32(t *testing.T) {
	secret, _ := NewTOTPSecret()
	encoded := EncodeTOTPSecret(secret)

	if strings.Contains(encoded, "=") {
		t.Error("the encoded secret contains padding, which several popular apps reject")
	}
	if encoded != strings.ToUpper(encoded) {
		t.Error("the encoded secret is not upper-case")
	}
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded); err != nil {
		t.Errorf("the encoded secret is not valid base32: %v", err)
	}
}

// A secret round-trips, including through the forms a person retyping produces.
func TestASecretSurvivesRetyping(t *testing.T) {
	secret, _ := NewTOTPSecret()
	encoded := EncodeTOTPSecret(secret)

	for _, variant := range []string{
		encoded,
		strings.ToLower(encoded),
		"  " + encoded + "  ",
		encoded + "======",
		strings.Join([]string{encoded[:8], encoded[8:16], encoded[16:]}, " "),
	} {
		got, err := DecodeTOTPSecret(variant)
		if err != nil {
			t.Errorf("%q failed to decode: %v", variant, err)
			continue
		}
		if string(got) != string(secret) {
			t.Errorf("%q decoded to a different secret", variant)
		}
	}
}

func TestAnEmptySecretIsRefused(t *testing.T) {
	if _, err := DecodeTOTPSecret(""); err == nil {
		t.Error("an empty string decoded into a secret")
	}
	if _, err := DecodeTOTPSecret("not base32!!!"); err == nil {
		t.Error("a malformed secret decoded")
	}
}

// --- the provisioning URI -------------------------------------------------------

// The URI carries everything an app needs, and the issuer twice.
//
// The duplication looks like a mistake and is what the de-facto standard
// requires: apps that predate the parameter read the path prefix, apps that
// postdate it read the parameter. Omitting either puts the entry under the
// wrong name in somebody's app, which is how a user with three accounts deletes
// the wrong one.
func TestTheProvisioningURICarriesWhatAnAppNeeds(t *testing.T) {
	secret, _ := NewTOTPSecret()
	uri := TOTPProvisioningURI("Zed Auth", "someone@example.test", secret)

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("the URI does not parse: %v", err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Errorf("the URI is %s://%s, want otpauth://totp", parsed.Scheme, parsed.Host)
	}

	query := parsed.Query()
	if query.Get("secret") != EncodeTOTPSecret(secret) {
		t.Error("the URI does not carry the secret an app would store")
	}
	if query.Get("issuer") != "Zed Auth" {
		t.Errorf("the issuer parameter is %q", query.Get("issuer"))
	}
	if query.Get("digits") != "6" || query.Get("period") != "30" || query.Get("algorithm") != "SHA1" {
		t.Errorf("the URI describes a different algorithm than this service computes: %v", query)
	}

	// The path prefix, for apps that predate the parameter.
	if !strings.Contains(parsed.Path, "Zed%20Auth") && !strings.Contains(parsed.Path, "Zed Auth") {
		t.Errorf("the label has no issuer prefix: %q", parsed.Path)
	}
}

// An account with a colon or a space in it does not break the label.
func TestAnAwkwardAccountNameIsEscaped(t *testing.T) {
	secret, _ := NewTOTPSecret()
	uri := TOTPProvisioningURI("Zed Auth", "first last:weird@example.test", secret)

	if _, err := url.Parse(uri); err != nil {
		t.Errorf("an account name with a colon and a space produced an unparseable URI: %v", err)
	}
}

// --- sealing ---------------------------------------------------------------------

func TestASealedSecretOpensAgain(t *testing.T) {
	sealer, err := NewSealer([]byte("a-key-long-enough-to-be-random-material"))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	secret, _ := NewTOTPSecret()
	sealed, err := sealer.Seal(secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if string(sealed) == string(secret) {
		t.Fatal("the sealed value is the plaintext")
	}

	opened, err := sealer.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(opened) != string(secret) {
		t.Error("the secret did not survive the round trip")
	}
}

// Two seals of one secret differ, so the column does not leak equality.
//
// Without a fresh nonce, two users with the same secret — or one user's secret
// stored twice — would produce identical ciphertext, and anybody reading the
// column could tell. It would also break GCM outright.
func TestTwoSealsOfOneSecretDiffer(t *testing.T) {
	sealer, _ := NewSealer([]byte("a-key-long-enough-to-be-random-material"))
	secret, _ := NewTOTPSecret()

	first, _ := sealer.Seal(secret)
	second, _ := sealer.Seal(secret)

	if string(first) == string(second) {
		t.Error("two seals of one secret are identical — the nonce is not fresh, which " +
			"both leaks equality and breaks GCM")
	}
}

// A tampered ciphertext is an error, not a secret that verifies nothing.
//
// The difference matters to the user: a silent wrong answer presents as "your
// authenticator app stopped working" and sends somebody to their recovery
// codes, while an error is an operator's problem and reads like one.
func TestATamperedCiphertextIsRefused(t *testing.T) {
	sealer, _ := NewSealer([]byte("a-key-long-enough-to-be-random-material"))
	secret, _ := NewTOTPSecret()
	sealed, _ := sealer.Seal(secret)

	tampered := make([]byte, len(sealed))
	copy(tampered, sealed)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := sealer.Open(tampered); err == nil {
		t.Error("a tampered ciphertext opened")
	}
}

// A different key does not open it.
func TestADifferentKeyDoesNotOpenASeal(t *testing.T) {
	mine, _ := NewSealer([]byte("a-key-long-enough-to-be-random-material"))
	theirs, _ := NewSealer([]byte("a-different-key-also-long-enough-here"))

	secret, _ := NewTOTPSecret()
	sealed, _ := mine.Seal(secret)

	if _, err := theirs.Open(sealed); err == nil {
		t.Error("a seal opened under a different key")
	}
}

// No key means refusal, never plaintext.
//
// A service that stores secrets unencrypted because a key was missing is one
// whose security depends on nobody having made a configuration mistake — and
// the mistake is invisible, because everything keeps working.
func TestWithoutAKeyNothingIsSealedOrOpened(t *testing.T) {
	var none *Sealer

	if none.Configured() {
		t.Error("a nil sealer reports itself as configured")
	}
	if _, err := none.Seal([]byte("secret")); err == nil {
		t.Error("sealing without a key returned no error — which means plaintext")
	}
	if _, err := none.Open([]byte("anything")); err == nil {
		t.Error("opening without a key returned no error")
	}

	if _, err := NewSealer(nil); err == nil {
		t.Error("a sealer was built from no key material")
	}
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Error("a sealer was built from five bytes of key material")
	}
}

func TestAnEmptySecretIsNotSealed(t *testing.T) {
	sealer, _ := NewSealer([]byte("a-key-long-enough-to-be-random-material"))

	if _, err := sealer.Seal(nil); err == nil {
		t.Error("an empty secret was sealed — the factor would read as enrolled and " +
			"verify nothing")
	}
}

func TestATruncatedCiphertextIsRefused(t *testing.T) {
	sealer, _ := NewSealer([]byte("a-key-long-enough-to-be-random-material"))

	for _, short := range [][]byte{nil, {1}, make([]byte, 8)} {
		if _, err := sealer.Open(short); err == nil {
			t.Errorf("a %d-byte value opened", len(short))
		}
	}
}

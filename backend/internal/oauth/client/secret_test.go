package client

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- generation -------------------------------------------------------------

// The SHA-256 decision in secret.go rests entirely on the secret being
// high-entropy and generated here. This test is what holds that up: weaken the
// length or the alphabet and the hashing choice becomes wrong, silently.
func TestSecretEntropy(t *testing.T) {
	secret, hash, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	plaintext := secret.Reveal()

	// 32 bytes, base64url without padding, is 43 characters.
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		t.Fatalf("the secret is not base64url: %v", err)
	}
	if len(decoded) != secretBytes {
		t.Errorf("secret carries %d bytes of entropy, want %d — the hashing choice "+
			"in secret.go depends on this number", len(decoded), secretBytes)
	}
	if secretBytes < 32 {
		t.Errorf("secretBytes is %d; below 32 the argument for SHA-256 over a slow KDF "+
			"no longer holds", secretBytes)
	}

	if hash == plaintext {
		t.Fatal("the stored hash IS the plaintext")
	}
	if strings.Contains(hash, plaintext) {
		t.Fatal("the stored hash contains the plaintext")
	}
}

// Two calls must differ. A generator returning a constant would pass every
// other test in this file.
func TestGenerateIsNotDeterministic(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		secret, _, err := Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if seen[secret.Reveal()] {
			t.Fatal("Generate returned a duplicate secret")
		}
		seen[secret.Reveal()] = true
	}
}

// --- the Secret type does not leak ------------------------------------------

// Abuse case A-7. The type exists to make accidental disclosure impossible, so
// every formatting verb a logger might reach for is checked, not just %s.
func TestSecretDoesNotPrintItself(t *testing.T) {
	secret, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	plaintext := secret.Reveal()

	for _, format := range []string{"%s", "%v", "%q", "%#v", "%+v", "%d"} {
		rendered := fmt.Sprintf(format, secret)
		if strings.Contains(rendered, plaintext) {
			t.Errorf("Sprintf(%q, secret) leaked the secret: %s", format, rendered)
		}
	}

	// And inside a struct, which is how it would actually reach a log.
	wrapper := struct {
		Name   string
		Secret Secret
	}{Name: "billing", Secret: secret}

	if rendered := fmt.Sprintf("%+v", wrapper); strings.Contains(rendered, plaintext) {
		t.Errorf("a struct containing a Secret leaked it: %s", rendered)
	}
}

// Marshalling refuses rather than redacting: a JSON field named client_secret
// containing "[REDACTED]" is a string a consumer would store and then fail to
// authenticate with, having been told nothing was wrong.
func TestSecretRefusesToMarshal(t *testing.T) {
	secret, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	encoded, err := json.Marshal(struct {
		Secret Secret `json:"client_secret"`
	}{Secret: secret})

	if err == nil {
		t.Fatalf("marshalling a Secret succeeded, producing: %s", encoded)
	}
	if strings.Contains(err.Error(), secret.Reveal()) {
		t.Error("the marshalling error itself contains the secret")
	}
}

// The control on the two tests above: they would both pass against a Secret
// that was simply always empty, so a real value must survive Reveal.
func TestRevealReturnsTheSecret(t *testing.T) {
	secret, hash, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if secret.Reveal() == "" {
		t.Fatal("Reveal returned empty; the redaction tests above would pass vacuously")
	}
	if !Verify(hash, secret.Reveal()) {
		t.Fatal("the revealed secret does not verify against its own hash")
	}
}

// --- verification -----------------------------------------------------------

func TestVerify(t *testing.T) {
	secret, hash, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	other, _, _ := Generate()

	cases := []struct {
		name      string
		hash      string
		presented string
		want      bool
	}{
		{"correct", hash, secret.Reveal(), true},
		{"a different secret", hash, other.Reveal(), false},
		{"empty presented", hash, "", false},
		{"empty stored hash", "", secret.Reveal(), false},
		{"both empty", "", "", false},
		{"the hash presented as the secret", hash, hash, false},
		{"a prefix of the secret", hash, secret.Reveal()[:20], false},
		{"the secret with a trailing character", hash, secret.Reveal() + "x", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Verify(tc.hash, tc.presented); got != tc.want {
				t.Errorf("Verify() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- rotation ---------------------------------------------------------------

// P1-05 DoD item 4: both secrets work during the overlap, only the new one
// after it.
func TestRotationOverlap(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	oldSecret, oldHash, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	creds := Credentials{Hash: oldHash}

	newSecret, rotated, err := creds.Rotate(DefaultRotationOverlap, now)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	during := now.Add(DefaultRotationOverlap / 2)
	after := now.Add(DefaultRotationOverlap + time.Second)

	cases := []struct {
		name      string
		presented string
		at        time.Time
		want      bool
	}{
		{"new secret, immediately", newSecret.Reveal(), now, true},
		{"old secret, during the overlap", oldSecret.Reveal(), during, true},
		{"new secret, during the overlap", newSecret.Reveal(), during, true},
		{"old secret, after expiry", oldSecret.Reveal(), after, false},
		{"new secret, after expiry", newSecret.Reveal(), after, true},
		{"old secret exactly at expiry", oldSecret.Reveal(), rotated.PreviousExpiresAt, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rotated.Verify(tc.presented, tc.at); got != tc.want {
				t.Errorf("Verify() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Rotating during an open window is allowed, and it restarts the window.
// Refusing would leave an operator rotating *because of a leak* blocked by an
// unrelated planned rotation, which is exactly backwards.
func TestRotationDuringAnOpenWindowIsAllowed(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	first, hash, _ := Generate()
	creds := Credentials{Hash: hash}

	second, afterFirst, err := creds.Rotate(DefaultRotationOverlap, now)
	if err != nil {
		t.Fatalf("first rotation: %v", err)
	}

	later := now.Add(time.Hour)
	third, afterSecond, err := afterFirst.Rotate(DefaultRotationOverlap, later)
	if err != nil {
		t.Fatalf("second rotation: %v", err)
	}

	if !afterSecond.Verify(third.Reveal(), later) {
		t.Error("the newest secret does not verify")
	}
	if !afterSecond.Verify(second.Reveal(), later) {
		t.Error("the immediately-previous secret does not verify during its window")
	}
	// The first secret is two rotations back. Only one previous is retained,
	// so it is gone — which is the intended bound on how long an old
	// credential stays useful.
	if afterSecond.Verify(first.Reveal(), later) {
		t.Error("a secret from two rotations ago still verifies; only one previous is kept")
	}
}

// For a secret believed compromised there is nothing to trade, so the overlap
// is skipped entirely.
func TestRotateNowLeavesNoWindow(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	old, hash, _ := Generate()
	creds := Credentials{Hash: hash}

	fresh, rotated, err := creds.RotateNow()
	if err != nil {
		t.Fatalf("RotateNow: %v", err)
	}

	if !rotated.Verify(fresh.Reveal(), now) {
		t.Error("the new secret does not verify")
	}
	if rotated.Verify(old.Reveal(), now) {
		t.Error("the old secret still verifies after an immediate rotation")
	}
	if rotated.PreviousHash != "" {
		t.Error("an immediate rotation left a previous hash behind")
	}
}

// Abuse case: rotating a client that has no secret. A public client has
// nothing to rotate, and the error says so rather than silently issuing one —
// which would put a secret on a client the database CHECK forbids.
func TestRotatingWithoutASecretFails(t *testing.T) {
	if _, _, err := (Credentials{}).Rotate(DefaultRotationOverlap, time.Now()); err == nil {
		t.Error("Rotate succeeded on an application with no secret")
	}
	if _, _, err := (Credentials{}).RotateNow(); err == nil {
		t.Error("RotateNow succeeded on an application with no secret")
	}
}

// An expired window must be indistinguishable from no window at all: an
// attacker presenting a long-retired secret learns nothing about whether it
// was ever valid.
func TestExpiredPreviousSecretIsJustWrong(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	old, hash, _ := Generate()
	creds := Credentials{Hash: hash}
	_, rotated, _ := creds.Rotate(time.Hour, now)

	unrelated, _, _ := Generate()
	after := now.Add(2 * time.Hour)

	if rotated.Verify(old.Reveal(), after) != rotated.Verify(unrelated.Reveal(), after) {
		t.Error("an expired secret is distinguishable from an unrelated one")
	}
}

func TestHasSecret(t *testing.T) {
	if (Credentials{}).HasSecret() {
		t.Error("empty Credentials reported as having a secret")
	}
	_, hash, _ := Generate()
	if !(Credentials{Hash: hash}).HasSecret() {
		t.Error("Credentials with a hash reported as having none")
	}
}

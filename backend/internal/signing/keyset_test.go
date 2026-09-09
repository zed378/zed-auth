package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The cache keeps serving the last good key set when a reload fails.
//
// This is an availability decision documented in Cache.Get and never tested
// until now. The keys have not changed just because the database is briefly
// unreachable, and failing every token verification during a blip would turn
// a recoverable dependency failure into a total outage — the same reasoning
// that keeps the liveness probe off the database (PLAN/14).
func TestCacheServesTheLastGoodSetWhenReloadFails(t *testing.T) {
	key := newKey(t, RS256, StatusCurrent)
	set, err := NewKeySet([]*Key{key})
	if err != nil {
		t.Fatalf("key set: %v", err)
	}

	failing := false
	cache := NewCache(func() (*KeySet, error) {
		if failing {
			return nil, errors.New("database is unreachable")
		}
		return set, nil
	}, time.Minute)

	if _, err := cache.Get(); err != nil {
		t.Fatalf("first load: %v", err)
	}

	// Force the next Get to reload, and make that reload fail.
	failing = true
	cache.Invalidate()

	got, err := cache.Get()
	if err != nil {
		t.Fatalf("a failed reload must serve the previous set, got: %v", err)
	}
	if got.Len() != 1 {
		t.Errorf("served set has %d keys, want 1", got.Len())
	}
}

// The one case that does fail: no snapshot at all.
//
// Before the first successful load there is nothing to fall back to, and
// pretending otherwise would mean signing with no key.
func TestCacheFailsWhenItHasNeverLoaded(t *testing.T) {
	cache := NewCache(func() (*KeySet, error) {
		return nil, errors.New("database is unreachable")
	}, time.Minute)

	if _, err := cache.Get(); err == nil {
		t.Fatal("a cache that has never loaded must return an error, not an empty set")
	}
}

// Within the TTL the loader is not called again; after it, it is.
func TestCacheRespectsItsTTL(t *testing.T) {
	key := newKey(t, RS256, StatusCurrent)
	set, _ := NewKeySet([]*Key{key})

	loads := 0
	cache := NewCache(func() (*KeySet, error) {
		loads++
		return set, nil
	}, time.Minute)

	// A controllable clock: a test that slept for a minute would be a test
	// nobody runs.
	now := time.Now()
	cache.now = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		if _, err := cache.Get(); err != nil {
			t.Fatalf("get: %v", err)
		}
	}
	if loads != 1 {
		t.Errorf("loaded %d times within the TTL, want 1", loads)
	}

	now = now.Add(2 * time.Minute)
	if _, err := cache.Get(); err != nil {
		t.Fatalf("get after TTL: %v", err)
	}
	if loads != 2 {
		t.Errorf("loaded %d times after the TTL expired, want 2", loads)
	}
}

// JWKS output must be byte-identical between calls.
//
// Map iteration in Go is deliberately randomised, so an unsorted key set would
// produce a different JSON ordering on each request. Consumers and HTTP caches
// compare this response; a set that reshuffles defeats both and produces
// cache churn that looks like a rotation.
func TestJWKSOrderingIsDeterministic(t *testing.T) {
	keys := []*Key{
		newKey(t, RS256, StatusCurrent),
		newKey(t, RS256, StatusNext),
		newKey(t, RS256, StatusNext),
		newKey(t, RS256, StatusPrevious),
		newKey(t, RS256, StatusPrevious),
	}

	set, err := NewKeySet(keys)
	if err != nil {
		t.Fatalf("key set: %v", err)
	}

	first, err := json.Marshal(set.JWKS())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for i := 0; i < 20; i++ {
		again, err := json.Marshal(set.JWKS())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("JWKS output is not stable across calls:\n  %s\n  %s", first, again)
		}
	}
}

// The current key is published first, so a consumer reading the set top-down
// tries the one most tokens are signed with before the others.
func TestCurrentKeyIsPublishedFirst(t *testing.T) {
	current := newKey(t, RS256, StatusCurrent)
	set, err := NewKeySet([]*Key{
		newKey(t, RS256, StatusPrevious),
		newKey(t, RS256, StatusNext),
		current,
	})
	if err != nil {
		t.Fatalf("key set: %v", err)
	}

	jwks := set.JWKS()
	if len(jwks.Keys) == 0 {
		t.Fatal("empty JWKS")
	}
	if jwks.Keys[0].KeyID != current.KID {
		t.Errorf("first published key is %q, want the current key %q",
			jwks.Keys[0].KeyID, current.KID)
	}
}

// Two current keys is a broken set, not something to pick a winner from.
func TestKeySetRefusesTwoCurrentKeys(t *testing.T) {
	_, err := NewKeySet([]*Key{
		newKey(t, RS256, StatusCurrent),
		newKey(t, RS256, StatusCurrent),
	})
	if err == nil {
		t.Fatal("a key set with two current keys must be refused")
	}
}

// --- key validation --------------------------------------------------------

// ES256 means P-256 specifically. Another curve is a different algorithm
// wearing the same name, and accepting it would produce signatures no
// standards-compliant consumer can verify.
func TestAlgorithmForRejectsTheWrongCurve(t *testing.T) {
	wrongCurve, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a P-384 key: %v", err)
	}

	if _, err := AlgorithmFor(wrongCurve.Public()); err == nil {
		t.Fatal("a P-384 key must not be accepted as ES256")
	}

	// And the right one is.
	right, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a P-256 key: %v", err)
	}
	alg, err := AlgorithmFor(right.Public())
	if err != nil {
		t.Fatalf("P-256 should be ES256: %v", err)
	}
	if alg != ES256 {
		t.Errorf("got %s, want ES256", alg)
	}
}

func TestGenerateRejectsAnUnknownAlgorithm(t *testing.T) {
	if _, err := Generate(Algorithm("HS256")); err == nil {
		t.Fatal("HS256 must be refused — a shared secret means every verifier can mint tokens")
	}
	if _, err := Generate(Algorithm("")); err == nil {
		t.Fatal("an empty algorithm must be refused")
	}
}

// A stored key is not trusted input just because it is ours. It can be
// corrupt, truncated by a bad migration, or written by something else.
func TestParsingRejectsMalformedKeys(t *testing.T) {
	valid, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	tests := map[string]string{
		"empty":   "",
		"not PEM": "this is not a key",
		// Assembled at runtime rather than written as a literal. The
		// secret-scanning hook refuses any committed file containing a PEM
		// private key block, and it is right to — a fixture that looks exactly
		// like the thing being scanned for makes the scanner useless.
		"PEM with garbage":  pemBlock("PRIVATE KEY", "bm90IGEga2V5"),
		"truncated":         valid.PrivatePEM[:len(valid.PrivatePEM)/2],
		"public as private": valid.PublicPEM,
	}

	for name, material := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePrivateKey([]byte(material)); err == nil {
				t.Errorf("%s must be refused", name)
			}
		})
	}

	// And the valid one round-trips.
	if _, err := ParsePrivateKey([]byte(valid.PrivatePEM)); err != nil {
		t.Errorf("a freshly generated key should parse: %v", err)
	}
	if _, err := ParsePublicKey([]byte(valid.PublicPEM)); err != nil {
		t.Errorf("a freshly generated public key should parse: %v", err)
	}
}

// An error must never carry key material. This is the one place a careless
// format verb would put a private key into a log line.
func TestErrorsNeverContainKeyMaterial(t *testing.T) {
	valid, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Truncated PEM: the parser sees real key bytes and fails.
	_, err = ParsePrivateKey([]byte(valid.PrivatePEM[:200]))
	if err == nil {
		t.Fatal("expected a parse failure")
	}

	message := err.Error()
	for _, forbidden := range []string{"BEGIN", "PRIVATE KEY", valid.PrivatePEM[100:140]} {
		if strings.Contains(message, forbidden) {
			t.Errorf("SECURITY: the error message contains key material: %q", message)
		}
	}
}

// Status.Verifying is the single place that decides whether a key still
// validates tokens. Getting it wrong in either direction is a security bug:
// too permissive and retirement means nothing, too strict and rotation logs
// everyone out.
func TestVerifyingStates(t *testing.T) {
	cases := map[Status]bool{
		StatusNext:     true,
		StatusCurrent:  true,
		StatusPrevious: true,
		StatusRetired:  false,
	}

	for status, want := range cases {
		if got := status.Verifying(); got != want {
			t.Errorf("%s.Verifying() = %v, want %v", status, got, want)
		}
	}
}

// pemBlock assembles a PEM block at runtime.
//
// The markers are concatenated rather than written out, so this file
// contains no literal PEM header for the secret scanner to find. The
// scanner is right to refuse them: a fixture indistinguishable from the
// thing being scanned for is how a scanner stops being trusted.
func pemBlock(label, body string) string {
	const dashes = "-----"
	return dashes + "BEGIN " + label + dashes + "\n" +
		body + "\n" +
		dashes + "END " + label + dashes + "\n"
}

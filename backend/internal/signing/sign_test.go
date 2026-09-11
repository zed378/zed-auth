package signing

import (
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// testCache builds a cache over a fixed key set, with no database.
func testCache(t testing.TB, keys ...*Key) *Cache {
	t.Helper()

	set, err := NewKeySet(keys)
	if err != nil {
		t.Fatalf("building key set: %v", err)
	}

	return NewCache(func() (*KeySet, error) { return set, nil }, time.Minute)
}

// newKey generates a key in the given state.
func newKey(t testing.TB, alg Algorithm, status Status) *Key {
	t.Helper()

	pair, err := Generate(alg)
	if err != nil {
		t.Fatalf("generating %s key: %v", alg, err)
	}

	key := &Key{
		KID:       pair.KID,
		Algorithm: alg,
		Status:    status,
		Public:    pair.Public,
	}
	if status == StatusCurrent {
		key.private = pair.Private
	}
	return key
}

// --- the happy path --------------------------------------------------------

func TestSignAndVerifyRoundTrip(t *testing.T) {
	for _, alg := range []Algorithm{RS256, ES256} {
		t.Run(string(alg), func(t *testing.T) {
			cache := testCache(t, newKey(t, alg, StatusCurrent))
			payload := []byte(`{"sub":"usr_1","iss":"https://auth.example"}`)

			token, err := NewSigner(cache).Sign(payload)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}

			got, err := NewVerifier(cache).Verify(token, TypeJWT)
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if string(got) != string(payload) {
				t.Errorf("payload = %s, want %s", got, payload)
			}
		})
	}
}

// The kid must be in the header, or a verifier has to guess which key to try.
func TestSignedTokenCarriesItsKID(t *testing.T) {
	key := newKey(t, RS256, StatusCurrent)
	cache := testCache(t, key)

	token, err := NewSigner(cache).Sign([]byte(`{}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	parsed, err := jose.ParseSigned(token, allowedAlgorithms)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := parsed.Signatures[0].Header.KeyID; got != key.KID {
		t.Errorf("kid = %q, want %q", got, key.KID)
	}
}

// --- the rotation property that matters ------------------------------------

// docs/PLAN/09's overlap period, and P1-03's Definition of Done: a token signed
// before a rotation must still verify after it.
//
// This is the test the whole four-state design exists for. If it fails, every
// rotation logs every user out.
func TestTokenSignedByPreviousKeyStillVerifies(t *testing.T) {
	oldKey := newKey(t, RS256, StatusCurrent)

	before := testCache(t, oldKey)
	token, err := NewSigner(before).Sign([]byte(`{"sub":"usr_1"}`))
	if err != nil {
		t.Fatalf("sign before rotation: %v", err)
	}

	// Rotate: the old key becomes previous, a new one becomes current.
	demoted := &Key{
		KID:       oldKey.KID,
		Algorithm: oldKey.Algorithm,
		Status:    StatusPrevious,
		Public:    oldKey.Public,
		// No private key: a previous key must not be able to sign.
	}
	after := testCache(t, demoted, newKey(t, RS256, StatusCurrent))

	if _, err := NewVerifier(after).Verify(token, TypeJWT); err != nil {
		t.Fatalf("a token signed before rotation must still verify: %v", err)
	}
}

// Retirement has to actually end a key's life, or it means nothing.
func TestTokenSignedByRetiredKeyIsRejected(t *testing.T) {
	oldKey := newKey(t, RS256, StatusCurrent)

	token, err := NewSigner(testCache(t, oldKey)).Sign([]byte(`{}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	retired := &Key{
		KID: oldKey.KID, Algorithm: oldKey.Algorithm,
		Status: StatusRetired, Public: oldKey.Public,
	}
	after := testCache(t, retired, newKey(t, RS256, StatusCurrent))

	_, err = NewVerifier(after).Verify(token, TypeJWT)
	if err == nil {
		t.Fatal("a token signed by a retired key must not verify")
	}
	if !strings.Contains(err.Error(), "retired") {
		t.Errorf("error should say the key is retired, got: %v", err)
	}
}

// Only `current` signs. A `next` key is published early so consumers cache it;
// letting it sign would defeat the point of publishing it first.
func TestOnlyCurrentKeySigns(t *testing.T) {
	for _, status := range []Status{StatusNext, StatusPrevious, StatusRetired} {
		t.Run(string(status), func(t *testing.T) {
			cache := testCache(t, newKey(t, RS256, status))

			if _, err := NewSigner(cache).Sign([]byte(`{}`)); err == nil {
				t.Fatalf("a %s key must not be able to sign", status)
			}
		})
	}
}

// --- abuse cases (spec §12, docs/SECURITY/02 §1) --------------------------------

// A-1. A token claiming no algorithm at all.
//
// The classic JWT bug: a verifier reads `alg` from the header and dispatches
// on it, so `none` selects a path that accepts an empty signature. Here the
// header is checked against a fixed list BEFORE any key is looked up.
// The allowlist itself, because it cannot be observed from outside.
//
// `P1-27` reverted `allowedAlgorithms` to include `none` and `HS256` and every
// black-box test still passed — go-jose refuses an unsigned JWS on its own,
// refuses an HMAC algorithm against an RSA key set, and reports both in a way
// this package maps to ErrAlgorithmNotAllowed either way. The list is real
// defence in depth and it is invisible through `Verify`.
//
// So this asserts the list. It is the only thing that makes widening it a
// deliberate, reviewed edit rather than a line somebody adds to make a
// third-party token work.
func TestTheAlgorithmAllowlistIsExactlyTwoAsymmetricAlgorithms(t *testing.T) {
	want := []jose.SignatureAlgorithm{jose.RS256, jose.ES256}

	if len(allowedAlgorithms) != len(want) {
		t.Fatalf("allowedAlgorithms is %v, want %v", allowedAlgorithms, want)
	}
	for i, alg := range want {
		if allowedAlgorithms[i] != alg {
			t.Errorf("allowedAlgorithms[%d] = %q, want %q", i, allowedAlgorithms[i], alg)
		}
	}

	// Named separately from the equality above, so a failure says WHY rather
	// than printing two lists and leaving the reader to spot the difference.
	for _, alg := range allowedAlgorithms {
		switch alg {
		case "none", "":
			t.Errorf("the allowlist contains %q — a token claiming no algorithm", alg)
		case jose.HS256, jose.HS384, jose.HS512:
			t.Errorf("the allowlist contains %q, a SYMMETRIC algorithm. This service "+
				"publishes its keys, so an HMAC algorithm means anyone holding the "+
				"public key can forge a token", alg)
		}
	}
}

func TestAlgNoneIsRejected(t *testing.T) {
	key := newKey(t, RS256, StatusCurrent)
	cache := testCache(t, key)

	header := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"alg":"none","typ":"JWT","kid":"` + key.KID + `"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker"}`))
	forged := header + "." + payload + "." // empty signature

	_, err := NewVerifier(cache).Verify(forged, TypeJWT)
	if err == nil {
		t.Fatal("SECURITY: a token with alg:none was accepted")
	}

	// **And refused by OUR allowlist, not only by the library.**
	//
	// `P1-27` reverted `allowedAlgorithms` to include `none` and `HS256` and
	// this test still passed: go-jose refuses an unsigned JWS on its own, and
	// an HMAC algorithm against an RSA key set fails on the key type. The
	// protection is real and doubly held — and a test that only asks "was it
	// refused" cannot see the half this package owns.
	//
	// `ErrAlgorithmNotAllowed` is that half. It exists so the log can say
	// "algorithm not allowed", which is an attack signature, rather than
	// "parse error", which is usually a truncated token. Asserting it here
	// means widening the list breaks this test rather than silently changing
	// what the log says about an attack.
	if !errors.Is(err, ErrAlgorithmNotAllowed) {
		t.Errorf("refused with %v, which is not this package's own algorithm check", err)
	}
}

// A-2. Algorithm confusion: HS256 signed with the RSA PUBLIC key.
//
// The attack that makes publishing a public key dangerous if the verifier
// trusts the header. The verifier looks up the key by kid, finds an RSA public
// key, and — if it dispatched on `alg` — would use it as an HMAC shared
// secret. That key is public by definition, so anyone could forge tokens.
func TestAlgorithmConfusionIsRejected(t *testing.T) {
	key := newKey(t, RS256, StatusCurrent)
	cache := testCache(t, key)

	// The attacker's "secret" is the service's public key — the whole point.
	rsaPub, ok := key.Public.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("expected an RSA key, got %T", key.Public)
	}
	publicAsSecret := rsaPub.N.Bytes()

	hmacSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: publicAsSecret},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", key.KID),
	)
	if err != nil {
		t.Fatalf("building the attacker's signer: %v", err)
	}

	signed, err := hmacSigner.Sign([]byte(`{"sub":"attacker","role":"admin"}`))
	if err != nil {
		t.Fatalf("attacker signing: %v", err)
	}
	forged, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("serializing: %v", err)
	}

	_, err = NewVerifier(cache).Verify(forged, TypeJWT)
	if err == nil {
		t.Fatal("SECURITY: an HS256 token signed with the RSA public key was accepted")
	}
	if !strings.Contains(err.Error(), "algorithm") {
		t.Errorf("the rejection should name the algorithm, got: %v", err)
	}
}

// A-3. A kid that matches a legitimate key, on a token signed by someone else.
//
// The kid selects which key to try. It never confers trust — the signature
// still has to verify against that key.
func TestForgedTokenWithLegitimateKIDIsRejected(t *testing.T) {
	legitimate := newKey(t, RS256, StatusCurrent)
	cache := testCache(t, legitimate)

	// The attacker's own key, claiming the service's kid.
	attacker, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generating the attacker's key: %v", err)
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: attacker.Private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", legitimate.KID),
	)
	if err != nil {
		t.Fatalf("building the attacker's signer: %v", err)
	}

	signed, err := signer.Sign([]byte(`{"sub":"attacker","role":"admin"}`))
	if err != nil {
		t.Fatalf("attacker signing: %v", err)
	}
	forged, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("serializing: %v", err)
	}

	if _, err := NewVerifier(cache).Verify(forged, TypeJWT); err == nil {
		t.Fatal("SECURITY: a token signed by an unknown key but carrying a valid kid was accepted")
	}
}

// A-4. A valid token with its signature removed or damaged.
func TestTamperedSignatureIsRejected(t *testing.T) {
	cache := testCache(t, newKey(t, RS256, StatusCurrent))

	token, err := NewSigner(cache).Sign([]byte(`{"sub":"usr_1"}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parts := strings.Split(token, ".")

	tests := map[string]string{
		"signature stripped":  parts[0] + "." + parts[1] + ".",
		"signature truncated": parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])/2],
		// A byte in the MIDDLE, not the last character. See
		// TestSignatureEncodingIsMalleable below for why the last character is
		// the wrong place to tamper — it carries padding bits that decode away.
		"signature altered": parts[0] + "." + parts[1] + "." + flipMiddle(parts[2]),
		"payload tampered": parts[0] + "." +
			base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"admin"}`)) + "." + parts[2],
	}

	for name, forged := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewVerifier(cache).Verify(forged, TypeJWT); err == nil {
				t.Fatalf("SECURITY: %s was accepted", name)
			}
		})
	}
}

// A token with no kid is rejected rather than tried against every key.
//
// Trying them all is how a retired key ends up accepted by an implementation
// that iterates without re-checking status, and it turns a verification
// failure into an oracle for how many keys the service holds.
func TestTokenWithoutKIDIsRejected(t *testing.T) {
	key := newKey(t, RS256, StatusCurrent)
	cache := testCache(t, key)

	// Signed by the real key, but with no kid in the header.
	pair, _ := Generate(RS256)
	_ = pair
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key.private},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	signed, _ := signer.Sign([]byte(`{}`))
	compact, _ := signed.CompactSerialize()

	_, err = NewVerifier(cache).Verify(compact, TypeJWT)
	if err == nil {
		t.Fatal("a token without a kid must be rejected")
	}
}

// An unknown kid fails distinctly from a retired one. Both reject; the
// difference is what the operator reads in the log.
func TestUnknownKIDIsDistinctFromRetired(t *testing.T) {
	cache := testCache(t, newKey(t, RS256, StatusCurrent))

	set, err := cache.Get()
	if err != nil {
		t.Fatalf("cache: %v", err)
	}

	if _, err := set.ByKID("no-such-key"); err == nil {
		t.Fatal("an unknown kid must be an error")
	} else if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("want an 'unknown' error, got: %v", err)
	}
}

// --- JWKS ------------------------------------------------------------------

func TestJWKSPublishesEveryVerifyingKeyAndNoPrivateMaterial(t *testing.T) {
	current := newKey(t, RS256, StatusCurrent)
	next := newKey(t, ES256, StatusNext)
	previous := newKey(t, RS256, StatusPrevious)
	retired := newKey(t, RS256, StatusRetired)

	set, err := NewKeySet([]*Key{current, next, previous, retired})
	if err != nil {
		t.Fatalf("key set: %v", err)
	}

	jwks := set.JWKS()

	if len(jwks.Keys) != 3 {
		t.Fatalf("JWKS has %d keys, want 3 (current, next, previous — not retired)", len(jwks.Keys))
	}

	published := map[string]bool{}
	for _, k := range jwks.Keys {
		published[k.KeyID] = true

		if !k.IsPublic() {
			t.Errorf("SECURITY: JWKS entry %q contains private key material", k.KeyID)
		}
		if k.Use != "sig" {
			t.Errorf("key %q has use=%q, want sig", k.KeyID, k.Use)
		}
	}

	if published[retired.KID] {
		t.Error("a retired key must not appear in JWKS")
	}
	for _, want := range []*Key{current, next, previous} {
		if !published[want.KID] {
			t.Errorf("key %q (%s) is missing from JWKS", want.KID, want.Status)
		}
	}

	// Serialising must not leak private material either — the JWKS response is
	// the single most public thing this service produces.
	encoded, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshalling JWKS: %v", err)
	}
	for _, forbidden := range []string{"\"d\"", "\"p\"", "\"q\"", "PRIVATE"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("SECURITY: JWKS output contains %s", forbidden)
		}
	}
}

// --- kid derivation --------------------------------------------------------

// The kid must be stable across processes, or every restart invalidates every
// consumer's cached key set. Deriving it from the key material (RFC 7638)
// makes that structural rather than something a random generator might break.
func TestKIDIsDerivedFromTheKeyAndIsStable(t *testing.T) {
	pair, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	again, err := Thumbprint(pair.Public)
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}
	if again != pair.KID {
		t.Errorf("thumbprint is not stable: %q then %q", pair.KID, again)
	}

	other, err := Generate(RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if other.KID == pair.KID {
		t.Error("two different keys produced the same kid")
	}
}

// flipMiddle changes a character in the middle of a base64url string, where
// every bit is meaningful.
func flipMiddle(s string) string {
	if len(s) < 4 {
		return s
	}
	i := len(s) / 2
	replacement := byte('A')
	if s[i] == 'A' {
		replacement = 'B'
	}
	return s[:i] + string(replacement) + s[i+1:]
}

// The signature encoding is malleable, and P1-07 needs to know.
//
// An RSA-2048 signature is 256 bytes, which base64url-encodes to 342
// characters. 342 x 6 = 2052 bits for 2048 bits of data, so the final
// character carries four bits that decode away — and Go's base64 accepts them
// set to anything. Four different token STRINGS therefore decode to the same
// signature and all verify.
//
// This is not a forgery risk. The signature still has to be valid, so an
// attacker cannot change what a token says.
//
// It is a token IDENTITY risk, and that is the part with teeth. Anything that
// treats the token string as the token's identity sees four distinct tokens
// where there is one:
//
//   - Refresh token reuse detection (P1-07, P3-02). If reuse is detected by
//     storing a hash of the presented string, an attacker who steals a refresh
//     token can present it with a mutated final character, the hash will not
//     match, and the reuse goes undetected — defeating the whole mechanism.
//   - A revocation denylist keyed by the token string.
//   - An idempotency or replay cache keyed the same way.
//
// The fix in each case is to key on something canonical: the `jti` claim, or
// the DECODED signature bytes. Never the raw string.
//
// This test pins the property so it is discovered here rather than during
// P1-07's implementation, and fails loudly if a future library upgrade makes
// the encoding strict — which would be good news, and would still want
// noticing.
func TestSignatureEncodingIsMalleable(t *testing.T) {
	cache := testCache(t, newKey(t, RS256, StatusCurrent))

	token, err := NewSigner(cache).Sign([]byte(`{"sub":"usr_1"}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	parts := strings.Split(token, ".")
	signature := parts[2]

	canonical, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}

	verifier := NewVerifier(cache)
	variants := 0

	// The whole alphabet, not a guessed subset. Which replacements preserve the
	// decoded bytes depends on the signature's final bits, so a hand-picked
	// list finds nothing for most keys — and a test that silently finds
	// nothing is a test that reports success without measuring anything.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	for i := 0; i < len(alphabet); i++ {
		replacement := alphabet[i]
		mutated := signature[:len(signature)-1] + string(replacement)
		if mutated == signature {
			continue
		}

		decoded, err := base64.RawURLEncoding.DecodeString(mutated)
		if err != nil || !bytes.Equal(decoded, canonical) {
			continue // this replacement changed real bits; not what we are measuring
		}

		variants++

		if _, err := verifier.Verify(parts[0]+"."+parts[1]+"."+mutated, TypeJWT); err != nil {
			t.Fatalf("a variant decoding to the same signature should verify, got: %v", err)
		}
	}

	if variants == 0 {
		t.Skip("this key's signature has no spare padding bits; nothing to measure")
	}

	t.Logf("%d distinct token strings decode to the same signature and all verify. "+
		"P1-07 and P3-02 must key reuse detection on jti or the decoded "+
		"signature, never on the token string.", variants+1)
}

// --- the type header -------------------------------------------------------

// Abuse case A-5, at the layer that can actually stop it. An ID token and an
// access token are both signed by this service with the same key; `typ` is the
// only thing in the envelope that distinguishes them, which is why P1-07 sets
// it and why Verify requires the caller to say what it expects.
func TestATokenOfTheWrongTypeIsRefused(t *testing.T) {
	cache := testCache(t, newKey(t, RS256, StatusCurrent))
	payload := []byte(`{"sub":"usr_1"}`)

	idToken, err := NewSigner(cache).SignWithType(payload, TypeJWT)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Presented where an access token belongs.
	if _, err := NewVerifier(cache).Verify(idToken, TypeAccessToken); !errors.Is(err, ErrWrongType) {
		t.Errorf("an id_token verified as an access token: %v", err)
	}

	// And the reverse, which is the direction a client has to defend.
	accessToken, err := NewSigner(cache).SignWithType(payload, TypeAccessToken)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := NewVerifier(cache).Verify(accessToken, TypeJWT); !errors.Is(err, ErrWrongType) {
		t.Errorf("an access token verified as an id_token: %v", err)
	}

	// The control: each verifies as itself, so the two assertions above are
	// not passing because verification is broken outright.
	if _, err := NewVerifier(cache).Verify(idToken, TypeJWT); err != nil {
		t.Errorf("an id_token did not verify as itself: %v", err)
	}
	if _, err := NewVerifier(cache).Verify(accessToken, TypeAccessToken); err != nil {
		t.Errorf("an access token did not verify as itself: %v", err)
	}
}

// A token with no `typ` at all is not an access token. go-jose yields nil for
// the missing header, and the comparison has to treat that as a mismatch
// rather than panicking or — worse — reading it as an empty string that some
// caller passed as wantType.
func TestAnUntypedTokenIsRefused(t *testing.T) {
	cache := testCache(t, newKey(t, RS256, StatusCurrent))

	untyped, err := NewSigner(cache).SignWithType([]byte(`{"sub":"usr_1"}`), "")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := NewVerifier(cache).Verify(untyped, TypeAccessToken); !errors.Is(err, ErrWrongType) {
		t.Errorf("an untyped token verified as an access token: %v", err)
	}
}

// Verify refuses to be called without a wanted type, so a caller cannot get
// the permissive behaviour back by passing "".
func TestVerifyRequiresAWantedType(t *testing.T) {
	cache := testCache(t, newKey(t, RS256, StatusCurrent))

	token, err := NewSigner(cache).Sign([]byte(`{"sub":"usr_1"}`))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := NewVerifier(cache).Verify(token, ""); err == nil {
		t.Error("Verify accepted an empty wantType, which is the permissive " +
			"behaviour the parameter exists to remove")
	}
}

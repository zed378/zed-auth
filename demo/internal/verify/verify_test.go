package verify

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The verifier is the only security control these demo applications have, so
// every check it makes is tested by removing exactly one thing from an
// otherwise-valid token. A test that mints a bad token from scratch proves
// only that garbage is rejected; the interesting question is whether a token
// that is right in every respect but one gets through.

const (
	issuer   = "https://auth.example.test"
	appA     = "demo-webapp"
	appB     = "demo-spa"
	testKid  = "key-2026-09"
	otherKid = "key-2026-12"
)

// issuerFixture stands in for the auth service: a key set on an HTTP endpoint,
// and the private keys that match it.
type issuerFixture struct {
	server   *httptest.Server
	keys     map[string]*rsa.PrivateKey
	requests *atomic.Int64
	fail     *atomic.Bool
	empty    *atomic.Bool
}

func newIssuer(t *testing.T, kids ...string) *issuerFixture {
	t.Helper()

	fixture := &issuerFixture{
		keys:     map[string]*rsa.PrivateKey{},
		requests: &atomic.Int64{},
		fail:     &atomic.Bool{},
		empty:    &atomic.Bool{},
	}
	for _, kid := range kids {
		// 2048 is the smallest size worth testing against, and generating it
		// is the slowest thing in this file — so each test makes one issuer.
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generating a key: %v", err)
		}
		fixture.keys[kid] = key
	}

	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			// Any other path means the verifier called the auth service for
			// something — which is the thing this whole package exists in
			// order not to do.
			t.Errorf("the verifier requested %q; it must only ever fetch the key set", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fixture.requests.Add(1)
		if fixture.fail.Load() {
			http.Error(w, "the key set is unavailable", http.StatusInternalServerError)
			return
		}

		type jwk struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		document := struct {
			Keys []jwk `json:"keys"`
		}{Keys: []jwk{}}

		if !fixture.empty.Load() {
			for kid, key := range fixture.keys {
				document.Keys = append(document.Keys, jwk{
					Kid: kid,
					Kty: "RSA",
					N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(document)
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

// verifierFor builds a verifier pointed at the fixture, expecting `audience`.
//
// The issuer STRING stays the production-looking one while the JWKS URI points
// at the test server. Otherwise every test would assert against httptest's
// random port, and the issuer check would be testing itself.
func (f *issuerFixture) verifierFor(audience string, now func() time.Time) *Verifier {
	v := New(issuer, audience)
	v.jwksURI = f.server.URL + "/.well-known/jwks.json"
	v.Now = now
	return v
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// mint signs a token with the fixture's key for `h.Kid`.
func (f *issuerFixture) mint(t *testing.T, h tokenHeader, claims map[string]any) string {
	t.Helper()
	key, ok := f.keys[h.Kid]
	if !ok {
		t.Fatalf("no key %q in this fixture", h.Kid)
	}
	return sign(t, key, segment(t, h)+"."+segment(t, claims))
}

// mintUnpublished signs with a real key whose kid the issuer does not publish.
// Used where the kid is refused before the signature is ever considered.
func (f *issuerFixture) mintUnpublished(t *testing.T, kid string, claims map[string]any) string {
	t.Helper()
	h := tokenHeader{Alg: "RS256", Kid: kid, Typ: "JWT"}
	return sign(t, f.keys[testKid], segment(t, h)+"."+segment(t, claims))
}

// mintRaw signs a payload whose exact bytes the caller chose.
//
// Needed because Go marshals a map with its keys sorted: a token minted from a
// map is already in canonical form, so re-encoding it produces identical bytes
// and the test below could not tell a correct verifier from a broken one.
func (f *issuerFixture) mintRaw(t *testing.T, payload string) string {
	t.Helper()
	h := segment(t, validHeader())
	return sign(t, f.keys[testKid], h+"."+base64.RawURLEncoding.EncodeToString([]byte(payload)))
}

func sign(t *testing.T, key *rsa.PrivateKey, signing string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func segment(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding a segment: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func validHeader() tokenHeader { return tokenHeader{Alg: "RS256", Kid: testKid, Typ: "JWT"} }

func validClaims(audience string, now time.Time) map[string]any {
	return map[string]any{
		"iss":       issuer,
		"sub":       "user-1",
		"aud":       audience,
		"iat":       now.Unix(),
		"exp":       now.Add(5 * time.Minute).Unix(),
		"auth_time": now.Unix(),
		"amr":       []string{"pwd"},
		"org_id":    "org-1",
		"nonce":     "n-1",
	}
}

func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

var noon = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

// --- The happy path ---------------------------------------------------------

func TestAValidTokenVerifiesAndCarriesItsClaims(t *testing.T) {
	fixture := newIssuer(t, testKid)
	v := fixture.verifierFor(appA, fixedClock(noon))

	claims, err := v.Verify(context.Background(), fixture.mint(t, validHeader(), validClaims(appA, noon)))
	if err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}

	if claims.Subject != "user-1" || claims.OrgID != "org-1" || claims.Nonce != "n-1" {
		t.Errorf("claims came back wrong: %+v", claims)
	}
	if len(claims.AuthMethods) != 1 || claims.AuthMethods[0] != "pwd" {
		t.Errorf("amr came back as %v", claims.AuthMethods)
	}
}

// --- The definition of done: a token for the other application --------------

func TestATokenMintedForTheOtherApplicationIsRefused(t *testing.T) {
	fixture := newIssuer(t, testKid)

	// One issuer, one signing key, one user — and a token addressed to the
	// SPA. Everything about it is genuine except who it is for.
	token := fixture.mint(t, validHeader(), validClaims(appB, noon))

	err := expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)), token)
	if !strings.Contains(err.Error(), "audience") {
		t.Errorf("the refusal should name the audience; it said %q", err)
	}

	// And the same token IS accepted by the application it was minted for,
	// which is what makes the refusal above a check rather than a coincidence.
	if _, err := fixture.verifierFor(appB, fixedClock(noon)).Verify(context.Background(), token); err != nil {
		t.Fatalf("the token was refused by its own audience too: %v", err)
	}
}

func TestAnAudienceArrayIsAcceptedOnlyWhenItContainsUs(t *testing.T) {
	fixture := newIssuer(t, testKid)

	// RFC 7519 permits `aud` to be an array. A verifier that handles only the
	// string form either refuses conforming tokens or — worse — never compares.
	contains := validClaims(appA, noon)
	contains["aud"] = []string{"something-else", appA}
	if _, err := fixture.verifierFor(appA, fixedClock(noon)).
		Verify(context.Background(), fixture.mint(t, validHeader(), contains)); err != nil {
		t.Fatalf("an array containing us was refused: %v", err)
	}

	without := validClaims(appA, noon)
	without["aud"] = []string{"something-else", appB}
	expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)), fixture.mint(t, validHeader(), without))
}

// --- Issuer, expiry, type ---------------------------------------------------

func TestATrailingSlashIsADifferentIssuer(t *testing.T) {
	fixture := newIssuer(t, testKid)

	claims := validClaims(appA, noon)
	claims["iss"] = issuer + "/"

	err := expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)), fixture.mint(t, validHeader(), claims))
	// P1-04's finding: this misconfiguration is invisible unless the message
	// names both sides. "invalid issuer" sends people to the wrong file.
	if !strings.Contains(err.Error(), issuer+"/") || !strings.Contains(err.Error(), "not") {
		t.Errorf("the refusal should name both issuers; it said %q", err)
	}
}

func TestExpiryIsCheckedAtTheBoundary(t *testing.T) {
	fixture := newIssuer(t, testKid)
	claims := validClaims(appA, noon)
	expiry := time.Unix(claims["exp"].(int64), 0)
	token := fixture.mint(t, validHeader(), claims)

	// One second before expiry: still good.
	if _, err := fixture.verifierFor(appA, fixedClock(expiry.Add(-time.Second))).
		Verify(context.Background(), token); err != nil {
		t.Fatalf("a token one second from expiry was refused: %v", err)
	}

	// Exactly at expiry: `exp` is the moment it stops being valid, not the
	// last moment it is. An off-by-one here is a token that works a second
	// longer than the issuer promised, which is the kind of thing nobody
	// notices until it is a finding.
	expectRefused(t, fixture.verifierFor(appA, fixedClock(expiry)), token)
}

func TestATokenWithNoExpiryIsRefused(t *testing.T) {
	fixture := newIssuer(t, testKid)
	claims := validClaims(appA, noon)
	delete(claims, "exp")

	// Absent is not "never expires"; it is a token this verifier cannot reason
	// about, and accepting it means accepting one forever.
	err := expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)), fixture.mint(t, validHeader(), claims))

	// The refusal has to SAY no expiry. A missing `exp` decodes to zero, so
	// the expiry comparison below it refuses this token anyway — and reports
	// it as "expired", which sends whoever is debugging to look for a clock
	// problem that does not exist. The explicit case earns its place by being
	// the difference between a five-minute fix and an afternoon.
	if !strings.Contains(err.Error(), "no expiry") {
		t.Errorf("the refusal said %q; it must say the expiry is missing", err)
	}
}

func TestAnAccessTokenIsNotAcceptedWhereAnIDTokenBelongs(t *testing.T) {
	fixture := newIssuer(t, testKid)

	// Abuse case A-5. The service marks access tokens `at+jwt` precisely so
	// this swap can be refused — and `aud` alone would not catch it, because
	// an access token's audience is the issuer, which some consumer somewhere
	// will inevitably also be called.
	h := validHeader()
	h.Typ = "at+jwt"

	err := expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)), fixture.mint(t, h, validClaims(appA, noon)))
	if !strings.Contains(err.Error(), "at+jwt") {
		t.Errorf("the refusal should name the type it got; it said %q", err)
	}
}

// --- Signature and algorithm ------------------------------------------------

func TestTheHeaderCannotChooseTheAlgorithm(t *testing.T) {
	// Both of these are ALSO refused by the signature check further down —
	// `none` carries no signature and an HMAC is not an RSA one — so
	// "was it refused?" cannot tell whether the algorithm check exists.
	//
	// What the check uniquely does is refuse them FIRST: before a key is
	// looked up, before the key set is fetched, and with a message that names
	// the algorithm. That is the assertion, because it is the part that is
	// only true when the check is there.
	for _, given := range []struct {
		name  string
		token func(*testing.T, *issuerFixture) string
	}{
		{
			// The classic: an unsigned token that claims not to need one.
			name: "none",
			token: func(t *testing.T, f *issuerFixture) string {
				h := segment(t, tokenHeader{Alg: "none", Kid: testKid, Typ: "JWT"})
				return h + "." + segment(t, validClaims(appA, noon)) + "."
			},
		},
		{
			// Algorithm confusion: the attacker knows the public key because
			// it is published, and a verifier that dispatched on `alg` would
			// hand that key to an HMAC function as the shared secret.
			name: "HS256 signed with the public key",
			token: func(t *testing.T, f *issuerFixture) string {
				h := segment(t, tokenHeader{Alg: "HS256", Kid: testKid, Typ: "JWT"})
				signing := h + "." + segment(t, validClaims(appA, noon))
				mac := hmac.New(sha256.New, f.keys[testKid].PublicKey.N.Bytes())
				mac.Write([]byte(signing))
				return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			},
		},
	} {
		t.Run(given.name, func(t *testing.T) {
			fixture := newIssuer(t, testKid)
			v := fixture.verifierFor(appA, fixedClock(noon))

			err := expectRefused(t, v, given.token(t, fixture))
			if !strings.Contains(err.Error(), "algorithm") {
				t.Errorf("refused for some other reason: %v", err)
			}
			// Nothing was fetched. An attacker cannot turn a stream of
			// `alg: none` tokens into load on the auth service's key set.
			if got := fixture.requests.Load(); got != 0 {
				t.Errorf("refusing it took %d key-set fetches; it must take none", got)
			}
		})
	}
}

func TestAnAlteredPayloadBreaksTheSignature(t *testing.T) {
	fixture := newIssuer(t, testKid)
	parts := strings.Split(fixture.mint(t, validHeader(), validClaims(appA, noon)), ".")

	// Same shape, different subject — the edit an attacker actually wants.
	elevated := validClaims(appA, noon)
	elevated["sub"] = "user-2"

	expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)),
		parts[0]+"."+segment(t, elevated)+"."+parts[2])
}

func TestTheSignatureCoversTheBytesAsTheyArrived(t *testing.T) {
	fixture := newIssuer(t, testKid)

	// A payload written the way an issuer actually emits one: keys in the
	// order the code assembled them, not in alphabetical order.
	payload := `{"sub":"user-1","iss":"` + issuer + `","aud":"` + appA +
		`","exp":` + strconv.FormatInt(noon.Add(time.Hour).Unix(), 10) + `,"org_id":"org-1"}`
	parts := strings.Split(fixture.mintRaw(t, payload), ".")

	// It verifies as it arrived.
	if _, err := fixture.verifierFor(appA, fixedClock(noon)).
		Verify(context.Background(), strings.Join(parts, ".")); err != nil {
		t.Fatalf("the token was refused before anything was changed: %v", err)
	}

	// Now re-encode it: decode the claims and marshal them again, which is
	// what a verifier that rebuilt its own signing input would end up
	// comparing. The meaning is identical; the bytes are not, because Go sorts
	// a map's keys. If this VERIFIED, the implementation would be checking a
	// signature over a string the issuer never signed — and it would keep
	// appearing to work right up until an issuer changed its key order.
	var claims map[string]any
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("parsing the payload: %v", err)
	}
	reencoded := segment(t, claims)
	if reencoded == parts[1] {
		t.Fatal("the re-encoded payload is byte-identical, so this test would pass vacuously")
	}

	expectRefused(t, fixture.verifierFor(appA, fixedClock(noon)), parts[0]+"."+reencoded+"."+parts[2])
}

func TestAMalformedTokenIsRefusedRatherThanPanicking(t *testing.T) {
	fixture := newIssuer(t, testKid)
	v := fixture.verifierFor(appA, fixedClock(noon))

	for _, token := range []string{
		"",
		".",
		"..",
		"a.b",
		"a.b.c.d",
		"not-base64!.not-base64!.not-base64!",
		strings.Repeat("a", 4096),
		strings.Repeat(".", 3),
	} {
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Errorf("%q was accepted", token)
		}
	}
}

// --- The key set: the only network call ------------------------------------

func TestVerifyingMakesNoNetworkCallOnceTheKeySetIsCached(t *testing.T) {
	fixture := newIssuer(t, testKid)
	v := fixture.verifierFor(appA, fixedClock(noon))
	token := fixture.mint(t, validHeader(), validClaims(appA, noon))

	// `docs/PLAN/12`'s claim, stated as a measurement rather than an
	// assumption: the first verification fetches the key set, and the next
	// hundred touch nothing at all.
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("the first verification failed: %v", err)
	}
	if got := fixture.requests.Load(); got != 1 {
		t.Fatalf("the first verification made %d requests, want 1", got)
	}

	for range 100 {
		if _, err := v.Verify(context.Background(), token); err != nil {
			t.Fatalf("a cached verification failed: %v", err)
		}
	}
	if got := fixture.requests.Load(); got != 1 {
		t.Errorf("100 further verifications made %d more requests; they must make none", got-1)
	}
}

func TestARotatedKeyIsPickedUpWithoutConfiguration(t *testing.T) {
	fixture := newIssuer(t, testKid, otherKid)
	v := fixture.verifierFor(appA, fixedClock(noon))

	if _, err := v.Verify(context.Background(), fixture.mint(t, validHeader(), validClaims(appA, noon))); err != nil {
		t.Fatalf("the current key failed: %v", err)
	}

	// A token signed with the key that was `next` yesterday. Nothing about the
	// consumer changes; the unknown kid triggers exactly one refetch, which is
	// what makes rotation invisible to a consumer team.
	rotated := validHeader()
	rotated.Kid = otherKid
	if _, err := v.Verify(context.Background(), fixture.mint(t, rotated, validClaims(appA, noon))); err != nil {
		t.Fatalf("a rotated key was refused: %v", err)
	}
}

func TestAForgedKeyIsRefusedAndDoesNotBecomeAWayToHammerTheIssuer(t *testing.T) {
	fixture := newIssuer(t, testKid)
	v := fixture.verifierFor(appA, fixedClock(noon))

	// Signed by a key this issuer never published: an attacker's own RSA key,
	// with a kid chosen to look plausible.
	attacker := newIssuer(t, "key-2027-01")
	forged := attacker.mint(t, tokenHeader{Alg: "RS256", Kid: "key-2027-01", Typ: "JWT"}, validClaims(appA, noon))

	expectRefused(t, v, forged)

	before := fixture.requests.Load()
	for range 10 {
		expectRefused(t, v, forged)
	}
	// One refetch per miss is the design; ten tokens must not become a
	// multiplier on the auth service. The cap here is deliberately loose — the
	// assertion is "bounded by the number of tokens", not a precise count.
	if got := fixture.requests.Load() - before; got > 10 {
		t.Errorf("10 forged tokens caused %d key-set fetches", got)
	}
}

func TestAKeySetOutageDoesNotRejectTokensSignedByAKeyWeAlreadyHold(t *testing.T) {
	clock := noon
	fixture := newIssuer(t, testKid)
	v := fixture.verifierFor(appA, func() time.Time { return clock })
	// Deliberately outliving the key TTL: this test is about the KEY going
	// stale, and a token that expired on the way would pass it for the wrong
	// reason.
	longLived := validClaims(appA, noon)
	longLived["exp"] = noon.Add(time.Hour).Unix()
	token := fixture.mint(t, validHeader(), longLived)

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("the first verification failed: %v", err)
	}

	// The TTL runs out and the key set becomes unreachable. The published key
	// has not changed — refusing here would take the consumer down because the
	// auth service had a bad minute, which is precisely the coupling local
	// validation exists to remove.
	clock = noon.Add(keyTTL + time.Second)
	fixture.fail.Store(true)

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("a token signed by a key we already hold was refused during an outage: %v", err)
	}

	// But an unknown kid during the same outage has nothing to fall back on,
	// and gets an error that is NOT ErrInvalid — the caller must be able to
	// tell "your credential is bad" from "we cannot check right now".
	unknown := fixture.mintUnpublished(t, "key-we-never-saw", longLived)
	_, err := v.Verify(context.Background(), unknown)
	if err == nil {
		t.Fatal("an unknown kid was accepted during a key-set outage")
	}
	if errors.Is(err, ErrInvalid) {
		t.Errorf("an unreachable key set was reported as a bad token: %v", err)
	}
}

func TestAnEmptyKeySetDoesNotWipeAWorkingOne(t *testing.T) {
	clock := noon
	fixture := newIssuer(t, testKid)
	v := fixture.verifierFor(appA, func() time.Time { return clock })
	longLived := validClaims(appA, noon)
	longLived["exp"] = noon.Add(time.Hour).Unix()
	token := fixture.mint(t, validHeader(), longLived)

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("the first verification failed: %v", err)
	}

	// A 200 carrying `{"keys":[]}` — a deploy caught mid-rotation, a proxy
	// serving a cached empty body. Replacing a working key set with nothing
	// turns a transient fault into a total outage.
	clock = noon.Add(keyTTL + time.Second)
	fixture.empty.Store(true)

	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatalf("an empty key set wiped the working one: %v", err)
	}
}

// expectRefused asserts the token is refused, and refused AS ErrInvalid rather
// than as some incidental failure.
func expectRefused(t *testing.T, v *Verifier, token string) error {
	t.Helper()
	claims, err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatalf("the token was ACCEPTED; claims: %+v", claims)
	}
	if !errors.Is(err, ErrInvalid) {
		// Not a wrapping nicety: callers distinguish "this token is not
		// acceptable" from "the key set could not be reached", and the second
		// must never reach a user as a bad credential.
		t.Fatalf("refused with %v, which does not wrap ErrInvalid", err)
	}
	if claims.Subject != "" {
		t.Errorf("claims leaked out of a refusal: %+v", claims)
	}
	return err
}

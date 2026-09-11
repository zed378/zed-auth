// Package verify validates an access token the way a consumer application
// should: locally, against the JWKS, with no call back to the auth service
// (P1-26).
//
// **That is the point of the whole demo.** `docs/PLAN/12` names local
// validation as the biggest available latency win, and `docs/PLAN/03`'s data
// flow assumes it — but an assumption in a plan document is not a
// demonstration. These applications never call the auth service on a request
// they can answer from a cached key set.
//
// It is deliberately small and deliberately strict. A consumer team copying
// this file should get something correct; a consumer team copying something
// permissive gets a service that accepts tokens meant for somebody else.
package verify

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Claims is what a validated ID token asserts.
//
// Only the claims a consumer application has a use for. `amr` is here because
// a consumer that cares how somebody authenticated — and a step-up flow is
// exactly that — must read it from the token rather than assume it.
type Claims struct {
	Subject     string   `json:"sub"`
	Issuer      string   `json:"iss"`
	Audience    audience `json:"aud"`
	ExpiresAt   int64    `json:"exp"`
	IssuedAt    int64    `json:"iat"`
	AuthTime    int64    `json:"auth_time"`
	AuthMethods []string `json:"amr"`
	OrgID       string   `json:"org_id"`
	Nonce       string   `json:"nonce"`
	SessionID   string   `json:"sid"`
}

// audience handles `aud` being either a string or an array.
//
// RFC 7519 permits both, and a verifier that handles only one rejects tokens
// from a conforming issuer — or, worse, accepts a token whose audience it
// never actually compared.
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("aud is neither a string nor an array: %w", err)
	}
	*a = many
	return nil
}

func (a audience) contains(want string) bool {
	for _, entry := range a {
		if entry == want {
			return true
		}
	}
	return false
}

// Verifier checks tokens against a cached key set.
type Verifier struct {
	issuer    string
	audience  string
	tokenType string
	jwksURI   string

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time

	// Now is overridable for tests. Expiry is a decision about time, and a
	// function that reads the clock cannot be tested at a boundary.
	Now func() time.Time
}

// New builds a verifier for one issuer and one audience.
//
// The audience a consumer application expects is **its own `client_id`**,
// because the token it verifies is the ID token — the assertion addressed to
// it. The access token issued alongside carries `aud: <the issuer>`: it is a
// capability at the auth service's own API, identical for every application,
// and therefore says nothing about which application it was minted for. A
// consumer that authenticated its users from the access token would accept
// any application's token as its own.
func New(issuer, expectedAudience string) *Verifier {
	return &Verifier{
		issuer:    strings.TrimRight(issuer, "/"),
		audience:  expectedAudience,
		tokenType: "JWT",
		jwksURI:   strings.TrimRight(issuer, "/") + "/.well-known/jwks.json",
		keys:      map[string]*rsa.PublicKey{},
		Now:       time.Now,
	}
}

// keyTTL is how long a fetched key set is trusted without refetching.
//
// Five minutes. `P1-03`'s rotation keeps a retiring key published through an
// overlap window measured in days, so this is far shorter than it needs to be
// — which is the right direction to err: a stale key set rejects valid tokens,
// and rejecting is louder than accepting.
const keyTTL = 5 * time.Minute

// ErrInvalid means the token is not acceptable. One error for every reason.
var ErrInvalid = errors.New("verify: the token is not valid")

// Verify checks a token and returns its claims.
//
// Every check is mandatory and none is skippable by configuration. The order
// matters: the signature is checked before anything in the payload is
// believed, because an unverified payload is a string an attacker wrote.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: not three segments", ErrInvalid)
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return Claims{}, fmt.Errorf("%w: unreadable header", ErrInvalid)
	}

	// **`alg` is checked against what we accept, not used to choose.** A
	// verifier that trusts the header's algorithm accepts `none`, and one that
	// accepts HS256 from an RSA issuer lets an attacker sign with the public
	// key. This is the single most repeated JWT vulnerability.
	if header.Alg != "RS256" {
		return Claims{}, fmt.Errorf("%w: algorithm %q is not accepted", ErrInvalid, header.Alg)
	}

	// **`typ` is the half of abuse case A-5 that still works when `aud` is
	// lax.** This service marks access tokens `at+jwt` (RFC 9068) and ID
	// tokens `JWT` precisely so each can be refused where the other belongs.
	// An exact match is required rather than "absent is fine": an issuer that
	// omits `typ` should break a consumer loudly here, not silently widen what
	// the consumer accepts.
	if header.Typ != v.tokenType {
		return Claims{}, fmt.Errorf("%w: token type %q is not accepted here, %q is",
			ErrInvalid, header.Typ, v.tokenType)
	}

	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return Claims{}, err
	}

	if err := verifySignature(key, parts); err != nil {
		return Claims{}, err
	}

	var claims Claims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return Claims{}, fmt.Errorf("%w: unreadable payload", ErrInvalid)
	}

	now := v.Now()

	switch {
	case claims.Issuer != v.issuer:
		// Exact string comparison. A trailing slash is a different issuer, and
		// that mismatch is a classic misconfiguration whose error message
		// names neither side (P1-04).
		return Claims{}, fmt.Errorf("%w: issued by %q, not %q", ErrInvalid, claims.Issuer, v.issuer)

	case !claims.Audience.contains(v.audience):
		// **The check that stops a token for another application being
		// accepted here.** Without it, any token this issuer signs works
		// everywhere it is presented, and a compromised low-value client
		// becomes a key to every high-value one.
		return Claims{}, fmt.Errorf("%w: audience %v does not include %q",
			ErrInvalid, []string(claims.Audience), v.audience)

	case claims.ExpiresAt == 0:
		// No expiry is not "never expires"; it is a token this verifier cannot
		// reason about.
		return Claims{}, fmt.Errorf("%w: no expiry", ErrInvalid)

	case now.Unix() >= claims.ExpiresAt:
		return Claims{}, fmt.Errorf("%w: expired", ErrInvalid)
	}

	return claims, nil
}

// key returns the public key for a kid, fetching the key set if needed.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	cached, ok := v.keys[kid]
	fresh := v.Now().Sub(v.fetched) < keyTTL
	v.mu.RUnlock()

	if ok && fresh {
		return cached, nil
	}

	// A miss triggers exactly one refetch. A rotated key appears here the
	// first time a token signed with it arrives, which is what makes rotation
	// invisible to a consumer — and the refetch is the ONLY network call this
	// package ever makes.
	if err := v.refresh(ctx); err != nil {
		if ok {
			// We already hold this key and only the TTL had run out. Failing
			// here would mean a JWKS blip rejects every valid token — the
			// consumer goes down because the key set was briefly unreachable,
			// while the key itself never changed. The published key is the
			// same bytes it was a minute ago; the freshness check exists to
			// pick up ROTATION, and a rotation that has not been fetched yet
			// is not a reason to refuse the key that is still current.
			return cached, nil
		}
		// No key for this kid and no way to fetch one. Here there is nothing
		// to fall back to, and refusing is the only honest answer.
		return nil, err
	}

	v.mu.RLock()
	defer v.mu.RUnlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: no key %q in the key set", ErrInvalid, kid)
}

func (v *Verifier) refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURI, nil)
	if err != nil {
		return fmt.Errorf("verify: building the JWKS request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("verify: fetching the key set: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("verify: the key set answered %d", response.StatusCode)
	}

	var document struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return fmt.Errorf("verify: decoding the key set: %w", err)
	}

	keys := map[string]*rsa.PublicKey{}
	for _, entry := range document.Keys {
		if entry.Kty != "RSA" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(entry.N)
		if err != nil {
			continue
		}
		exponent, err := base64.RawURLEncoding.DecodeString(entry.E)
		if err != nil {
			continue
		}
		keys[entry.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(modulus),
			E: int(new(big.Int).SetBytes(exponent).Int64()),
		}
	}

	if len(keys) == 0 {
		// Replacing a working key set with an empty one turns a bad fetch into
		// a total outage. Keeping the old keys means a transient problem stays
		// transient.
		return fmt.Errorf("verify: the key set contains no usable RSA keys")
	}

	v.mu.Lock()
	v.keys = keys
	v.fetched = v.Now()
	v.mu.Unlock()
	return nil
}

func decodeSegment(segment string, into any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

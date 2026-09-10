package signing

import (
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

var (
	// ErrInvalidSignature means the token was not signed by the key it names.
	ErrInvalidSignature = errors.New("signature is not valid")

	// ErrAlgorithmNotAllowed means the token asked to be verified with an
	// algorithm this service does not accept. This is the abuse case, not a
	// configuration error — see Verify.
	ErrAlgorithmNotAllowed = errors.New("token algorithm is not allowed")

	// ErrWrongType means the token is not the kind the caller asked for — an
	// ID token presented where an access token belongs, most usefully.
	ErrWrongType = errors.New("token is not the expected type")

	// ErrMissingKID means the token header carries no key id.
	ErrMissingKID = errors.New("token has no key id")
)

// Signer produces signatures with the current key.
type Signer struct {
	cache *Cache
}

// NewSigner returns a Signer reading keys through the cache.
func NewSigner(cache *Cache) *Signer { return &Signer{cache: cache} }

// Sign signs payload with the current key and returns compact JWS.
//
// Only `current` ever signs. A `next` key is published so consumers can cache
// it before it is used, and a `previous` key exists so old tokens still
// verify — neither is allowed to produce a new signature, and the key set
// enforces that rather than the caller remembering.
// TypeJWT and TypeAccessToken are the `typ` header values this service issues.
//
// The distinction is a security control rather than bookkeeping (P1-07): an
// access token is marked at+jwt per RFC 9068 so a resource server can refuse
// an ID token presented as a bearer credential, and a client can refuse the
// reverse. Two tokens that differ only in their claims are two tokens somebody
// eventually swaps.
const (
	TypeJWT         = "JWT"
	TypeAccessToken = "at+jwt"
)

// Sign produces a compact JWS with `typ: JWT`.
func (s *Signer) Sign(payload []byte) (string, error) {
	return s.SignWithType(payload, TypeJWT)
}

// SignWithType produces a compact JWS with an explicit `typ` header.
func (s *Signer) SignWithType(payload []byte, typ string) (string, error) {
	set, err := s.cache.Get()
	if err != nil {
		return "", err
	}

	key, err := set.Current()
	if err != nil {
		return "", err
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: key.Algorithm.jose(), Key: key.private},
		// The kid goes in the header so a verifier knows which key to try
		// without guessing. Guessing — trying every key until one works — is
		// both slower and an oracle: response timing would reveal how many
		// keys the service holds.
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", key.KID),
	)
	if err != nil {
		return "", fmt.Errorf("creating signer: %w", err)
	}

	signed, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("signing: %w", err)
	}

	compact, err := signed.CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("serializing: %w", err)
	}

	return compact, nil
}

// CurrentKID reports which key is signing, for logging and metrics.
func (s *Signer) CurrentKID() (string, error) {
	set, err := s.cache.Get()
	if err != nil {
		return "", err
	}
	key, err := set.Current()
	if err != nil {
		return "", err
	}
	return key.KID, nil
}

// Verifier validates signatures against the published key set.
type Verifier struct {
	cache *Cache
}

// NewVerifier returns a Verifier reading keys through the cache.
func NewVerifier(cache *Cache) *Verifier { return &Verifier{cache: cache} }

// allowedAlgorithms is the closed set this service will verify with.
//
// Passed to go-jose's parser, which rejects anything else BEFORE looking at a
// key. That ordering is the entire defence against two classic attacks, and it
// is worth being explicit about why:
//
//	alg: none — a token claiming no algorithm at all. A parser that reads the
//	  algorithm from the header and dispatches on it will happily "verify" a
//	  token with no signature. Here the header is checked against this list
//	  first, so `none` never reaches a code path that could accept it.
//
//	Algorithm confusion — a token with `alg: HS256` signed using the RSA
//	  PUBLIC key as the HMAC secret. A verifier that trusts the header looks up
//	  the key by kid, finds an RSA public key, and uses it as a shared secret —
//	  and that key is, by definition, public, so anyone can forge tokens. This
//	  list contains no HMAC algorithm, so such a token is rejected before the
//	  key is fetched.
//
// The general rule: the algorithm is decided by the SERVER's policy, never by
// the attacker-supplied header (docs/SECURITY/02 §1).
var allowedAlgorithms = []jose.SignatureAlgorithm{jose.RS256, jose.ES256}

// Verify checks a compact JWS of the expected type and returns its payload.
//
// The payload is returned unparsed. Claim validation — issuer, audience,
// expiry — belongs to the caller that knows what those should be; this
// function answers two questions: did this service sign this with a key that
// is still valid, and is it the KIND of token the caller asked for?
//
// `wantType` is a required parameter rather than an optional check on a
// separate method, and that is deliberate. The `typ` header is what stops an
// ID token being presented as an access token (abuse case A-5, and half of the
// reason P1-07 marks access tokens `at+jwt` at all). A permissive Verify
// sitting next to a strict VerifyTyped would be a pair where the shorter,
// more obvious name is the unsafe one — and the unsafe one is what gets
// called. Made a parameter, the check cannot be forgotten, only got wrong,
// and got wrong is visible in review.
func (v *Verifier) Verify(compact, wantType string) ([]byte, error) {
	if wantType == "" {
		return nil, fmt.Errorf("signing: a token type is required to verify")
	}

	// Parsing with an explicit algorithm list is what makes the two abuse
	// cases above structural rather than a check somebody has to remember.
	parsed, err := jose.ParseSigned(compact, allowedAlgorithms)
	if err != nil {
		// go-jose reports a disallowed algorithm as a parse error. Mapping it
		// to a distinct error matters for the log: "algorithm not allowed" is
		// an attack signature, while a generic parse failure is usually a
		// truncated token or a copy-paste error.
		if isAlgorithmError(err) {
			return nil, fmt.Errorf("%w: %v", ErrAlgorithmNotAllowed, err)
		}
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	if len(parsed.Signatures) != 1 {
		// Multiple signatures are legal JWS and meaningless for a JWT. A
		// verifier that checked only the first would accept a token whose
		// second signature is the one a consumer validates.
		return nil, fmt.Errorf("%w: expected exactly one signature, got %d",
			ErrInvalidSignature, len(parsed.Signatures))
	}

	header := parsed.Signatures[0].Header

	// The type, before the key lookup — for the same reason the algorithm list
	// is applied before it: a token of the wrong kind is refused without this
	// service doing any work on its behalf.
	//
	// go-jose parses `typ` into ExtraHeaders rather than a named field, and it
	// arrives as a string. A token with no `typ` at all yields nil, which
	// compares unequal to any wanted type — which is the answer we want, since
	// an untyped token is not an access token.
	if got, _ := header.ExtraHeaders[jose.HeaderType].(string); got != wantType {
		return nil, fmt.Errorf("%w: token is typ %q, wanted %q", ErrWrongType, got, wantType)
	}

	if header.KeyID == "" {
		// No fallback to "try every key". Trying them all turns a verification
		// failure into an oracle for how many keys exist, and it is how a
		// retired key ends up accepted by an implementation that iterates
		// without re-checking status.
		return nil, ErrMissingKID
	}

	set, err := v.cache.Get()
	if err != nil {
		return nil, err
	}

	key, err := set.ByKID(header.KeyID)
	if err != nil {
		return nil, err
	}

	// A second, independent check that the algorithm in the header matches
	// the one recorded for this key. go-jose has already restricted the
	// header to the allowed list; this additionally stops a token claiming
	// RS256 from being verified against a key registered as ES256, which
	// would otherwise fail with a confusing signature error rather than a
	// clear one.
	if string(key.Algorithm) != header.Algorithm {
		return nil, fmt.Errorf("%w: token says %s, key %q is %s",
			ErrAlgorithmNotAllowed, header.Algorithm, key.KID, key.Algorithm)
	}

	payload, err := parsed.Verify(key.Public)
	if err != nil {
		// The underlying error is not wrapped: it varies by algorithm and can
		// describe the key. The caller needs to know it failed, not how.
		return nil, ErrInvalidSignature
	}

	return payload, nil
}

// isAlgorithmError reports whether a go-jose parse error was caused by a
// disallowed algorithm rather than by malformed input.
//
// ErrUnexpectedSignatureAlgorithm is a struct rather than a sentinel — it
// carries the algorithm that was seen and the ones that were allowed — so it
// needs errors.As. Reaching for errors.Is compiles against a sentinel and
// silently never matches against a type, which would have quietly reclassified
// every algorithm attack as an ordinary parse failure.
func isAlgorithmError(err error) bool {
	var unexpected *jose.ErrUnexpectedSignatureAlgorithm
	if errors.As(err, &unexpected) {
		return true
	}
	return errors.Is(err, jose.ErrUnsupportedAlgorithm)
}

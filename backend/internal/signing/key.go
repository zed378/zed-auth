// Package signing owns the asymmetric keys this service signs tokens with.
//
// Two properties have to hold at once, and they pull against each other:
// only this service can sign (docs/PLAN/02 § Constraints is absolute — no third
// party holds the private key), and anyone can verify without asking us
// (consumer services validate against a published public key, so an
// authorization check does not depend on this service being reachable).
//
// Rotation is where both get tested. A rotation that invalidates outstanding
// tokens logs every user out at once; one that leaves the old key signing
// forever is not a rotation. The overlap window between the two is the whole
// design (P1-03, docs/PLAN/09 § Tokens & Keys).
//
// Spec: MEMORY/specs/P1-03-signing-keys.md
package signing

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

// Algorithm is a signing algorithm this service will use.
//
// A closed set of two, both asymmetric. HS256 is deliberately absent and not
// merely undocumented: a shared secret would mean every consumer service that
// can verify a token can also mint one, which is the opposite of what
// publishing a public key is for (docs/PLAN/07 § Cryptography).
type Algorithm string

const (
	RS256 Algorithm = "RS256"
	ES256 Algorithm = "ES256"
)

// Valid reports whether alg is one this service will sign or verify with.
func (a Algorithm) Valid() bool { return a == RS256 || a == ES256 }

func (a Algorithm) jose() jose.SignatureAlgorithm {
	switch a {
	case RS256:
		return jose.RS256
	case ES256:
		return jose.ES256
	default:
		// Unreachable for a validated Algorithm, and returning a zero value
		// here would let an invalid algorithm reach go-jose as "".
		return jose.SignatureAlgorithm("")
	}
}

// minRSABits is the shortest RSA key this service will generate or load.
//
// Checked at load as well as at generation. A key that was acceptable when it
// was created is not automatically acceptable now, and the load path is the
// one that runs against keys somebody else made.
const minRSABits = 2048

var (
	// ErrUnsupportedAlgorithm is returned for anything outside {RS256, ES256}.
	ErrUnsupportedAlgorithm = errors.New("unsupported signing algorithm")

	// ErrWeakKey is returned for an RSA key below minRSABits.
	ErrWeakKey = errors.New("signing key is too short")

	// ErrMalformedKey is returned when key material cannot be parsed. It never
	// wraps the material itself.
	ErrMalformedKey = errors.New("signing key is malformed")
)

// KeyPair is a generated key pair, before it is stored anywhere.
//
// The private key is a live crypto.Signer rather than bytes, so that callers
// which only need to sign never handle the encoded form. PrivatePEM exists for
// exactly one caller — the generator writing it to the secret store — and
// nothing else should read it.
type KeyPair struct {
	KID        string
	Algorithm  Algorithm
	Public     crypto.PublicKey
	Private    crypto.Signer
	PublicPEM  string
	PrivatePEM string
}

// Generate creates a key pair and derives its kid.
func Generate(alg Algorithm) (*KeyPair, error) {
	if !alg.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, alg)
	}

	var (
		signer crypto.Signer
		err    error
	)

	switch alg {
	case RS256:
		signer, err = rsa.GenerateKey(rand.Reader, minRSABits)
	case ES256:
		signer, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	if err != nil {
		// Deliberately not wrapped with %w on a value that could carry key
		// material in some future implementation.
		return nil, fmt.Errorf("generating %s key: %w", alg, err)
	}

	kid, err := Thumbprint(signer.Public())
	if err != nil {
		return nil, err
	}

	publicPEM, err := encodePublicPEM(signer.Public())
	if err != nil {
		return nil, err
	}

	privatePEM, err := encodePrivatePEM(signer)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		KID:        kid,
		Algorithm:  alg,
		Public:     signer.Public(),
		Private:    signer,
		PublicPEM:  publicPEM,
		PrivatePEM: privatePEM,
	}, nil
}

// Thumbprint derives a key id from the public key, per RFC 7638.
//
// Derived rather than random, for three reasons that all matter:
//
//   - It is STABLE. The same key produces the same kid on every restart and in
//     every process, so a consumer's cached key set stays valid. A randomly
//     generated kid regenerated at startup would break every consumer on every
//     deploy — and would pass every test that only ever ran one process.
//   - It cannot be CHOSEN. An attacker cannot craft a key whose thumbprint
//     collides with a legitimate one without breaking SHA-256, which closes
//     the kid-collision abuse case structurally rather than by comparison.
//   - It is a function of the key alone, so two deployments that somehow share
//     a key agree on its name.
func Thumbprint(pub crypto.PublicKey) (string, error) {
	jwk := jose.JSONWebKey{Key: pub}

	sum, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("%w: computing thumbprint: %v", ErrMalformedKey, err)
	}

	return base64url(sum), nil
}

func encodePublicPEM(pub crypto.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("%w: encoding public key", ErrMalformedKey)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func encodePrivatePEM(signer crypto.Signer) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(signer)
	if err != nil {
		// No %w and no detail: an error from key marshalling is the one place
		// a careless format verb could put material into a log line.
		return "", fmt.Errorf("%w: encoding private key", ErrMalformedKey)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// ParsePrivateKey reads a PKCS#8 PEM private key and checks it is strong
// enough to use.
//
// The strength check runs here, not only at generation: this is the path that
// loads a key somebody else created, possibly years ago, and "it was fine when
// we made it" is not a property that survives.
func ParsePrivateKey(pemBytes []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: not PEM-encoded", ErrMalformedKey)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: not a PKCS#8 private key", ErrMalformedKey)
	}

	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("%w: key cannot sign", ErrMalformedKey)
	}

	if rsaKey, ok := signer.(*rsa.PrivateKey); ok {
		if bits := rsaKey.N.BitLen(); bits < minRSABits {
			return nil, fmt.Errorf("%w: RSA key is %d bits, minimum is %d",
				ErrWeakKey, bits, minRSABits)
		}
	}

	return signer, nil
}

// ParsePublicKey reads a PKIX PEM public key.
func ParsePublicKey(pemBytes []byte) (crypto.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: not PEM-encoded", ErrMalformedKey)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: not a PKIX public key", ErrMalformedKey)
	}

	if rsaKey, ok := pub.(*rsa.PublicKey); ok {
		if bits := rsaKey.N.BitLen(); bits < minRSABits {
			return nil, fmt.Errorf("%w: RSA key is %d bits, minimum is %d",
				ErrWeakKey, bits, minRSABits)
		}
	}

	return pub, nil
}

// AlgorithmFor reports the algorithm a key can be used with.
//
// Used to check that the algorithm recorded in the database matches the key
// actually stored. A mismatch means a row was edited by hand or a key was
// replaced without its metadata, and signing with the wrong algorithm for a
// key type fails in ways that are hard to read.
func AlgorithmFor(pub crypto.PublicKey) (Algorithm, error) {
	switch key := pub.(type) {
	case *rsa.PublicKey:
		return RS256, nil
	case *ecdsa.PublicKey:
		if key.Curve != elliptic.P256() {
			return "", fmt.Errorf("%w: ES256 requires P-256, got %s",
				ErrUnsupportedAlgorithm, key.Curve.Params().Name)
		}
		return ES256, nil
	default:
		return "", fmt.Errorf("%w: %T", ErrUnsupportedAlgorithm, pub)
	}
}

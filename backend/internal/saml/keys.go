package saml

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/zed378/zed-auth/backend/internal/signing"
)

// The SAML signing key, and why it is not the OIDC one (P4-07 A-7).
//
// `signing_keys` has carried `purpose IN ('oidc','saml')` with one `current` per
// purpose since `P1-03`, so the separation is a schema property rather than a
// convention this package maintains. What it buys is worth stating: a service
// provider that pins the SAML certificate is trusting exactly one key for
// exactly one protocol, and a compromise of either key does not forge the
// other's credentials. Sharing one key would also mean rotating both at once,
// which turns two independent operational decisions into one.
//
// SAML also needs something OIDC does not: an X.509 CERTIFICATE. A JWKS
// consumer looks a key up by `kid`; a SAML service provider pins a certificate
// from metadata and has no equivalent lookup. So a SAML key is a key and a
// certificate, and the certificate is stored rather than derived — see
// migration 041.

// CertificateLifetime is how long a generated SAML certificate is valid.
//
// Five years, deliberately long. This certificate is a carrier for a public key
// that a partner has pinned by hand, in a configuration file, in another
// organization — and every renewal is a coordinated change across every service
// provider. Key rotation is still the operational control (`P1-03`); the
// certificate's expiry is a backstop, not a schedule.
const CertificateLifetime = 5 * 365 * 24 * time.Hour

// ErrNoCertificate is a SAML key with no certificate.
var ErrNoCertificate = errors.New("saml: the signing key has no certificate")

// SelfSignedCertificate wraps a signing key in a certificate for SAML metadata.
//
// Self-signed because there is nothing for a CA to attest here. A SAML service
// provider does not build a chain to a public root; it pins this exact
// certificate out of band, which is the trust model SAML actually uses. Paying
// a CA would add a step and no property.
func SelfSignedCertificate(pair *signing.KeyPair, entityID string, notBefore time.Time) (string, error) {
	if pair == nil || pair.Private == nil {
		return "", fmt.Errorf("saml: no key to certify")
	}
	if _, ok := pair.Private.(*rsa.PrivateKey); !ok {
		// goxmldsig signs with RSA. An EC key would be accepted here and fail
		// at the first signature, which is a worse place to find out.
		return "", fmt.Errorf("saml: the signing key is %T, want RSA", pair.Private)
	}

	// The serial is derived from the key id rather than random, so regenerating
	// a certificate for the same key produces the same serial and an operator
	// comparing two of them can see they describe one key.
	serial := new(big.Int).SetBytes([]byte(pair.KID))
	if serial.Sign() == 0 {
		serial = big.NewInt(1)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: entityID},
		Issuer:       pkix.Name{CommonName: entityID},
		NotBefore:    notBefore.UTC(),
		NotAfter:     notBefore.Add(CertificateLifetime).UTC(),

		// Signing only. Not a TLS certificate, not an encryption certificate,
		// and saying so limits what it can be misused for if it is ever
		// installed somewhere it does not belong.
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: nil,

		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, pair.Public, pair.Private)
	if err != nil {
		return "", fmt.Errorf("saml: creating the certificate: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

// ParseCertificate reads a stored PEM certificate.
func ParseCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%w: not a PEM CERTIFICATE block", ErrNoCertificate)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("saml: parsing the certificate: %w", err)
	}
	return cert, nil
}

// SigningKey pairs a private key with the certificate published for it.
//
// It implements goxmldsig's X509KeyStore, which is how the signature carries the
// certificate a service provider pinned.
type SigningKey struct {
	private *rsa.PrivateKey
	der     []byte

	// KID is the key's id, for logging and for matching a signature back to the
	// key that made it.
	KID string
}

// NewSigningKey adapts a stored key pair and its certificate for signing.
func NewSigningKey(pair *signing.KeyPair, certPEM string) (*SigningKey, error) {
	if pair == nil {
		return nil, fmt.Errorf("saml: no key")
	}
	return NewSigningKeyFrom(pair.KID, pair.Private, certPEM)
}

// NewSigningKeyFrom adapts a signer and its certificate.
//
// The form the running service uses: `internal/signing` hands out a
// `crypto.Signer` for the current key rather than a whole KeyPair, because the
// private half does not leave that package as PEM.
func NewSigningKeyFrom(kid string, signer crypto.Signer, certPEM string) (*SigningKey, error) {
	private, ok := signer.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("saml: the signing key is %T, want RSA", signer)
	}
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}

	// The certificate must describe THIS key. A certificate for a different key
	// produces signatures nobody can verify, and the failure appears at the
	// service provider rather than here — which is the worst place for it,
	// because the service provider cannot see either of the inputs.
	certPublic, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || !certPublic.Equal(&private.PublicKey) {
		return nil, fmt.Errorf("%w: the certificate is for a different key", ErrNoCertificate)
	}

	return &SigningKey{private: private, der: cert.Raw, KID: kid}, nil
}

// GetKeyPair implements goxmldsig's X509KeyStore.
func (k *SigningKey) GetKeyPair() (*rsa.PrivateKey, []byte, error) {
	if k == nil || k.private == nil {
		return nil, nil, fmt.Errorf("saml: no signing key")
	}
	return k.private, k.der, nil
}

// Certificate returns the certificate this key signs with, for metadata.
func (k *SigningKey) Certificate() (*x509.Certificate, error) {
	if k == nil || len(k.der) == 0 {
		return nil, ErrNoCertificate
	}
	return x509.ParseCertificate(k.der)
}

package samlapi

import (
	"context"
	"crypto/x509"
	"fmt"

	"github.com/zed378/zed-auth/backend/internal/saml"
	signingpkg "github.com/zed378/zed-auth/backend/internal/signing"
)

// Finding the SAML signing key (P4-08 F-1).
//
// It is a different key from the OIDC one, by `purpose`, and it carries a
// certificate the OIDC key has no use for — a service provider pins the
// certificate out of metadata, where a JWKS consumer looks a key up by `kid`.
//
// Absence is an ordinary answer rather than an error worth logging at every
// request: an instance that has never run `keyctl generate --purpose saml` is
// an instance that does not offer SAML, and the endpoints answer 503 and say
// so. It is not a broken deployment.

// KeySource reads the SAML key set.
//
// An interface over the cache rather than the cache itself, so this package
// does not depend on how keys are loaded or how long they are held.
type KeySource interface {
	Get() (*signingpkg.KeySet, error)
}

// CachedKeys resolves the current SAML signing key through a key set cache.
type CachedKeys struct {
	Source KeySource
}

// NewCachedKeys returns a CachedKeys.
func NewCachedKeys(source KeySource) *CachedKeys { return &CachedKeys{Source: source} }

// Published returns the certificates a service provider should trust.
//
// The one assertions are signed with now, then the one that will sign next
// (P4-09). A service provider pins what it reads from /saml/metadata, so
// publishing `next` before it signs anything is the only thing that makes a
// rotation safe without a round of emails — the SAML counterpart of
// publishing a key in JWKS before signing with it.
//
// # Certificates, not keys
//
// This returns x509 certificates rather than `saml.SigningKey`s, and the
// distinction is load-bearing rather than cosmetic. A `SigningKey` carries a
// signer, and `signing.Key.Signer` deliberately refuses any key that is not
// `current` — a `next` key exists so consumers can fetch it before it is used,
// and handing out its private half would be the one place that rule was not
// enforced.
//
// Publication needs no private half at all. An earlier version of this asked
// for one anyway, and every `next` key failed that request and was silently
// skipped: the document went back to naming exactly one certificate and the
// overlap this function exists for did not exist. Asking only for what
// publication needs makes that mistake unavailable.
//
// `previous` is not included. This service issues rather than consumes: a
// demoted key verifies nothing anybody is asking about, and publishing it
// would keep it trusted after it stopped being used.
func (k *CachedKeys) Published(ctx context.Context) ([]*x509.Certificate, error) {
	if k == nil || k.Source == nil {
		return nil, fmt.Errorf("%w: no key source", ErrNotConfigured)
	}

	set, err := k.Source.Get()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}

	// The key that signs, first and from the same accessor the issuing path
	// uses. A document whose first certificate is not the one in use is the
	// failure metadata exists to prevent, so it comes from the one place that
	// can answer "what signs now".
	current, err := set.Current()
	if err != nil {
		return nil, fmt.Errorf("%w: no current SAML key", ErrNotConfigured)
	}
	signing, err := certificateOf(current)
	if err != nil {
		return nil, err
	}
	out := []*x509.Certificate{signing}

	for _, key := range set.Verifying() {
		if key.Status != signingpkg.StatusNext || key.KID == current.KID {
			continue
		}
		cert, err := certificateOf(key)
		if err != nil {
			// A `next` key with an unreadable certificate is skipped rather
			// than failing the document: one unusable key must not take the
			// working one with it, and the service provider still gets what
			// signs today.
			continue
		}
		out = append(out, cert)
	}

	return out, nil
}

// certificateOf reads the certificate published for a key.
func certificateOf(key *signingpkg.Key) (*x509.Certificate, error) {
	if key.CertificatePEM == "" {
		// Migration 041 makes this unstorable for a SAML key, so reaching it
		// means a row predates the constraint or was written by hand.
		return nil, fmt.Errorf("%w: key %s has no certificate", ErrNotConfigured, key.KID)
	}
	return saml.ParseCertificate(key.CertificatePEM)
}

// SAML returns the key assertions are signed with.
//
// The certificate is read from the same row as the key, so the certificate in
// metadata and the key that signs cannot come apart — which is a failure only
// the service provider could see.
func (k *CachedKeys) SAML(ctx context.Context) (*saml.SigningKey, error) {
	if k == nil || k.Source == nil {
		return nil, fmt.Errorf("%w: no key source", ErrNotConfigured)
	}

	set, err := k.Source.Get()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}

	current, err := set.Current()
	if err != nil {
		return nil, fmt.Errorf("%w: no current SAML key", ErrNotConfigured)
	}
	if current.CertificatePEM == "" {
		// Migration 041 makes this impossible to store, so reaching it means
		// the row predates the constraint or was written by hand. Refused
		// rather than signing with a key nothing can verify.
		return nil, fmt.Errorf("%w: the SAML key has no certificate", ErrNotConfigured)
	}

	// The private half comes from the key set, which loaded it through the
	// secret resolver; nothing here touches key material directly.
	signer, err := current.Signer()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	return saml.NewSigningKeyFrom(current.KID, signer, current.CertificatePEM)
}

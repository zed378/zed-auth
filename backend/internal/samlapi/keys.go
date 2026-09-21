package samlapi

import (
	"context"
	"fmt"

	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/signing"
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
	Get() (*signing.KeySet, error)
}

// CachedKeys resolves the current SAML signing key through a key set cache.
type CachedKeys struct {
	Source KeySource
}

// NewCachedKeys returns a CachedKeys.
func NewCachedKeys(source KeySource) *CachedKeys { return &CachedKeys{Source: source} }

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

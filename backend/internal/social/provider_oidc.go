package social

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-jose/go-jose/v4"
)

func (a *Adapter) verifyIDToken(ctx context.Context, compact, nonce string) (ProviderIdentity, error) {
	if len(compact) == 0 || len(compact) > 32768 {
		return ProviderIdentity{}, ErrProviderResponse
	}
	signed, err := jose.ParseSigned(compact, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(signed.Signatures) != 1 {
		return ProviderIdentity{}, ErrProviderResponse
	}
	header := signed.Signatures[0].Header
	if header.KeyID == "" || len(header.KeyID) > 255 {
		return ProviderIdentity{}, ErrProviderResponse
	}
	key, err := a.verificationKey(ctx, header.KeyID)
	if err != nil {
		return ProviderIdentity{}, ErrProviderResponse
	}
	payload, err := signed.Verify(key)
	if err != nil {
		return ProviderIdentity{}, ErrProviderResponse
	}
	var c struct {
		Issuer          string          `json:"iss"`
		Subject         string          `json:"sub"`
		Audience        json.RawMessage `json:"aud"`
		AuthorizedParty string          `json:"azp"`
		Expiry          int64           `json:"exp"`
		IssuedAt        int64           `json:"iat"`
		NotBefore       int64           `json:"nbf"`
		Nonce           string          `json:"nonce"`
		EmailVerified   bool            `json:"email_verified"`
		Tenant          string          `json:"tid"`
	}
	if err = json.Unmarshal(payload, &c); err != nil {
		return ProviderIdentity{}, ErrProviderResponse
	}
	now := a.now().Unix()
	issuerOK := c.Issuer == a.issuer || a.cfg.Provider == Google && c.Issuer == "accounts.google.com"
	if !issuerOK || !validSubject(c.Subject) || c.Expiry <= now || c.IssuedAt <= 0 || c.IssuedAt > now+30 || c.Expiry <= c.IssuedAt || c.NotBefore > now+30 || subtle.ConstantTimeCompare([]byte(c.Nonce), []byte(nonce)) != 1 {
		return ProviderIdentity{}, ErrProviderResponse
	}
	var aud []string
	var single string
	if json.Unmarshal(c.Audience, &single) == nil {
		aud = []string{single}
	} else if json.Unmarshal(c.Audience, &aud) != nil {
		return ProviderIdentity{}, ErrProviderResponse
	}
	found := false
	for _, v := range aud {
		if v == a.cfg.ClientID {
			found = true
		}
	}
	if !found || (len(aud) > 1 && c.AuthorizedParty != a.cfg.ClientID) || (c.AuthorizedParty != "" && c.AuthorizedParty != a.cfg.ClientID) {
		return ProviderIdentity{}, ErrProviderResponse
	}
	if a.cfg.Provider == Google && !c.EmailVerified || a.cfg.Provider == Microsoft && c.Tenant != a.cfg.MicrosoftTenantID {
		return ProviderIdentity{}, ErrProviderResponse
	}
	return ProviderIdentity{Provider: a.cfg.Provider, Subject: c.Subject}, nil
}

func (a *Adapter) verificationKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	find := func() *rsa.PublicKey {
		var found *rsa.PublicKey
		matches := 0
		for _, key := range a.keys {
			if key.KeyID != kid {
				continue
			}
			matches++
			if key.Algorithm != "" && key.Algorithm != "RS256" || key.Use != "" && key.Use != "sig" {
				continue
			}
			pub, ok := key.Key.(*rsa.PublicKey)
			if ok && pub.N.BitLen() >= 2048 && key.IsPublic() {
				found = pub
			}
		}
		if matches != 1 {
			return nil
		}
		return found
	}
	if now.Before(a.keysExpire) {
		if key := find(); key != nil {
			return key, nil
		}
	}
	if !a.lastRefresh.IsZero() && now.Sub(a.lastRefresh) < 30*time.Second {
		return nil, ErrProviderResponse
	}
	a.lastRefresh = now
	var metadata struct {
		Issuer       string `json:"issuer"`
		AuthorizeURL string `json:"authorization_endpoint"`
		TokenURL     string `json:"token_endpoint"`
		JWKSURL      string `json:"jwks_uri"`
	}
	// Discover only at the pinned issuer. Metadata must confirm the supported
	// endpoints; it cannot redirect credentials or key fetches to another host.
	if err := a.request(ctx, http.MethodGet, a.issuer+"/.well-known/openid-configuration", "", nil, &metadata); err != nil ||
		metadata.Issuer != a.issuer || metadata.AuthorizeURL != a.authorize ||
		metadata.TokenURL != a.token || metadata.JWKSURL != a.jwks {
		return nil, ErrProviderResponse
	}
	var set jose.JSONWebKeySet
	if err := a.request(ctx, http.MethodGet, a.jwks, "", nil, &set); err != nil || len(set.Keys) == 0 || len(set.Keys) > 100 {
		return nil, ErrProviderResponse
	}
	a.keys = set.Keys
	a.keysExpire = now.Add(5 * time.Minute)
	if key := find(); key != nil {
		return key, nil
	}
	return nil, ErrProviderResponse
}

package social

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func providerMetadata(a *Adapter) map[string]string {
	return map[string]string{
		"issuer": a.issuer, "authorization_endpoint": a.authorize,
		"token_endpoint": a.token, "jwks_uri": a.jwks,
	}
}

func providerMetadataResponse(a *Adapter) *http.Response {
	raw, _ := json.Marshal(providerMetadata(a))
	return providerResponse(http.StatusOK, string(raw))
}

func TestOIDCDiscoveryCannotReplacePinnedEndpoints(t *testing.T) {
	for _, provider := range []Provider{Google, Microsoft} {
		for _, field := range []string{"issuer", "authorization_endpoint", "token_endpoint", "jwks_uri"} {
			t.Run(string(provider)+"/"+field, func(t *testing.T) {
				a := testProvider(t, provider)
				metadata := providerMetadata(a)
				metadata[field] = "https://attacker.example/credential-sink"
				calls := 0
				a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.String() != a.issuer+"/.well-known/openid-configuration" || r.Header.Get("Authorization") != "" {
						t.Fatal("untrusted endpoint or credential-bearing discovery request")
					}
					raw, _ := json.Marshal(metadata)
					return providerResponse(http.StatusOK, string(raw)), nil
				})
				for i := 0; i < 2; i++ {
					if _, err := a.verificationKey(context.Background(), "kid"); !errors.Is(err, ErrProviderResponse) {
						t.Fatalf("untrusted metadata accepted: %v", err)
					}
				}
				if calls != 1 {
					t.Fatalf("failed discovery amplified to %d requests", calls)
				}
			})
		}
	}
}

func TestOIDCDiscoveryAndKeysShareTheRefreshCache(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []Provider{Google, Microsoft} {
		t.Run(string(provider), func(t *testing.T) {
			a := testProvider(t, provider)
			discoveryCalls, keyCalls := 0, 0
			a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case a.issuer + "/.well-known/openid-configuration":
					discoveryCalls++
					return providerMetadataResponse(a), nil
				case a.jwks:
					keyCalls++
					raw, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "kid", Algorithm: "RS256", Use: "sig"}}})
					return providerResponse(http.StatusOK, string(raw)), nil
				default:
					t.Fatal("unexpected provider request")
					return nil, ErrProviderResponse
				}
			})
			for i := 0; i < 3; i++ {
				if _, err := a.verificationKey(context.Background(), "kid"); err != nil {
					t.Fatal(err)
				}
			}
			if discoveryCalls != 1 || keyCalls != 1 {
				t.Fatalf("uncached metadata/keys: %d/%d", discoveryCalls, keyCalls)
			}
		})
	}
}

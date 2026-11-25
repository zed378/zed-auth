//go:build integration

package social

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func TestProviderRoundTripWithRedisStateAndTLS(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := redisStateStore(t)
	ctx := context.Background()
	for _, provider := range []Provider{Google, Microsoft, GitHub} {
		t.Run(string(provider), func(t *testing.T) {
			a := testProvider(t, provider)
			binding := testStateBinding()
			binding.Provider = provider
			binding.RedirectURI = a.cfg.RedirectURI
			issued, err := store.Issue(ctx, binding)
			if err != nil {
				t.Fatal(err)
			}
			authorizeURL, err := a.AuthorizeURL(issued.State, issued.Nonce, issued.PKCEChallenge)
			if err != nil {
				t.Fatal(err)
			}
			params, _ := url.Parse(authorizeURL)
			if params.Query().Get("code_challenge") != issued.PKCEChallenge || params.Query().Get("redirect_uri") != binding.RedirectURI {
				t.Fatal("authorization request lost its PKCE or callback binding")
			}
			claims := providerClaims(a)
			claims["nonce"] = issued.Nonce
			compact := signProviderToken(t, key, claims, jose.RS256, "current")
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				endpoint := "https://" + r.Host + r.URL.RequestURI()
				switch endpoint {
				case a.token:
					if err := r.ParseForm(); err != nil || r.Method != http.MethodPost || r.Form.Get("code") != "one-time-code" || r.Form.Get("code_verifier") != issued.PKCEVerifier || r.Form.Get("client_secret") != "provider-secret" || r.Form.Get("redirect_uri") != binding.RedirectURI {
						t.Error("token exchange lost its credential or PKCE binding")
						http.Error(w, "invalid request", http.StatusBadRequest)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"id_token": compact, "access_token": "ephemeral-token", "token_type": "bearer"})
				case a.issuer + "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(providerMetadata(a))
				case a.jwks:
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "current", Algorithm: "RS256", Use: "sig"}}})
				case "https://api.github.com/user", "https://api.github.com/user/emails":
					if r.Header.Get("Authorization") != "Bearer ephemeral-token" {
						t.Error("missing GitHub bearer credential")
					}
					if r.URL.Path == "/user" {
						_ = json.NewEncoder(w).Encode(map[string]int{"id": 123})
					} else {
						_ = json.NewEncoder(w).Encode([]githubEmail{{Email: "verified@example.test", Verified: true}})
					}
				default:
					t.Errorf("unexpected provider endpoint: %s", endpoint)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			local, _ := url.Parse(server.URL)
			transport := server.Client().Transport
			// Keep production endpoint selection, but send test traffic through TLS
			// to the local fixture. This transport is never used by production code.
			a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
				cloned := r.Clone(r.Context())
				cloned.Host = r.URL.Host
				cloned.URL.Host = local.Host
				return transport.RoundTrip(cloned)
			})
			transaction, err := store.Consume(ctx, issued.State, binding)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := a.Exchange(ctx, "one-time-code", transaction.PKCEVerifier, transaction.Nonce)
			expectedSubject := "stable-subject"
			if provider == GitHub {
				expectedSubject = "123"
			}
			if err != nil || identity.Provider != provider || identity.Subject != expectedSubject {
				t.Fatalf("provider round trip failed: %+v %v", identity, err)
			}
			if _, err := store.Consume(ctx, issued.State, binding); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("callback replay accepted: %v", err)
			}
			if calls.Load() != 3 {
				t.Fatalf("unexpected provider request count: %d", calls.Load())
			}
		})
	}
}

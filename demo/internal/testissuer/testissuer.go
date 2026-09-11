// Package testissuer is a miniature auth service for the demo applications'
// tests: a key set, a token endpoint, and a way to mint ID tokens.
//
// It exists so the two applications can be tested against something that
// behaves like the real issuer over HTTP — signing with a real RSA key,
// answering a real token exchange — rather than against a stubbed verifier.
// Stubbing the verifier would remove the only security control these
// applications have from the test, which is the one thing a test of a demo
// consumer must not do.
//
// `internal/verify` deliberately keeps its OWN fixture instead of using this
// one: it needs to mint malformed, misaligned and forged tokens, and a helper
// with a knob for every one of those is a helper nobody can read.
package testissuer

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const kid = "test-key"

// Issuer is a signing key behind an HTTP server. Its URL is the issuer.
type Issuer struct {
	Server *httptest.Server
	key    *rsa.PrivateKey

	// Exchange is consulted by the token endpoint. It receives the posted form
	// and the Authorization header, and returns the ID token to answer with —
	// or an empty string to answer 400. A test that wants to assert PKCE or
	// client authentication actually happened does it here.
	Exchange func(form url.Values, authorization string) string
}

// New starts an issuer and returns it. The server is closed with the test.
func New(t *testing.T) *Issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	issuer := &Issuer{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kid": kid,
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if issuer.Exchange == nil {
			http.Error(w, "this test set no Exchange", http.StatusNotImplemented)
			return
		}
		token := issuer.Exchange(r.PostForm, r.Header.Get("Authorization"))
		if token == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id_token":     token,
			"access_token": "an-access-token-the-demo-does-not-verify",
			"token_type":   "Bearer",
			"expires_in":   600,
		})
	})

	issuer.Server = httptest.NewServer(mux)
	t.Cleanup(issuer.Server.Close)
	return issuer
}

// URL is the issuer identifier, which is also where the key set is published.
func (i *Issuer) URL() string { return i.Server.URL }

// IDToken mints a valid ID token for one audience and nonce.
func (i *Issuer) IDToken(t *testing.T, audience, nonce string) string {
	t.Helper()
	return i.IDTokenAt(t, audience, nonce, time.Now())
}

// IDTokenAt mints an ID token as though it were issued at `at`.
func (i *Issuer) IDTokenAt(t *testing.T, audience, nonce string, at time.Time) string {
	t.Helper()

	header := map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}
	claims := map[string]any{
		"iss":       i.URL(),
		"sub":       "user-" + audience,
		"aud":       audience,
		"iat":       at.Unix(),
		"exp":       at.Add(5 * time.Minute).Unix(),
		"auth_time": at.Unix(),
		"amr":       []string{"pwd"},
		"org_id":    "org-1",
		"nonce":     nonce,
	}

	signing := encode(t, header) + "." + encode(t, claims)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func encode(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

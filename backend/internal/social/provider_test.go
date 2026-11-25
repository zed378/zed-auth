package social

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/zed378/zed-auth/backend/internal/config"
)

type providerSecrets struct{ err error }

func (s providerSecrets) Resolve(config.SecretRef) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []byte("provider-secret"), nil
}

type providerTransport func(*http.Request) (*http.Response, error)

func (f providerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func providerResponse(status int, raw string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
}

func testProvider(t *testing.T, p Provider) *Adapter {
	t.Helper()
	c := ProviderConfig{Provider: p, ClientID: "client", ClientSecretRef: "env:TEST_SECRET", RedirectURI: "https://auth.example/login/social/" + string(p) + "/callback"}
	if p == Microsoft {
		c.MicrosoftTenantID = "11111111-1111-4111-8111-111111111111"
	}
	a, err := NewAdapter(c, providerSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return time.Unix(1800000000, 0) }
	return a
}

func TestProviderConfigurationRejectsUnsafeSettings(t *testing.T) {
	base := testProvider(t, Google).cfg
	cases := map[string]func(*ProviderConfig){
		"unknown provider":        func(c *ProviderConfig) { c.Provider = "other" },
		"empty client":            func(c *ProviderConfig) { c.ClientID = "" },
		"client whitespace":       func(c *ProviderConfig) { c.ClientID = " client " },
		"plain secret":            func(c *ProviderConfig) { c.ClientSecretRef = "plaintext" },
		"empty secret ref":        func(c *ProviderConfig) { c.ClientSecretRef = "env:" },
		"http callback":           func(c *ProviderConfig) { c.RedirectURI = "http://auth.example/login/social/google/callback" },
		"userinfo":                func(c *ProviderConfig) { c.RedirectURI = "https://secret@auth.example/login/social/google/callback" },
		"query":                   func(c *ProviderConfig) { c.RedirectURI += "?next=evil" },
		"empty query":             func(c *ProviderConfig) { c.RedirectURI += "?" },
		"fragment":                func(c *ProviderConfig) { c.RedirectURI += "#secret" },
		"empty fragment":          func(c *ProviderConfig) { c.RedirectURI += "#" },
		"wrong provider callback": func(c *ProviderConfig) { c.RedirectURI = "https://auth.example/login/social/github/callback" },
		"tenant on google":        func(c *ProviderConfig) { c.MicrosoftTenantID = "11111111-1111-4111-8111-111111111111" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			mutate(&c)
			if _, err := NewAdapter(c, providerSecrets{}); !errors.Is(err, ErrProviderConfig) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for _, tenant := range []string{"common", "organizations", "consumers", "", "00000000-0000-0000-0000-000000000000"} {
		c := testProvider(t, Microsoft).cfg
		c.MicrosoftTenantID = tenant
		if c.Validate() == nil {
			t.Fatalf("accepted tenant %q", tenant)
		}
	}
	if _, err := NewAdapter(base, nil); err == nil {
		t.Fatal("nil resolver accepted")
	}
}

func TestProviderAuthorizationUsesFixedCallbackAndPKCE(t *testing.T) {
	for _, p := range []Provider{Google, Microsoft, GitHub} {
		t.Run(string(p), func(t *testing.T) {
			a := testProvider(t, p)
			raw, err := a.AuthorizeURL(strings.Repeat("s", 43), strings.Repeat("n", 43), strings.Repeat("c", 43))
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(raw)
			q := u.Query()
			if u.Scheme != "https" || q.Get("redirect_uri") != a.cfg.RedirectURI || q.Get("code_challenge_method") != "S256" || q.Get("state") != strings.Repeat("s", 43) || q.Get("client_secret") != "" {
				t.Fatalf("unsafe URL: %v", u)
			}
			if p == GitHub && q.Get("nonce") != "" {
				t.Fatal("GitHub is OAuth, not OIDC")
			}
			if p != GitHub && q.Get("nonce") == "" {
				t.Fatal("missing nonce")
			}
			if _, err = a.AuthorizeURL("x", strings.Repeat("n", 43), strings.Repeat("c", 43)); err == nil {
				t.Fatal("short state accepted")
			}
		})
	}
}

func signProviderToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any, alg jose.SignatureAlgorithm, kid string) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return compact
}

func providerClaims(a *Adapter) map[string]any {
	return map[string]any{"iss": a.issuer, "sub": "stable-subject", "aud": "client", "iat": a.now().Unix() - 60, "exp": a.now().Unix() + 300, "nonce": strings.Repeat("n", 43), "email_verified": true, "tid": a.cfg.MicrosoftTenantID}
}

func TestOIDCProviderVerifiesClaimsAndRejectsAbuse(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Provider{Google, Microsoft} {
		t.Run(string(p), func(t *testing.T) {
			cases := map[string]struct {
				mutate func(map[string]any)
				valid  bool
			}{
				"valid":              {func(map[string]any) {}, true},
				"audience array":     {func(c map[string]any) { c["aud"] = []string{"client", "other"}; c["azp"] = "client" }, true},
				"wrong issuer":       {func(c map[string]any) { c["iss"] = "https://attacker.example" }, false},
				"wrong audience":     {func(c map[string]any) { c["aud"] = "other" }, false},
				"missing azp":        {func(c map[string]any) { c["aud"] = []string{"client", "other"} }, false},
				"wrong azp":          {func(c map[string]any) { c["azp"] = "other" }, false},
				"expired":            {func(c map[string]any) { c["exp"] = int64(1800000000) }, false},
				"missing expiry":     {func(c map[string]any) { delete(c, "exp") }, false},
				"missing issued at":  {func(c map[string]any) { delete(c, "iat") }, false},
				"future issued at":   {func(c map[string]any) { c["iat"] = int64(1800000031) }, false},
				"not yet valid":      {func(c map[string]any) { c["nbf"] = int64(1800000031) }, false},
				"nonce replay":       {func(c map[string]any) { c["nonce"] = strings.Repeat("x", 43) }, false},
				"empty subject":      {func(c map[string]any) { c["sub"] = "" }, false},
				"long subject":       {func(c map[string]any) { c["sub"] = strings.Repeat("x", 256) }, false},
				"non string subject": {func(c map[string]any) { c["sub"] = 1 }, false},
			}
			if p == Google {
				cases["unverified email"] = struct {
					mutate func(map[string]any)
					valid  bool
				}{func(c map[string]any) { c["email_verified"] = false }, false}
			} else {
				cases["no email verification claim"] = struct {
					mutate func(map[string]any)
					valid  bool
				}{func(c map[string]any) { delete(c, "email_verified") }, true}
				cases["wrong tenant"] = struct {
					mutate func(map[string]any)
					valid  bool
				}{func(c map[string]any) { c["tid"] = "other" }, false}
			}
			for name, tc := range cases {
				t.Run(name, func(t *testing.T) {
					a := testProvider(t, p)
					a.keys = []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "kid", Algorithm: "RS256", Use: "sig"}}
					a.keysExpire = a.now().Add(time.Minute)
					a.http.Transport = providerTransport(func(*http.Request) (*http.Response, error) {
						t.Fatal("unexpected network")
						return nil, errors.New("unreachable")
					})
					claims := providerClaims(a)
					tc.mutate(claims)
					token := signProviderToken(t, key, claims, jose.RS256, "kid")
					got, err := a.verifyIDToken(context.Background(), token, strings.Repeat("n", 43))
					if tc.valid {
						if err != nil || got.Subject != "stable-subject" || got.Provider != p {
							t.Fatalf("got %+v %v", got, err)
						}
					} else if !errors.Is(err, ErrProviderResponse) {
						t.Fatalf("got %+v %v", got, err)
					}
				})
			}
		})
	}
}

func TestOIDCJWKSCacheAndUnknownKeyThrottle(t *testing.T) {
	a := testProvider(t, Google)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "first", Algorithm: "RS256", Use: "sig"}}}
	calls := 0
	a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == a.issuer+"/.well-known/openid-configuration" {
			return providerMetadataResponse(a), nil
		}
		calls++
		if r.URL.String() != a.jwks {
			t.Fatal("wrong JWKS endpoint")
		}
		raw, _ := json.Marshal(set)
		return providerResponse(200, string(raw)), nil
	})
	for i := 0; i < 2; i++ {
		if _, err = a.verificationKey(context.Background(), "first"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err = a.verificationKey(context.Background(), "attacker"); err == nil {
			t.Fatal("unknown kid accepted")
		}
	}
	if calls != 1 {
		t.Fatalf("JWKS amplification: %d requests", calls)
	}
	a.now = func() time.Time { return time.Unix(1800000031, 0) }
	set.Keys[0].KeyID = "rotated"
	if _, err = a.verificationKey(context.Background(), "rotated"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("rotation refresh requests %d", calls)
	}
	a.now = func() time.Time { return time.Unix(1800000400, 0) }
	a.http.Transport = providerTransport(func(*http.Request) (*http.Response, error) { return providerResponse(503, "provider-secret"), nil })
	if _, err = a.verificationKey(context.Background(), "rotated"); err == nil {
		t.Fatal("expired cache used during outage")
	}
}

func TestOIDCRejectsInvalidSignatureAndKeys(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	for _, tc := range []struct {
		name     string
		pub      any
		alg, use string
		signer   *rsa.PrivateKey
		signAlg  jose.SignatureAlgorithm
		kid      string
	}{
		{"wrong signature", &other.PublicKey, "RS256", "sig", key, jose.RS256, "kid"},
		{"wrong key algorithm", &key.PublicKey, "RS512", "sig", key, jose.RS256, "kid"},
		{"encryption key", &key.PublicKey, "RS256", "enc", key, jose.RS256, "kid"},
		{"private JWK", key, "RS256", "sig", key, jose.RS256, "kid"},
		{"algorithm substitution", &key.PublicKey, "RS256", "sig", key, jose.RS512, "kid"},
		{"missing kid", &key.PublicKey, "RS256", "sig", key, jose.RS256, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testProvider(t, Google)
			a.keys = []jose.JSONWebKey{{Key: tc.pub, KeyID: "kid", Algorithm: tc.alg, Use: tc.use}}
			a.keysExpire = a.now().Add(time.Minute)
			a.lastRefresh = a.now()
			token := signProviderToken(t, tc.signer, providerClaims(a), tc.signAlg, tc.kid)
			if _, err := a.verifyIDToken(context.Background(), token, strings.Repeat("n", 43)); !errors.Is(err, ErrProviderResponse) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestGitHubExchangeUsesStableIDAndVerifiedEmail(t *testing.T) {
	for _, tc := range []struct {
		name, profile, emails string
		valid                 bool
	}{
		{"verified primary", `{"id":123,"email":"mutable@example.test"}`, `[{"email":"verified@example.test","verified":true,"primary":true}]`, true},
		{"verified secondary", `{"id":123}`, `[{"email":"a@example.test","verified":false},{"email":"b@example.test","verified":true}]`, true},
		{"missing profile email", `{"id":123}`, `[{"email":"a@example.test","verified":true}]`, true},
		{"unverified", `{"id":123}`, `[{"email":"a@example.test","verified":false}]`, false},
		{"missing emails", `{"id":123}`, `[]`, false},
		{"invalid verified email", `{"id":123}`, `[{"email":"Name <a@example.test>","verified":true}]`, false},
		{"invalid id", `{"id":0}`, `[{"email":"a@example.test","verified":true}]`, false},
		{"fractional id", `{"id":123.5}`, `[{"email":"a@example.test","verified":true}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testProvider(t, GitHub)
			calls := 0
			a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.URL.String() {
				case a.token:
					if r.Method != http.MethodPost {
						t.Fatal("wrong token method")
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.Form.Get("client_secret") != "provider-secret" || r.Form.Get("code_verifier") != strings.Repeat("v", 43) || r.Form.Get("redirect_uri") != a.cfg.RedirectURI {
						t.Fatal("incorrect token binding")
					}
					return providerResponse(200, `{"access_token":"provider-token","token_type":"bearer"}`), nil
				case "https://api.github.com/user":
					if r.Header.Get("Authorization") != "Bearer provider-token" {
						t.Fatal("missing credential")
					}
					return providerResponse(200, tc.profile), nil
				case "https://api.github.com/user/emails":
					return providerResponse(200, tc.emails), nil
				default:
					t.Fatal("unexpected endpoint")
					return nil, ErrProviderResponse
				}
			})
			got, err := a.Exchange(context.Background(), "code", strings.Repeat("v", 43), "")
			if tc.valid {
				if err != nil || got.Subject != "123" || got.Provider != GitHub || calls != 3 {
					t.Fatalf("got %+v %v, calls %d", got, err, calls)
				}
			} else if !errors.Is(err, ErrProviderResponse) {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestProviderNetworkAndSecretFailuresAreBoundedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"provider error", 400, `{"error":"provider-secret"}`},
		{"error at 200", 200, `{"error":"provider-secret"}`},
		{"invalid json", 200, "provider-secret"},
		{"oversize", 200, strings.Repeat("s", maxProviderResponse+1)},
		{"redirect", 302, "provider-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testProvider(t, GitHub)
			a.http.Transport = providerTransport(func(*http.Request) (*http.Response, error) {
				r := providerResponse(tc.status, tc.body)
				r.Header.Set("Location", "https://evil.example/steal")
				return r, nil
			})
			_, err := a.Exchange(context.Background(), "private-code", strings.Repeat("v", 43), "")
			if !errors.Is(err, ErrProviderResponse) || strings.Contains(err.Error(), "provider-secret") || strings.Contains(err.Error(), "private-code") {
				t.Fatalf("unsafe error %v", err)
			}
		})
	}
	a := testProvider(t, GitHub)
	a.secrets = providerSecrets{err: errors.New("provider-secret")}
	a.http.Transport = providerTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("network after resolver failure")
		return nil, ErrProviderResponse
	})
	if _, err := a.Exchange(context.Background(), "code", strings.Repeat("v", 43), ""); !errors.Is(err, ErrProviderResponse) {
		t.Fatal(err)
	}
	a = testProvider(t, Google)
	if a.http.Timeout <= 0 || a.http.Timeout > 10*time.Second {
		t.Fatal("unbounded timeout")
	}
	transport := a.http.Transport.(*http.Transport)
	if transport.ResponseHeaderTimeout <= 0 || transport.TLSClientConfig.MinVersion < 0x0303 {
		t.Fatal("unsafe transport")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	if _, err := a.Exchange(ctx, "code", strings.Repeat("v", 43), strings.Repeat("n", 43)); !errors.Is(err, ErrProviderResponse) {
		t.Fatal(err)
	}
	if err := a.request(context.Background(), "GET", "http://attacker.example", "", nil, &struct{}{}); err == nil {
		t.Fatal("HTTP accepted")
	}
}

func TestOIDCExchangeVerifiesIDTokenAndIgnoresProfileEmail(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Provider{Google, Microsoft} {
		t.Run(string(p), func(t *testing.T) {
			a := testProvider(t, p)
			claims := providerClaims(a)
			claims["email"] = "a-different-local-account@example.test"
			if p == Google {
				claims["iss"] = "accounts.google.com"
			}
			compact := signProviderToken(t, key, claims, jose.RS256, "current")
			calls := 0
			a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.URL.String() {
				case a.token:
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.Form.Get("client_secret") != "provider-secret" || r.Form.Get("code_verifier") != strings.Repeat("v", 43) || r.Form.Get("code") != "provider-code" {
						t.Fatal("unbound exchange")
					}
					raw, _ := json.Marshal(map[string]string{"id_token": compact, "access_token": "unused"})
					return providerResponse(200, string(raw)), nil
				case a.jwks:
					raw, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "current", Algorithm: "RS256", Use: "sig"}}})
					return providerResponse(200, string(raw)), nil
				case a.issuer + "/.well-known/openid-configuration":
					return providerMetadataResponse(a), nil
				default:
					t.Fatal("unexpected provider request")
					return nil, ErrProviderResponse
				}
			})
			got, err := a.Exchange(context.Background(), "provider-code", strings.Repeat("v", 43), strings.Repeat("n", 43))
			if err != nil || got.Subject != "stable-subject" || got.Provider != p || calls != 3 {
				t.Fatalf("got %+v %v (%d calls)", got, err, calls)
			}
		})
	}
}

func TestOIDCJWKSRejectsMalformedWeakAndAmbiguousKeys(t *testing.T) {
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	strong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	weakSet, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &weak.PublicKey, KeyID: "kid", Use: "sig", Algorithm: "RS256"}}})
	duplicateSet, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &strong.PublicKey, KeyID: "kid", Use: "sig", Algorithm: "RS256"}, {Key: &strong.PublicKey, KeyID: "kid", Use: "sig", Algorithm: "RS256"}}})
	for name, raw := range map[string]string{"malformed": "invalid", "empty": `{"keys":[]}`, "weak": string(weakSet), "duplicate kid": string(duplicateSet)} {
		t.Run(name, func(t *testing.T) {
			a := testProvider(t, Google)
			a.http.Transport = providerTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == a.issuer+"/.well-known/openid-configuration" {
					return providerMetadataResponse(a), nil
				}
				return providerResponse(200, raw), nil
			})
			if _, err := a.verificationKey(context.Background(), "kid"); !errors.Is(err, ErrProviderResponse) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestProviderValidationPrecedesNetworkAndSecretResolution(t *testing.T) {
	for _, p := range []Provider{Google, Microsoft, GitHub} {
		a := testProvider(t, p)
		a.http.Transport = providerTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid input reached provider")
			return nil, ErrProviderResponse
		})
		for _, tc := range []struct{ code, verifier, nonce string }{
			{"", strings.Repeat("v", 43), strings.Repeat("n", 43)},
			{strings.Repeat("c", 4097), strings.Repeat("v", 43), strings.Repeat("n", 43)},
			{"code\r\nsecret", strings.Repeat("v", 43), strings.Repeat("n", 43)},
			{"code", "short", strings.Repeat("n", 43)},
			{"code", strings.Repeat("v", 129), strings.Repeat("n", 43)},
			{"code", strings.Repeat("v", 42) + "!", strings.Repeat("n", 43)},
		} {
			if _, err := a.Exchange(context.Background(), tc.code, tc.verifier, tc.nonce); !errors.Is(err, ErrProviderResponse) {
				t.Fatalf("got %v", err)
			}
		}
		if p != GitHub {
			if _, err := a.Exchange(context.Background(), "code", strings.Repeat("v", 43), ""); err == nil {
				t.Fatal("empty nonce accepted")
			}
		}
		raw, err := json.Marshal(a.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "TEST_SECRET") || strings.Contains(string(raw), "provider-secret") {
			t.Fatal("secret reference serialized")
		}
	}
}

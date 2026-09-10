package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/signing"
)

func testKeys(t *testing.T, keys ...*signing.Key) *signing.Cache {
	t.Helper()

	set, err := signing.NewKeySet(keys)
	if err != nil {
		t.Fatalf("key set: %v", err)
	}
	return signing.NewCache(func() (*signing.KeySet, error) { return set, nil }, time.Minute)
}

func currentKey(t *testing.T) *signing.Key {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	set, err := signing.NewKeySet([]*signing.Key{{
		KID:       pair.KID,
		Algorithm: signing.RS256,
		Status:    signing.StatusCurrent,
		Public:    pair.Public,
	}})
	if err != nil {
		t.Fatalf("key set: %v", err)
	}
	return set.Verifying()[0]
}

func baseCapabilities() Capabilities {
	return Capabilities{
		Issuer:            "https://auth.example",
		JWKSURI:           "https://auth.example/.well-known/jwks.json",
		SigningAlgorithms: []string{"RS256", "ES256"},
	}
}

// fetchDiscovery drives the generated handler and returns what a client sees.
//
// Through the generated response type rather than around it: the Content-Type
// and Cache-Control come from the contract, so a test that wrote them itself
// would be asserting on its own behaviour.
func fetchDiscovery(t *testing.T, h *Handler) (*http.Response, map[string]any) {
	t.Helper()

	resp, err := h.GetOpenIDConfiguration(context.Background(), api.GetOpenIDConfigurationRequestObject{})
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}

	rec := httptest.NewRecorder()
	if err := resp.VisitGetOpenIDConfigurationResponse(rec); err != nil {
		t.Fatalf("writing discovery response: %v", err)
	}
	return decode(t, rec)
}

func fetchJWKS(t *testing.T, h *Handler) (*http.Response, map[string]any) {
	t.Helper()

	resp, err := h.GetJWKS(context.Background(), api.GetJWKSRequestObject{})
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}

	rec := httptest.NewRecorder()
	if err := resp.VisitGetJWKSResponse(rec); err != nil {
		t.Fatalf("writing jwks response: %v", err)
	}
	return decode(t, rec)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) (*http.Response, map[string]any) {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v\nbody: %s", err, rec.Body.String())
	}
	return rec.Result(), body
}

// --- the governance rule ---------------------------------------------------

// P1-04 step 2, and the reason this package derives the document rather than
// writing it out: an endpoint that is advertised and absent makes a client
// configure successfully and fail at its first login. That failure surfaces in
// the consumer's logs, not ours.
func TestUnimplementedEndpointsAreAbsentEntirely(t *testing.T) {
	h, err := NewHandler(baseCapabilities(), testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	_, body := fetchDiscovery(t, h)

	// Phase 0/1-03: neither endpoint exists yet.
	for _, absent := range []string{
		"authorization_endpoint", "token_endpoint", "userinfo_endpoint",
		"revocation_endpoint", "introspection_endpoint", "end_session_endpoint",
	} {
		if _, present := body[absent]; present {
			t.Errorf("%s is advertised but not implemented; a client would fail at runtime", absent)
		}
	}

	// What IS true is present.
	if body["issuer"] != "https://auth.example" {
		t.Errorf("issuer = %v", body["issuer"])
	}
	if body["jwks_uri"] == nil {
		t.Error("jwks_uri must always be present — without it a client cannot verify a token")
	}
}

// And they appear once they exist, without the document being edited.
func TestEndpointsAppearWhenImplemented(t *testing.T) {
	caps := baseCapabilities()
	caps.AuthorizationEndpoint = "https://auth.example/oauth/authorize"
	caps.TokenEndpoint = "https://auth.example/oauth/token"
	caps.UserInfoEndpoint = "https://auth.example/oauth/userinfo"
	caps.GrantTypes = []string{"authorization_code", "refresh_token", "client_credentials"}
	caps.ResponseTypes = []string{"code"}

	h, err := NewHandler(caps, testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	_, body := fetchDiscovery(t, h)

	if body["authorization_endpoint"] != caps.AuthorizationEndpoint {
		t.Errorf("authorization_endpoint = %v", body["authorization_endpoint"])
	}
	if body["token_endpoint"] != caps.TokenEndpoint {
		t.Errorf("token_endpoint = %v", body["token_endpoint"])
	}
	// P1-08. With this the document names all four endpoints a conforming
	// client configures itself from, which is the point at which discovery
	// stops being a partial description of the service.
	if body["userinfo_endpoint"] != caps.UserInfoEndpoint {
		t.Errorf("userinfo_endpoint = %v", body["userinfo_endpoint"])
	}
}

// --- PKCE ------------------------------------------------------------------

// P1-04's Definition of Done: S256 and never `plain`.
//
// `plain` is in the PKCE specification and transmits the verifier unprotected,
// so an attacker who can observe the authorization request can complete the
// exchange — which removes the entire reason PKCE exists. Advertising it
// invites a client library to negotiate down to it.
func TestPKCEAdvertisesS256AndNeverPlain(t *testing.T) {
	caps := baseCapabilities()
	caps.AuthorizationEndpoint = "https://auth.example/oauth/authorize"

	h, err := NewHandler(caps, testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	_, body := fetchDiscovery(t, h)

	methods, ok := body["code_challenge_methods_supported"].([]any)
	if !ok {
		t.Fatal("code_challenge_methods_supported is missing once an authorization endpoint exists")
	}

	var found []string
	for _, m := range methods {
		found = append(found, m.(string))
	}

	if len(found) != 1 || found[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want exactly [S256]", found)
	}
	for _, m := range found {
		if m == "plain" {
			t.Error("SECURITY: `plain` PKCE is advertised; it transmits the verifier unprotected")
		}
	}
}

// PKCE is meaningless without an authorization endpoint to apply it to.
func TestPKCEIsNotAdvertisedWithoutAnAuthorizationEndpoint(t *testing.T) {
	h, err := NewHandler(baseCapabilities(), testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	_, body := fetchDiscovery(t, h)

	if _, present := body["code_challenge_methods_supported"]; present {
		t.Error("PKCE methods advertised with no authorization endpoint to use them on")
	}
}

// --- forbidden grants ------------------------------------------------------

// docs/PLAN/05 rules both out permanently. Advertising a grant this service refuses
// invites a client to build against it and discover the refusal in production.
func TestForbiddenGrantsAreRejectedAtConstruction(t *testing.T) {
	for _, grant := range []string{"implicit", "password"} {
		t.Run(grant, func(t *testing.T) {
			caps := baseCapabilities()
			caps.GrantTypes = []string{"authorization_code", grant}

			_, err := NewHandler(caps, testKeys(t, currentKey(t)))
			if err == nil {
				t.Fatalf("SECURITY: grant type %q was accepted; docs/PLAN/05 rules it out permanently", grant)
			}
			if !strings.Contains(err.Error(), grant) {
				t.Errorf("the error should name the grant, got: %v", err)
			}
		})
	}
}

// --- issuer ----------------------------------------------------------------

// A trailing slash makes it a different string, and clients compare the `iss`
// claim byte for byte. This is the classic OIDC misconfiguration: every
// conforming library rejects every token, and the error appears in the
// client rather than here.
func TestIssuerWithATrailingSlashIsRejected(t *testing.T) {
	caps := baseCapabilities()
	caps.Issuer = "https://auth.example/"

	if _, err := NewHandler(caps, testKeys(t, currentKey(t))); err == nil {
		t.Fatal("an issuer with a trailing slash must be refused")
	}
}

func TestIssuerAndJWKSURIAreRequired(t *testing.T) {
	t.Run("no issuer", func(t *testing.T) {
		caps := baseCapabilities()
		caps.Issuer = ""
		if _, err := NewHandler(caps, testKeys(t, currentKey(t))); err == nil {
			t.Fatal("an empty issuer must be refused")
		}
	})

	t.Run("no jwks_uri", func(t *testing.T) {
		caps := baseCapabilities()
		caps.JWKSURI = ""
		if _, err := NewHandler(caps, testKeys(t, currentKey(t))); err == nil {
			t.Fatal("a missing jwks_uri must be refused — a client could not verify anything")
		}
	})
}

// --- JWKS ------------------------------------------------------------------

// P1-04's Definition of Done, inspected rather than assumed: the response must
// contain no private key parameters. This is the single most public document
// this service produces.
func TestJWKSExposesNoPrivateParameters(t *testing.T) {
	h, err := NewHandler(baseCapabilities(), testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	resp, err := h.GetJWKS(context.Background(), api.GetJWKSRequestObject{})
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitGetJWKSResponse(rec); err != nil {
		t.Fatalf("writing jwks: %v", err)
	}

	var parsed struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decoding JWKS: %v", err)
	}
	if len(parsed.Keys) == 0 {
		t.Fatal("JWKS is empty; the test would pass vacuously")
	}

	// RFC 7517: `d` is the RSA/EC private exponent; `p`, `q`, `dp`, `dq`, `qi`
	// are the RSA CRT parameters. Any of them means the private key shipped.
	private := []string{"d", "p", "q", "dp", "dq", "qi"}

	for _, key := range parsed.Keys {
		for _, param := range private {
			if _, present := key[param]; present {
				t.Errorf("SECURITY: JWKS key %v contains the private parameter %q", key["kid"], param)
			}
		}
		if key["kid"] == nil {
			t.Error("a published key has no kid; a verifier could not select it")
		}
	}
}

// The cache headers are the trade P1-04 step 4 names.
func TestCacheHeadersAreSetAndJWKSIsShorterThanDiscovery(t *testing.T) {
	h, err := NewHandler(baseCapabilities(), testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	discovery, _ := fetchDiscovery(t, h)
	jwks, _ := fetchJWKS(t, h)

	for name, res := range map[string]*http.Response{"discovery": discovery, "jwks": jwks} {
		if cc := res.Header.Get("Cache-Control"); cc == "" {
			t.Errorf("%s has no Cache-Control; every token verification would become a request", name)
		}
	}

	// The key set must go stale sooner than the endpoint list, because a
	// rotation has to propagate inside the overlap window while an endpoint
	// list only changes on a deploy.
	if jwksMaxAge >= discoveryMaxAge {
		t.Errorf("jwks max-age (%s) must be shorter than discovery's (%s): a rotation "+
			"has to reach consumers within the overlap window", jwksMaxAge, discoveryMaxAge)
	}
}

// Content types matter to strict client libraries.
func TestContentTypes(t *testing.T) {
	h, err := NewHandler(baseCapabilities(), testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	discovery, _ := fetchDiscovery(t, h)
	if got := discovery.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("discovery Content-Type = %q", got)
	}

	jwks, _ := fetchJWKS(t, h)
	if got := jwks.Header.Get("Content-Type"); got != "application/jwk-set+json" {
		t.Errorf("jwks Content-Type = %q, want application/jwk-set+json", got)
	}
}

// An unavailable key set must not describe why to an anonymous caller.
func TestJWKSFailureRevealsNothing(t *testing.T) {
	failing := signing.NewCache(func() (*signing.KeySet, error) {
		return nil, errAlways
	}, time.Minute)

	h, err := NewHandler(baseCapabilities(), failing)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	resp, err := h.GetJWKS(context.Background(), api.GetJWKSRequestObject{})
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitGetJWKSResponse(rec); err != nil {
		t.Fatalf("writing jwks: %v", err)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	for _, leak := range []string{"postgres", "database", "connection", "dial", "sql"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), leak) {
			t.Errorf("the failure response leaks infrastructure detail: %s", rec.Body.String())
		}
	}
}

type constError string

func (e constError) Error() string { return string(e) }

const errAlways = constError("dial tcp 10.0.4.2:5432: connect: connection refused")

// --- the document a client actually reads ----------------------------------

// The fields OIDC Discovery marks REQUIRED must always be present, even in a
// deployment serving almost nothing. A client library validates their
// presence before it looks at anything else.
func TestAlwaysPresentFields(t *testing.T) {
	h, err := NewHandler(baseCapabilities(), testKeys(t, currentKey(t)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	_, body := fetchDiscovery(t, h)

	for _, required := range []string{
		"issuer", "jwks_uri", "subject_types_supported",
		"id_token_signing_alg_values_supported",
	} {
		if _, present := body[required]; !present {
			t.Errorf("%s is missing; it is REQUIRED by OIDC Discovery", required)
		}
	}

	// Pairwise is a privacy feature this service does not implement, so
	// claiming it would be a statement about behaviour that does not exist.
	subjects := body["subject_types_supported"].([]any)
	if len(subjects) != 1 || subjects[0] != "public" {
		t.Errorf("subject_types_supported = %v, want [public]", subjects)
	}
}

package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Cross-origin policy (P1-29, closing PG-17).
//
// These test the header logic, which is the part that decides whether a
// browser hands a response to a page. What they cannot test is a browser, and
// that distinction is the reason this task exists at all: every test the login
// flow had drove it with curl, which has no same-origin policy, so a page that
// could not log anybody in passed all of them. The browser's half is
// console/e2e/sso.spec.ts.

// stubOrigins answers the two questions without a database.
//
// Named for what it stubs rather than for its interface: `stubChecker` is
// already taken in this package by a health checker, and two stubs sharing a
// name in one package is a compile error that reads like a mystery.
type stubOrigins struct {
	anyOrigins map[string]bool
	perApp     map[string]map[string]bool
}

func (s stubOrigins) AnyApplicationAllows(_ *http.Request, origin string) bool {
	return s.anyOrigins[origin]
}

func (s stubOrigins) ApplicationAllows(_ *http.Request, clientID, origin string) bool {
	return s.perApp[clientID][origin]
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"users":[]}`))
	})
}

// bearerFor builds a token whose payload names a client. Unsigned: the
// middleware reads this claim without verifying, deliberately — see allowed().
func bearerFor(t *testing.T, clientID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"client_id": clientID})
	if err != nil {
		t.Fatalf("encoding a payload: %v", err)
	}
	return "Bearer header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// --- the public set ---------------------------------------------------------

func TestThePublicEndpointsAnswerAnyOriginWithoutCredentials(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	request.Header.Set("Origin", "https://anything.example")

	PublicCORS(okHandler()).ServeHTTP(recorder, request)

	if got := recorder.Header().Get(headerACAO); got != "*" {
		t.Errorf("Access-Control-Allow-Origin is %q, want *", got)
	}

	// The half that makes `*` defensible. `*` with credentials is refused by
	// browsers anyway; saying "false" out loud is what stops somebody later
	// "fixing" that refusal by echoing the origin instead.
	if got := recorder.Header().Get(headerACAC); got == "true" {
		t.Error("a wildcard origin is permitted to send credentials")
	}
}

func TestAPreflightOnTheTokenEndpointIsAnsweredWithoutRunningTheHandler(t *testing.T) {
	ran := false
	handler := PublicCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ran = true }))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/oauth/token", nil)
	request.Header.Set("Origin", "https://app.example.com")
	request.Header.Set(headerACRM, "POST")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("a preflight answered %d, want 204", recorder.Code)
	}
	if ran {
		t.Error("the preflight reached the token endpoint, which would try to redeem a code that is not there")
	}
	if !strings.Contains(recorder.Header().Get(headerACAH), "Content-Type") {
		t.Errorf("the preflight does not permit Content-Type: %q", recorder.Header().Get(headerACAH))
	}
}

func TestASameOriginRequestGetsNoCorsHeadersAtAll(t *testing.T) {
	recorder := httptest.NewRecorder()
	// No Origin header: not a cross-origin request, and nothing to say about it.
	PublicCORS(okHandler()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

	if recorder.Header().Get(headerACAO) != "" {
		t.Error("a same-origin request was given CORS headers, which makes every cache entry vary for nothing")
	}
}

// --- the restricted set -----------------------------------------------------

func TestOnlyAnOriginTheApplicationRegisteredMayReadItsData(t *testing.T) {
	checker := stubOrigins{
		perApp: map[string]map[string]bool{
			"app-a": {"https://a.example.com": true},
			"app-b": {"https://b.example.com": true},
		},
	}
	middleware := RestrictedCORS(checker, nil)

	t.Run("its own origin", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
		request.Header.Set("Origin", "https://a.example.com")
		request.Header.Set("Authorization", bearerFor(t, "app-a"))

		middleware(okHandler()).ServeHTTP(recorder, request)

		if got := recorder.Header().Get(headerACAO); got != "https://a.example.com" {
			t.Errorf("Access-Control-Allow-Origin is %q", got)
		}
	})

	t.Run("another application's origin", func(t *testing.T) {
		// **PG-17's objection, as a test.** An origin registered by one
		// application must not read a response obtained with another's token,
		// and an instance-wide allowlist would let it.
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
		request.Header.Set("Origin", "https://b.example.com")
		request.Header.Set("Authorization", bearerFor(t, "app-a"))

		middleware(okHandler()).ServeHTTP(recorder, request)

		if got := recorder.Header().Get(headerACAO); got != "" {
			t.Errorf("another application's origin may read this response: %q", got)
		}
	})

	t.Run("never a wildcard", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
		request.Header.Set("Origin", "https://a.example.com")
		request.Header.Set("Authorization", bearerFor(t, "app-a"))

		middleware(okHandler()).ServeHTTP(recorder, request)

		if recorder.Header().Get(headerACAO) == "*" {
			t.Error("an endpoint that returns personal data answered a wildcard origin")
		}
		if recorder.Header().Get(headerACAC) == "true" {
			t.Error("credentials are permitted on an endpoint that reads no cookie")
		}
	})
}

// Forging the client_id is the obvious attack on an unverified claim, and it
// buys nothing: the origin still has to be registered by whichever application
// is named, so naming somebody else's only makes the check fail.
func TestForgingTheClientIdDoesNotWidenWhatAnOriginMayRead(t *testing.T) {
	checker := stubOrigins{
		perApp: map[string]map[string]bool{"victim": {"https://victim.example.com": true}},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
	request.Header.Set("Origin", "https://evil.example")
	// An attacker's page claiming to be the victim's application.
	request.Header.Set("Authorization", bearerFor(t, "victim"))

	RestrictedCORS(checker, nil)(okHandler()).ServeHTTP(recorder, request)

	if got := recorder.Header().Get(headerACAO); got != "" {
		t.Errorf("a forged client_id let %q read the response", got)
	}
}

func TestTheResponseVariesByOriginEvenWhenRefused(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
	request.Header.Set("Origin", "https://refused.example")
	request.Header.Set("Authorization", bearerFor(t, "app-a"))

	RestrictedCORS(stubOrigins{}, nil)(okHandler()).ServeHTTP(recorder, request)

	// Without this, a cache keyed on the URL alone serves an allowed origin's
	// headers to a refused one — which turns a correct policy into a policy
	// that holds until something puts a CDN in front of it.
	if !strings.Contains(recorder.Header().Get(headerVary), headerOrigin) {
		t.Errorf("Vary is %q; a refusal is origin-dependent too", recorder.Header().Get(headerVary))
	}
}

func TestARefusedPreflightIsIndistinguishableFromAnAllowedOneInItsStatus(t *testing.T) {
	middleware := RestrictedCORS(stubOrigins{
		anyOrigins: map[string]bool{"https://known.example": true},
	}, nil)

	statuses := map[string]int{}
	for _, origin := range []string{"https://known.example", "https://unknown.example"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodOptions, "/v1/organizations/x/users", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set(headerACRM, "GET")

		middleware(okHandler()).ServeHTTP(recorder, request)
		statuses[origin] = recorder.Code

		if origin == "https://known.example" && recorder.Header().Get(headerACAO) != origin {
			t.Errorf("a registered origin's preflight was not allowed")
		}
		if origin == "https://unknown.example" && recorder.Header().Get(headerACAO) != "" {
			t.Errorf("an unregistered origin's preflight was allowed")
		}
	}

	// Same status either way. A 403 for a refused preflight enumerates the
	// registered origins, one guess at a time.
	if statuses["https://known.example"] != statuses["https://unknown.example"] {
		t.Errorf("the status distinguishes a registered origin: %v", statuses)
	}
}

// The console has to be able to READ its own 401 to know its token expired. A
// 401 with no CORS headers is a fetch that rejects with no status at all, and
// the console cannot tell that from an unreachable service.
func TestAnUnauthenticatedRequestIsStillReadableByARegisteredOrigin(t *testing.T) {
	checker := stubOrigins{anyOrigins: map[string]bool{"https://console.example": true}}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
	request.Header.Set("Origin", "https://console.example")
	// No Authorization header at all — the request is about to be refused.

	RestrictedCORS(checker, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})).ServeHTTP(recorder, request)

	if got := recorder.Header().Get(headerACAO); got != "https://console.example" {
		t.Errorf("a registered origin cannot read its own 401: %q", got)
	}
}

// --- the split --------------------------------------------------------------

func TestTheWildcardReachesOnlyThePublicEndpoints(t *testing.T) {
	// The list this asserts is the whole security argument: every public path
	// must be one whose content is public AND which is never authenticated by
	// anything a browser attaches on its own.
	for path, wantPublic := range map[string]bool{
		"/oauth/token":                      true,
		"/.well-known/openid-configuration": true,
		"/.well-known/jwks.json":            true,
		"/v1/organizations":                 false,
		"/v1/organizations/x/users":         false,
		"/oauth/userinfo":                   false,
		"/oauth/authorize":                  false,
		"/login":                            false,
		"/healthz":                          false,
		// A path that merely starts with something public-looking must not be.
		"/oauth/tokens-of-other-people": false,
	} {
		if got := isPublicCORSPath(path); got != wantPublic {
			t.Errorf("isPublicCORSPath(%q) = %v, want %v", path, got, wantPublic)
		}
	}
}

func TestCORSAppliesTheRightPolicyPerPath(t *testing.T) {
	middleware := CORS(stubOrigins{
		perApp: map[string]map[string]bool{"app-a": {"https://a.example.com": true}},
	}, nil)

	t.Run("public", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
		request.Header.Set("Origin", "https://anything.example")

		middleware(okHandler()).ServeHTTP(recorder, request)

		if got := recorder.Header().Get(headerACAO); got != "*" {
			t.Errorf("the key set is not readable by any origin: %q", got)
		}
	})

	t.Run("restricted", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/organizations/x/users", nil)
		request.Header.Set("Origin", "https://anything.example")
		request.Header.Set("Authorization", bearerFor(t, "app-a"))

		middleware(okHandler()).ServeHTTP(recorder, request)

		if got := recorder.Header().Get(headerACAO); got != "" {
			t.Errorf("an unregistered origin may read user data: %q", got)
		}
	})
}

func TestAMalformedBearerYieldsNoClientId(t *testing.T) {
	for _, header := range []string{
		"",
		"Bearer",
		"Bearer ",
		"Basic abc",
		"Bearer not.a.jwt",
		"Bearer a.b",
		"Bearer a.b.c.d",
		"Bearer header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig",
	} {
		if got := clientIDFromBearer(header); got != "" {
			t.Errorf("clientIDFromBearer(%q) = %q, want empty", header, got)
		}
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/demo/internal/testissuer"
	"github.com/zed378/zed-auth/demo/internal/verify"
)

// The SPA's API is where this application's authorization decision is actually
// made — the browser's copy of the token decides only what to draw. So these
// tests go at the API, never at the page.

const (
	thisApp  = "demo-spa"
	otherApp = "demo-webapp"
)

func newSPA(t *testing.T) (*spa, *testissuer.Issuer) {
	t.Helper()
	issuer := testissuer.New(t)
	return &spa{verifier: verify.New(issuer.URL(), thisApp)}, issuer
}

func call(t *testing.T, app *spa, token, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if nonce != "" {
		request.Header.Set(nonceHeader, nonce)
	}
	recorder := httptest.NewRecorder()
	app.me(recorder, request)
	return recorder
}

func TestAValidTokenGetsTheSession(t *testing.T) {
	app, issuer := newSPA(t)
	token := issuer.IDToken(t, thisApp, "nonce-1")

	response := call(t, app, token, "nonce-1")
	if response.Code != http.StatusOK {
		t.Fatalf("a valid token was refused: %d %s", response.Code, response.Body)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	if body["subject"] != "user-"+thisApp {
		t.Errorf("subject came back as %v", body["subject"])
	}

	// An authorization decision must never be cached. A shared cache holding
	// this response would hand one user's session to the next request.
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want no-store", got)
	}
}

// The definition of done, at the HTTP layer: App A's token, presented to App B.
func TestATokenForTheOtherApplicationIsRefused(t *testing.T) {
	app, issuer := newSPA(t)

	// A genuine token: the same issuer, the same signing key, the same user,
	// still in date. The only thing wrong with it is who it is addressed to.
	token := issuer.IDToken(t, otherApp, "nonce-1")

	response := call(t, app, token, "nonce-1")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("a token for %s was accepted by %s: %d", otherApp, thisApp, response.Code)
	}
	if !strings.Contains(response.Body.String(), "audience") {
		t.Errorf("the refusal should say why; it said %s", response.Body)
	}
	if got := response.Header().Get("WWW-Authenticate"); !strings.Contains(got, "invalid_token") {
		t.Errorf("WWW-Authenticate is %q", got)
	}

	// And the refusal must not echo the credential back. A resource server
	// that quotes a rejected token puts it in the browser's console, its own
	// access log, and whatever aggregates them.
	if strings.Contains(response.Body.String(), token) {
		t.Error("the refusal echoed the token back")
	}
}

func TestATokenWithoutTheNonceThisTabAskedForIsRefused(t *testing.T) {
	app, issuer := newSPA(t)
	token := issuer.IDToken(t, thisApp, "the-nonce-from-an-earlier-sign-in")

	// Correct audience, correct issuer, in date, correctly signed — and minted
	// for a different sign-in. This is the replay the nonce exists to stop.
	response := call(t, app, token, "the-nonce-this-tab-asked-for")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("a replayed token was accepted: %d %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "not minted for this sign-in") {
		t.Errorf("the refusal should name the reason; it said %s", response.Body)
	}
}

func TestATokenPresentedWithNoExpectedNonceIsRefused(t *testing.T) {
	app, issuer := newSPA(t)

	t.Run("a token that has a nonce", func(t *testing.T) {
		// A stolen token, presented on its own. Without the value the tab
		// asked for, this API cannot tell it from a replay.
		if got := call(t, app, issuer.IDToken(t, thisApp, "nonce-1"), "").Code; got != http.StatusUnauthorized {
			t.Fatalf("a token with no expected nonce was accepted: %d", got)
		}
	})

	t.Run("a token that has no nonce at all", func(t *testing.T) {
		// **This is the case the empty-header check exists for.** With a token
		// whose nonce claim is absent, comparing it against an absent header
		// compares "" with "" — which matches. So without the explicit
		// refusal, a token minted by a client that never sent a nonce is
		// accepted by anyone who presents it with no nonce header, which is
		// every attacker holding a stolen one.
		if got := call(t, app, issuer.IDToken(t, thisApp, ""), "").Code; got != http.StatusUnauthorized {
			t.Fatalf("a nonce-less token was accepted with no nonce header: %d", got)
		}
	})
}

func TestNoCredentialAtAllIsAChallengeRatherThanAnError(t *testing.T) {
	app, _ := newSPA(t)

	response := call(t, app, "", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous request was answered %d", response.Code)
	}

	// RFC 6750 draws a line the checks below it cannot: no credential gets a
	// plain challenge, a BAD credential gets `error="invalid_token"`. Both are
	// 401, so the status code proves nothing here — and a client told its
	// token is invalid when it never sent one discards a good session and
	// makes the user sign in again.
	got := response.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(got, "Bearer") {
		t.Errorf("no bearer challenge; WWW-Authenticate is %q", got)
	}
	if strings.Contains(got, "error=") {
		t.Errorf("an absent credential was reported as an invalid one: %q", got)
	}
}

func TestAGarbageTokenIsRefusedRatherThanCrashing(t *testing.T) {
	app, _ := newSPA(t)

	for _, token := range []string{"...", "a.b.c", "Bearer", strings.Repeat("x", 8192)} {
		response := call(t, app, token, "nonce-1")
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%q was answered %d", token, response.Code)
		}
	}
}

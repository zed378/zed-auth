package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/demo/internal/testissuer"
	"github.com/zed378/zed-auth/demo/internal/verify"
)

// These tests drive the application the way a browser does — /login, follow
// the redirect's parameters, /callback — because every defect worth having in
// a confidential client lives in the handoff between those two requests, not
// inside either one.

const (
	thisApp  = "demo-webapp"
	otherApp = "demo-spa"
	baseURL  = "http://demo-webapp.test"
)

func newApp(t *testing.T) (*webapp, *testissuer.Issuer) {
	t.Helper()
	issuer := testissuer.New(t)
	return &webapp{
		cfg: config{
			issuer:       issuer.URL(),
			clientID:     thisApp,
			clientSecret: "a-secret-the-browser-never-sees",
			baseURL:      baseURL,
		},
		verifier: verify.New(issuer.URL(), thisApp),
		sessions: map[string]session{},
		pending:  map[string]pending{},
	}, issuer
}

// begin runs /login and returns the authorization request the user is sent to.
func begin(t *testing.T, app *webapp) url.Values {
	t.Helper()
	recorder := httptest.NewRecorder()
	app.login(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("/login answered %d, want a redirect", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("the redirect is not a URL: %v", err)
	}
	return location.Query()
}

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func callback(t *testing.T, app *webapp, query url.Values) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	app.callback(recorder, httptest.NewRequest(http.MethodGet, "/callback?"+query.Encode(), nil))
	return recorder
}

func sessionCookie(response *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == cookieName && cookie.Value != "" {
			return cookie
		}
	}
	return nil
}

func TestAFullSignInRoundTrip(t *testing.T) {
	app, issuer := newApp(t)

	request := begin(t, app)
	if request.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method is %q; a plain challenge IS the verifier",
			request.Get("code_challenge_method"))
	}
	if request.Get("client_id") != thisApp || request.Get("redirect_uri") != baseURL+"/callback" {
		t.Errorf("the authorization request is addressed wrongly: %v", request)
	}

	// The token endpoint is where the two halves of PKCE meet. Checking them
	// here is what makes the challenge above more than decoration: a client
	// that generated a challenge and then sent an unrelated verifier would
	// still pass every assertion made before this point.
	var sawBasicAuth bool
	issuer.Exchange = func(form url.Values, authorization string) string {
		if got := challengeFor(form.Get("code_verifier")); got != request.Get("code_challenge") {
			t.Errorf("the verifier does not hash to the challenge: %q vs %q",
				got, request.Get("code_challenge"))
			return ""
		}
		if form.Get("grant_type") != "authorization_code" || form.Get("code") != "the-code" {
			t.Errorf("the exchange posted %v", form)
			return ""
		}
		// Client authentication in the header, and the secret NOT in the body:
		// a secret in a form body is a secret in more logs.
		user, password, ok := parseBasic(authorization)
		sawBasicAuth = ok && user == url.QueryEscape(thisApp) && password != ""
		if form.Get("client_secret") != "" {
			t.Error("the client secret was posted in the form body")
		}
		return issuer.IDToken(t, thisApp, request.Get("nonce"))
	}

	response := callback(t, app, url.Values{
		"state": {request.Get("state")},
		"code":  {"the-code"},
	})

	if response.Code != http.StatusFound {
		t.Fatalf("the callback answered %d: %s", response.Code, response.Body)
	}
	if !sawBasicAuth {
		t.Error("the token request carried no client authentication in its header")
	}

	cookie := sessionCookie(response)
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	// HttpOnly is the whole point of this profile: the browser holds a
	// reference to a session, never a token, and no script on these pages can
	// read even the reference.
	if !cookie.HttpOnly {
		t.Error("the session cookie is readable by script")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite is %v", cookie.SameSite)
	}
	if strings.Contains(response.Body.String(), "eyJ") {
		t.Error("a token appeared in the callback's response body")
	}

	// And the session works: the home page renders the subject without any
	// further contact with the issuer.
	home := httptest.NewRecorder()
	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(cookie)
	app.home(home, homeRequest)

	if !strings.Contains(home.Body.String(), "user-"+thisApp) {
		t.Errorf("the home page does not show the signed-in user: %s", home.Body)
	}
}

func TestACallbackForASignInThatDidNotStartHereIsRefused(t *testing.T) {
	app, _ := newApp(t)

	// No /login first: a state this application never issued, arriving with
	// somebody else's code. That is login CSRF — the victim ends up signed in
	// as the attacker, and everything they do next is in the attacker's
	// account.
	response := callback(t, app, url.Values{"state": {"a-state-we-never-issued"}, "code": {"c"}})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("an unknown state was answered %d", response.Code)
	}
	if sessionCookie(response) != nil {
		t.Error("a session was created for a sign-in that did not start here")
	}
}

func TestAPendingSignInIsConsumedOnUse(t *testing.T) {
	app, issuer := newApp(t)
	request := begin(t, app)
	issuer.Exchange = func(_ url.Values, _ string) string {
		return issuer.IDToken(t, thisApp, request.Get("nonce"))
	}

	query := url.Values{"state": {request.Get("state")}, "code": {"the-code"}}
	if first := callback(t, app, query); first.Code != http.StatusFound {
		t.Fatalf("the first callback answered %d", first.Code)
	}

	// The same callback again. A pending request left in place is one an
	// attacker who captured the URL can complete a second time.
	second := callback(t, app, query)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("the callback was replayable: %d", second.Code)
	}
}

func TestAnExpiredSignInAttemptIsRefused(t *testing.T) {
	app, issuer := newApp(t)
	request := begin(t, app)
	issuer.Exchange = func(_ url.Values, _ string) string {
		return issuer.IDToken(t, thisApp, request.Get("nonce"))
	}

	// Backdate the pending request past its window. An authorization request
	// that stays valid indefinitely is a code-interception window that never
	// closes.
	state := request.Get("state")
	app.mu.Lock()
	waiting := app.pending[state]
	waiting.created = time.Now().Add(-11 * time.Minute)
	app.pending[state] = waiting
	app.mu.Unlock()

	response := callback(t, app, url.Values{"state": {state}, "code": {"the-code"}})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("a stale sign-in was answered %d", response.Code)
	}
}

// The definition of done, from the confidential client's side.
func TestATokenMintedForTheOtherApplicationEndsTheSignIn(t *testing.T) {
	app, issuer := newApp(t)
	request := begin(t, app)

	// The issuer answers with a perfectly genuine token — for the SPA. A
	// consumer that trusted the token because it came back over TLS from the
	// endpoint it just called would sign this user in.
	issuer.Exchange = func(_ url.Values, _ string) string {
		return issuer.IDToken(t, otherApp, request.Get("nonce"))
	}

	response := callback(t, app, url.Values{"state": {request.Get("state")}, "code": {"the-code"}})
	if response.Code == http.StatusFound {
		t.Fatal("a token for the other application signed the user in here")
	}
	if sessionCookie(response) != nil {
		t.Error("a session was created from a token addressed elsewhere")
	}
}

func TestATokenMintedForADifferentSignInEndsTheSignIn(t *testing.T) {
	app, issuer := newApp(t)
	request := begin(t, app)

	// Right audience, right issuer, in date, correctly signed — and carrying
	// the nonce of some earlier sign-in. Without the nonce check this is a
	// replay that works.
	issuer.Exchange = func(_ url.Values, _ string) string {
		return issuer.IDToken(t, thisApp, "a-nonce-from-an-earlier-sign-in")
	}

	response := callback(t, app, url.Values{"state": {request.Get("state")}, "code": {"the-code"}})
	if response.Code == http.StatusFound {
		t.Fatal("a replayed ID token signed the user in")
	}
	if sessionCookie(response) != nil {
		t.Error("a session was created from a replayed token")
	}
}

func TestSigningOutEndsBothThisSessionAndTheSSOSession(t *testing.T) {
	app, issuer := newApp(t)
	request := begin(t, app)
	issuer.Exchange = func(_ url.Values, _ string) string {
		return issuer.IDToken(t, thisApp, request.Get("nonce"))
	}
	cookie := sessionCookie(callback(t, app, url.Values{
		"state": {request.Get("state")}, "code": {"the-code"},
	}))
	if cookie == nil {
		t.Fatal("no session to sign out of")
	}

	recorder := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodGet, "/logout", nil)
	logoutRequest.AddCookie(cookie)
	app.logout(recorder, logoutRequest)

	// The local session is gone…
	app.mu.Lock()
	_, still := app.sessions[cookie.Value]
	app.mu.Unlock()
	if still {
		t.Error("the server-side session survived the sign-out")
	}

	// …and the user is sent on to end the SSO session. A logout that leaves
	// the identity provider's session alive means the next click on "sign in"
	// signs straight back in, which reads as a broken logout.
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, issuer.URL()+"/oidc/logout") {
		t.Errorf("sign-out redirected to %q", location)
	}

	// And the old cookie no longer opens anything.
	home := httptest.NewRecorder()
	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(cookie)
	app.home(home, homeRequest)
	if strings.Contains(home.Body.String(), "You are signed in") {
		t.Error("the signed-out cookie still opens a session")
	}
}

func TestTheHomePageShowsNothingWithoutASession(t *testing.T) {
	app, _ := newApp(t)

	recorder := httptest.NewRecorder()
	app.home(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(recorder.Body.String(), "You are not signed in") {
		t.Errorf("an anonymous visitor saw: %s", recorder.Body)
	}
}

// parseBasic reads an Authorization: Basic header.
func parseBasic(header string) (string, string, bool) {
	raw, ok := strings.CutPrefix(header, "Basic ")
	if !ok {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", "", false
	}
	user, password, ok := strings.Cut(string(decoded), ":")
	return user, password, ok
}

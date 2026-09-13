package login

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// The passkey step's page and policy (P3-05).
//
// `P1-12` made "no script, ever" a load-bearing property of the hosted login
// flow, and WebAuthn cannot work without script. The resolution — recorded as
// `PG-40` — is that the relaxation is scoped to the one page that needs it, and
// these are the tests that hold that line. If any of them go red, the
// containment has leaked.

const testOptions = `{"publicKey":{"challenge":"abc","allowCredentials":[]}}`

// postAssertion submits to the passkey step with a CSRF token the caller
// controls, so two responses can be compared byte for byte.
func (f *fixture) postAssertion(t *testing.T, assertion, handle, csrf string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{
		"assertion": {assertion},
		"request":   {testPendingID},
		csrfField:   {csrf},
	}

	r := httptest.NewRequest(http.MethodPost, WebAuthnPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, csrf)
	if handle != "" {
		r.AddCookie(&http.Cookie{Name: ChallengeCookieName, Value: handle})
	}

	w := httptest.NewRecorder()
	f.handler.WebAuthnStep(w, r)
	return w
}

// --- the containment, which is the whole trade -------------------------------------

// The PASSWORD page still has no script, and its policy still says so.
//
// This is the property `PG-40` promises. Nothing about the passkey step may
// reach the page where a password is typed.
func TestThePasswordPageStillHasNoScript(t *testing.T) {
	f := newFixture(t)

	w, _ := f.get(t)
	body := w.Body.String()

	if strings.Contains(body, "<script") {
		t.Errorf("the password page carries a script:\n%s", body)
	}

	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "script-src") {
		t.Errorf("the password page's policy names a script source: %q", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("the password page lost default-src 'none': %q", csp)
	}
}

// A challenge offering only TOTP also has no script, so the relaxation is
// scoped to the REQUEST rather than to the route.
func TestATOTPOnlyChallengeHasNoScript(t *testing.T) {
	f, _ := challenged(t)

	w := f.post(t, credentials(), "")
	body := w.Body.String()

	if strings.Contains(body, "<script") {
		t.Errorf("a TOTP-only challenge carries a script:\n%s", body)
	}
	if csp := w.Header().Get("Content-Security-Policy"); strings.Contains(csp, "script-src") {
		t.Errorf("a TOTP-only challenge names a script source: %q", csp)
	}
}

// A challenge offering a passkey carries the script, and its policy pins the
// script BY HASH.
func TestAPasskeyChallengePinsItsScriptByHash(t *testing.T) {
	f, c := challenged(t)
	c.decision.Offered = []mfa.Type{mfa.TypeWebAuthn}
	c.decision.WebAuthnOptions = []byte(testOptions)

	w := f.post(t, credentials(), "")
	csp := w.Header().Get("Content-Security-Policy")

	if !strings.Contains(csp, "script-src 'sha256-") {
		t.Fatalf("the passkey page does not pin its script by hash: %q", csp)
	}
	if strings.Contains(csp, "nonce-") {
		t.Errorf("the policy uses a nonce, which authorises whatever the server put it on: %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("the policy allows unsafe-inline, which authorises everything: %q", csp)
	}
	// The script cannot talk anywhere. `connect-src` is absent, so `default-src
	// 'none'` still governs it.
	if strings.Contains(csp, "connect-src") {
		t.Errorf("the policy opens connect-src: %q", csp)
	}
}

// The pinned hash matches the script the page actually carries.
//
// A hash written by hand is one somebody forgets to update, and the symptom
// would be a button that silently does nothing in every browser.
func TestThePinnedHashMatchesTheScriptOnThePage(t *testing.T) {
	f, c := challenged(t)
	c.decision.Offered = []mfa.Type{mfa.TypeWebAuthn}
	c.decision.WebAuthnOptions = []byte(testOptions)

	w := f.post(t, credentials(), "")
	body := w.Body.String()
	csp := w.Header().Get("Content-Security-Policy")

	if !strings.Contains(body, "navigator.credentials.get") {
		t.Fatalf("the page carries no passkey script:\n%s", body)
	}
	if !strings.Contains(csp, passkeyScriptHash) {
		t.Errorf("the policy pins %q, which is not the script's hash", csp)
	}
	// And the hash is derived, not typed: recomputing it from the source agrees.
	if got := scriptHash(passkeyScript); got != passkeyScriptHash {
		t.Errorf("the recorded hash %q does not match the script's %q", passkeyScriptHash, got)
	}
}

// --- what the page discloses ----------------------------------------------------------

// The options carry no identity.
//
// This page is reachable with only a password, so what it renders must not
// answer questions about the account. The ceremony needs a challenge and a list
// of credential ids; it does not need a name, an address, or a count.
func TestThePasskeyOptionsCarryNoIdentity(t *testing.T) {
	f, c := challenged(t)
	c.decision.Offered = []mfa.Type{mfa.TypeWebAuthn}
	c.decision.WebAuthnOptions = []byte(testOptions)

	body := f.post(t, credentials(), "").Body.String()

	for _, leak := range []string{"someone@example.test", "user_id", "display_name", "webauthn_id"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
			t.Errorf("the passkey page discloses %q:\n%s", leak, body)
		}
	}
}

// --- graceful degradation (card step 8) -------------------------------------------------

// The passkey button starts hidden and the unsupported message is present but
// hidden, so a browser without WebAuthn reveals the explanation rather than an
// enabled button that cannot work.
func TestThePasskeyButtonStartsHidden(t *testing.T) {
	f, c := challenged(t)
	c.decision.Offered = []mfa.Type{mfa.TypeWebAuthn}
	c.decision.WebAuthnOptions = []byte(testOptions)

	body := f.post(t, credentials(), "").Body.String()

	if !strings.Contains(body, `id="passkey-button" hidden`) {
		t.Errorf("the passkey button is not hidden until script confirms support:\n%s", body)
	}
	if !strings.Contains(body, MsgPasskeyUnsupported) {
		t.Errorf("the page has no explanation for a browser that cannot use a passkey:\n%s", body)
	}
	// The script reveals one or the other. Both paths are in it.
	if !strings.Contains(passkeyScript, "window.PublicKeyCredential") {
		t.Error("the script does not check for WebAuthn support before revealing the button")
	}
}

// A user with both a passkey and TOTP is offered both, so an unsupported
// browser is not a dead end.
func TestAPasskeyDoesNotHideTheTOTPForm(t *testing.T) {
	f, c := challenged(t)
	c.decision.Offered = []mfa.Type{mfa.TypeWebAuthn, mfa.TypeTOTP}
	c.decision.WebAuthnOptions = []byte(testOptions)

	body := f.post(t, credentials(), "").Body.String()

	if !strings.Contains(body, `name="code"`) {
		t.Errorf("the TOTP form is gone when a passkey is offered:\n%s", body)
	}
	if !strings.Contains(body, `id="passkey-form"`) {
		t.Errorf("the passkey form is missing:\n%s", body)
	}
}

// --- the route ---------------------------------------------------------------------------

// The passkey step is POST only. A GET would be a second place a passkey step
// could appear to exist.
func TestTheWebAuthnStepIsPostOnly(t *testing.T) {
	f, _ := challenged(t)

	r := httptest.NewRequest(http.MethodGet, WebAuthnPath+"?request="+testPendingID, nil)
	w := httptest.NewRecorder()
	f.handler.WebAuthnStep(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Errorf("Allow = %q, want POST", got)
	}
}

// An assertion is routed to AnswerWebAuthn, with the pending request bound to
// it — not to AnswerType with a made-up factor kind.
func TestAnAssertionReachesTheCeremony(t *testing.T) {
	f, c := challenged(t)
	_, csrf := f.get(t)

	w := f.postAssertion(t, `{"id":"x"}`, testHandle, csrf)
	_ = w

	if c.passkeyCalls != 1 {
		t.Fatalf("AnswerWebAuthn was called %d times, want 1", c.passkeyCalls)
	}
	if c.seenPending != testPendingID {
		t.Errorf("the ceremony was given pending %q, want %q", c.seenPending, testPendingID)
	}
	if string(c.seenAssertion) != `{"id":"x"}` {
		t.Errorf("the ceremony was given assertion %q", c.seenAssertion)
	}
	if c.calls != 0 {
		t.Error("the assertion also reached AnswerType")
	}
}

// A refused assertion says what every other failure says.
//
// An origin mismatch is a phishing attempt and a counter regression is a
// possible clone; both are in the log. Neither is in the response, because
// telling a caller which of their attacks was detected helps only them.
func TestEveryPasskeyRefusalLooksTheSame(t *testing.T) {
	bodies := map[string]string{}

	for name, failure := range map[string]error{
		"wrong":    mfa.ErrWrongCode,
		"origin":   mfa.ErrOriginMismatch,
		"cloned":   mfa.ErrClonedAuthenticator,
		"no such":  mfa.ErrNoSuchFactor,
		"disabled": mfa.ErrUnsupported,
	} {
		f, c := challenged(t)
		c.peek = []mfa.Type{mfa.TypeWebAuthn}
		c.peekOptions = []byte(testOptions)
		c.passkeyErr = failure

		// One token across every case, so the CSRF value is not the difference.
		bodies[name] = f.postAssertion(t, "x", testHandle, sharedCSRF(t, f)).Body.String()
	}

	var first, firstName string
	for name, body := range bodies {
		if first == "" {
			first, firstName = body, name
			continue
		}
		if body != first {
			t.Errorf("the %q refusal differs from the %q one; a prober can tell them apart",
				name, firstName)
		}
	}
}

// sharedCSRF issues one token for a fixture, so responses compared byte for
// byte differ only in what the test is about.
func sharedCSRF(t *testing.T, f *fixture) string {
	t.Helper()
	_, token := f.get(t)
	return token
}

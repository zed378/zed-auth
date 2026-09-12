package login

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// The challenge page's GET path, and the dead ends (P3-03).
//
// `showChallengeAgain` exists for a refresh and a back button — the two things
// every user does and no happy-path test exercises. It must not consume the
// challenge, must not cost an attempt, and must not render a form when there is
// nothing behind it.

// getChallenge renders the challenge page, optionally carrying a handle.
func (f *fixture) getChallenge(t *testing.T, handle string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, MFAPath+"?request="+testPendingID, nil)
	if handle != "" {
		r.AddCookie(&http.Cookie{Name: ChallengeCookieName, Value: handle})
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}

	w := httptest.NewRecorder()
	f.handler.MFAStep(w, r)
	return w
}

// A refresh re-renders the form, consuming nothing.
func TestARefreshRendersTheChallengeAgain(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	w := f.getChallenge(t, testHandle)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `name="code"`) {
		t.Errorf("the refreshed page has no code field:\n%s", w.Body.String())
	}
	if c.calls != 0 {
		t.Errorf("a refresh answered the challenge %d time(s); it must consume nothing", c.calls)
	}
}

// A refresh after the CSRF cookie lapsed still produces a SUBMITTABLE form.
//
// Otherwise the page renders, the user types a code, and the submission is
// refused for a reason that has nothing to do with the code — which reads as
// "my authenticator is broken".
func TestARefreshWithoutACSRFCookieStillYieldsASubmittableForm(t *testing.T) {
	f, _ := challenged(t)

	w := f.getChallenge(t, testHandle)

	var issued string
	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			issued = c.Value
		}
	}
	if issued == "" {
		t.Fatal("no CSRF cookie was issued to a page that has none")
	}
	if !strings.Contains(w.Body.String(), issued) {
		t.Error("the issued CSRF token is not in the form, so the submission would be refused")
	}
}

// A GET with a live challenge whose factors have all gone renders the dead end
// rather than a form whose every answer is refused.
func TestAChallengeWithNothingToOfferIsADeadEnd(t *testing.T) {
	f, c := challenged(t)
	c.peek = nil // every factor removed since the challenge was issued

	w := f.getChallenge(t, testHandle)

	if strings.Contains(w.Body.String(), `name="code"`) {
		t.Errorf("a code form rendered with no answerable factor:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), MsgChallengeGone) {
		t.Errorf("the page does not send the visitor back to the password step:\n%s", w.Body.String())
	}
}

// An expired challenge on a GET ends the same way as a spent one.
func TestAGetOnAnExpiredChallengeEndsTheFlow(t *testing.T) {
	f, c := challenged(t)
	c.peekErr = mfa.ErrNoChallenge

	w := f.getChallenge(t, testHandle)

	if !strings.Contains(w.Body.String(), MsgChallengeGone) {
		t.Errorf("an expired challenge did not end the flow:\n%s", w.Body.String())
	}
	// And the stale cookie is cleared, so the browser stops presenting a handle
	// that names nothing.
	cleared := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == ChallengeCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the dead challenge cookie was not cleared")
	}
}

// The GET needs a pending authorization request, like every other page here.
// Reached directly there is nothing to sign in TO.
func TestTheChallengePageNeedsAPendingRequest(t *testing.T) {
	f, _ := challenged(t)

	r := httptest.NewRequest(http.MethodGet, MFAPath, nil)
	r.AddCookie(&http.Cookie{Name: ChallengeCookieName, Value: testHandle})
	w := httptest.NewRecorder()
	f.handler.MFAStep(w, r)

	if strings.Contains(w.Body.String(), `name="code"`) {
		t.Errorf("a code form rendered with no authorization request:\n%s", w.Body.String())
	}
}

// The method table. Anything but GET and POST is refused with an Allow header,
// matching /login rather than inventing a second convention.
func TestTheChallengePageRefusesOtherMethods(t *testing.T) {
	f, _ := challenged(t)

	r := httptest.NewRequest(http.MethodDelete, MFAPath+"?request="+testPendingID, nil)
	w := httptest.NewRecorder()
	f.handler.MFAStep(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, POST" {
		t.Errorf("Allow = %q, want \"GET, POST\"", got)
	}
}

// A failed CSRF check re-renders the challenge rather than dropping the user
// back to the password step — the challenge is still live, and making them
// retype a password they already proved would be a worse outcome than the stale
// tab that usually causes this.
func TestAFailedCSRFCheckKeepsTheChallenge(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	form := strings.NewReader("request=" + testPendingID + "&factor=totp&code=123456&" + csrfField + "=wrong")
	r := httptest.NewRequest(http.MethodPost, MFAPath, form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: ChallengeCookieName, Value: testHandle})
	w := httptest.NewRecorder()
	f.handler.MFAStep(w, r)

	if c.calls != 0 {
		t.Error("a submission with a bad CSRF token reached the framework")
	}
	if !strings.Contains(w.Body.String(), MsgSessionProblem) {
		t.Errorf("the page does not explain the stale form:\n%s", w.Body.String())
	}
}

// The page renders only factor types this build can actually challenge with.
//
// A type listed without a verifier behind it is an option nobody can complete,
// and a dead option on this page is a user who cannot sign in.
func TestOnlyAnswerableFactorKindsAreOffered(t *testing.T) {
	offered := offeredLabels([]mfa.Type{mfa.TypeTOTP, mfa.TypeWebAuthn, mfa.Type("something-else")})

	if len(offered) != 1 {
		t.Fatalf("offered %d kinds, want only the one this build can answer: %+v", len(offered), offered)
	}
	if offered[0].Type != string(mfa.TypeTOTP) {
		t.Errorf("offered %q", offered[0].Type)
	}
	if offered[0].Label == "" || offered[0].Hint == "" {
		t.Error("an offered factor has no label or hint for the person reading it")
	}
}

// A code longer than the field bound is truncated rather than passed on whole.
func TestASubmittedCodeIsBounded(t *testing.T) {
	long := strings.Repeat("1", maxCodeBytes*3)

	if got := boundedCode(long); len(got) != maxCodeBytes {
		t.Errorf("a %d-byte code was bounded to %d, want %d", len(long), len(got), maxCodeBytes)
	}
	if got := boundedCode("  123456  "); got != "123456" {
		t.Errorf("boundedCode(%q) = %q; surrounding space should not make a code wrong", "  123456  ", got)
	}
}

// firstType names the factor for the audit payload, and answers empty rather
// than panicking when a completion carried none.
func TestFirstTypeHandlesAnEmptyCompletion(t *testing.T) {
	if got := firstType(nil); got != "" {
		t.Errorf("firstType(nil) = %q, want empty", got)
	}
	if got := firstType([]mfa.Type{mfa.TypeTOTP}); got != mfa.TypeTOTP {
		t.Errorf("firstType = %q, want totp", got)
	}
}

// offeredFor reports no challenge when there is no handle, rather than asking
// the framework about an empty one.
func TestOfferedForNeedsAHandle(t *testing.T) {
	f, c := challenged(t)

	r := httptest.NewRequest(http.MethodGet, MFAPath, nil)
	if _, err := f.handler.offeredFor(r); !errors.Is(err, mfa.ErrNoChallenge) {
		t.Errorf("offeredFor gave %v, want ErrNoChallenge", err)
	}
	if c.calls != 0 {
		t.Error("offeredFor reached the framework with no handle")
	}
}

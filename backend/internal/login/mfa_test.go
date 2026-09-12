package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/session"
)

// The challenge step's branch table (P3-03).
//
// These drive the handler with a fake Challenger rather than Redis and
// Postgres, which is the whole reason `Challenger` is an interface: the
// questions worth asking most often here are "does a wrong code create a
// session" and "can the page be reached without a challenge", and a test that
// needs two containers to answer them is a test that gets run less.
//
// What is NOT here is whether a code verifies — that is `internal/mfa`'s
// business and is tested against RFC 6238's own vectors.

// --- a fake framework ----------------------------------------------------------

type fakeChallenger struct {
	// decision is what Required answers.
	decision mfa.Decision
	required error

	// outcome and answerErr are what AnswerType answers.
	outcome   mfa.Outcome
	answerErr error

	// peek is what a live challenge offers.
	peek    []mfa.Type
	peekErr error

	// seen records what AnswerType was called with, so a test can assert the
	// handler passed the form's factor type through rather than inventing one.
	seenType    mfa.Type
	seenCode    string
	seenPending string
	calls       int
}

func (f *fakeChallenger) Required(context.Context, string, string, string) (mfa.Decision, error) {
	return f.decision, f.required
}

func (f *fakeChallenger) AnswerType(
	_ context.Context, _, pendingID string, t mfa.Type, code string,
) (mfa.Outcome, error) {
	f.calls++
	f.seenPending = pendingID
	f.seenType = t
	f.seenCode = code
	return f.outcome, f.answerErr
}

func (f *fakeChallenger) Peek(context.Context, string) ([]mfa.Type, error) {
	return f.peek, f.peekErr
}

// challenged builds a fixture whose user holds one TOTP factor.
func challenged(t *testing.T) (*fixture, *fakeChallenger) {
	t.Helper()

	f := newFixture(t)
	f.users.verified = true
	f.users.user = signedInUser()

	c := &fakeChallenger{
		decision: mfa.Decision{
			Challenge: true,
			Handle:    testHandle,
			Offered:   []mfa.Type{mfa.TypeTOTP},
		},
		peek: []mfa.Type{mfa.TypeTOTP},
	}
	f.handler.MFA = c
	return f, c
}

// testHandle is a correctly shaped handle — 32 bytes base64url, which is what
// mfa.NewHandle produces and what challengeFromRequest insists on.
const testHandle = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// postCode submits a code to the challenge step, carrying both cookies.
func (f *fixture) postCode(t *testing.T, code, handle string) *httptest.ResponseRecorder {
	t.Helper()
	_, csrf := f.get(t)
	return f.postCodeWith(t, code, handle, csrf)
}

// postCodeWith submits with a CSRF token the caller controls, so two responses
// can be compared byte for byte without the token being the difference.
func (f *fixture) postCodeWith(t *testing.T, code, handle, csrf string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{
		"code":    {code},
		"factor":  {string(mfa.TypeTOTP)},
		"request": {testPendingID},
		csrfField: {csrf},
	}

	r := httptest.NewRequest(http.MethodPost, MFAPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, csrf)
	if handle != "" {
		r.AddCookie(&http.Cookie{Name: ChallengeCookieName, Value: handle})
	}

	w := httptest.NewRecorder()
	f.handler.MFAStep(w, r)
	return w
}

// --- the case docs/PLAN/11 names ------------------------------------------------

// DoD item 1, and the literal sentence in docs/PLAN/11 § E2E: a correct
// password with a wrong code is REJECTED.
//
// The assertion is on what the browser is given, not on the branch taken: no
// session cookie, and the authorization flow is not resumed. A test that
// checked only for an error message would pass against a handler that showed
// the message and set the cookie anyway.
func TestACorrectPasswordWithAWrongCodeIsRejected(t *testing.T) {
	f, c := challenged(t)

	// Step one: the password. It must NOT produce a session.
	w := f.post(t, credentials(), "")

	if hasSessionCookie(w) {
		t.Fatal("the password step issued a session cookie to a user with a factor")
	}
	if len(f.auth.resumed) != 0 {
		t.Fatal("the password step resumed the authorization flow without a factor")
	}
	if handle := challengeCookieIn(w); handle != testHandle {
		t.Fatalf("the challenge handle cookie is %q, want the issued handle", handle)
	}

	// Step two: a wrong code.
	c.answerErr = mfa.ErrWrongCode
	c.outcome = mfa.Outcome{UserID: testUserID, OrgID: testOrgID, Offered: []mfa.Type{mfa.TypeTOTP}}

	w = f.postCode(t, "000000", testHandle)

	if hasSessionCookie(w) {
		t.Error("a wrong code issued a session cookie")
	}
	if len(f.auth.resumed) != 0 {
		t.Error("a wrong code resumed the authorization flow")
	}
	if body := w.Body.String(); !strings.Contains(body, MsgWrongCode) {
		t.Errorf("the page does not say the code was wrong:\n%s", body)
	}
}

// The other half of the same case: the RIGHT code does complete it. Without
// this, the test above passes against a handler that rejects everything.
func TestACorrectCodeCompletesTheLogin(t *testing.T) {
	f, c := challenged(t)

	f.post(t, credentials(), "")

	c.answerErr = nil
	c.outcome = mfa.Outcome{
		Complete:  true,
		UserID:    testUserID,
		OrgID:     testOrgID,
		PendingID: testPendingID,
		Methods:   []mfa.Type{mfa.TypeTOTP},
	}

	w := f.postCode(t, "123456", testHandle)

	if len(f.auth.resumed) == 0 {
		t.Fatalf("a correct code did not resume the flow (status %d):\n%s", w.Code, w.Body.String())
	}
	if !hasSessionCookie(w) {
		t.Error("a completed challenge did not issue a session cookie")
	}
}

// --- what the session records ----------------------------------------------------

// DoD item 4: auth_methods reflects the factors USED.
//
// The values are RFC 8176's — `pwd`, `otp`, and `mfa` for two categories —
// which is P3-01 § 7's contract and what P1-07's `amr` claim is built from. A
// consumer refusing a payment unless `amr` contains `otp` is trusting this
// line.
func TestACompletedChallengeRecordsTheFactorsUsed(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	c.outcome = mfa.Outcome{
		Complete: true, UserID: testUserID, OrgID: testOrgID,
		PendingID: testPendingID, Methods: []mfa.Type{mfa.TypeTOTP},
	}
	f.postCode(t, "123456", testHandle)

	if len(f.sessions.created) == 0 {
		t.Fatal("no session was created")
	}
	got := f.sessions.created[len(f.sessions.created)-1].AuthMethods

	want := map[string]bool{"pwd": true, "otp": true, "mfa": true}
	if len(got) != len(want) {
		t.Fatalf("auth_methods = %v, want exactly %v", got, keysOf(want))
	}
	for _, method := range got {
		if !want[method] {
			t.Errorf("auth_methods contains %q, which is not an RFC 8176 value for this login", method)
		}
	}
}

// A login with NO factor still records only the password — `mfa` must not
// appear because a second factor existed somewhere, only because one was used.
func TestALoginWithoutAFactorRecordsOnlyThePassword(t *testing.T) {
	f := newFixture(t)
	f.users.verified = true
	f.users.user = signedInUser()
	f.handler.MFA = &fakeChallenger{} // Required answers "no challenge".

	f.post(t, credentials(), "")

	if len(f.sessions.created) == 0 {
		t.Fatal("no session was created")
	}
	got := f.sessions.created[0].AuthMethods
	if len(got) != 1 || got[0] != "pwd" {
		t.Errorf("auth_methods = %v, want [pwd] exactly", got)
	}
}

// --- reaching the page without a challenge ----------------------------------------

// The page must not exist without a challenge behind it. A code form that
// renders for anybody is a place to guess against a challenge nobody issued.
func TestTheChallengePageIsNotReachableWithoutAChallenge(t *testing.T) {
	f, _ := challenged(t)

	r := httptest.NewRequest(http.MethodGet, MFAPath+"?request="+testPendingID, nil)
	w := httptest.NewRecorder()
	f.handler.MFAStep(w, r) // no challenge cookie

	if body := w.Body.String(); strings.Contains(body, `name="code"`) {
		t.Errorf("a code form rendered with no challenge cookie:\n%s", body)
	}
	if body := w.Body.String(); !strings.Contains(body, MsgChallengeGone) {
		t.Errorf("the page does not send the visitor back to the password step:\n%s", body)
	}
}

// A handle that is not shaped like one is a missing challenge, not a store
// lookup on arbitrary bytes.
func TestAMalformedHandleIsAMissingChallenge(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	w := f.postCode(t, "123456", "not-a-handle")

	if c.calls != 0 {
		t.Error("a malformed handle reached the framework")
	}
	if hasSessionCookie(w) {
		t.Error("a malformed handle produced a session")
	}
}

// --- uniformity within the step ----------------------------------------------------

// A wrong code and a factor type the challenge has no factor for must be
// indistinguishable. The second is a fact about what the user has ENROLLED,
// and somebody probing the form must not be able to read it off the response.
func TestAWrongCodeAndAnUnknownFactorAreIndistinguishable(t *testing.T) {
	// One fixture, one CSRF token, two answers. Two fixtures would mint two
	// tokens, and the hidden field would then differ for a reason that has
	// nothing to do with disclosure — a test that reads its own setup as the
	// bug it is looking for.
	f, c := challenged(t)
	f.post(t, credentials(), "")
	_, csrf := f.get(t)

	body := func(answerErr error) string {
		c.answerErr = answerErr
		c.outcome = mfa.Outcome{UserID: testUserID, OrgID: testOrgID, Offered: []mfa.Type{mfa.TypeTOTP}}
		return f.postCodeWith(t, "000000", testHandle, csrf).Body.String()
	}

	wrong := body(mfa.ErrWrongCode)
	unknown := body(mfa.ErrNoSuchFactor)

	if wrong != unknown {
		t.Errorf("a wrong code and an unknown factor render differently:\n--- wrong ---\n%s\n--- unknown ---\n%s",
			wrong, unknown)
	}
}

// An exhausted per-user bound says so, and that is deliberate rather than a
// slip: the counter is keyed on a user whose password the caller has already
// proven, so it describes only their own behaviour — MsgRateLimited's
// reasoning. Being vague would leave somebody typing correct codes and
// watching them fail.
func TestAnExhaustedBoundSaysToWait(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	c.answerErr = mfa.ErrTooManyAttempts
	c.outcome = mfa.Outcome{UserID: testUserID, OrgID: testOrgID}

	w := f.postCode(t, "000000", testHandle)

	if body := w.Body.String(); !strings.Contains(body, MsgCodeRateLimited) {
		t.Errorf("an exhausted bound does not tell the user to wait:\n%s", body)
	}
	if hasSessionCookie(w) {
		t.Error("an exhausted bound produced a session")
	}
}

// A spent or expired challenge sends the user back to the password step, and
// both render the same page: the difference between them is a fact about a
// login this caller may not own.
func TestASpentAndAnExpiredChallengeEndTheSameWay(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")
	_, csrf := f.get(t)

	body := func(answerErr error) string {
		c.answerErr = answerErr
		return f.postCodeWith(t, "000000", testHandle, csrf).Body.String()
	}

	if spent, gone := body(mfa.ErrChallengeSpent), body(mfa.ErrNoChallenge); spent != gone {
		t.Errorf("a spent and an expired challenge render differently:\n--- spent ---\n%s\n--- gone ---\n%s",
			spent, gone)
	}
}

// --- failing closed ------------------------------------------------------------------

// A factor store that cannot answer REFUSES the login. It must never complete
// one: an attacker who can make one service unavailable would otherwise have a
// way to skip the second factor, and the user would never learn it was not
// asked for.
func TestAFactorStoreThatCannotAnswerRefusesTheLogin(t *testing.T) {
	f := newFixture(t)
	f.users.verified = true
	f.users.user = signedInUser()
	f.handler.MFA = &fakeChallenger{required: errors.New("redis is unreachable")}

	w := f.post(t, credentials(), "")

	if hasSessionCookie(w) {
		t.Fatal("a login completed while the factor store was unreachable")
	}
	if len(f.auth.resumed) != 0 {
		t.Fatal("the authorization flow resumed while the factor store was unreachable")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 — an undecidable factor check is not a wrong password", w.Code)
	}
}

// A failure to DECIDE at the code step is a 500, not a wrong code. Reporting an
// operator's misconfiguration as a wrong code sends somebody to their recovery
// codes for a problem they cannot fix.
func TestAnUndecidableCodeIsNotAWrongCode(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	c.answerErr = errors.New("the seal key will not open this secret")

	w := f.postCode(t, "123456", testHandle)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, MsgWrongCode) {
		t.Error("an undecidable verification was reported as a wrong code")
	}
}

// --- the handle ----------------------------------------------------------------------

// The handle never reaches the HTML. It is the one value standing between a
// proven password and a session, and a handle in the page is a handle in the
// browser cache, in a saved page, and in anything that reads a screenshot.
func TestTheChallengeHandleIsNeverInThePage(t *testing.T) {
	f, _ := challenged(t)
	w := f.post(t, credentials(), "")

	if strings.Contains(w.Body.String(), testHandle) {
		t.Errorf("the challenge handle is rendered into the page:\n%s", w.Body.String())
	}
}

// And it is HttpOnly, Secure and SameSite=Lax — the same shape as the CSRF
// cookie, for stronger reasons.
func TestTheChallengeCookieIsProtected(t *testing.T) {
	f, _ := challenged(t)
	w := f.post(t, credentials(), "")

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == ChallengeCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no challenge cookie was set")
	}
	if !cookie.HttpOnly {
		t.Error("the challenge cookie is readable by script")
	}
	if !cookie.Secure {
		t.Error("the challenge cookie is sent over plain HTTP")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if !strings.HasPrefix(ChallengeCookieName, "__Host-") {
		t.Error("the challenge cookie has no __Host- prefix, so a subdomain could set it")
	}
}

// --- audit ------------------------------------------------------------------------------

// DoD item: MFA verification is audited separately from password verification.
func TestMFAOutcomesAreAuditedUnderTheirOwnEventTypes(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	// The password step, having demanded a factor, says so.
	if !wroteEvent(f, audit.EventMFAChallenged) {
		t.Error("a demanded factor was not audited")
	}

	c.answerErr = mfa.ErrWrongCode
	c.outcome = mfa.Outcome{UserID: testUserID, OrgID: testOrgID, Offered: []mfa.Type{mfa.TypeTOTP}}
	f.postCode(t, "000000", testHandle)

	if !wroteEvent(f, audit.EventMFAFailed) {
		t.Error("a failed code was not audited")
	}
	if wroteEvent(f, audit.EventLoginFailed) {
		t.Error("a failed CODE was audited as a failed LOGIN, which conflates two different signals")
	}
}

// The code is never in an audit payload. It is a live credential for the rest
// of its step, so a row containing one is a row holding a credential.
func TestTheCodeIsNeverAudited(t *testing.T) {
	f, c := challenged(t)
	f.post(t, credentials(), "")

	const code = "987654"
	c.answerErr = mfa.ErrWrongCode
	c.outcome = mfa.Outcome{UserID: testUserID, OrgID: testOrgID, Offered: []mfa.Type{mfa.TypeTOTP}}
	f.postCode(t, code, testHandle)

	for _, e := range f.auditor.events {
		for key, value := range e.Payload {
			if str, ok := value.(string); ok && strings.Contains(str, code) {
				t.Errorf("the submitted code appears in audit payload %q", key)
			}
		}
	}
}

// --- helpers ---------------------------------------------------------------------------

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// wroteEvent reports whether the fake auditor saw an event of this type.
func wroteEvent(f *fixture, want audit.EventType) bool {
	for _, e := range f.auditor.events {
		if e.Type == want {
			return true
		}
	}
	return false
}

// signedInUser is a user who can sign in — every challenge test starts from a
// correct password, so the account state is never the thing under test.
func signedInUser() authn.User {
	return authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
}

// hasSessionCookie reports whether a response handed the browser a session.
//
// Presence rather than value: the fake session manager returns a zero
// session.Token, and these tests are about whether a session was issued at all.
func hasSessionCookie(w *httptest.ResponseRecorder) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName {
			return true
		}
	}
	return false
}

// challengeCookieIn returns the challenge handle a response set, or "".
func challengeCookieIn(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == ChallengeCookieName {
			return c.Value
		}
	}
	return ""
}

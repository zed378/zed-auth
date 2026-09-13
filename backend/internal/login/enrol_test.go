package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// Forced enrolment's branch table (P3-07).
//
// The mandate's own decision is a pure function tested in `internal/authn`.
// What is here is what the HANDLER does with each answer, and the property
// that matters most is the one a reader would not guess: **no session exists
// until enrolment completes**, which is what makes the state unbypassable
// rather than merely awkward to leave.

// fakeEnroller is an enrolment in memory.
type fakeEnroller struct {
	secret string
	code   string

	beginErr   error
	confirmErr error

	// seenUser and seenOrg record what Begin was handed, so a test can assert
	// the factor is created for the user the CHALLENGE names rather than one a
	// request could name.
	seenUser string
	seenOrg  string
	begins   int
	confirms int
}

func (f *fakeEnroller) Begin(_ context.Context, userID, orgID, label string) (mfa.Enrolment, error) {
	f.begins++
	f.seenUser, f.seenOrg = userID, orgID
	if f.beginErr != nil {
		return mfa.Enrolment{}, f.beginErr
	}
	return mfa.Enrolment{
		FactorID:  "pending-factor",
		Secret:    f.secret,
		Challenge: map[string]any{"provisioning_uri": "otpauth://totp/Test:alice?secret=" + f.secret},
	}, nil
}

func (f *fakeEnroller) Confirm(_ context.Context, factorID, code string) error {
	f.confirms++
	if f.confirmErr != nil {
		return f.confirmErr
	}
	if code != f.code {
		return mfa.ErrWrongCode
	}
	return nil
}

// memoryEnrolments is an EnrolStore in a map.
type memoryEnrolments struct {
	at map[string]EnrolState
	n  int
}

func newMemoryEnrolments() *memoryEnrolments {
	return &memoryEnrolments{at: map[string]EnrolState{}}
}

func (m *memoryEnrolments) Put(_ context.Context, state EnrolState, _ int) (string, error) {
	m.n++
	handle := strings.Repeat("e", handleLength)
	if m.n > 1 {
		handle = strings.Repeat("f", handleLength)
	}
	m.at[handle] = state
	return handle, nil
}

func (m *memoryEnrolments) Get(_ context.Context, handle string) (EnrolState, error) {
	state, ok := m.at[handle]
	if !ok {
		return EnrolState{}, ErrNoEnrolment
	}
	return state, nil
}

func (m *memoryEnrolments) Replace(_ context.Context, handle string, state EnrolState) error {
	if _, ok := m.at[handle]; !ok {
		return ErrNoEnrolment
	}
	m.at[handle] = state
	return nil
}

func (m *memoryEnrolments) Delete(_ context.Context, handle string) error {
	delete(m.at, handle)
	return nil
}

// mandated builds a fixture whose organization requires MFA and whose user has
// no factor.
func mandated(t *testing.T, since time.Time) (*fixture, *fakeEnroller, *memoryEnrolments) {
	t.Helper()

	f := newFixture(t)
	f.users.verified = true
	f.users.user = signedInUser()

	policy := authn.LoginPolicy{
		SessionLifetimeHours: 12,
		AllowedMethods:       []string{authn.MethodPassword},
		MFARequired:          true,
		MFARequiredSince:     since,
	}
	f.handler.Policies = fakePolicies{policy: authn.DefaultPolicy, login: &policy}

	enroller := &fakeEnroller{secret: "JBSWY3DPEHPK3PXP", code: "123456"}
	store := newMemoryEnrolments()

	f.handler.Enrol = enroller
	f.handler.Enrolments = store
	// A framework that offers no challenge, which is the state of a user with
	// no factor.
	f.handler.MFA = &fakeChallenger{}

	return f, enroller, store
}

// signedIn reports whether the login actually completed.
//
// It asks whether the AUTHORIZATION WAS RESUMED rather than whether a session
// cookie was set, because the unit fixture's session store returns a zero
// token — so a cookie check would answer "no" on every path and every
// assertion built on it would pass for the wrong reason. Resuming is the thing
// that only happens when a login finishes, which is the property this task
// turns on: no completion until enrolment is done.
func signedIn(f *fixture) bool { return len(f.auth.resumed) > 0 }

func enrolHandle(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == EnrolCookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("no enrolment cookie was set (status %d):\n%s", w.Code, w.Body.String())
	return ""
}

// --- the forced route -------------------------------------------------------------------

// Past the grace, a user with no factor is routed into enrolment and gets NO
// session.
func TestPastTheGraceAUserWithNoFactorIsRoutedIntoEnrolment(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	w := f.post(t, credentials(), "")

	if signedIn(f) {
		t.Fatal("a session was issued to a user the mandate says must enrol first")
	}
	if enroller.begins != 1 {
		t.Fatalf("Begin was called %d times, want 1", enroller.begins)
	}
	if !strings.Contains(w.Body.String(), MsgEnrolTitle) {
		t.Errorf("the enrolment page was not rendered:\n%s", w.Body.String())
	}
	// The secret is shown, once.
	if !strings.Contains(w.Body.String(), enroller.secret) {
		t.Errorf("the enrolment page does not show the key, so nobody could set anything up:\n%s",
			w.Body.String())
	}
	enrolHandle(t, w)
}

// The factor is created for the user who proved the password, not for anybody
// a request could name.
func TestTheForcedFactorBelongsToTheAuthenticatedUser(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	f.post(t, credentials(), "")

	if enroller.seenUser != signedInUser().ID {
		t.Errorf("the factor was created for %q, want the user who signed in", enroller.seenUser)
	}
	if enroller.seenOrg != signedInUser().OrgID {
		t.Errorf("the factor was filed under %q, want the user's own organization", enroller.seenOrg)
	}
}

// Inside the grace, the same user IS signed in.
//
// This is what stops the mandate being a hard cutover, and it is the half most
// likely to be "simplified" away by somebody who reads only the requirement.
func TestInsideTheGraceTheUserIsSignedIn(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-time.Hour))

	w := f.post(t, credentials(), "")

	if !signedIn(f) {
		t.Fatalf("a user inside the grace period was refused a session:\n%s", w.Body.String())
	}
	if enroller.begins != 0 {
		t.Error("an enrolment was begun for somebody who was let in anyway")
	}
}

// A user who HAS a factor is challenged, not enrolled — the mandate has
// nothing to say about them.
func TestAUserWithAFactorIsChallengedNotEnrolled(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	f.handler.MFA = &fakeChallenger{
		decision: mfa.Decision{
			Challenge: true,
			Handle:    testHandle,
			Offered:   []mfa.Type{mfa.TypeTOTP},
		},
	}

	w := f.post(t, credentials(), "")

	if enroller.begins != 0 {
		t.Error("a user who already holds a factor was routed into enrolment")
	}
	if !strings.Contains(w.Body.String(), `name="code"`) {
		t.Errorf("the challenge page was not rendered:\n%s", w.Body.String())
	}
}

// A deployment that cannot enrol anybody does NOT lock the organization out.
//
// The mandate says yes and there is nothing to satisfy it with. Refusing would
// leave every user of that organization unable to sign in with no way forward,
// which is strictly worse than the policy not applying.
func TestABuildThatCannotEnrolDoesNotLockAnybodyOut(t *testing.T) {
	f, _, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))
	f.handler.Enrol = nil
	f.handler.Enrolments = nil

	w := f.post(t, credentials(), "")

	if !signedIn(f) {
		t.Fatalf("an organization was locked out by a mandate this build cannot satisfy:\n%s",
			w.Body.String())
	}
}

// No mandate, no factor: nothing changes.
func TestWithoutTheMandateNothingChanges(t *testing.T) {
	f, enroller, _ := mandated(t, time.Time{})
	f.handler.Policies = fakePolicies{policy: authn.DefaultPolicy, login: &authn.LoginPolicy{
		SessionLifetimeHours: 12,
		AllowedMethods:       []string{authn.MethodPassword},
	}}

	f.post(t, credentials(), "")

	if !signedIn(f) {
		t.Fatal("a user in an organization with no mandate was refused a session")
	}
	if enroller.begins != 0 {
		t.Error("an enrolment was begun with no mandate in force")
	}
}

// --- completing it -----------------------------------------------------------------------

func (f *fixture) postEnrolment(t *testing.T, code, handle, csrf string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{
		"code":    {code},
		"request": {testPendingID},
		csrfField: {csrf},
	}

	r := httptest.NewRequest(http.MethodPost, EnrolPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, csrf)
	if handle != "" {
		r.AddCookie(&http.Cookie{Name: EnrolCookieName, Value: handle})
	}

	w := httptest.NewRecorder()
	f.handler.EnrolStep(w, r)
	return w
}

// The right code completes the enrolment and signs the user in.
func TestAConfirmedEnrolmentSignsTheUserIn(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)
	_, csrf := f.get(t)

	w := f.postEnrolment(t, enroller.code, handle, csrf)

	if !signedIn(f) {
		t.Fatalf("a confirmed enrolment did not sign the user in (status %d):\n%s",
			w.Code, w.Body.String())
	}
	if enroller.confirms != 1 {
		t.Errorf("Confirm was called %d times, want 1", enroller.confirms)
	}
}

// A wrong code does not.
func TestAWrongEnrolmentCodeIssuesNoSession(t *testing.T) {
	f, _, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)
	_, csrf := f.get(t)

	w := f.postEnrolment(t, "000000", handle, csrf)

	if signedIn(f) {
		t.Fatal("a wrong enrolment code signed the user in")
	}
	if !strings.Contains(w.Body.String(), MsgWrongCode) {
		t.Errorf("the page does not say the code was wrong:\n%s", w.Body.String())
	}
}

// Wrong codes are bounded, so the forced page is not a place to guess.
func TestEnrolmentAttemptsAreBounded(t *testing.T) {
	f, _, store := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)
	_, csrf := f.get(t)

	var last *httptest.ResponseRecorder
	for i := 0; i < mfa.MaxAttempts+1; i++ {
		last = f.postEnrolment(t, "000000", handle, csrf)
	}

	if !strings.Contains(last.Body.String(), MsgEnrolmentGone) {
		t.Errorf("after %d wrong codes the enrolment was still open:\n%s",
			mfa.MaxAttempts+1, last.Body.String())
	}

	// And the state is DESTROYED rather than merely refused.
	//
	// A mutation run found that the message alone is produced by either of two
	// mutually redundant guards, so a test asserting only the message passed
	// with the deletion removed. What deletion uniquely prevents is a spent
	// enrolment sitting in the store for its full fifteen minutes, answerable
	// again the moment anything stopped consulting the other guard.
	if _, err := store.Get(context.Background(), handle); !errors.Is(err, ErrNoEnrolment) {
		t.Errorf("a spent enrolment is still in the store: %v", err)
	}
}

// --- the bypasses (A-1, A-3) -----------------------------------------------------------------

// An enrolment begun for one authorization request cannot complete another.
func TestAnEnrolmentCannotCompleteADifferentRequest(t *testing.T) {
	f, enroller, store := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)

	// The stored state names a different pending request.
	state, _ := store.Get(context.Background(), handle)
	state.PendingID = "some-other-request"
	_ = store.Replace(context.Background(), handle, state)

	_, csrf := f.get(t)
	f.postEnrolment(t, enroller.code, handle, csrf)

	if signedIn(f) {
		t.Fatal("an enrolment begun for one login completed another")
	}
	if enroller.confirms != 0 {
		t.Error("the mismatched request still reached the enroller")
	}
}

// Without the handle there is no enrolment to complete, even with the right
// code — the state is server-side and the cookie is the only way in.
func TestWithoutTheHandleThereIsNoEnrolment(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	f.post(t, credentials(), "")
	_, csrf := f.get(t)

	f.postEnrolment(t, enroller.code, "", csrf)

	if signedIn(f) {
		t.Fatal("an enrolment completed with no handle")
	}
	if enroller.confirms != 0 {
		t.Error("the enroller was reached with no enrolment state")
	}
}

// --- the refresh ---------------------------------------------------------------------------

// A refresh does NOT mint a second secret.
//
// Doing so would invalidate the one the user has already scanned, at the worst
// possible moment — and it would read as the service being broken.
func TestARefreshDoesNotMintASecondSecret(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)

	r := httptest.NewRequest(http.MethodGet, EnrolPath+"?request="+testPendingID, nil)
	r.AddCookie(&http.Cookie{Name: EnrolCookieName, Value: handle})
	w := httptest.NewRecorder()
	f.handler.EnrolStep(w, r)

	if enroller.begins != 1 {
		t.Errorf("Begin was called %d times across a submit and a refresh, want 1", enroller.begins)
	}
	// And the secret is not re-displayed: it was a one-time value.
	if strings.Contains(w.Body.String(), enroller.secret) {
		t.Errorf("a refresh re-displayed the one-time key:\n%s", w.Body.String())
	}
	// The code field is still there, so the user can still finish.
	if !strings.Contains(w.Body.String(), `name="code"`) {
		t.Errorf("the refreshed page has no code field, so the enrolment cannot be finished:\n%s",
			w.Body.String())
	}
}

// --- the page's own properties ----------------------------------------------------------------

// The forced page has no script, unlike the passkey step.
//
// `PG-40`'s exception is scoped to a page that needs a browser API. TOTP
// enrolment needs none, so this page keeps `P1-12`'s policy.
func TestTheEnrolmentPageHasNoScript(t *testing.T) {
	f, _, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	w := f.post(t, credentials(), "")

	if strings.Contains(w.Body.String(), "<script") {
		t.Errorf("the forced enrolment page carries a script:\n%s", w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); strings.Contains(csp, "script-src") {
		t.Errorf("the forced enrolment page's policy names a script source: %q", csp)
	}
}

// A store failure while beginning is an error, not a silent sign-in.
func TestAFailureToBeginRefusesRatherThanLettingThrough(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))
	enroller.beginErr = errors.New("the factor store is unreachable")

	w := f.post(t, credentials(), "")

	if signedIn(f) {
		t.Fatal("a user was signed in without a factor because enrolment could not begin")
	}

	// Not being signed in is not enough, and a mutation run proved it: ignoring
	// the error leaves the user on an enrolment page with no key and no
	// handle — a dead end they can neither escape nor act on. The refusal has
	// to SHOW as a failure.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 — a broken enrolment must not render as a working one:\n%s",
			w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), MsgEnrolCodeLabel) {
		t.Error("an enrolment form rendered for an enrolment that could not be created")
	}
}

// --- RecoveryNotice (P3-04) -----------------------------------------------------------

// The message after a recovery login depends on how many codes are left.
//
// Tested here because it had no test at all — the coverage floor surfaced it,
// and the thresholds are the part worth pinning: telling somebody they are
// "running low" when they have none left, or saying nothing when they have one,
// is how a user ends up with no way back.
func TestRecoveryNoticeFollowsTheRemainingCount(t *testing.T) {
	for _, tc := range []struct {
		remaining int
		want      string
	}{
		{0, MsgRecoveryExhausted},
		{1, MsgRecoveryLow},
		{mfa.RecoveryLowWaterMark, MsgRecoveryLow},
		{mfa.RecoveryLowWaterMark + 1, MsgRecoveryUsed},
		{mfa.RecoveryCodeCount - 1, MsgRecoveryUsed},
	} {
		if got := RecoveryNotice(tc.remaining); got != tc.want {
			t.Errorf("RecoveryNotice(%d) = %q, want %q", tc.remaining, got, tc.want)
		}
	}
}

// --- the step's edges ---------------------------------------------------------------

// The forced step refuses methods other than GET and POST, like its siblings.
func TestTheEnrolmentStepRefusesOtherMethods(t *testing.T) {
	f, _, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	r := httptest.NewRequest(http.MethodDelete, EnrolPath+"?request="+testPendingID, nil)
	w := httptest.NewRecorder()
	f.handler.EnrolStep(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, POST" {
		t.Errorf("Allow = %q, want \"GET, POST\"", got)
	}
}

// A refresh with no pending request, or an expired enrolment, ends the flow
// rather than rendering a form with nothing behind it.
func TestARefreshWithNothingBehindItEndsTheFlow(t *testing.T) {
	f, _, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	// No request id at all.
	r := httptest.NewRequest(http.MethodGet, EnrolPath, nil)
	w := httptest.NewRecorder()
	f.handler.EnrolStep(w, r)
	if strings.Contains(w.Body.String(), `name="code"`) {
		t.Error("an enrolment form rendered with no authorization request")
	}

	// A request, and a handle naming nothing.
	r = httptest.NewRequest(http.MethodGet, EnrolPath+"?request="+testPendingID, nil)
	r.AddCookie(&http.Cookie{Name: EnrolCookieName, Value: strings.Repeat("z", handleLength)})
	w = httptest.NewRecorder()
	f.handler.EnrolStep(w, r)
	if !strings.Contains(w.Body.String(), MsgEnrolmentGone) {
		t.Errorf("an unknown enrolment did not end the flow:\n%s", w.Body.String())
	}
}

// A refresh for a different authorization request than the enrolment's ends
// the flow — the same binding the submission enforces.
func TestARefreshForAnotherRequestEndsTheFlow(t *testing.T) {
	f, _, store := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)

	state, _ := store.Get(context.Background(), handle)
	state.PendingID = "a-different-request"
	_ = store.Replace(context.Background(), handle, state)

	r := httptest.NewRequest(http.MethodGet, EnrolPath+"?request="+testPendingID, nil)
	r.AddCookie(&http.Cookie{Name: EnrolCookieName, Value: handle})
	w := httptest.NewRecorder()
	f.handler.EnrolStep(w, r)

	if !strings.Contains(w.Body.String(), MsgEnrolmentGone) {
		t.Errorf("a refresh for another request rendered the enrolment:\n%s", w.Body.String())
	}
}

// A stale CSRF token re-renders the enrolment with an explanation rather than
// sending the user back to the password step.
func TestAStaleCSRFTokenKeepsTheEnrolment(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)

	form := url.Values{"code": {enroller.code}, "request": {testPendingID}, csrfField: {"stale"}}
	r := httptest.NewRequest(http.MethodPost, EnrolPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: EnrolCookieName, Value: handle})
	w := httptest.NewRecorder()
	f.handler.EnrolStep(w, r)

	if signedIn(f) {
		t.Fatal("a submission with a bad CSRF token completed the enrolment")
	}
	if enroller.confirms != 0 {
		t.Error("a submission with a bad CSRF token reached the enroller")
	}
	if !strings.Contains(w.Body.String(), MsgSessionProblem) {
		t.Errorf("the page does not explain the stale form:\n%s", w.Body.String())
	}
}

// A failure to confirm that is NOT a wrong code is an error, not a retry prompt —
// telling somebody their code was wrong when the store is down sends them back to
// their authenticator for a problem they cannot fix.
func TestAConfirmFailureIsNotAWrongCode(t *testing.T) {
	f, enroller, _ := mandated(t, time.Now().Add(-authn.MFAGracePeriod-time.Hour))
	enroller.confirmErr = errors.New("the factor store is unreachable")

	first := f.post(t, credentials(), "")
	handle := enrolHandle(t, first)
	_, csrf := f.get(t)

	w := f.postEnrolment(t, enroller.code, handle, csrf)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), MsgWrongCode) {
		t.Error("a store failure was reported to the user as a wrong code")
	}
}

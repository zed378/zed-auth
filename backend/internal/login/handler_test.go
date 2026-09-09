package login

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// These tests are about what the browser can observe: which headers are set,
// what the page contains, whether a submission is accepted. The database side
// — the equal-cost not-found path, the byte-identical failure responses under
// a real query — is exercised in handler_integration_test.go against real
// Postgres and Redis.

const (
	testOrgID     = "22222222-2222-2222-2222-222222222222"
	testUserID    = "44444444-4444-4444-4444-444444444444"
	testPendingID = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG" // 43 chars
)

// --- fakes ---------------------------------------------------------------------

type fakeAuth struct {
	pending  authorize.Pending
	peekErr  error
	resumed  []string
	resumeTo string
}

func (f *fakeAuth) Peek(context.Context, string) (authorize.Pending, error) {
	if f.peekErr != nil {
		return authorize.Pending{}, f.peekErr
	}
	return f.pending, nil
}

func (f *fakeAuth) Resume(w http.ResponseWriter, r *http.Request, id string, _ session.Session) {
	f.resumed = append(f.resumed, id)
	target := f.resumeTo
	if target == "" {
		target = "https://app.example/cb?code=code-1&state=xyz"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

type fakeSessions struct {
	created []session.New
	current session.Session
	hasCurrent
	revoked   []string
	createErr error
}

type hasCurrent bool

func (f *fakeSessions) Create(
	_ context.Context, _ *postgres.Tx, in session.New, _ session.Policy, _ time.Time,
) (session.Session, session.Token, error) {
	if f.createErr != nil {
		return session.Session{}, session.Token{}, f.createErr
	}
	f.created = append(f.created, in)
	return session.Session{
		ID:          "33333333-3333-3333-3333-333333333333",
		UserID:      in.UserID,
		OrgID:       in.OrgID,
		AuthMethods: in.AuthMethods,
	}, session.Token{}, nil
}

func (f *fakeSessions) Lookup(context.Context, string, session.Policy, time.Time) (session.Session, error) {
	if !bool(f.hasCurrent) {
		return session.Session{}, session.ErrNotFound
	}
	return f.current, nil
}

func (f *fakeSessions) Revoke(
	_ context.Context, _ *postgres.Tx, sessionID string, _ session.Reason, _ string, _ time.Time,
) (func(context.Context) error, error) {
	f.revoked = append(f.revoked, sessionID)
	return func(context.Context) error { return nil }, nil
}

type fakeUsers struct {
	user     authn.User
	verified bool
	err      error
	rehashed []string
}

func (f *fakeUsers) Authenticate(context.Context, *postgres.Tx, string, string) (authn.User, bool, error) {
	return f.user, f.verified, f.err
}

func (f *fakeUsers) RecordRehash(_ context.Context, _ *postgres.Tx, userID, _ string) error {
	f.rehashed = append(f.rehashed, userID)
	return nil
}

type fakePolicies struct{ policy authn.Policy }

func (f fakePolicies) Policy(context.Context, *postgres.Tx, string) (authn.Policy, error) {
	return f.policy, nil
}

type fakeBrandings struct{ branding Branding }

func (f fakeBrandings) Branding(context.Context, *postgres.Tx, string) (Branding, []Rejection, error) {
	return f.branding, nil, nil
}

// fakeTenant runs the work with a nil transaction, which every fake above
// ignores. The transaction's real behaviour is RLS, and RLS is not something a
// fake can pretend to have — which is why the tenant-scoped reads are also
// tested against a real database.
type fakeTenant struct{ err error }

func (f fakeTenant) WithTenant(ctx context.Context, _ string, fn func(*postgres.Tx) error) error {
	if f.err != nil {
		return f.err
	}
	return fn(nil)
}

type fakeAuditor struct{ events []audit.Event }

func (f *fakeAuditor) Write(_ context.Context, _ *postgres.Tx, e audit.Event) error {
	f.events = append(f.events, e)
	return nil
}

type countingObserver struct{ outcomes []string }

func (c *countingObserver) LoginAttempt(outcome string) { c.outcomes = append(c.outcomes, outcome) }

// --- fixture -----------------------------------------------------------------------

type fixture struct {
	handler   *Handler
	auth      *fakeAuth
	sessions  *fakeSessions
	users     *fakeUsers
	auditor   *fakeAuditor
	observer  *countingObserver
	brandings fakeBrandings
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	auth := &fakeAuth{pending: authorize.Pending{
		ID: testPendingID,
		Request: authorize.Request{
			ClientID:    "client-1",
			RedirectURI: "https://app.example/cb",
			Scope:       []string{"openid"},
			State:       "xyz",
		},
		App: client.Application{
			ID:    "11111111-1111-1111-1111-111111111111",
			OrgID: testOrgID,
			Name:  "Billing",
			Type:  client.TypeWeb,
		},
	}}

	f := &fixture{
		auth:      auth,
		sessions:  &fakeSessions{},
		users:     &fakeUsers{},
		auditor:   &fakeAuditor{},
		observer:  &countingObserver{},
		brandings: fakeBrandings{branding: DefaultBranding},
	}

	f.handler = &Handler{
		Authorization: auth,
		Sessions:      f.sessions,
		Users:         f.users,
		Policies:      fakePolicies{policy: authn.DefaultPolicy},
		Brandings:     f.brandings,
		DB:            fakeTenant{},
		Audit:         f.auditor,
		Observer:      f.observer,
		Policy:        session.DefaultPolicy,
	}
	return f
}

// get renders the form and returns the response plus the CSRF token it minted.
func (f *fixture) get(t *testing.T) (*httptest.ResponseRecorder, string) {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, Path+"?request="+testPendingID, nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	var token string
	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			token = c.Value
		}
	}
	return w, token
}

// post submits the form with a valid CSRF token unless one is given.
func (f *fixture) post(t *testing.T, form url.Values, csrf string) *httptest.ResponseRecorder {
	t.Helper()

	if csrf == "" {
		_, csrf = f.get(t)
	}
	form.Set(csrfField, csrf)
	if form.Get("request") == "" {
		form.Set("request", testPendingID)
	}

	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, csrf)

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func credentials() url.Values {
	return url.Values{"email": {"someone@example.test"}, "password": {"correct horse battery"}}
}

// --- headers ----------------------------------------------------------------------

// DoD item 4: the page cannot be framed, asserted on the headers rather than
// on a comment saying it is unframable.
func TestTheLoginPageCannotBeFramed(t *testing.T) {
	f := newFixture(t)
	w, _ := f.get(t)

	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("the policy does not forbid framing: %q", csp)
	}
}

// The headers are set by the handler as well as by the middleware. On this
// page their absence is exploitable, so it does not depend on a chain somebody
// else maintains staying in the order it is in today.
func TestSecurityHeadersDoNotDependOnMiddleware(t *testing.T) {
	f := newFixture(t)
	w, _ := f.get(t)

	want := map[string]string{
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"Content-Type":           "text/html; charset=utf-8",
	}
	for header, value := range want {
		if got := w.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Error("no Content-Security-Policy was set")
	}
}

// Every response from this handler carries them, not only the happy path.
func TestEveryResponseCarriesTheHeaders(t *testing.T) {
	responses := map[string]func() *httptest.ResponseRecorder{
		"no request": func() *httptest.ResponseRecorder {
			f := newFixture(t)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, Path, nil))
			return w
		},
		"expired request": func() *httptest.ResponseRecorder {
			f := newFixture(t)
			f.auth.peekErr = authorize.ErrPendingNotFound
			w, _ := f.get(t)
			return w
		},
		"failed credentials": func() *httptest.ResponseRecorder {
			f := newFixture(t)
			return f.post(t, credentials(), "")
		},
		"forgotten password": func() *httptest.ResponseRecorder {
			f := newFixture(t)
			w := httptest.NewRecorder()
			f.handler.Forgot(w, httptest.NewRequest(http.MethodGet, ForgotPath, nil))
			return w
		},
	}

	for name, produce := range responses {
		t.Run(name, func(t *testing.T) {
			w := produce()
			if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q", got)
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q", got)
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
				t.Errorf("Content-Security-Policy = %q", w.Header().Get("Content-Security-Policy"))
			}
		})
	}
}

// --- the query string ---------------------------------------------------------------

// Abuse case A-7, and the strongest form of it: not "the value is escaped" but
// "there is no code path that renders a query parameter". A crafted URL can
// therefore do nothing at all.
func TestNothingFromTheQueryStringIsRendered(t *testing.T) {
	f := newFixture(t)

	crafted := url.Values{
		"request":           {testPendingID},
		"error":             {"<script>alert(1)</script>"},
		"error_description": {"MARKER-DESCRIPTION"},
		"state":             {"MARKER-STATE"},
		"redirect_uri":      {"https://attacker.example/MARKER-REDIRECT"},
		"next":              {"MARKER-NEXT"},
	}

	r := httptest.NewRequest(http.MethodGet, Path+"?"+crafted.Encode(), nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	body := w.Body.String()
	for _, marker := range []string{
		"MARKER-DESCRIPTION", "MARKER-STATE", "MARKER-REDIRECT", "MARKER-NEXT",
		"alert(1)", "attacker.example",
	} {
		if strings.Contains(body, marker) {
			t.Errorf("a query parameter reached the page: %q", marker)
		}
	}
	// The control: the page did render, so the assertions above are about a
	// page that exists rather than about an error response.
	if !strings.Contains(body, `name="password"`) {
		t.Fatal("the form did not render; the assertions above prove nothing")
	}
}

// --- CSRF ---------------------------------------------------------------------------

func TestSubmissionRequiresACSRFToken(t *testing.T) {
	cases := map[string]func(*http.Request, url.Values){
		"no cookie, no field": func(*http.Request, url.Values) {},
		"field but no cookie": func(_ *http.Request, form url.Values) {
			form.Set(csrfField, strings.Repeat("a", csrfLength))
		},
		"cookie but no field": func(r *http.Request, _ url.Values) {
			withCSRF(r, strings.Repeat("a", csrfLength))
		},
		"mismatched": func(r *http.Request, form url.Values) {
			withCSRF(r, strings.Repeat("a", csrfLength))
			form.Set(csrfField, strings.Repeat("b", csrfLength))
		},
	}

	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			form := credentials()
			form.Set("request", testPendingID)

			r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(""))
			arrange(r, form)
			r = httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			arrange(r, form)

			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)

			if len(f.sessions.created) != 0 {
				t.Fatal("a session was created despite a failed CSRF check")
			}
			if len(f.auth.resumed) != 0 {
				t.Fatal("the authorization was resumed despite a failed CSRF check")
			}
			if !strings.Contains(w.Body.String(), MsgSessionProblem) {
				t.Errorf("the refusal was not explained:\n%s", w.Body.String())
			}
		})
	}
}

// The control for the tests above: with a matching token the same submission
// gets through. Without this they would all pass against a handler that
// refuses every POST.
func TestAMatchingCSRFTokenIsAccepted(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	w := f.post(t, credentials(), "")

	if len(f.sessions.created) != 1 {
		t.Fatalf("no session was created; the CSRF tests would pass vacuously\n%s", w.Body.String())
	}
}

// A refused submission comes back with a token that works, or the user is
// stuck refusing their own retries.
func TestARefusedSubmissionCanBeRetried(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	form := credentials()
	form.Set("request", testPendingID)
	form.Set(csrfField, strings.Repeat("b", csrfLength))

	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, strings.Repeat("a", csrfLength))

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	var fresh string
	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			fresh = c.Value
		}
	}
	if fresh == "" {
		t.Fatal("no fresh CSRF token was issued, so the retry cannot succeed")
	}
	if !strings.Contains(w.Body.String(), `value="`+fresh+`"`) {
		t.Fatal("the token in the cookie is not the one rendered into the form")
	}

	if retry := f.post(t, credentials(), fresh); len(f.sessions.created) != 1 {
		t.Fatalf("the retry with the fresh token was refused too:\n%s", retry.Body.String())
	}
}

// --- the uniform answer ------------------------------------------------------------

// Abuse cases A-1, A-2 and A-4. The response to a wrong password, an unknown
// address, a locked account and a deactivated one must be the same response.
//
// Compared as whole recorded responses, not as "both mention a problem".
func TestEveryCredentialFailureLooksTheSame(t *testing.T) {
	arrange := map[string]func(*fixture){
		"unknown address": func(f *fixture) {
			f.users.user, f.users.verified = authn.User{}, false
		},
		"wrong password": func(f *fixture) {
			f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
			f.users.verified = false
		},
		"locked account": func(f *fixture) {
			f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusLocked}
			f.users.verified = true
		},
		"deactivated account": func(f *fixture) {
			f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusDeactivated}
			f.users.verified = true
		},
		"invited, no password set": func(f *fixture) {
			f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusInvited}
			f.users.verified = false
		},
		"unusable stored hash": func(f *fixture) {
			f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
			f.users.verified, f.users.err = false, errors.New("the stored hash is corrupt")
		},
	}

	// One CSRF token shared across the cases, so the responses differ only in
	// what the test is varying.
	seed := newFixture(t)
	_, token := seed.get(t)

	type response struct {
		status  int
		headers string
		body    string
	}
	var (
		reference response
		refName   string
	)

	for name, setup := range arrange {
		f := newFixture(t)
		setup(f)

		w := f.post(t, credentials(), token)

		got := response{status: w.Code, headers: headerDump(w), body: w.Body.String()}

		if refName == "" {
			reference, refName = got, name
			continue
		}
		if got.status != reference.status {
			t.Errorf("%q answers %d and %q answers %d; the status distinguishes them",
				name, got.status, refName, reference.status)
		}
		if got.headers != reference.headers {
			t.Errorf("%q and %q differ in their headers:\n%s\n---\n%s",
				name, refName, got.headers, reference.headers)
		}
		if got.body != reference.body {
			t.Errorf("%q and %q differ in their bodies; an attacker can tell which "+
				"addresses have accounts", name, refName)
		}
	}

	if !strings.Contains(reference.body, MsgCredentials) {
		t.Errorf("the shared response does not carry the credential message; the "+
			"comparison above may be over error pages:\n%s", reference.body)
	}
	if reference.status != http.StatusOK {
		t.Errorf("a credential failure answers %d; re-rendering the form is a 200",
			reference.status)
	}
}

// headerDump renders every header in a stable order, so two responses can be
// compared including headers a hand-written list would forget — Set-Cookie and
// Content-Length among them.
func headerDump(w *httptest.ResponseRecorder) string {
	result := w.Result()
	names := make([]string, 0, len(result.Header))
	for name := range result.Header {
		names = append(names, name)
	}
	sortStrings(names)

	var out strings.Builder
	for _, name := range names {
		for _, value := range result.Header[name] {
			out.WriteString(name)
			out.WriteString(": ")
			out.WriteString(value)
			out.WriteString("\n")
		}
	}
	return out.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// The control for the test above. If the handler answered every submission
// identically — including a correct one — the uniformity test would pass and
// mean nothing.
func TestASuccessfulLoginIsDistinguishableFromAFailure(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	w := f.post(t, credentials(), "")

	if w.Code != http.StatusFound {
		t.Fatalf("a successful login answered %d, want a redirect back to the client", w.Code)
	}
	if len(f.auth.resumed) != 1 || f.auth.resumed[0] != testPendingID {
		t.Errorf("the pending request was not resumed: %v", f.auth.resumed)
	}
}

// An expired password is a different message, and may be: by the time it is
// shown the password was correct, so the reader has already proved they own
// the account.
func TestAnExpiredPasswordIsRefusedWithItsOwnMessage(t *testing.T) {
	f := newFixture(t)
	long := time.Now().Add(-365 * 24 * time.Hour)
	f.users.user = authn.User{
		ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive,
		PasswordChangedAt: &long,
	}
	f.users.verified = true

	w := f.post(t, credentials(), "")

	// Escaped, because the message contains an apostrophe and html/template
	// writes it as &#39;. Comparing against the raw constant fails on a page
	// that is rendering it perfectly well.
	if !strings.Contains(w.Body.String(), template.HTMLEscapeString(MsgPasswordExpired)) {
		t.Errorf("an expired password was not reported:\n%s", w.Body.String())
	}
	if len(f.sessions.created) != 0 {
		t.Error("a session was created for an expired password")
	}
	if len(f.auth.resumed) != 0 {
		t.Error("the authorization was resumed with an expired password")
	}
}

// PG-13's rule: a password whose change was never recorded is NOT expired.
// Treating unknown as infinitely old would make deploying the migration a mass
// lockout — an outage wearing a security control's clothes.
func TestANeverRecordedPasswordChangeIsNotExpired(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	if w := f.post(t, credentials(), ""); w.Code != http.StatusFound {
		t.Fatalf("a user with no password_changed_at was refused: %d\n%s", w.Code, w.Body.String())
	}
}

// --- empty fields ---------------------------------------------------------------

// Refused before any lookup, so an empty submission is not a free probe — and
// refused at field level, because nothing about the account was consulted and
// there is therefore nothing to conceal.
func TestEmptyFieldsAreRefusedWithoutALookup(t *testing.T) {
	cases := map[string]url.Values{
		"no email":    {"email": {""}, "password": {"something"}},
		"no password": {"email": {"someone@example.test"}, "password": {""}},
		"neither":     {"email": {""}, "password": {""}},
	}

	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
			f.users.verified = true

			w := f.post(t, form, "")

			if len(f.sessions.created) != 0 {
				t.Fatal("an empty submission created a session")
			}
			if len(f.auditor.events) != 0 {
				t.Errorf("an empty submission reached the audit log: %v", f.auditor.events)
			}
			if !strings.Contains(w.Body.String(), "aria-invalid") {
				t.Error("no field-level error was shown")
			}
		})
	}
}

// --- the session ------------------------------------------------------------------

// P1-11 requires a session to record the factor actually used, because P1-07's
// `amr` claim is built from it and Phase 3's step-up authentication reads that
// claim. A session created without one would make the claim a lie.
func TestTheSessionRecordsThePasswordFactor(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	f.post(t, credentials(), "")

	if len(f.sessions.created) != 1 {
		t.Fatal("no session was created")
	}
	created := f.sessions.created[0]
	if len(created.AuthMethods) != 1 || created.AuthMethods[0] != "pwd" {
		t.Errorf("auth_methods = %v, want [pwd]", created.AuthMethods)
	}
	if created.UserID != testUserID || created.OrgID != testOrgID {
		t.Errorf("the session was created for %s in %s", created.UserID, created.OrgID)
	}
}

// prompt=login sends a user here with a live session. The cookie is about to
// be replaced, so what it pointed at becomes unreachable — and an unreachable
// LIVE session still appears on the sessions screen and still authorises a
// refresh token. session.ReasonReauth exists for exactly this.
func TestReauthenticationReplacesTheSessionItSupersedes(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true
	f.sessions.hasCurrent = true
	f.sessions.current = session.Session{ID: "old-session", UserID: testUserID, OrgID: testOrgID}

	r := httptest.NewRequest(http.MethodPost, Path, nil)
	_, token := f.get(t)
	form := credentials()
	form.Set("request", testPendingID)
	form.Set(csrfField, token)
	r = httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, token)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: strings.Repeat("a", 43)})

	f.handler.ServeHTTP(httptest.NewRecorder(), r)

	if len(f.sessions.revoked) != 1 || f.sessions.revoked[0] != "old-session" {
		t.Errorf("the superseded session was not revoked: %v", f.sessions.revoked)
	}
	if len(f.sessions.created) != 1 {
		t.Error("no replacement session was created")
	}
}

// Somebody else's live session in the same browser must not be revoked by this
// person signing in. The two are different accounts and only one of them asked
// for anything.
func TestAnotherUsersSessionIsNotRevoked(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true
	f.sessions.hasCurrent = true
	f.sessions.current = session.Session{ID: "someone-else", UserID: "99999999-9999-9999-9999-999999999999", OrgID: testOrgID}

	_, token := f.get(t)
	form := credentials()
	form.Set("request", testPendingID)
	form.Set(csrfField, token)
	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, token)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: strings.Repeat("a", 43)})

	f.handler.ServeHTTP(httptest.NewRecorder(), r)

	if len(f.sessions.revoked) != 0 {
		t.Errorf("another user's session was revoked: %v", f.sessions.revoked)
	}
}

// Rehash-on-login is the only moment the plaintext and the stored hash are
// both available, which is why P1-01's NeedsRehash has had no caller until now.
func TestAWeakHashIsUpgradedOnLogin(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{
		ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive, NeedsRehash: true,
	}
	f.users.verified = true

	f.post(t, credentials(), "")

	if len(f.users.rehashed) != 1 {
		t.Error("a hash below current cost was not upgraded")
	}
}

func TestAHashAtCurrentCostIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	f.post(t, credentials(), "")

	if len(f.users.rehashed) != 0 {
		t.Error("a hash at current cost was rewritten for no reason")
	}
}

// --- audit and metrics ---------------------------------------------------------------

// The submitted address is deliberately absent from a failure event. Including
// it would write every mistyped and every probed address into an append-only
// table with 24-month retention — the artefact an attacker who reaches the log
// most wants.
func TestAFailedLoginDoesNotRecordTheAddressTried(t *testing.T) {
	f := newFixture(t)
	f.users.user, f.users.verified = authn.User{}, false

	f.post(t, credentials(), "")

	if len(f.auditor.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(f.auditor.events))
	}
	event := f.auditor.events[0]
	if event.Type != audit.EventLoginFailed {
		t.Errorf("event type = %q", event.Type)
	}
	for key, value := range event.Payload {
		if s, ok := value.(string); ok && strings.Contains(s, "someone@example.test") {
			t.Errorf("the submitted address was written to the audit log under %q", key)
		}
	}
	if event.Payload["reason"] == nil {
		t.Error("no reason class was recorded, so the log cannot support an investigation")
	}
}

// The audit log is not the response body, so it MAY distinguish a locked
// account from a wrong password — and should, because that is what an
// operator investigating an account needs.
func TestTheAuditLogDistinguishesWhatTheBrowserCannot(t *testing.T) {
	reasons := map[string]struct {
		user     authn.User
		verified bool
		want     string
	}{
		"unknown address": {authn.User{}, false, "credentials"},
		"wrong password":  {authn.User{ID: testUserID, Status: authn.StatusActive}, false, "wrong_password"},
		"locked":          {authn.User{ID: testUserID, Status: authn.StatusLocked}, true, "account_locked"},
	}

	for name, c := range reasons {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.users.user, f.users.verified = c.user, c.verified

			f.post(t, credentials(), "")

			if len(f.auditor.events) != 1 {
				t.Fatalf("expected one event, got %d", len(f.auditor.events))
			}
			if got := f.auditor.events[0].Payload["reason"]; got != c.want {
				t.Errorf("reason = %v, want %q", got, c.want)
			}
		})
	}
}

func TestASuccessfulLoginIsAudited(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true

	f.post(t, credentials(), "")

	// One event here, not two: session.created is written by session.Manager
	// inside Create, which is faked in this package. P1-11 tests that one
	// against a real database.
	if len(f.auditor.events) != 1 {
		t.Fatalf("expected the login event, got %d", len(f.auditor.events))
	}
	var found bool
	for _, e := range f.auditor.events {
		if e.Type == audit.EventLoginSucceeded {
			found = true
			if e.ActorUserID != testUserID {
				t.Errorf("the successful login names actor %q", e.ActorUserID)
			}
		}
	}
	if !found {
		t.Error("no user.login.success event was written")
	}
}

// The metric label is coarse for the same reason the page is: a metric that
// separated an unknown address from a wrong password would move the
// enumeration disclosure into /metrics.
func TestTheMetricDoesNotDistinguishFailureKinds(t *testing.T) {
	for _, arrange := range []func(*fixture){
		func(f *fixture) { f.users.user, f.users.verified = authn.User{}, false },
		func(f *fixture) {
			f.users.user = authn.User{ID: testUserID, Status: authn.StatusLocked}
			f.users.verified = true
		},
	} {
		f := newFixture(t)
		arrange(f)
		f.post(t, credentials(), "")

		if len(f.observer.outcomes) == 0 {
			t.Fatal("nothing was counted")
		}
		last := f.observer.outcomes[len(f.observer.outcomes)-1]
		if last != OutcomeFailed {
			t.Errorf("outcome = %q, want %q", last, OutcomeFailed)
		}
	}
}

// --- the pending request ---------------------------------------------------------------

// Rendering the form must not consume the pending request, or a refresh, a
// back button or a mistyped password would each destroy the flow.
func TestRenderingDoesNotConsumeThePendingRequest(t *testing.T) {
	f := newFixture(t)

	for i := range 3 {
		w, _ := f.get(t)
		if w.Code != http.StatusOK {
			t.Fatalf("render %d answered %d; the request was consumed by an earlier one", i+1, w.Code)
		}
	}
	if len(f.auth.resumed) != 0 {
		t.Error("rendering the form resumed the authorization")
	}
}

// The login page has no meaning outside an authorization flow, and there is no
// way to find out where the person meant to go — that is the whole point of
// the opaque reference. So it explains rather than redirecting anywhere.
func TestReachedWithNoRequest(t *testing.T) {
	f := newFixture(t)

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, Path, nil))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if w.Header().Get("Location") != "" {
		t.Errorf("it redirected to %q; there is nowhere it could know to send anyone",
			w.Header().Get("Location"))
	}
	if strings.Contains(w.Body.String(), `name="password"`) {
		t.Error("a password field was rendered for a flow that does not exist")
	}
}

// A crafted id must not reach Redis as a key, and must not reach the template.
func TestAMalformedRequestIdIsRefusedBeforeAnyLookup(t *testing.T) {
	for _, id := range []string{
		"", "short", strings.Repeat("a", 44), "../../etc/passwd",
		"has spaces in it aaaaaaaaaaaaaaaaaaaaaaaaa", "<script>aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		f := newFixture(t)
		f.auth.peekErr = errors.New("Peek must not have been called")

		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, Path+"?request="+url.QueryEscape(id), nil))

		if w.Code != http.StatusBadRequest {
			t.Errorf("request=%q answered %d, want 400", id, w.Code)
		}
		if strings.Contains(w.Body.String(), "<script>") {
			t.Errorf("request=%q reached the page", id)
		}
	}
}

// An expired or already-completed request gets a page that explains and offers
// nothing onward. There is nothing to offer: without a request there is no
// redirect URI, and inventing one would be the open redirect the whole
// endpoint is built to avoid.
func TestAnExpiredRequestIsExplained(t *testing.T) {
	f := newFixture(t)
	f.auth.peekErr = authorize.ErrPendingNotFound

	w, _ := f.get(t)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if w.Header().Get("Location") != "" {
		t.Errorf("it redirected to %q", w.Header().Get("Location"))
	}
	if !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("the page does not explain what happened:\n%s", w.Body.String())
	}
}

// --- branding -------------------------------------------------------------------------

func TestOrganizationBrandingReachesThePage(t *testing.T) {
	f := newFixture(t)
	f.handler.Brandings = fakeBrandings{branding: Branding{
		AccentColor: "#046c4e",
		LogoURL:     "https://cdn.example/logo.svg",
	}}

	w, _ := f.get(t)
	body := w.Body.String()

	if !strings.Contains(body, "--color-accent:#046c4e") {
		t.Error("the organization's accent was not applied")
	}
	if !strings.Contains(body, `src="https://cdn.example/logo.svg"`) {
		t.Error("the organization's logo was not rendered")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "img-src https:") {
		t.Error("the policy would block the logo it just rendered")
	}
	// The alt is empty on purpose: the logo repeats the application name in
	// the heading beside it, and a screen reader announcing both reads the
	// organization's name twice.
	if !strings.Contains(body, `alt=""`) {
		t.Error("the logo has no alt attribute")
	}
}

// --- method routing -------------------------------------------------------------------

func TestUnsupportedMethods(t *testing.T) {
	f := newFixture(t)

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, Path, nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, POST" {
		t.Errorf("Allow = %q", got)
	}
}

// FR-9's entry point exists and says something true. P1-19.4 owns the flow
// behind it; a reset page that appears to work and does not would be worse
// than an honest dead end, because the person waiting for an email never asks
// anybody for help.
func TestTheForgottenPasswordPageIsHonest(t *testing.T) {
	f := newFixture(t)

	w := httptest.NewRecorder()
	f.handler.Forgot(w,
		httptest.NewRequest(http.MethodGet, ForgotPath+"?request="+testPendingID, nil))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "not available yet") {
		t.Errorf("the page does not say the flow is unbuilt:\n%s", body)
	}
	if strings.Contains(body, "<form") {
		t.Error("the page offers a form that cannot do anything")
	}
	if !strings.Contains(body, Path+"?request="+testPendingID) {
		t.Error("there is no way back to the sign-in the user came from")
	}
}

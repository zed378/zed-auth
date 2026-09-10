package login

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The decision this endpoint exists to get right is in shouldActWithoutAsking:
// a GET logs somebody out only when the request proves it came from a party
// that already holds a token for THAT session. Most of what follows is that
// rule, seen from each direction it can be got wrong.

const (
	logoutSessionID = "33333333-3333-3333-3333-333333333333"
	otherSessionID  = "77777777-7777-7777-7777-777777777777"
	logoutRedirect  = "https://app.example/after-logout"
)

// --- fakes ---------------------------------------------------------------------

type logoutSessions struct {
	current    session.Session
	has        bool
	revoked    []string
	revokedAll []string
	err        error
}

func (s *logoutSessions) Lookup(context.Context, string, session.Policy, time.Time) (session.Session, error) {
	if !s.has {
		return session.Session{}, session.ErrNotFound
	}
	return s.current, nil
}

func (s *logoutSessions) Revoke(
	_ context.Context, _ *postgres.Tx, sessionID string, _ session.Reason, _ string, _ time.Time,
) (func(context.Context) error, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.revoked = append(s.revoked, sessionID)
	return func(context.Context) error { return nil }, nil
}

func (s *logoutSessions) RevokeAllForUser(
	_ context.Context, _ *postgres.Tx, userID, _, _ string, _ time.Time,
) (func(context.Context) error, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.revokedAll = append(s.revokedAll, userID)
	return func(context.Context) error { return nil }, nil
}

type logoutRefresh struct{ users []string }

func (r *logoutRefresh) RevokeAllForUser(_ context.Context, _ *postgres.Tx, userID string) (int64, error) {
	r.users = append(r.users, userID)
	return 1, nil
}

type logoutClients struct {
	app client.Application
	err error
}

func (c logoutClients) ByClientID(context.Context, string) (client.Application, error) {
	return c.app, c.err
}

type logoutVerifier struct {
	claims map[string]any
	err    error
	asked  string
}

func (v *logoutVerifier) Verify(_, wantType string) ([]byte, error) {
	v.asked = wantType
	if v.err != nil {
		return nil, v.err
	}
	raw, _ := json.Marshal(v.claims)
	return raw, nil
}

type logoutObserverFake struct{ seen []string }

func (o *logoutObserverFake) Logout(outcome string) { o.seen = append(o.seen, outcome) }

// --- fixture -----------------------------------------------------------------------

type logoutFixture struct {
	handler  *LogoutHandler
	sessions *logoutSessions
	refresh  *logoutRefresh
	clients  logoutClients
	verifier *logoutVerifier
	auditor  *fakeAuditor
	observer *logoutObserverFake
}

func logoutApp() client.Application {
	return client.Application{
		ID:                     "11111111-1111-1111-1111-111111111111",
		OrgID:                  testOrgID,
		Name:                   "Billing",
		Type:                   client.TypeWeb,
		PostLogoutRedirectURIs: []string{logoutRedirect},
	}
}

func liveLogoutSession() session.Session {
	return session.Session{ID: logoutSessionID, UserID: testUserID, OrgID: testOrgID}
}

func matchingHint() map[string]any {
	return map[string]any{
		"iss": "https://auth.example",
		"aud": logoutApp().ID,
		"sub": testUserID,
		"sid": logoutSessionID,
	}
}

func newLogoutFixture(t *testing.T) *logoutFixture {
	t.Helper()

	f := &logoutFixture{
		sessions: &logoutSessions{current: liveLogoutSession(), has: true},
		refresh:  &logoutRefresh{},
		clients:  logoutClients{app: logoutApp()},
		verifier: &logoutVerifier{claims: matchingHint()},
		auditor:  &fakeAuditor{},
		observer: &logoutObserverFake{},
	}

	f.handler = &LogoutHandler{
		Issuer:    "https://auth.example",
		Clients:   f.clients,
		Sessions:  f.sessions,
		Refresh:   f.refresh,
		Verifier:  f.verifier,
		Brandings: fakeBrandings{branding: DefaultBranding},
		DB:        fakeTenant{},
		Audit:     f.auditor,
		Observer:  f.observer,
		Policy:    session.DefaultPolicy,
	}
	return f
}

// get issues a GET, with a session cookie unless told otherwise.
func (f *logoutFixture) get(t *testing.T, query url.Values, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, LogoutPath+"?"+query.Encode(), nil)
	if withCookie {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: strings.Repeat("a", 43)})
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func hinted() url.Values {
	return url.Values{"id_token_hint": {"a.signed.hint"}}
}

// --- when a GET may act ---------------------------------------------------------

// The one case that acts: a hint that verifies, for this very session.
func TestAMatchingHintLogsOutWithoutAsking(t *testing.T) {
	f := newLogoutFixture(t)

	w := f.get(t, hinted(), true)

	if len(f.sessions.revoked) != 1 || f.sessions.revoked[0] != logoutSessionID {
		t.Fatalf("the session was not revoked: %v\n%s", f.sessions.revoked, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "<form") {
		t.Error("a provable request was still asked to confirm")
	}

	// The cookie is cleared as WELL as revoked. Clearing alone is not logout:
	// a cookie the user already copied still works if the row survives.
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the session cookie was not cleared")
	}
}

// Abuse case A-1 and A-2, in every shape it arrives in. A GET that cannot
// prove itself must render the interstitial and revoke NOTHING.
func TestAGetThatCannotProveItselfNeverRevokes(t *testing.T) {
	cases := map[string]func(*logoutFixture) (url.Values, bool){
		"no hint at all — a crafted URL or an <img src>": func(*logoutFixture) (url.Values, bool) {
			return url.Values{}, true
		},
		"a hint that does not verify": func(f *logoutFixture) (url.Values, bool) {
			f.verifier.err = signing.ErrInvalidSignature
			return hinted(), true
		},
		"a hint of the wrong token type": func(f *logoutFixture) (url.Values, bool) {
			f.verifier.err = signing.ErrWrongType
			return hinted(), true
		},
		"a hint from another issuer": func(f *logoutFixture) (url.Values, bool) {
			claims := matchingHint()
			claims["iss"] = "https://other.example"
			f.verifier.claims = claims
			return hinted(), true
		},
		"a hint for another session": func(f *logoutFixture) (url.Values, bool) {
			claims := matchingHint()
			claims["sid"] = otherSessionID
			f.verifier.claims = claims
			return hinted(), true
		},
		"a hint for another user": func(f *logoutFixture) (url.Values, bool) {
			claims := matchingHint()
			claims["sub"] = "99999999-9999-9999-9999-999999999999"
			f.verifier.claims = claims
			return hinted(), true
		},
		"a hint with no sid": func(f *logoutFixture) (url.Values, bool) {
			claims := matchingHint()
			delete(claims, "sid")
			f.verifier.claims = claims
			return hinted(), true
		},
		"a matching hint but no session cookie": func(*logoutFixture) (url.Values, bool) {
			return hinted(), false
		},
	}

	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f := newLogoutFixture(t)
			query, withCookie := arrange(f)

			w := f.get(t, query, withCookie)

			if len(f.sessions.revoked) != 0 || len(f.sessions.revokedAll) != 0 {
				t.Fatalf("a GET that could not prove itself revoked something: %v %v",
					f.sessions.revoked, f.sessions.revokedAll)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want the interstitial", w.Code)
			}
			if !strings.Contains(w.Body.String(), "<form") {
				t.Errorf("no confirmation was shown:\n%s", w.Body.String())
			}
		})
	}
}

// A hint for someone else's session logs nobody out — not its own subject
// either. It falls through to the interstitial, which acts on the COOKIE's
// session and only with a click.
func TestAHintForAnotherSessionCannotForceItsOwnersLogout(t *testing.T) {
	f := newLogoutFixture(t)
	claims := matchingHint()
	claims["sid"] = otherSessionID
	f.verifier.claims = claims

	f.get(t, hinted(), true)

	for _, revoked := range f.sessions.revoked {
		if revoked == otherSessionID {
			t.Error("a crafted hint logged out the session it named")
		}
	}
	if len(f.sessions.revoked) != 0 {
		t.Errorf("something was revoked: %v", f.sessions.revoked)
	}
}

// The hint is an id_token, not an access token. Presenting an access token
// here would be the substitution abuse case arriving at a third endpoint.
func TestTheHintIsVerifiedAsAnIDToken(t *testing.T) {
	f := newLogoutFixture(t)

	f.get(t, hinted(), true)

	if f.verifier.asked != signing.TypeJWT {
		t.Errorf("the hint was verified as typ %q, want %q", f.verifier.asked, signing.TypeJWT)
	}
}

// --- the redirect target ------------------------------------------------------------

// Abuse case A-5, and the same branch P1-06 calls the most important in its
// file: an unregistered address is refused with NO redirect, because reporting
// the error by redirecting to it is the vulnerability itself.
func TestAnUnregisteredPostLogoutRedirectIsRefusedWithoutRedirecting(t *testing.T) {
	f := newLogoutFixture(t)

	w := f.get(t, url.Values{
		"client_id":                {logoutApp().ID},
		"post_logout_redirect_uri": {"https://attacker.example/steal"},
	}, true)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if location := w.Header().Get("Location"); location != "" {
		t.Errorf("it redirected to %q", location)
	}
	if strings.Contains(w.Body.String(), "attacker.example") {
		t.Error("the refused address was echoed onto the page")
	}
	if len(f.sessions.revoked) != 0 {
		t.Error("a refused request still logged the user out")
	}
}

// Exact matching. A prefix, a trailing slash, a different scheme and an
// appended query are all different addresses.
func TestPostLogoutMatchingIsExact(t *testing.T) {
	for _, presented := range []string{
		logoutRedirect + "/",
		logoutRedirect + "?next=x",
		logoutRedirect + "/../evil",
		strings.Replace(logoutRedirect, "https", "http", 1),
		strings.ToUpper(logoutRedirect),
		logoutRedirect + "#fragment",
	} {
		f := newLogoutFixture(t)

		w := f.get(t, url.Values{
			"client_id":                {logoutApp().ID},
			"post_logout_redirect_uri": {presented},
		}, true)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%q was accepted as a match", presented)
		}
	}
}

// The control: the registered address IS accepted, or the test above would
// pass against a handler that refuses everything.
func TestTheRegisteredPostLogoutRedirectIsAccepted(t *testing.T) {
	f := newLogoutFixture(t)

	query := hinted()
	query.Set("post_logout_redirect_uri", logoutRedirect)
	query.Set("state", "opaque-state")

	w := f.get(t, query, true)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want a redirect:\n%s", w.Code, w.Body.String())
	}
	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing the redirect: %v", err)
	}
	if target.Scheme+"://"+target.Host+target.Path != logoutRedirect {
		t.Errorf("redirected to %q", target.String())
	}
	if got := target.Query().Get("state"); got != "opaque-state" {
		t.Errorf("state = %q, want it echoed unchanged", got)
	}
}

// Abuse case A-6: an unknown client and an unregistered URI answer identically,
// so the endpoint does not say which client ids exist.
func TestAnUnknownClientAndAnUnregisteredURIAnswerIdentically(t *testing.T) {
	unknown := newLogoutFixture(t)
	unknown.handler.Clients = logoutClients{err: errors.New("no such client")}
	a := unknown.get(t, url.Values{
		"client_id":                {"22222222-2222-2222-2222-222222222222"},
		"post_logout_redirect_uri": {logoutRedirect},
	}, true)

	unregistered := newLogoutFixture(t)
	b := unregistered.get(t, url.Values{
		"client_id":                {logoutApp().ID},
		"post_logout_redirect_uri": {"https://attacker.example/steal"},
	}, true)

	if a.Code != b.Code {
		t.Errorf("statuses differ: %d and %d", a.Code, b.Code)
	}
	if a.Body.String() != b.Body.String() {
		t.Error("an unknown client is distinguishable from an unregistered address")
	}
}

// --- the interstitial ------------------------------------------------------------------

func TestTheInterstitialCarriesTheLoginPagesHeaders(t *testing.T) {
	f := newLogoutFixture(t)

	w := f.get(t, url.Values{}, true)

	want := map[string]string{
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	}
	for header, value := range want {
		if got := w.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'none'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("the policy is missing %q: %s", directive, csp)
		}
	}
}

func TestTheInterstitialHasNoScript(t *testing.T) {
	f := newLogoutFixture(t)

	body := f.get(t, url.Values{}, true).Body.String()

	if strings.Contains(strings.ToLower(body), "<script") {
		t.Error("the confirmation page contains a script element")
	}
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Error("the confirmation has no CSRF token")
	}
	if !strings.Contains(body, `name="everywhere"`) {
		t.Error("there is no 'sign out everywhere' control; docs/PLAN/05 asks for one")
	}
}

// Unchecked by default: signing out of one application should not silently end
// a user's session in every other one.
func TestSignOutEverywhereIsNotTheDefault(t *testing.T) {
	f := newLogoutFixture(t)

	body := f.get(t, url.Values{}, true).Body.String()

	field := body[strings.Index(body, `name="everywhere"`):]
	field = field[:strings.Index(field, ">")]
	if strings.Contains(field, "checked") {
		t.Errorf("the everywhere checkbox is pre-checked: %s", field)
	}
}

// --- the confirmation POST ---------------------------------------------------------------

// post submits the interstitial, taking a CSRF token from a prior render.
func (f *logoutFixture) post(t *testing.T, form url.Values, csrf string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()

	if csrf == "" {
		rendered := f.get(t, url.Values{}, withCookie)
		for _, c := range rendered.Result().Cookies() {
			if c.Name == CSRFCookieName {
				csrf = c.Value
			}
		}
	}
	form.Set(csrfField, csrf)

	r := httptest.NewRequest(http.MethodPost, LogoutPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, csrf)
	if withCookie {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: strings.Repeat("a", 43)})
	}

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func TestConfirmingLogsOut(t *testing.T) {
	f := newLogoutFixture(t)

	f.post(t, url.Values{}, "", true)

	if len(f.sessions.revoked) != 1 {
		t.Fatalf("the session was not revoked: %v", f.sessions.revoked)
	}
	if len(f.sessions.revokedAll) != 0 {
		t.Error("a plain confirmation ended every session")
	}
}

// Step 4: everywhere means every session AND the refresh tokens. Without the
// second half an application holding one mints a fresh access token minutes
// later, which is not what pressing the button means.
func TestSignOutEverywhereEndsSessionsAndRefreshTokens(t *testing.T) {
	f := newLogoutFixture(t)

	f.post(t, url.Values{"everywhere": {"1"}}, "", true)

	if len(f.sessions.revokedAll) != 1 || f.sessions.revokedAll[0] != testUserID {
		t.Errorf("sessions were not all revoked: %v", f.sessions.revokedAll)
	}
	if len(f.refresh.users) != 1 || f.refresh.users[0] != testUserID {
		t.Errorf("the refresh tokens were not revoked: %v", f.refresh.users)
	}
}

// Abuse case A-7.
func TestTheConfirmationRequiresACSRFToken(t *testing.T) {
	for name, csrf := range map[string]string{
		"no token":     "",
		"wrong token":  strings.Repeat("b", 43),
		"short token":  "too-short",
		"random bytes": strings.Repeat("c", 43),
	} {
		t.Run(name, func(t *testing.T) {
			f := newLogoutFixture(t)

			form := url.Values{csrfField: {csrf}}
			r := httptest.NewRequest(http.MethodPost, LogoutPath, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: strings.Repeat("a", 43)})
			withCSRF(r, strings.Repeat("a", 43))

			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)

			if len(f.sessions.revoked) != 0 {
				t.Fatal("a submission with a bad CSRF token logged the user out")
			}
			if !strings.Contains(w.Body.String(), "<form") {
				t.Errorf("the form did not come back:\n%s", w.Body.String())
			}
		})
	}
}

// The control for the test above: with a matching token the same submission
// works, or those assertions would pass against a handler that refuses every
// POST.
func TestAMatchingCSRFTokenLogsOut(t *testing.T) {
	f := newLogoutFixture(t)

	f.post(t, url.Values{}, "", true)

	if len(f.sessions.revoked) != 1 {
		t.Fatal("no session was revoked; the CSRF tests prove nothing")
	}
}

// Logging out twice is not an error. A logout link clicked again, or clicked
// after the session expired, should still take the user where the application
// said to send them.
func TestLoggingOutWithNoSessionStillRedirects(t *testing.T) {
	f := newLogoutFixture(t)
	f.sessions.has = false

	w := f.post(t, url.Values{
		"client_id":                {logoutApp().ID},
		"post_logout_redirect_uri": {logoutRedirect},
	}, "", false)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want a redirect:\n%s", w.Code, w.Body.String())
	}
}

// The parameters travel through the interstitial in hidden fields, so the POST
// re-validates them. A user who edits the field gets the same refusal a
// crafted GET does.
func TestTheConfirmationRevalidatesTheRedirectTarget(t *testing.T) {
	f := newLogoutFixture(t)

	w := f.post(t, url.Values{
		"client_id":                {logoutApp().ID},
		"post_logout_redirect_uri": {"https://attacker.example/steal"},
	}, "", true)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d; an edited hidden field was honoured", w.Code)
	}
	if w.Header().Get("Location") != "" {
		t.Errorf("it redirected to %q", w.Header().Get("Location"))
	}
	if len(f.sessions.revoked) != 0 {
		t.Error("a refused confirmation still logged the user out")
	}
}

// --- audit and failure -----------------------------------------------------------------

func TestALogoutIsAuditedWithItsScope(t *testing.T) {
	for scope, form := range map[string]url.Values{
		"session": {},
		"all":     {"everywhere": {"1"}},
	} {
		t.Run(scope, func(t *testing.T) {
			f := newLogoutFixture(t)

			f.post(t, form, "", true)

			if len(f.auditor.events) != 1 {
				t.Fatalf("expected one audit event, got %d", len(f.auditor.events))
			}
			event := f.auditor.events[0]
			if event.Type != audit.EventLogout {
				t.Errorf("event type = %q", event.Type)
			}
			if event.Payload["scope"] != scope {
				t.Errorf("scope = %v, want %q", event.Payload["scope"], scope)
			}
			// The session id is safe to name — PG-14 separated it from the
			// cookie. The cookie is not.
			if event.Payload["session_id"] != logoutSessionID {
				t.Errorf("session_id = %v", event.Payload["session_id"])
			}
			for key, value := range event.Payload {
				if s, ok := value.(string); ok && strings.Contains(s, strings.Repeat("a", 43)) {
					t.Errorf("the cookie was written to the audit log under %q", key)
				}
			}
		})
	}
}

// The user must not be told they are signed out when they are not.
func TestAFailedRevocationDoesNotClaimSuccess(t *testing.T) {
	f := newLogoutFixture(t)
	f.sessions.err = errors.New("the database is unavailable")

	w := f.post(t, url.Values{}, "", true)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "you have been signed out") {
		t.Error("the page claims the user is signed out after a failed revocation")
	}
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if cleared {
		t.Error("the cookie was cleared even though the session survived, which " +
			"leaves a live session nobody can reach or revoke")
	}
}

func TestLogoutRefusesOtherMethods(t *testing.T) {
	f := newLogoutFixture(t)

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, LogoutPath, nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// Parameter pollution: a validator that reads one value while a consumer reads
// another is how a bypass gets in. Duplicates are refused by taking neither.
func TestDuplicateParametersAreNotHonoured(t *testing.T) {
	f := newLogoutFixture(t)

	raw := LogoutPath + "?client_id=" + logoutApp().ID +
		"&post_logout_redirect_uri=" + url.QueryEscape(logoutRedirect) +
		"&post_logout_redirect_uri=" + url.QueryEscape("https://attacker.example/steal")

	r := httptest.NewRequest(http.MethodGet, raw, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	if location := w.Header().Get("Location"); strings.Contains(location, "attacker.example") {
		t.Fatalf("a duplicated parameter reached the redirect: %q", location)
	}
	// Neither value is taken, so there is no redirect target at all — the
	// interstitial renders and the confirmation lands on the signed-out page.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}

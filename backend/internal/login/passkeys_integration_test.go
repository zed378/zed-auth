//go:build integration

// Registering a passkey on the hosted page (P3-10, PG-43), against real
// Postgres and Redis and the real relying-party library, with a software
// authenticator that produces genuinely signed credentials.
package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/testsupport/webauthntest"
)

const (
	rpOrigin = "https://auth.example.test"
	rpID     = "auth.example.test"
)

// withPasskeys wires the page the way cmd/authservice does.
func (s *stack) withPasskeys(t *testing.T) {
	t.Helper()

	rp, err := mfa.NewWebAuthn(rpOrigin, "Auth Service")
	if err != nil {
		t.Fatalf("NewWebAuthn: %v", err)
	}
	clients := client.NewStore(audit.NewWriter(s.db, discard(), nil))
	s.login.PasskeyRegistration = &PasskeyRegistration{
		Ceremony: &mfa.WebAuthnVerifier{Store: mfa.NewWebAuthnStore(), DB: s.db, RP: rp, Log: discard()},
		Recovery: mfa.NewRecoveryStore(),
		States:   &RedisPasskeyStates{Client: s.rdb},
		Clients: func(ctx context.Context, id string) (client.Application, error) {
			return clients.ByClientID(ctx, s.db, id)
		},
	}
}

var optionsPattern = regexp.MustCompile(`(?s)<script type="application/json" id="passkey-register-options">(.*?)</script>`)

type passkeyPage struct {
	status  int
	body    string
	options []byte
	cookies []*http.Cookie
	csrf    string
}

func (s *stack) openPasskeys(t *testing.T, query string, cookies ...*http.Cookie) passkeyPage {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, PasskeysPath+query, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.login.Passkeys(w, r)

	page := passkeyPage{status: w.Code, body: w.Body.String(), cookies: w.Result().Cookies()}
	if m := optionsPattern.FindStringSubmatch(page.body); m != nil {
		page.options = []byte(m[1])
	}
	for _, c := range page.cookies {
		if c.Name == CSRFCookieName {
			page.csrf = c.Value
		}
	}
	return page
}

func (s *stack) submitPasskey(t *testing.T, page passkeyPage, credential []byte, sessionCookie *http.Cookie, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"credential": {string(credential)}, "label": {"work laptop"}}
	if withCSRF {
		form.Set("csrf_token", page.csrf)
	}
	r := httptest.NewRequest(http.MethodPost, PasskeysPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sessionCookie != nil {
		r.AddCookie(sessionCookie)
	}
	for _, c := range page.cookies {
		if c.Name == CSRFCookieName && !withCSRF {
			continue
		}
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	w := httptest.NewRecorder()
	s.login.Passkeys(w, r)
	return w
}

func (s *stack) activeFactors(t *testing.T, kind string) int {
	t.Helper()
	var n int
	s.factory.QueryRow(&n,
		`SELECT count(*) FROM user_mfa_factors WHERE user_id = $1 AND type = $2 AND status = 'active'`, s.userID, kind)
	return n
}

// --- the whole registration ------------------------------------------------------------------

func TestAPasskeyIsRegisteredOnTheHostedPage(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	cookie := s.signIn(t)

	page := s.openPasskeys(t, "", cookie)
	if page.status != http.StatusOK || page.options == nil {
		t.Fatalf("the page did not render a ceremony (%d):\n%s", page.status, page.body)
	}

	authenticator := webauthntest.New(t)
	credential := authenticator.Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, page.options))

	w := s.submitPasskey(t, page, credential, cookie, true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Passkey added") {
		t.Fatalf("registration answered %d:\n%s", w.Code, w.Body.String())
	}
	if s.activeFactors(t, "webauthn") != 1 {
		t.Fatal("no active passkey after a successful registration")
	}

	// The first factor comes with recovery codes, shown once.
	if n := strings.Count(w.Body.String(), "<li><code>"); n != mfa.RecoveryCodeCount {
		t.Errorf("the result page shows %d recovery codes, want %d", n, mfa.RecoveryCodeCount)
	}
	var enrolled int
	s.factory.QueryRow(&enrolled, `SELECT count(*) FROM events WHERE event_type = 'user.mfa.enrolled'
		AND payload->>'factor_type' = 'webauthn'`)
	if enrolled != 1 {
		t.Errorf("found %d enrolment events", enrolled)
	}

	// The label the user typed is stored.
	var label string
	s.factory.QueryRow(&label, `SELECT COALESCE(label, '') FROM user_mfa_factors WHERE user_id = $1`, s.userID)
	if label != "work laptop" {
		t.Errorf("label = %q", label)
	}
}

// A second registration, when codes exist, shows none.
func TestASecondPasskeyIssuesNoNewCodes(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	cookie := s.signIn(t)

	for i := 0; i < 2; i++ {
		page := s.openPasskeys(t, "", cookie)
		credential := webauthntest.New(t).Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, page.options))
		w := s.submitPasskey(t, page, credential, cookie, true)
		if w.Code != http.StatusOK {
			t.Fatalf("registration %d answered %d", i+1, w.Code)
		}
		if i == 1 && strings.Contains(w.Body.String(), "<li><code>") {
			t.Error("a second passkey replaced the user's recovery codes")
		}
	}
	if s.activeFactors(t, "webauthn") != 2 {
		t.Errorf("found %d passkeys, want 2", s.activeFactors(t, "webauthn"))
	}
}

// --- A-1: who may register ------------------------------------------------------------------------

func TestRegistrationNeedsARecentSignIn(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	cookie := s.signIn(t)

	if page := s.openPasskeys(t, ""); page.status != http.StatusUnauthorized || page.options != nil {
		t.Errorf("no session answered %d with options=%v", page.status, page.options != nil)
	}

	// The same session, eleven minutes later.
	s.login.Now = func() time.Time { return time.Now().Add(mfa.RecentAuthentication + time.Minute) }
	if page := s.openPasskeys(t, "", cookie); page.status != http.StatusForbidden || page.options != nil {
		t.Errorf("a stale session answered %d with options=%v", page.status, page.options != nil)
	}
}

// A ceremony begun by one session cannot be finished by another.
func TestAnotherSessionCannotFinishTheCeremony(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	first := s.signIn(t)
	second := s.signIn(t)

	page := s.openPasskeys(t, "", first)
	credential := webauthntest.New(t).Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, page.options))

	w := s.submitPasskey(t, page, credential, second, true)
	if strings.Contains(w.Body.String(), "Passkey added") || s.activeFactors(t, "webauthn") != 0 {
		t.Errorf("a different session finished the ceremony:\n%s", w.Body.String())
	}
}

// A-9: a cross-site POST registers nothing.
func TestRegistrationNeedsTheCSRFToken(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	cookie := s.signIn(t)

	page := s.openPasskeys(t, "", cookie)
	credential := webauthntest.New(t).Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, page.options))

	s.submitPasskey(t, page, credential, cookie, false)
	if s.activeFactors(t, "webauthn") != 0 {
		t.Error("a POST without the CSRF token registered a passkey")
	}
}

// The phishing defence: a credential created on a lookalike origin is refused.
func TestARegistrationFromAnotherOriginIsRefused(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	cookie := s.signIn(t)

	page := s.openPasskeys(t, "", cookie)
	credential := webauthntest.New(t).Register(t, rpID, "https://auth.example.test.evil.example",
		webauthntest.ChallengeFrom(t, page.options))

	w := s.submitPasskey(t, page, credential, cookie, true)
	if strings.Contains(w.Body.String(), "Passkey added") || s.activeFactors(t, "webauthn") != 0 {
		t.Errorf("a credential from another origin was registered:\n%s", w.Body.String())
	}
}

// --- A-8: not an open redirect --------------------------------------------------------------------

func TestTheReturnLinkMustBeTheApplicationsOwnOrigin(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	s.factory.Exec(`UPDATE applications SET allowed_origins = ARRAY['https://console.example.test'] WHERE id = $1`, s.appID)
	cookie := s.signIn(t)

	for _, bad := range []string{
		"https://evil.example/steal",
		"javascript:alert(1)",
		"//console.example.test/x",
	} {
		page := s.openPasskeys(t, "?client_id="+url.QueryEscape(s.appID)+"&return_to="+url.QueryEscape(bad), cookie)
		if page.status != http.StatusBadRequest {
			t.Errorf("return_to %q answered %d, want 400", bad, page.status)
		}
	}
	if page := s.openPasskeys(t, "?return_to="+url.QueryEscape("https://console.example.test/"), cookie); page.status != http.StatusBadRequest {
		t.Errorf("a return_to with no client answered %d, want 400", page.status)
	}

	good := "https://console.example.test/account"
	page := s.openPasskeys(t, "?client_id="+url.QueryEscape(s.appID)+"&return_to="+url.QueryEscape(good), cookie)
	if page.status != http.StatusOK || !strings.Contains(page.body, `href="`+good+`"`) {
		t.Errorf("a registered origin was not honoured (%d):\n%s", page.status, page.body)
	}
}

// The page's policy pins exactly its own script.
func TestThePasskeyPageScriptIsPinned(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)
	cookie := s.signIn(t)

	r := httptest.NewRequest(http.MethodGet, PasskeysPath, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.login.Passkeys(w, r)

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src '"+passkeyRegisterScriptHash+"'") || strings.Contains(csp, "unsafe") ||
		strings.Contains(csp, "connect-src") {
		t.Errorf("the passkey page policy is wrong: %s", csp)
	}
}

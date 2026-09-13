//go:build integration

// "This wasn't me" and login anomaly detection (P3-08), against a real
// database.
//
// What a container answers that a fake cannot: that the history query really
// runs under the service's tenant-scoped role and really excludes the session
// just created; that a report really ends sessions the cache would otherwise
// keep honouring; and that the cleared password really refuses the thief.
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

	"github.com/zed378/zed-auth/backend/internal/anomaly"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// --- helpers ----------------------------------------------------------------------------

// withReports wires the report page the way cmd/authservice does.
func (s *stack) withReports() {
	s.login.Reports = &NotMeFlow{
		Sessions:  s.sessions,
		Refresh:   token.NewRefreshStore(),
		Passwords: s.users,
	}
}

// reportToken issues a report link for the stack's user, as a notice would.
func (s *stack) reportToken(t *testing.T) string {
	t.Helper()

	var issued user.Token
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		var err error
		issued, err = s.users.IssueToken(context.Background(), tx, s.userID,
			user.PurposeReportNotMe, user.ReportNotMeLifetime, time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing a report link: %v", err)
	}
	return issued.Plaintext
}

// openReport GETs the confirmation page.
func (s *stack) openReport(t *testing.T, tok string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, NotMePath+"?token="+url.QueryEscape(tok), nil)
	w := httptest.NewRecorder()
	s.login.NotMe(w, r)
	return w
}

// confirmReport renders the page for its CSRF cookie, then submits it.
func (s *stack) confirmReport(t *testing.T, tok string) *httptest.ResponseRecorder {
	t.Helper()

	first := s.openReport(t, tok)
	var csrf string
	for _, c := range first.Result().Cookies() {
		if c.Name == CSRFCookieName {
			csrf = c.Value
		}
	}
	if csrf == "" {
		// The page refused the link, so there is no form to submit. Use a
		// minted token so the POST is judged on the link and not on CSRF.
		csrf = s.csrfToken(t)
	}
	return s.postReport(t, tok, csrf, true)
}

func (s *stack) postReport(t *testing.T, tok, csrf string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{"csrf_token": {csrf}, "token": {tok}}
	r := httptest.NewRequest(http.MethodPost, NotMePath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withCookie {
		r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
	}
	w := httptest.NewRecorder()
	s.login.NotMe(w, r)
	return w
}

func (s *stack) liveSessions(t *testing.T) int {
	t.Helper()
	var n int
	s.factory.QueryRow(&n,
		`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, s.userID)
	return n
}

func (s *stack) liveRefreshTokens(t *testing.T) int {
	t.Helper()
	var n int
	s.factory.QueryRow(&n,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND NOT revoked`, s.userID)
	return n
}

func (s *stack) passwordHash(t *testing.T) string {
	t.Helper()
	var hash string
	s.factory.QueryRow(&hash, `SELECT COALESCE(password_hash, '') FROM users WHERE id = $1`, s.userID)
	return hash
}

func (s *stack) issueRefresh(t *testing.T) {
	t.Helper()

	var sessionID string
	s.factory.QueryRow(&sessionID,
		`SELECT id FROM sessions WHERE user_id = $1 AND revoked_at IS NULL LIMIT 1`, s.userID)

	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		_, _, err := token.NewRefreshStore().Issue(context.Background(), tx, token.Refresh{
			UserID: s.userID, ClientID: s.appID, OrgID: s.orgID,
			SessionID: sessionID, Scope: []string{"openid"},
			ExpiresAt: time.Now().Add(token.RefreshTokenLifetime),
		}, "", time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing a refresh token: %v", err)
	}
}

func (s *stack) passwordSignInWorks(t *testing.T, password string) bool {
	t.Helper()
	id := s.begin(t)
	csrf := s.form(t, id)
	return s.submit(t, id, csrf, testEmail, password).Code == http.StatusFound
}

var linkToken = regexp.MustCompile(`token=([A-Za-z0-9_%\-]+)`)

// tokenIn extracts the token from a link in a mail body.
func tokenIn(t *testing.T, body, path string) string {
	t.Helper()

	i := strings.Index(body, path+"?token=")
	if i < 0 {
		t.Fatalf("the message has no %s link:\n%s", path, body)
	}
	m := linkToken.FindStringSubmatch(body[i:])
	if m == nil {
		t.Fatalf("no token in the %s link:\n%s", path, body)
	}
	plain, err := url.QueryUnescape(m[1])
	if err != nil {
		t.Fatalf("unescaping the token: %v", err)
	}
	return plain
}

// --- the GET changes nothing (A-5) --------------------------------------------------------

// A mail client that pre-fetches the link must not sign anybody out.
func TestOpeningAReportLinkChangesNothing(t *testing.T) {
	s := setup(t)
	s.withReports()

	s.signIn(t)
	tok := s.reportToken(t)

	w := s.openReport(t, tok)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Sign out everywhere and reset my password") {
		t.Fatalf("the confirmation did not render (%d):\n%s", w.Code, w.Body.String())
	}

	if s.liveSessions(t) != 1 {
		t.Error("opening the link ended a session; a pre-fetching mail client would sign the user out")
	}
	if s.passwordHash(t) == "" {
		t.Error("opening the link cleared the password")
	}
	var used int
	s.factory.QueryRow(&used,
		`SELECT count(*) FROM user_tokens WHERE user_id = $1 AND purpose = 'report_not_me' AND used_at IS NOT NULL`,
		s.userID)
	if used != 0 {
		t.Error("opening the link consumed it")
	}
}

// --- the POST does everything (F-5) ----------------------------------------------------------

func TestConfirmingAReportEndsEverythingAndForcesAReset(t *testing.T) {
	s := setup(t)
	s.withReports()

	first := s.signIn(t)
	second := s.signIn(t)
	s.issueRefresh(t)

	// Controls, so the assertions below cannot pass on a stack that never had
	// anything to revoke.
	if s.liveSessions(t) < 2 || s.liveRefreshTokens(t) < 1 {
		t.Fatalf("setup produced %d sessions and %d refresh tokens; this test would prove nothing",
			s.liveSessions(t), s.liveRefreshTokens(t))
	}
	if target := s.silentAuthorize(t, second); target.Path == Path {
		t.Fatal("the cookie did not authorise before the report; this test would prove nothing")
	}

	sentBefore := s.mailer.count()
	w := s.confirmReport(t, s.reportToken(t))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "signed out everywhere") {
		t.Fatalf("the report answered %d:\n%s", w.Code, w.Body.String())
	}

	if n := s.liveSessions(t); n != 0 {
		t.Errorf("%d session(s) survived the report", n)
	}
	if n := s.liveRefreshTokens(t); n != 0 {
		t.Errorf("%d refresh token(s) survived the report; the stranger's application keeps minting tokens", n)
	}

	// The consequence, not the column: the stranger's cookie no longer works,
	// including through the cache.
	for _, c := range []*http.Cookie{first, second} {
		if target := s.silentAuthorize(t, c); target.Path != Path {
			t.Error("a session cookie still authorises after the report")
		}
	}

	// The password is gone, and the stolen one is refused.
	if s.passwordHash(t) != "" {
		t.Error("the password hash is still stored")
	}
	if s.passwordSignInWorks(t, testPassword) {
		t.Fatal("the stolen password still signs in after the report")
	}

	// A reset link went to the account's own address, and it works.
	if s.mailer.count() != sentBefore+1 {
		t.Fatalf("sent %d messages, want exactly one reset link", s.mailer.count()-sentBefore)
	}
	msg, _ := s.mailer.last()
	if msg.To != testEmail {
		t.Errorf("the reset link went to %q, not the account's address", msg.To)
	}
	reset := tokenIn(t, msg.Body, "/password/set")

	const chosen = "A New Password Chosen By The Owner"
	if w := s.setPassword(t, reset, chosen); !strings.Contains(w.Body.String(), "password is set") {
		t.Fatalf("the reset link from the report did not set a password:\n%s", w.Body.String())
	}
	if !s.passwordSignInWorks(t, chosen) {
		t.Error("the owner could not sign in with the password they chose")
	}

	// And it is audited.
	events := s.events(t, string(audit.EventLoginReportedNotMe))
	if len(events) != 1 {
		t.Fatalf("found %d report events, want 1", len(events))
	}
	payload, _ := events[0]["payload"].(map[string]any)
	if payload["password_cleared"] != true || payload["reset_link_issued"] != true {
		t.Errorf("the report event does not record what happened: %v", payload)
	}
	if n, _ := payload["refresh_tokens_revoked"].(float64); n < 1 {
		t.Errorf("the report event records %v refresh tokens revoked, want at least 1", payload["refresh_tokens_revoked"])
	}
}

// Every outstanding link dies with the report — an invitation or reset somebody
// else requested while in the account is a way back in.
//
// An INVITATION link, deliberately. A stale reset link would be retired anyway
// by the fresh reset the report issues (IssueToken retires its own purpose), so
// a test using one would pass with RetireTokens deleted and prove nothing about
// it.
func TestAReportRetiresOutstandingLinks(t *testing.T) {
	s := setup(t)
	s.withReports()

	var stale user.Token
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		var err error
		stale, err = s.users.IssueToken(context.Background(), tx, s.userID,
			user.PurposeInvite, user.InviteLifetime, time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing: %v", err)
	}

	s.confirmReport(t, s.reportToken(t))

	if w := s.setPassword(t, stale.Plaintext, "The Stranger's Chosen Password"); strings.Contains(w.Body.String(), "password is set") {
		t.Error("a reset link issued before the report still set a password")
	}
}

// --- single use, purpose-bound, CSRF ---------------------------------------------------------

func TestAReportLinkWorksOnce(t *testing.T) {
	s := setup(t)
	s.withReports()
	tok := s.reportToken(t)

	s.confirmReport(t, tok)
	w := s.confirmReport(t, tok)

	if !strings.Contains(w.Body.String(), "This link is not valid") {
		t.Errorf("a used report link was accepted again:\n%s", w.Body.String())
	}
	if n := len(s.events(t, string(audit.EventLoginReportedNotMe))); n != 1 {
		t.Errorf("found %d report events after two submissions, want 1", n)
	}
}

// A reset or invitation link cannot drive the report page.
//
// The mirror of TestAReportNotMeTokenCannotSetAPassword: each page accepts its
// own purpose only.
func TestAResetLinkCannotSignAUserOutEverywhere(t *testing.T) {
	s := setup(t)
	s.withReports()
	s.signIn(t)

	var reset user.Token
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		var err error
		reset, err = s.users.IssueToken(context.Background(), tx, s.userID,
			user.PurposeReset, user.ResetLifetime, time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing: %v", err)
	}

	if w := s.openReport(t, reset.Plaintext); strings.Contains(w.Body.String(), "<form") {
		t.Errorf("a reset link rendered the report form:\n%s", w.Body.String())
	}

	s.postReport(t, reset.Plaintext, s.csrfToken(t), true)

	if s.liveSessions(t) != 1 {
		t.Error("a reset link signed the user out everywhere")
	}
	if s.passwordHash(t) == "" {
		t.Error("a reset link cleared the password")
	}
	if w := s.setPassword(t, reset.Plaintext, "Still A Valid Reset Password"); !strings.Contains(w.Body.String(), "password is set") {
		t.Error("the refused reset link was consumed by the report page")
	}
}

// Without a matching CSRF cookie the POST does nothing and keeps the link alive.
func TestAReportWithoutCSRFDoesNothing(t *testing.T) {
	s := setup(t)
	s.withReports()
	s.signIn(t)
	tok := s.reportToken(t)

	w := s.postReport(t, tok, "forged-token-value", false)
	if !strings.Contains(w.Body.String(), "That form expired") {
		t.Errorf("a POST with no CSRF cookie was not refused as stale:\n%s", w.Body.String())
	}
	if s.liveSessions(t) != 1 || s.passwordHash(t) == "" {
		t.Error("a cross-site POST acted on the report")
	}

	// The link still works for the real owner.
	if w := s.confirmReport(t, tok); !strings.Contains(w.Body.String(), "signed out everywhere") {
		t.Errorf("the link did not survive a refused POST:\n%s", w.Body.String())
	}
}

// --- detection, end to end ---------------------------------------------------------------------

// doneDetector signals when a detection run finishes, so a test can wait for
// one rather than sleep — and can wait for a run that produced NOTHING, which
// polling for a row could never do.
type doneDetector struct {
	inner *anomaly.Detector
	done  chan struct{}
}

func (d doneDetector) Observe(ctx context.Context, c anomaly.Current) {
	defer func() { d.done <- struct{}{} }()
	d.inner.Observe(ctx, c)
}

func (d doneDetector) wait(t *testing.T) {
	t.Helper()
	select {
	case <-d.done:
	case <-time.After(20 * time.Second):
		t.Fatal("anomaly detection never ran")
	}
}

const (
	agentWindowsChrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	agentIPhoneSafari  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1"
)

// The DoD's detection path, through the real login, the real history query
// under the service's role, the real audit writer, and the notifier — then the
// link in that notice, opened.
func TestANewDeviceIsDetectedNotifiedAndReportable(t *testing.T) {
	s := setup(t)
	s.withReports()

	detector := doneDetector{
		inner: &anomaly.Detector{
			History:  &anomaly.PostgresHistory{DB: s.db},
			Recorder: &anomaly.AuditRecorder{DB: s.db, Audit: audit.NewWriter(s.db, discard(), nil)},
			Notifier: &AnomalyMail{
				Users: s.users, DB: s.db, Mailer: s.mailer, BaseURL: "https://auth.example.test",
			},
			Log: discard(),
		},
		done: make(chan struct{}, 4),
	}
	s.login.Anomalies = detector

	signInWith := func(agent string) {
		t.Helper()
		id := s.begin(t)
		csrf := s.form(t, id)
		if w := s.submitWithAgent(t, id, csrf, testEmail, testPassword, agent); w.Code != http.StatusFound {
			t.Fatalf("the login failed: %d\n%s", w.Code, w.Body.String())
		}
		detector.wait(t)
	}

	// The first login ever: nothing to be unlike.
	signInWith(agentWindowsChrome)
	// The same device again: familiar. This one also proves the history read
	// excludes the session just created — otherwise the FIRST login would
	// have matched itself, and the THIRD below would match itself too.
	signInWith(agentWindowsChrome)

	if n := len(s.events(t, string(audit.EventLoginAnomaly))); n != 0 {
		t.Fatalf("routine logins produced %d anomaly events", n)
	}
	sentBefore := s.mailer.count()

	signInWith(agentIPhoneSafari)

	events := s.events(t, string(audit.EventLoginAnomaly))
	if len(events) != 1 {
		t.Fatalf("a login from a new device produced %d anomaly events, want 1", len(events))
	}
	payload, _ := events[0]["payload"].(map[string]any)
	signals, _ := payload["signals"].([]any)
	if len(signals) != 1 || signals[0] != string(anomaly.SignalNewDevice) {
		t.Errorf("signals = %v, want [new_device]", payload["signals"])
	}
	if _, hasIP := payload["ip"]; hasIP {
		t.Error("the anomaly payload carries an IP")
	}

	if s.mailer.count() != sentBefore+1 {
		t.Fatalf("sent %d notices, want 1", s.mailer.count()-sentBefore)
	}
	notice, _ := s.mailer.last()
	if notice.To != testEmail {
		t.Errorf("the notice went to %q", notice.To)
	}

	// The link in the notice opens the confirmation page.
	report := tokenIn(t, notice.Body, NotMePath)
	if w := s.openReport(t, report); !strings.Contains(w.Body.String(), "Sign out everywhere and reset my password") {
		t.Errorf("the link in the notice did not open the report page:\n%s", w.Body.String())
	}
}

// A revoked session still counts as history: "sign out everywhere" must not make
// every device new again.
//
// A second, LIVE device is signed in between, deliberately. Without it the only
// history is the revoked session, so a query that dropped revoked rows would
// return nothing — and an empty history is a first login, which is never an
// anomaly. The test would pass against exactly the bug it names.
func TestASignedOutDeviceIsStillFamiliar(t *testing.T) {
	s := setup(t)

	recorder := &anomaly.AuditRecorder{DB: s.db, Audit: audit.NewWriter(s.db, discard(), nil)}
	detector := doneDetector{
		inner: &anomaly.Detector{History: &anomaly.PostgresHistory{DB: s.db}, Recorder: recorder, Log: discard()},
		done:  make(chan struct{}, 4),
	}
	s.login.Anomalies = detector

	signInWith := func(agent string) {
		t.Helper()
		id := s.begin(t)
		csrf := s.form(t, id)
		if w := s.submitWithAgent(t, id, csrf, testEmail, testPassword, agent); w.Code != http.StatusFound {
			t.Fatalf("the login failed: %d", w.Code)
		}
		detector.wait(t)
	}

	signInWith(agentIPhoneSafari)

	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		_, err := s.sessions.RevokeAllForUser(context.Background(), tx, s.userID, s.orgID, s.userID, time.Now())
		return err
	}); err != nil {
		t.Fatalf("signing out everywhere: %v", err)
	}

	// A live session on another device, so the history is never empty below.
	signInWith(agentWindowsChrome)
	before := len(s.events(t, string(audit.EventLoginAnomaly)))

	// Back on the phone that was signed out.
	signInWith(agentIPhoneSafari)

	if after := len(s.events(t, string(audit.EventLoginAnomaly))); after != before {
		t.Errorf("signing back in on a phone that was signed out everywhere was reported as a new device "+
			"(%d anomaly events became %d)", before, after)
	}
}

// refusingQuota refuses every message.
type refusingQuota struct{}

func (refusingQuota) ConsumeMail(context.Context, string, time.Time) ratelimit.Verdict {
	return ratelimit.Verdict{Allowed: false}
}

// A notice the mail quota refuses must not kill the link in the last notice the
// user DID receive — issuing a report link retires the previous one, so the
// quota has to be asked first.
func TestARefusedNoticeKeepsThePreviousReportLinkAlive(t *testing.T) {
	s := setup(t)
	s.withReports()
	previous := s.reportToken(t)

	notifier := &AnomalyMail{
		Users: s.users, DB: s.db, Mailer: s.mailer, MailLimit: refusingQuota{},
		BaseURL: "https://auth.example.test",
	}
	sentBefore := s.mailer.count()

	if err := notifier.NotifyAnomaly(context.Background(), anomaly.Finding{
		OrgID: s.orgID, UserID: s.userID, SessionID: "33333333-3333-3333-3333-333333333333",
		At: time.Now(), Signals: []anomaly.Signal{anomaly.SignalNewDevice},
	}); err != nil {
		t.Fatalf("NotifyAnomaly: %v", err)
	}

	if s.mailer.count() != sentBefore {
		t.Error("a notice was sent past a refusing quota")
	}
	if w := s.openReport(t, previous); !strings.Contains(w.Body.String(), "Sign out everywhere and reset my password") {
		t.Error("a refused notice retired the report link the user already holds")
	}
}

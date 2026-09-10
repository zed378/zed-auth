//go:build integration

// The two hosted pages a person uses to take possession of an account
// (P1-19.4, P1-19.5), against a real database.
//
// The claim worth a container is the one a fake cannot answer honestly:
// whether the two forgot responses are ACTUALLY identical when a real query
// stands behind one of them and not the other. Everything else here follows
// from that being true.
package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// capturingMailer records what would have been sent.
type capturingMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (c *capturingMailer) Send(_ context.Context, msg mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *capturingMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *capturingMailer) last() (mail.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return mail.Message{}, false
	}
	return c.sent[len(c.sent)-1], true
}

// testPolicy is the real evaluator over the organization's real settings —
// the same one cmd/authservice wires, so a password refused here is refused
// for the reason a deployment would refuse it.
type testPolicy struct{ store *authn.PolicyStore }

func (p testPolicy) Validate(
	ctx context.Context, tx *postgres.Tx, orgID, _ string, password string,
) error {
	policy, err := p.store.Policy(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if violations := authn.Evaluate(password, policy); len(violations) > 0 {
		reasons := make([]string, 0, len(violations))
		for _, v := range violations {
			reasons = append(reasons, v.Message)
		}
		return errors.New(strings.Join(reasons, " "))
	}
	return nil
}

// --- helpers ------------------------------------------------------------------------------

// forgot posts an address to the forgot form and returns the response.
func (s *stack) forgot(t *testing.T, pendingID, email string) *httptest.ResponseRecorder {
	t.Helper()

	csrf := s.csrfToken(t)
	form := url.Values{
		"csrf_token": {csrf},
		"request":    {pendingID},
		"email":      {email},
	}

	r := httptest.NewRequest(http.MethodPost, "/login/forgot", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})

	w := httptest.NewRecorder()
	s.login.Forgot(w, r)
	return w
}

// csrfToken mints one the way the form does.
func (s *stack) csrfToken(t *testing.T) string {
	t.Helper()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login/forgot", nil)
	s.login.Forgot(w, r)

	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			return c.Value
		}
	}
	t.Fatal("the forgot form issued no CSRF cookie")
	return ""
}

// inviteToken creates an invited user and returns their token's plaintext.
func (s *stack) inviteToken(t *testing.T, email string) (string, string) {
	t.Helper()

	var (
		created user.User
		token   user.Token
	)
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		var err error
		if created, err = s.users.Create(context.Background(), tx, email, "", "Invited"); err != nil {
			return err
		}
		token, err = s.users.IssueToken(
			context.Background(), tx, created.ID, user.PurposeInvite, user.InviteLifetime, time.Now())
		return err
	}); err != nil {
		t.Fatalf("inviting: %v", err)
	}
	return created.ID, token.Plaintext
}

// setPassword drives the hosted page.
func (s *stack) setPassword(t *testing.T, token, password string) *httptest.ResponseRecorder {
	t.Helper()

	csrf := s.csrfToken(t)
	form := url.Values{
		"csrf_token":       {csrf},
		"token":            {token},
		"password":         {password},
		"password_confirm": {password},
	}

	r := httptest.NewRequest(http.MethodPost, "/password/set", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})

	w := httptest.NewRecorder()
	s.login.SetPassword(w, r)
	return w
}

// --- the enumeration property (abuse case A-1) ----------------------------------------------

// **The whole security property of the forgot endpoint, asserted on bytes.**
//
// Not "both return 200" — that passes against a service whose two pages differ
// in a sentence. The full body and the status must be identical for an address
// that exists and one that does not.
func TestTheForgotResponseIsIdenticalForARealAndAnUnknownAddress(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	real := s.forgot(t, pending, testEmail)
	unknown := s.forgot(t, pending, "nobody@example.test")

	if real.Code != unknown.Code {
		t.Errorf("status differs: %d vs %d", real.Code, unknown.Code)
	}
	if real.Body.String() != unknown.Body.String() {
		t.Errorf("the bodies differ:\n--- real ---\n%s\n--- unknown ---\n%s",
			real.Body.String(), unknown.Body.String())
	}

	// Headers too, minus the CSRF cookie, which is per response by design.
	for _, header := range []string{"Content-Type", "Content-Security-Policy", "Cache-Control"} {
		if real.Header().Get(header) != unknown.Header().Get(header) {
			t.Errorf("%s differs: %q vs %q",
				header, real.Header().Get(header), unknown.Header().Get(header))
		}
	}

	// The control: exactly one message was sent, so the identical answers are
	// not identical because nothing happened in either case.
	if s.mailer.count() != 1 {
		t.Errorf("%d messages sent, want 1 — the responses would match trivially if none were",
			s.mailer.count())
	}
}

// A deactivated account is also indistinguishable.
func TestADeactivatedAccountLooksLikeAnUnknownOne(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	s.factory.Exec(`UPDATE users SET status = 'deactivated' WHERE id = $1`, s.userID)

	deactivated := s.forgot(t, pending, testEmail)
	unknown := s.forgot(t, pending, "nobody@example.test")

	if deactivated.Body.String() != unknown.Body.String() {
		t.Error("a deactivated account answers differently from an unknown one")
	}
	if s.mailer.count() != 0 {
		t.Error("a message was sent for a deactivated account")
	}
}

// The two paths cost comparable time.
//
// A tolerance rather than an equality, and a wide one: this measures a real
// database under a container. What it rules out is an order-of-magnitude
// difference, which is what a short-circuit produces.
func TestTheForgotPathsCostComparableTime(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	const rounds = 6
	measure := func(email string) time.Duration {
		start := time.Now()
		for range rounds {
			s.forgot(t, pending, email)
		}
		return time.Since(start) / rounds
	}

	// Warm both paths first: the first query of a session pays for planning.
	measure(testEmail)
	measure("nobody@example.test")

	withUser := measure(testEmail)
	withNone := measure("nobody@example.test")

	ratio := float64(withUser) / float64(withNone)
	if ratio < 0.2 || ratio > 5 {
		t.Errorf("the two paths differ by %.2fx (%v vs %v), which is visible from outside",
			ratio, withUser, withNone)
	}
}

// The audit log records a reset only when a user was found — otherwise it
// would be the enumeration list the endpoint exists to withhold, stored
// durably.
func TestNoAuditEventIsWrittenForAnUnknownAddress(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	s.forgot(t, pending, "nobody@example.test")

	var count int
	s.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		s.orgID, string(audit.EventPasswordResetSent))
	if count != 0 {
		t.Errorf("%d reset events for an address that does not exist", count)
	}

	// The control: a real address does write one.
	s.forgot(t, pending, testEmail)
	s.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		s.orgID, string(audit.EventPasswordResetSent))
	if count != 1 {
		t.Errorf("%d reset events for a real address, want 1", count)
	}
}

// --- accepting an invitation (P1-19.5) ----------------------------------------------------------

func TestAcceptingAnInvitationSetsThePasswordAndActivatesTheAccount(t *testing.T) {
	s := setup(t)
	id, token := s.inviteToken(t, "newcomer@example.test")

	w := s.setPassword(t, token, "Correct horse battery staple two")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "password is set") {
		t.Errorf("unexpected page: %s", w.Body.String())
	}

	var status string
	var hasPassword, verified bool
	s.factory.QueryRow(&status, `SELECT status FROM users WHERE id = $1`, id)
	s.factory.QueryRow(&hasPassword, `SELECT password_hash IS NOT NULL FROM users WHERE id = $1`, id)
	s.factory.QueryRow(&verified, `SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, id)

	if status != user.StatusActive {
		t.Errorf("status = %q, want active", status)
	}
	if !hasPassword {
		t.Error("no password was stored")
	}
	if !verified {
		t.Error("accepting an invitation did not verify the address (PG-18)")
	}

	// And the event that says it happened.
	var events int
	s.factory.QueryRow(&events,
		`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		s.orgID, string(audit.EventUserInviteAccepted))
	if events != 1 {
		t.Errorf("%d invite_accepted events, want 1", events)
	}
}

// **The page issues no session.** A page that both consumes an emailed link
// and signs somebody in is a second authentication path with none of the login
// page's rate limiting, and it would turn a stolen link into a live session in
// one step rather than two.
func TestSettingAPasswordDoesNotSignAnybodyIn(t *testing.T) {
	s := setup(t)
	_, token := s.inviteToken(t, "nosession@example.test")

	w := s.setPassword(t, token, "Correct horse battery staple three")

	for _, c := range w.Result().Cookies() {
		if strings.Contains(strings.ToLower(c.Name), "session") {
			t.Errorf("the page set a session cookie: %s", c.Name)
		}
	}

	var sessions int
	s.factory.QueryRow(&sessions, `SELECT count(*) FROM sessions`)
	if sessions != 0 {
		t.Errorf("%d sessions exist after setting a password", sessions)
	}
}

// The link works once.
func TestALinkCannotBeUsedTwice(t *testing.T) {
	s := setup(t)
	id, token := s.inviteToken(t, "once@example.test")

	first := s.setPassword(t, token, "Correct horse battery staple four")
	if !strings.Contains(first.Body.String(), "password is set") {
		t.Fatalf("the first use failed: %s", first.Body.String())
	}

	second := s.setPassword(t, token, "A completely different password here")
	if !strings.Contains(second.Body.String(), "not valid") {
		t.Errorf("the second use was accepted: %s", second.Body.String())
	}

	// The second password did not take.
	var hash string
	s.factory.QueryRow(&hash, `SELECT password_hash FROM users WHERE id = $1`, id)
	if result, _ := authn.Verify(hash, "A completely different password here"); result.Match {
		t.Error("the second use changed the password")
	}
}

// Every way a link can fail looks the same.
func TestEveryInvalidLinkLooksIdentical(t *testing.T) {
	s := setup(t)
	_, valid := s.inviteToken(t, "shapes@example.test")
	s.setPassword(t, valid, "Correct horse battery staple five") // consume it

	bodies := map[string]string{}
	for name, token := range map[string]string{
		"used":        valid,
		"nonexisting": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"malformed":   "not-a-token",
	} {
		w := s.setPassword(t, token, "Correct horse battery staple six")
		bodies[name] = w.Body.String()
	}

	first := ""
	for name, body := range bodies {
		if !strings.Contains(body, "not valid") {
			t.Errorf("%s did not produce the invalid-link page: %s", name, body)
		}
		if first == "" {
			first = body
			continue
		}
		if body != first {
			t.Errorf("%s produced a different page from the others", name)
		}
	}
}

// A password the policy refuses does NOT burn the token.
//
// Consuming it first and then refusing the password would leave the person
// holding a dead link and no account — the worst possible combination, and the
// one that is easy to write by accident.
func TestARefusedPasswordLeavesTheLinkUsable(t *testing.T) {
	s := setup(t)
	id, token := s.inviteToken(t, "weak@example.test")

	w := s.setPassword(t, token, "short")
	if strings.Contains(w.Body.String(), "password is set") {
		t.Fatal("a password the policy refuses was accepted")
	}

	var used bool
	s.factory.QueryRow(&used,
		`SELECT used_at IS NOT NULL FROM user_tokens WHERE user_id = $1`, id)
	if used {
		t.Error("the token was consumed by a refused password")
	}

	// And the link still works for an acceptable one.
	second := s.setPassword(t, token, "Correct horse battery staple seven")
	if !strings.Contains(second.Body.String(), "password is set") {
		t.Errorf("the link stopped working after a refused attempt: %s", second.Body.String())
	}
}

// The form itself carries no user identity — abuse case A-3 has nowhere to
// land, because there is nothing in the request that names whose password is
// being set except the token.
func TestTheFormNamesNoUser(t *testing.T) {
	s := setup(t)
	_, token := s.inviteToken(t, "anonymous-form@example.test")

	r := httptest.NewRequest(http.MethodGet, "/password/set?token="+url.QueryEscape(token), nil)
	w := httptest.NewRecorder()
	s.login.SetPassword(w, r)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, body)
	}
	for _, forbidden := range []string{"user_id", "anonymous-form@example.test", s.orgID} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the form carries %q", forbidden)
		}
	}
	if !strings.Contains(body, `name="token"`) {
		t.Error("the form does not carry the token")
	}
}

// A reset link does NOT verify the address.
//
// Accepting an invitation proves the mailbox was reachable at that moment;
// a reset proves it too, but widening the claim here would be a security
// statement made by accident rather than by decision (PG-18).
func TestAResetDoesNotVerifyTheAddress(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	s.forgot(t, pending, testEmail)
	msg, ok := s.mailer.last()
	if !ok {
		t.Fatal("no reset message")
	}

	token := ""
	for _, line := range strings.Split(msg.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/password/set?token=") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatalf("bad link: %v", err)
			}
			token = u.Query().Get("token")
		}
	}
	if token == "" {
		t.Fatalf("no link in:\n%s", msg.Body)
	}

	s.setPassword(t, token, "Correct horse battery staple eight")

	var verified bool
	s.factory.QueryRow(&verified,
		`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, s.userID)
	if verified {
		t.Error("a password reset verified the address")
	}

	// The control: the password DID change, so the absence above is about
	// verification rather than about the reset failing.
	var hash string
	s.factory.QueryRow(&hash, `SELECT password_hash FROM users WHERE id = $1`, s.userID)
	if result, _ := authn.Verify(hash, "Correct horse battery staple eight"); !result.Match {
		t.Error("the reset did not change the password")
	}
}

// A stale CSRF token re-renders the form rather than condemning the link.
func TestAStaleFormDoesNotCondemnTheLink(t *testing.T) {
	s := setup(t)
	_, token := s.inviteToken(t, "stale@example.test")

	form := url.Values{
		"csrf_token":       {"a-stale-token-that-is-not-ours"},
		"token":            {token},
		"password":         {"Correct horse battery staple nine"},
		"password_confirm": {"Correct horse battery staple nine"},
	}
	r := httptest.NewRequest(http.MethodPost, "/password/set", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.login.SetPassword(w, r)

	if strings.Contains(w.Body.String(), "not valid") {
		t.Error("a stale form was reported as an invalid link")
	}
	if !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("the page does not explain the stale form: %s", w.Body.String())
	}

	// And the link still works.
	if second := s.setPassword(t, token, "Correct horse battery staple ten"); !strings.Contains(
		second.Body.String(), "password is set") {
		t.Error("the link stopped working after a stale submission")
	}
}

func TestMismatchedConfirmationIsRefused(t *testing.T) {
	s := setup(t)
	_, token := s.inviteToken(t, "mismatch@example.test")

	csrf := s.csrfToken(t)
	form := url.Values{
		"csrf_token":       {csrf},
		"token":            {token},
		"password":         {"Correct horse battery staple eleven"},
		"password_confirm": {"Something else entirely here"},
	}
	r := httptest.NewRequest(http.MethodPost, "/password/set", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
	w := httptest.NewRecorder()
	s.login.SetPassword(w, r)

	if !strings.Contains(w.Body.String(), "do not match") {
		t.Errorf("unexpected page: %s", w.Body.String())
	}
}

// The link in the message points where it should, and the token is in the
// query rather than the path — so a Referer from the page cannot carry it
// onward (the page also sends referrer: no-referrer).
func TestTheResetMessageCarriesAUsableLink(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	s.forgot(t, pending, testEmail)
	msg, ok := s.mailer.last()
	if !ok {
		t.Fatal("no message")
	}

	if msg.To != testEmail {
		t.Errorf("the message went to %q", msg.To)
	}
	if !strings.Contains(msg.Body, "https://auth.example.test/password/set?token=") {
		t.Errorf("no usable link in:\n%s", msg.Body)
	}
	// It says nothing has happened yet — the recipient's first question when
	// a reset arrives unrequested.
	if !strings.Contains(msg.Body, "nothing has happened yet") {
		t.Error("the message does not say the password has not changed")
	}
}

// The forgot form with no pending request still answers the same way.
func TestForgotWithNoPendingRequestAnswersIdentically(t *testing.T) {
	s := setup(t)
	pending := s.begin(t)

	withRequest := s.forgot(t, pending, testEmail)
	withNone := s.forgot(t, "", testEmail)

	// The bodies differ only in the back link, which is present only when
	// there is somewhere to go back to. Compare the part that matters.
	for _, w := range []*httptest.ResponseRecorder{withRequest, withNone} {
		if !strings.Contains(w.Body.String(), "Check your email") {
			t.Errorf("unexpected page: %s", w.Body.String())
		}
	}
	if withRequest.Code != withNone.Code {
		t.Errorf("status differs: %d vs %d", withRequest.Code, withNone.Code)
	}
}

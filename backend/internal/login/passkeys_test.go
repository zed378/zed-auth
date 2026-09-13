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

	"github.com/zed378/zed-auth/backend/internal/mail"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// The hosted passkey page's edges, and the report page's mail edges, without a
// database (P3-10). The ceremony itself is exercised against real Postgres,
// Redis and the relying-party library in passkeys_integration_test.go.

func TestThePasskeyPageWithoutConfigurationSaysSo(t *testing.T) {
	f := newFixture(t)

	w := httptest.NewRecorder()
	f.handler.Passkeys(w, httptest.NewRequest(http.MethodGet, PasskeysPath, nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "not configured") {
		t.Errorf("an unconfigured passkey page answered %d:\n%s", w.Code, w.Body.String())
	}
}

func TestThePasskeyPageRefusesOtherMethods(t *testing.T) {
	f := newFixture(t)
	f.handler.PasskeyRegistration = &PasskeyRegistration{}

	w := httptest.NewRecorder()
	f.handler.Passkeys(w, httptest.NewRequest(http.MethodPut, PasskeysPath, nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, POST" {
		t.Errorf("PUT answered %d with Allow %q", w.Code, w.Header().Get("Allow"))
	}
}

// A POST with a valid CSRF token but no ceremony cookie registers nothing, and
// never reaches the ceremony (which is nil here, so reaching it would panic).
func TestFinishingWithoutACeremonyIsRefused(t *testing.T) {
	f := newFixture(t)
	f.handler.PasskeyRegistration = &PasskeyRegistration{}

	csrf := "a-csrf-token-value-long-enough-to-look-real"
	form := url.Values{"csrf_token": {csrf}, "credential": {"{}"}}
	r := httptest.NewRequest(http.MethodPost, PasskeysPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCSRF(r, csrf)

	w := httptest.NewRecorder()
	f.handler.Passkeys(w, r)
	if !strings.Contains(w.Body.String(), "was not added") || !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("a POST with no ceremony answered:\n%s", w.Body.String())
	}
}

func TestFinishingWithoutCSRFIsRefused(t *testing.T) {
	f := newFixture(t)
	f.handler.PasskeyRegistration = &PasskeyRegistration{}

	r := httptest.NewRequest(http.MethodPost, PasskeysPath, strings.NewReader("credential=%7B%7D"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.handler.Passkeys(w, r)
	if !strings.Contains(w.Body.String(), "was not added") {
		t.Errorf("a POST without CSRF answered:\n%s", w.Body.String())
	}
}

// A return_to is refused before anything else happens, including for an
// application that cannot be found.
func TestAReturnToForAnUnknownApplicationIsRefused(t *testing.T) {
	f := newFixture(t)
	f.handler.PasskeyRegistration = &PasskeyRegistration{
		Clients: func(context.Context, string) (client.Application, error) {
			return client.Application{}, errors.New("no such application")
		},
	}

	target := PasskeysPath + "?client_id=nope&return_to=" + url.QueryEscape("https://console.example.test/")
	w := httptest.NewRecorder()
	f.handler.Passkeys(w, httptest.NewRequest(http.MethodGet, target, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("an unknown application's return_to answered %d, want 400", w.Code)
	}
}

// --- the report page's reset mail ------------------------------------------------------

type failingMailer struct{ sent int }

func (m *failingMailer) Send(context.Context, mail.Message) error {
	m.sent++
	return errors.New("the mail server refused")
}

type noQuota struct{}

func (noQuota) ConsumeMail(context.Context, string, time.Time) ratelimit.Verdict {
	return ratelimit.Verdict{Allowed: false}
}

func TestAReportResetIsNotSentPastTheQuotaOrWhenMailFails(t *testing.T) {
	f := newFixture(t)
	mailer := &failingMailer{}
	f.handler.Password = &PasswordFlow{Mailer: mailer, BaseURL: "https://auth.example.test"}
	outcome := notMeOutcome{email: "someone@example.test", orgName: "Acme",
		reset: user.Token{Plaintext: "t"}, resetReady: true}

	// Nothing ready: nothing sent.
	if f.handler.sendReportReset(context.Background(), notMeOutcome{}) {
		t.Error("a reset with nothing issued reported as sent")
	}

	// Past the quota: refused before the mailer.
	f.handler.Password.MailLimit = noQuota{}
	if f.handler.sendReportReset(context.Background(), outcome) || mailer.sent != 0 {
		t.Error("a reset past the mail quota was sent")
	}

	// The mailer failing is reported as not sent, so the page says so.
	f.handler.Password.MailLimit = nil
	if f.handler.sendReportReset(context.Background(), outcome) {
		t.Error("a reset the mail server refused reported as sent")
	}
	if mailer.sent != 1 {
		t.Errorf("the mailer was called %d times, want 1", mailer.sent)
	}
}

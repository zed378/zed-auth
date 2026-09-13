package login

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/anomaly"
	"github.com/zed378/zed-auth/backend/internal/authn"
)

// The detection hook (P3-08), without a database.
//
// The detector's own rules are tested in `anomaly`. What is here is the one
// property of the hook that a wrong implementation would break silently: a
// login is observed once it succeeds, never before, and never at the cost of the
// login itself.

type recordingDetector struct {
	seen    chan anomaly.Current
	release chan struct{} // nil: return at once
	panics  bool
}

func (d *recordingDetector) Observe(_ context.Context, c anomaly.Current) {
	d.seen <- c
	if d.release != nil {
		<-d.release
	}
	if d.panics {
		panic("detector exploded")
	}
}

// postWithAgent submits valid credentials with a user agent set.
func (f *fixture) postWithAgent(t *testing.T, agent string) *httptest.ResponseRecorder {
	t.Helper()

	_, csrf := f.get(t)
	form := credentials()
	form.Set(csrfField, csrf)
	form.Set("request", testPendingID)

	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("User-Agent", agent)
	r.RemoteAddr = "198.51.100.7:44321"
	withCSRF(r, csrf)

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) signInable() {
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = true
}

func TestASuccessfulLoginIsObservedForAnomalies(t *testing.T) {
	f := newFixture(t)
	f.signInable()
	detector := &recordingDetector{seen: make(chan anomaly.Current, 1)}
	f.handler.Anomalies = detector

	if w := f.postWithAgent(t, "agent/1.0"); w.Code != http.StatusFound {
		t.Fatalf("the login answered %d", w.Code)
	}

	select {
	case c := <-detector.seen:
		if c.SessionID != "33333333-3333-3333-3333-333333333333" {
			t.Errorf("SessionID = %q, want the session just created", c.SessionID)
		}
		if c.UserID != testUserID || c.OrgID != testOrgID {
			t.Errorf("observed user=%q org=%q", c.UserID, c.OrgID)
		}
		if c.UserAgent != "agent/1.0" {
			t.Errorf("UserAgent = %q", c.UserAgent)
		}
		if c.IP != "198.51.100.7" {
			t.Errorf("IP = %q, want the client address without its port", c.IP)
		}
		if c.At.IsZero() {
			t.Error("At is zero")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a successful login was never observed")
	}
}

// A refused login is not a login, and must not become history-shaped noise.
func TestARefusedLoginIsNotObserved(t *testing.T) {
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
	f.users.verified = false
	detector := &recordingDetector{seen: make(chan anomaly.Current, 1)}
	f.handler.Anomalies = detector

	f.postWithAgent(t, "agent/1.0")

	select {
	case <-detector.seen:
		t.Error("a refused login was observed for anomalies")
	case <-time.After(300 * time.Millisecond):
	}
}

// A detector that never returns does not hold the login.
func TestASlowDetectorDoesNotDelayTheLogin(t *testing.T) {
	f := newFixture(t)
	f.signInable()
	detector := &recordingDetector{seen: make(chan anomaly.Current, 1), release: make(chan struct{})}
	defer close(detector.release)
	f.handler.Anomalies = detector

	done := make(chan int, 1)
	go func() { done <- f.postWithAgent(t, "agent/1.0").Code }()

	select {
	case code := <-done:
		if code != http.StatusFound {
			t.Errorf("the login answered %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the login waited on the anomaly detector")
	}
}

// syncBuffer is a log sink safe to read while a goroutine writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A panicking detector is recovered. Unrecovered, it would end the process —
// and every other user's login with it.
func TestAPanickingDetectorIsRecovered(t *testing.T) {
	f := newFixture(t)
	f.signInable()
	logs := &syncBuffer{}
	f.handler.Log = slog.New(slog.NewTextHandler(logs, nil))
	f.handler.Anomalies = &recordingDetector{seen: make(chan anomaly.Current, 1), panics: true}

	if w := f.postWithAgent(t, "agent/1.0"); w.Code != http.StatusFound {
		t.Fatalf("the login answered %d", w.Code)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "login anomaly detection panicked") {
		if time.Now().After(deadline) {
			t.Fatal("the panic was never recovered and logged")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// --- the report page's own edges -------------------------------------------------------------

func TestTheReportPageWithoutConfigurationSaysSo(t *testing.T) {
	f := newFixture(t)

	r := httptest.NewRequest(http.MethodGet, NotMePath+"?token=anything", nil)
	w := httptest.NewRecorder()
	f.handler.NotMe(w, r)

	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "not configured") {
		t.Errorf("an unconfigured report page answered %d:\n%s", w.Code, w.Body.String())
	}

	// PasswordFlow alone is not enough: the report flow's revokers are what
	// make the page do anything.
	f.handler.Password = &PasswordFlow{}
	w = httptest.NewRecorder()
	f.handler.NotMe(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("a report page with no revokers answered %d", w.Code)
	}
}

func TestTheReportPageRefusesOtherMethods(t *testing.T) {
	f := newFixture(t)
	f.handler.Password = &PasswordFlow{}
	f.handler.Reports = &NotMeFlow{}

	r := httptest.NewRequest(http.MethodDelete, NotMePath, nil)
	w := httptest.NewRecorder()
	f.handler.NotMe(w, r)

	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, POST" {
		t.Errorf("DELETE answered %d with Allow %q", w.Code, w.Header().Get("Allow"))
	}
}

// A link with no token never reaches the lookup.
func TestAReportLinkWithNoTokenIsInvalid(t *testing.T) {
	f := newFixture(t)
	f.handler.Password = &PasswordFlow{}
	f.handler.Reports = &NotMeFlow{}
	f.handler.Password.Lookup = nil // a call would panic; the empty token must not make one

	r := httptest.NewRequest(http.MethodGet, NotMePath, nil)
	w := httptest.NewRecorder()
	f.handler.NotMe(w, r)

	if !strings.Contains(w.Body.String(), "This link is not valid") {
		t.Errorf("a link with no token answered:\n%s", w.Body.String())
	}
}

// The confirmation page is a destructive action: its button uses the danger
// style, and the page loads nothing but its own hashed stylesheet.
func TestTheReportPageIsLockedDown(t *testing.T) {
	page := NotMePage{CSRFToken: "c", Token: "t"}
	page.Style, page.StyleHash = renderStyle(DefaultAccent)

	body, err := render(notMeTemplate, page)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	html := string(body)

	if !strings.Contains(html, `class="danger"`) {
		t.Error("the sign-out-everywhere button is not styled as destructive")
	}
	if strings.Contains(html, "<script") {
		t.Error("the report page carries a script")
	}
	if !strings.Contains(html, `<meta name="referrer" content="no-referrer">`) {
		t.Error("the page would leak its token-bearing URL in a Referer header")
	}
	if !strings.Contains(html, `action="/account/not-me"`) || strings.Contains(html, `action="/account/not-me?`) {
		t.Error("the form must post to the bare path, with the token in the body")
	}

	csp := page.ContentSecurityPolicy()
	for _, want := range []string{"default-src 'none'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the CSP lacks %q: %s", want, csp)
		}
	}
}

// --- the notifier without mail ----------------------------------------------------------------

func TestANotifierWithoutMailSendsNothingAndFailsNothing(t *testing.T) {
	var n *AnomalyMail
	if err := n.NotifyAnomaly(context.Background(), anomaly.Finding{}); err != nil {
		t.Errorf("a nil notifier returned %v", err)
	}
	if err := (&AnomalyMail{}).NotifyAnomaly(context.Background(), anomaly.Finding{}); err != nil {
		t.Errorf("a notifier with no mailer returned %v", err)
	}
}

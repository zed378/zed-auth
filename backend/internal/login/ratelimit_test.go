package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"html/template"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
)

// What the login handler does with the limiter. The limiter's own progression
// is tested in internal/ratelimit, where it is a pure function; what is here
// is the wiring, which is where the two failures that matter live: doing the
// expensive work anyway, and telling an attacker something.

type fakeLimiter struct {
	refuse     bool
	retryAfter time.Duration
	bound      string

	checked   []string
	failed    []string
	succeeded []string
	startsOn  int // the nth Fail call starts a cooldown; 0 = never
}

func (l *fakeLimiter) Check(_ context.Context, address, ip string, _ time.Time) (ratelimit.Decision, string) {
	l.checked = append(l.checked, address+"|"+ip)
	if l.refuse {
		return ratelimit.Decision{Allowed: false, RetryAfter: l.retryAfter}, l.bound
	}
	return ratelimit.Decision{Allowed: true}, ""
}

func (l *fakeLimiter) Fail(_ context.Context, address, ip string, _ time.Time) (bool, string) {
	l.failed = append(l.failed, address+"|"+ip)
	return l.startsOn != 0 && len(l.failed) == l.startsOn, ratelimit.BoundAddress
}

func (l *fakeLimiter) Succeed(_ context.Context, address string) {
	l.succeeded = append(l.succeeded, address)
}

type fixedIP struct{ ip string }

func (f fixedIP) Of(*http.Request) string { return f.ip }

func limited(t *testing.T) (*fixture, *fakeLimiter) {
	t.Helper()

	f := newFixture(t)
	l := &fakeLimiter{bound: ratelimit.BoundAddress, retryAfter: time.Minute}
	f.handler.Limiter = l
	f.handler.IP = fixedIP{ip: "203.0.113.7"}
	return f, l
}

// The point of the whole feature: a refused attempt costs no password
// verification. If the check ran after, the limiter would bound how often an
// attacker is TOLD no, not how much work they can make the service do.
func TestARefusedAttemptNeverReachesThePassword(t *testing.T) {
	f, l := limited(t)
	l.refuse = true
	f.users.verified = true // would succeed if it got that far

	w := f.post(t, credentials(), "")

	if len(f.users.rehashed) != 0 || len(f.sessions.created) != 0 {
		t.Fatal("a rate-limited attempt authenticated")
	}
	if len(l.checked) == 0 {
		t.Fatal("the limiter was not consulted")
	}
	if !strings.Contains(w.Body.String(), template.HTMLEscapeString(MsgRateLimited)) {
		t.Errorf("the refusal does not say what happened:\n%s", w.Body.String())
	}
}

// The message is specific and true, which every other refusal on this page is
// deliberately not. That is safe because the counter is keyed on the SUBMITTED
// ADDRESS: it exists for an address with no account exactly as for a real one,
// so the message tells an attacker only about their own behaviour.
//
// This test pins the distinction: a rate-limited answer and a wrong-password
// answer are DIFFERENT, on purpose, and the reason they may be is the keying.
func TestTheRateLimitedMessageIsDistinctFromTheCredentialMessage(t *testing.T) {
	refused, l := limited(t)
	l.refuse = true
	a := refused.post(t, credentials(), "")

	wrong, _ := limited(t)
	b := wrong.post(t, credentials(), "")

	if a.Body.String() == b.Body.String() {
		t.Fatal("a rate-limited attempt is indistinguishable from a wrong password; " +
			"the user would keep retrying through the cooldown")
	}
	if !strings.Contains(b.Body.String(), template.HTMLEscapeString(MsgCredentials)) {
		t.Error("the control case is not the credential message")
	}
}

// FR-9. The address counter clears on success; the IP counter does NOT, or one
// valid credential of an attacker's own would clear the bound for everybody
// sharing that address.
func TestASuccessClearsTheAddressCounterOnly(t *testing.T) {
	f, l := limited(t)
	f.users.user = activeUser()
	f.users.verified = true

	f.post(t, credentials(), "")

	if len(l.succeeded) != 1 {
		t.Fatalf("Succeed was called %d times", len(l.succeeded))
	}
	if l.succeeded[0] != "someone@example.test" {
		t.Errorf("cleared %q", l.succeeded[0])
	}
	// Succeed takes only the address — there is no parameter through which the
	// IP counter could be cleared, which is the point.
}

// Every credential failure feeds the limiter, including one for an address
// with no account. A limiter fed only on real accounts would be a limiter that
// tells an attacker which addresses exist by which ones slow down.
func TestEveryCredentialFailureFeedsTheLimiter(t *testing.T) {
	cases := map[string]func(*fixture){
		"unknown address": func(f *fixture) { f.users.user, f.users.verified = authnUser(""), false },
		"wrong password":  func(f *fixture) { f.users.user, f.users.verified = activeUser(), false },
		"locked account":  func(f *fixture) { f.users.user, f.users.verified = lockedUser(), true },
	}

	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f, l := limited(t)
			arrange(f)

			f.post(t, credentials(), "")

			if len(l.failed) != 1 {
				t.Fatalf("Fail was called %d times", len(l.failed))
			}
			if !strings.HasPrefix(l.failed[0], "someone@example.test|") {
				t.Errorf("the counter was fed %q, want the submitted address", l.failed[0])
			}
		})
	}
}

// A successful login must NOT feed the failure counter, or a user who signs in
// every morning would eventually lock themselves out.
func TestASuccessDoesNotFeedTheFailureCounter(t *testing.T) {
	f, l := limited(t)
	f.users.user = activeUser()
	f.users.verified = true

	f.post(t, credentials(), "")

	if len(l.failed) != 0 {
		t.Errorf("a successful login recorded %d failure(s)", len(l.failed))
	}
}

// One audit entry per cooldown, not one per attempt. Auditing every refusal
// would let an attacker write to an append-only table as fast as they can send
// requests — a different attack, handed to them by the control meant to stop
// the first.
func TestALockoutIsAuditedOncePerCooldown(t *testing.T) {
	f, l := limited(t)
	l.startsOn = 3 // the third failure begins the cooldown

	for range 5 {
		f.post(t, credentials(), "")
	}

	var lockouts int
	for _, e := range f.auditor.events {
		if e.Type == audit.EventUserLockedOut {
			lockouts++
			// The bound, and NOT the address. P1-12 explains why the audit log
			// must not become a list of addresses somebody tried.
			for key, value := range e.Payload {
				if s, ok := value.(string); ok && strings.Contains(s, "@") {
					t.Errorf("the lockout entry carries an address under %q", key)
				}
			}
			if e.Payload["bound"] == nil {
				t.Error("the entry does not say which bound refused")
			}
		}
	}

	if lockouts != 1 {
		t.Errorf("%d lockout events for one cooldown across five attempts", lockouts)
	}
}

// The limiter is fed the resolved client IP, not RemoteAddr. On the deployment
// this service runs on those differ, and using the wrong one makes the per-IP
// bound global.
func TestTheLimiterIsFedTheResolvedClientIP(t *testing.T) {
	f, l := limited(t)

	f.post(t, credentials(), "")

	if len(l.checked) == 0 {
		t.Fatal("the limiter was not consulted")
	}
	if !strings.HasSuffix(l.checked[0], "|203.0.113.7") {
		t.Errorf("the limiter was fed %q, want the resolved client IP", l.checked[0])
	}
}

// FR-6, at this layer: the handler asks the resolver, and the resolver decides
// whether a header is believed. A handler that read the header itself would be
// a handler somebody could bypass by setting it.
func TestTheHandlerDoesNotReadForwardingHeadersItself(t *testing.T) {
	f, l := limited(t)
	f.handler.IP = nil // no resolver configured: RemoteAddr, unforgeable

	form := credentials()
	form.Set("request", testPendingID)
	_, csrf := f.get(t)
	form.Set(csrfField, csrf)

	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("CF-Connecting-IP", "198.51.100.2")
	r.RemoteAddr = "172.18.0.1:5000"
	withCSRF(r, csrf)

	f.handler.ServeHTTP(httptest.NewRecorder(), r)

	for _, seen := range append(l.checked, l.failed...) {
		if strings.Contains(seen, "198.51.100") {
			t.Errorf("a client-supplied header reached the limiter: %q", seen)
		}
		if !strings.HasSuffix(seen, "|172.18.0.1") {
			t.Errorf("the limiter was fed %q, want the peer address", seen)
		}
	}
}

// With no limiter the page behaves exactly as it did before P1-13 — which is
// what every test in this package that is not about limiting relies on.
func TestWithNoLimiterNothingChanges(t *testing.T) {
	f := newFixture(t)
	f.users.user = activeUser()
	f.users.verified = true

	if w := f.post(t, credentials(), ""); w.Code != http.StatusFound {
		t.Fatalf("a login without a limiter answered %d", w.Code)
	}
}

// --- helpers ------------------------------------------------------------------------

func activeUser() authn.User {
	return authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusActive}
}

func lockedUser() authn.User {
	return authn.User{ID: testUserID, OrgID: testOrgID, Status: authn.StatusLocked}
}

func authnUser(id string) authn.User {
	return authn.User{ID: id, OrgID: testOrgID, Status: authn.StatusActive}
}

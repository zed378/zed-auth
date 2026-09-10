package userinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What the caller can observe: the status, the challenge header, and the exact
// body. The database side is exercised against real Postgres in
// userinfo_integration_test.go.

const presentedToken = "header.payload.signature-that-must-never-be-logged"

type fakeVerifier struct {
	payload  []byte
	err      error
	wantType string
}

func (f *fakeVerifier) Verify(_, wantType string) ([]byte, error) {
	f.wantType = wantType
	if f.err != nil {
		return nil, f.err
	}
	return f.payload, nil
}

type fakeSubjects struct {
	subject Subject
	email   string
	err     error
}

func (f fakeSubjects) Subject(
	context.Context, *postgres.DB, AccessToken, time.Time,
) (Subject, string, error) {
	return f.subject, f.email, f.err
}

type recordingObserver struct{ outcomes []string }

func (r *recordingObserver) UserInfo(outcome string, _ time.Duration) {
	r.outcomes = append(r.outcomes, outcome)
}

type fixture struct {
	handler  *Handler
	verifier *fakeVerifier
	subjects *fakeSubjects
	observer *recordingObserver
	logs     *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	now := time.Now()
	logs := &bytes.Buffer{}

	f := &fixture{
		verifier: &fakeVerifier{payload: payloadOf(t, accessToken(now))},
		subjects: &fakeSubjects{subject: subject(), email: subjectEmail},
		observer: &recordingObserver{},
		logs:     logs,
	}

	f.handler = &Handler{
		Issuer:   testIssuer,
		Verifier: f.verifier,
		Subjects: f.subjects,
		Observer: f.observer,
		Log:      slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:      func() time.Time { return now },
	}
	return f
}

func (f *fixture) get(t *testing.T, header string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) authorized(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.get(t, "Bearer "+presentedToken)
}

// --- the happy path -----------------------------------------------------------

func TestAValidTokenReturnsTheScopedClaims(t *testing.T) {
	f := newFixture(t)

	w := f.authorized(t)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; this is personal data addressed to one caller", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	var claims map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &claims); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}

	got := make([]string, 0, len(claims))
	for k := range claims {
		got = append(got, k)
	}
	sort.Strings(got)

	want := []string{"email", "name", "preferred_username", "sub", "updated_at"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("claims = %v, want exactly %v", got, want)
	}
}

// The `typ` the handler asks for is the one thing standing between an id_token
// and a userinfo response. Asserted on the call, because a handler that passed
// TypeJWT here would accept exactly the token abuse case A-5 is about.
func TestTheHandlerAsksForAnAccessToken(t *testing.T) {
	f := newFixture(t)

	f.authorized(t)

	if f.verifier.wantType != signing.TypeAccessToken {
		t.Errorf("the handler verified with typ %q, want %q",
			f.verifier.wantType, signing.TypeAccessToken)
	}
}

// DoD item 3. The token the handler is holding carries org_id, sid and
// client_id; none of them may reach the response.
func TestNoInternalIdentifierReachesTheResponse(t *testing.T) {
	f := newFixture(t)
	token := accessToken(time.Now())

	body := f.authorized(t).Body.String()

	for _, secret := range []string{token.OrgID, token.SessionID, token.ClientID} {
		if strings.Contains(body, secret) {
			t.Errorf("the response carries %q", secret)
		}
	}
	for _, key := range []string{"org_id", "sid", "client_id", "session_id"} {
		if strings.Contains(body, `"`+key+`"`) {
			t.Errorf("the response carries the %q claim", key)
		}
	}
}

// --- refusals -------------------------------------------------------------------

// RFC 6750 §3.1: a request with NO credential gets the bare challenge. An
// error code describes a credential that was presented, and there was none —
// a client that sees error="invalid_token" on its first unauthenticated probe
// will conclude its token is broken rather than absent.
func TestAMissingCredentialGetsTheBareChallenge(t *testing.T) {
	f := newFixture(t)

	w := f.get(t, "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	challenge := w.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Bearer ") {
		t.Fatalf("WWW-Authenticate = %q", challenge)
	}
	if strings.Contains(challenge, "error=") {
		t.Errorf("the challenge names an error for a request that presented no "+
			"credential: %q", challenge)
	}
	if !strings.Contains(challenge, `realm="`+testIssuer+`"`) {
		t.Errorf("the challenge has no realm: %q", challenge)
	}
}

func TestAnUnusableTokenGetsInvalidToken(t *testing.T) {
	for name, arrange := range map[string]func(*fixture){
		"a bad signature": func(f *fixture) {
			f.verifier.err = signing.ErrInvalidSignature
		},
		"an id_token presented as a bearer token": func(f *fixture) {
			f.verifier.err = signing.ErrWrongType
		},
		"a retired key": func(f *fixture) {
			f.verifier.err = errors.New("no key with that id")
		},
		"expired": func(f *fixture) {
			token := accessToken(time.Now().Add(-time.Hour))
			f.verifier.payload = mustJSON(token)
		},
		"another issuer": func(f *fixture) {
			token := accessToken(time.Now())
			token.Issuer = "https://other.example"
			f.verifier.payload = mustJSON(token)
		},
		"a client_credentials token": func(f *fixture) {
			token := accessToken(time.Now())
			token.SessionID = ""
			f.verifier.payload = mustJSON(token)
		},
		"a revoked session": func(f *fixture) {
			f.subjects.err = ErrNoSubject
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			arrange(f)

			w := f.authorized(t)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401:\n%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
				t.Errorf("WWW-Authenticate = %q", w.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

// The property the spec is built around: every unusable token produces the
// SAME response. A caller that could tell a revoked session from a bad
// signature could ask this endpoint whether somebody has logged out.
func TestEveryRefusalIsIndistinguishable(t *testing.T) {
	arrange := map[string]func(*fixture){
		"a bad signature": func(f *fixture) { f.verifier.err = signing.ErrInvalidSignature },
		"the wrong type":  func(f *fixture) { f.verifier.err = signing.ErrWrongType },
		"expired": func(f *fixture) {
			f.verifier.payload = mustJSON(accessToken(time.Now().Add(-time.Hour)))
		},
		"another audience": func(f *fixture) {
			token := accessToken(time.Now())
			token.Audience = token.ClientID
			f.verifier.payload = mustJSON(token)
		},
		"a revoked session":   func(f *fixture) { f.subjects.err = ErrNoSubject },
		"a deactivated user":  func(f *fixture) { f.subjects.err = ErrNoSubject },
		"no subject at all":   func(f *fixture) { f.subjects.err = ErrNoSubject },
		"a malformed payload": func(f *fixture) { f.verifier.payload = []byte("not json") },
	}

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

		w := f.authorized(t)
		got := response{status: w.Code, headers: headerDump(w), body: w.Body.String()}

		if refName == "" {
			reference, refName = got, name
			continue
		}
		if got.status != reference.status {
			t.Errorf("%q answers %d and %q answers %d", name, got.status, refName, reference.status)
		}
		if got.headers != reference.headers {
			t.Errorf("%q and %q differ in their headers:\n%s\n---\n%s",
				name, refName, got.headers, reference.headers)
		}
		if got.body != reference.body {
			t.Errorf("%q and %q differ in their bodies", name, refName)
		}
	}

	// The control. Without it the comparison would pass against a handler that
	// answered every request with an empty 500.
	if reference.status != http.StatusUnauthorized ||
		!strings.Contains(reference.headers, `error="invalid_token"`) {
		t.Fatalf("the shared response is not a 401 invalid_token; the comparison "+
			"is over the wrong pages:\n%d\n%s", reference.status, reference.headers)
	}
}

// The control for the test above: a SUCCESSFUL request is distinguishable.
func TestASuccessIsDistinguishableFromARefusal(t *testing.T) {
	f := newFixture(t)

	if w := f.authorized(t); w.Code != http.StatusOK {
		t.Fatalf("a valid token answered %d; the uniformity test proves nothing", w.Code)
	}
}

// insufficient_scope is 403 and names the scope. Not a disclosure — the caller
// already knows what it asked for — and conflating it with invalid_token sends
// a working client into a re-authentication loop that cannot help it.
func TestAMissingScopeIsAnsweredSeparately(t *testing.T) {
	f := newFixture(t)
	token := accessToken(time.Now())
	token.Scope = "profile email"
	f.verifier.payload = mustJSON(token)

	w := f.authorized(t)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403:\n%s", w.Code, w.Body.String())
	}
	challenge := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `error="insufficient_scope"`) {
		t.Errorf("WWW-Authenticate = %q", challenge)
	}
	if !strings.Contains(challenge, `scope="openid"`) {
		t.Errorf("the challenge does not name the scope required: %q", challenge)
	}
}

// --- what must never be written ---------------------------------------------------

// CLAUDE.md: never log tokens. Checked across every branch, because the branch
// that logs one is the branch nobody looked at.
func TestTheTokenIsNeverLoggedOrReturned(t *testing.T) {
	branches := map[string]func(*fixture){
		"success":        func(*fixture) {},
		"bad signature":  func(f *fixture) { f.verifier.err = signing.ErrInvalidSignature },
		"expired":        func(f *fixture) { f.verifier.payload = mustJSON(accessToken(time.Now().Add(-time.Hour))) },
		"no subject":     func(f *fixture) { f.subjects.err = ErrNoSubject },
		"missing scope":  func(f *fixture) { f.verifier.payload = scopeless() },
		"store failure":  func(f *fixture) { f.subjects.err = errors.New("the database is down") },
		"malformed body": func(f *fixture) { f.verifier.payload = []byte("{{{") },
	}

	for name, arrange := range branches {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			arrange(f)

			w := f.authorized(t)

			if strings.Contains(f.logs.String(), presentedToken) {
				t.Errorf("the access token was written to the log:\n%s", f.logs.String())
			}
			if strings.Contains(w.Body.String(), presentedToken) {
				t.Error("the access token was echoed into the response")
			}
		})
	}
}

// A failure the operator has to be able to diagnose still says why, in the log
// — which is the whole point of separating the reason from the response.
func TestTheReasonIsLoggedEvenThoughItIsNotReturned(t *testing.T) {
	f := newFixture(t)
	f.verifier.payload = mustJSON(accessToken(time.Now().Add(-time.Hour)))

	w := f.authorized(t)

	if !strings.Contains(f.logs.String(), "expired") {
		t.Errorf("the log does not say why the token was refused:\n%s", f.logs.String())
	}
	if strings.Contains(w.Body.String(), "expired") {
		t.Error("the response says the token expired, which tells a caller holding " +
			"a captured token that it was otherwise genuine")
	}
}

// --- metrics and method routing ----------------------------------------------------

// The metric label must not separate the refusal kinds, or the oracle the body
// refuses to be moves into /metrics — which is scraped and retained.
func TestTheMetricDoesNotSeparateRefusalKinds(t *testing.T) {
	for _, arrange := range []func(*fixture){
		func(f *fixture) { f.verifier.err = signing.ErrInvalidSignature },
		func(f *fixture) { f.subjects.err = ErrNoSubject },
		func(f *fixture) { f.verifier.payload = mustJSON(accessToken(time.Now().Add(-time.Hour))) },
	} {
		f := newFixture(t)
		arrange(f)
		f.authorized(t)

		if len(f.observer.outcomes) != 1 || f.observer.outcomes[0] != OutcomeInvalidToken {
			t.Errorf("outcomes = %v, want exactly [%s]", f.observer.outcomes, OutcomeInvalidToken)
		}
	}

	f := newFixture(t)
	f.authorized(t)
	if len(f.observer.outcomes) != 1 || f.observer.outcomes[0] != OutcomeOK {
		t.Errorf("a success was counted as %v", f.observer.outcomes)
	}
}

// OIDC Core 5.3.1 requires both GET and POST.
func TestBothMethodsAreAccepted(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		f := newFixture(t)

		r := httptest.NewRequest(method, "/oauth/userinfo", nil)
		r.Header.Set("Authorization", "Bearer "+presentedToken)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("%s answered %d", method, w.Code)
		}
	}
}

func TestOtherMethodsAreRefused(t *testing.T) {
	f := newFixture(t)

	r := httptest.NewRequest(http.MethodDelete, "/oauth/userinfo", nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, POST" {
		t.Errorf("Allow = %q", got)
	}
}

// RFC 6750 §2.3 defines an access_token query parameter and calls it NOT
// RECOMMENDED. Accepting one would put a credential in every proxy log, the
// browser history, and the Referer of whatever loads next.
func TestATokenInTheQueryStringIsNotAccepted(t *testing.T) {
	f := newFixture(t)

	r := httptest.NewRequest(http.MethodGet, "/oauth/userinfo?access_token="+presentedToken, nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; a token in the query string was accepted", w.Code)
	}
}

// --- helpers ------------------------------------------------------------------------

func mustJSON(token AccessToken) []byte {
	raw, err := json.Marshal(token)
	if err != nil {
		panic(err)
	}
	return raw
}

func scopeless() []byte {
	token := accessToken(time.Now())
	token.Scope = "profile"
	return mustJSON(token)
}

// headerDump renders every header in a stable order, so two responses are
// compared including the ones a hand-written list would forget.
func headerDump(w *httptest.ResponseRecorder) string {
	result := w.Result()
	names := make([]string, 0, len(result.Header))
	for name := range result.Header {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	for _, name := range names {
		for _, value := range result.Header[name] {
			out.WriteString(name + ": " + value + "\n")
		}
	}
	return out.String()
}

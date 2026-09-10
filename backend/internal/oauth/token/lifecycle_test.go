package token

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What a caller can observe. The database side — the ownership rule against
// two real clients, family revocation, the audit entry — is in
// token_integration_test.go against real Postgres.

const (
	lifeIssuer   = "https://auth.example"
	lifeClientID = "11111111-1111-1111-1111-111111111111"
	lifeOtherID  = "99999999-9999-9999-9999-999999999999"
	lifeUserID   = "44444444-4444-4444-4444-444444444444"
	lifeOrgID    = "22222222-2222-2222-2222-222222222222"
	lifeSession  = "33333333-3333-3333-3333-333333333333"
	lifeSecret   = "a-secret-that-is-long-enough-to-be-real"
)

// --- fakes ---------------------------------------------------------------------

type lifeClients struct {
	app     client.Application
	creds   client.Credentials
	err     error
	credErr error
}

func (c lifeClients) ByClientID(context.Context, string) (client.Application, error) {
	return c.app, c.err
}

func (c lifeClients) CredentialsFor(context.Context, client.Application) (client.Credentials, error) {
	return c.creds, c.credErr
}

type lifeVerifier struct {
	payload  []byte
	err      error
	wantType string
}

func (v *lifeVerifier) Verify(_, wantType string) ([]byte, error) {
	v.wantType = wantType
	if v.err != nil {
		return nil, v.err
	}
	return v.payload, nil
}

// lifeRefresh resolves nothing unless a test says otherwise, so the negative
// paths — which all fall through the access-token check into the refresh
// fallback — need no database.
type lifeRefresh struct {
	stored Refresh
	found  bool
}

func (r lifeRefresh) Lookup(context.Context, string, time.Time) (Refresh, error) {
	if !r.found {
		return Refresh{}, ErrRefreshNotFound
	}
	return r.stored, nil
}

func (r lifeRefresh) RevokeFamily(context.Context, *postgres.Tx, string) (int64, error) {
	return 0, nil
}

func (r lifeRefresh) RevokeForSessionAndClient(
	context.Context, *postgres.Tx, string, string,
) (int64, error) {
	return 0, nil
}

// lifeTenant runs the work with a nil transaction, which lifeRefresh and
// lifeAuditor both ignore.
type lifeTenant struct{ err error }

func (t lifeTenant) WithTenant(_ context.Context, _ string, fn func(*postgres.Tx) error) error {
	if t.err != nil {
		return t.err
	}
	return fn(nil)
}

type lifeAuditor struct{ events []audit.Event }

func (a *lifeAuditor) Write(_ context.Context, _ *postgres.Tx, e audit.Event) error {
	a.events = append(a.events, e)
	return nil
}

type lifeSessions struct{ live bool }

func (s lifeSessions) IsLive(context.Context, string, time.Time) bool { return s.live }

type lifeObserver struct{ seen []string }

func (o *lifeObserver) Lifecycle(endpoint, outcome string) {
	o.seen = append(o.seen, endpoint+":"+outcome)
}

// --- fixture -----------------------------------------------------------------------

type lifeFixture struct {
	handler  *LifecycleHandler
	clients  *lifeClients
	verifier *lifeVerifier
	observer *lifeObserver
	auditor  *lifeAuditor
	now      time.Time
}

func confidentialApp() client.Application {
	return client.Application{
		ID: lifeClientID, OrgID: lifeOrgID, Type: client.TypeWeb,
		GrantTypes: []string{GrantAuthorizationCode, GrantRefreshToken},
	}
}

func accessClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss":       lifeIssuer,
		"sub":       lifeUserID,
		"aud":       lifeIssuer,
		"exp":       float64(now.Add(10 * time.Minute).Unix()),
		"iat":       float64(now.Unix()),
		"client_id": lifeClientID,
		"org_id":    lifeOrgID,
		"sid":       lifeSession,
		"scope":     "openid profile",
	}
}

func newLifeFixture(t *testing.T) *lifeFixture {
	t.Helper()

	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	payload, err := json.Marshal(accessClaims(now))
	if err != nil {
		t.Fatalf("marshalling claims: %v", err)
	}

	f := &lifeFixture{
		clients: &lifeClients{
			app:   confidentialApp(),
			creds: client.Credentials{Hash: client.Hash(lifeSecret)},
		},
		verifier: &lifeVerifier{payload: payload},
		observer: &lifeObserver{},
		auditor:  &lifeAuditor{},
		now:      now,
	}

	f.handler = &LifecycleHandler{
		Issuer:   lifeIssuer,
		Clients:  f.clients,
		Verifier: f.verifier,
		Refresh:  lifeRefresh{},
		Tenant:   lifeTenant{},
		Audit:    f.auditor,
		Sessions: lifeSessions{live: true},
		Observer: f.observer,
		Now:      func() time.Time { return now },
	}
	return f
}

func (f *lifeFixture) call(
	t *testing.T, target string, form url.Values, authenticate bool,
) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authenticate {
		r.SetBasicAuth(url.QueryEscape(lifeClientID), url.QueryEscape(lifeSecret))
	}

	w := httptest.NewRecorder()
	if strings.Contains(target, "introspect") {
		f.handler.Introspect(w, r)
	} else {
		f.handler.Revoke(w, r)
	}
	return w
}

func (f *lifeFixture) introspect(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, "/oauth/introspect", form, true)
}

func token(value string) url.Values { return url.Values{"token": {value}} }

// --- client authentication ------------------------------------------------------

// DoD item 1, and step 1 of the card: an unauthenticated introspection
// endpoint is a token oracle.
func TestUnauthenticatedIntrospectionIsRejected(t *testing.T) {
	f := newLifeFixture(t)

	w := f.call(t, "/oauth/introspect", token("anything"), false)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ErrInvalidClient) {
		t.Errorf("error = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"active"`) {
		t.Error("an unauthenticated caller learned something about the token")
	}
}

func TestAWrongSecretIsRejected(t *testing.T) {
	f := newLifeFixture(t)

	r := httptest.NewRequest(http.MethodPost, "/oauth/introspect",
		strings.NewReader(token("anything").Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(lifeClientID, "not-the-secret")

	w := httptest.NewRecorder()
	f.handler.Introspect(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// Stricter than RFC 7662, deliberately. A public client has no secret, so
// admitting one would make introspection reachable by anyone who can read a
// client_id out of a browser URL — which is the token oracle step 1 forbids.
func TestAPublicClientCannotIntrospect(t *testing.T) {
	f := newLifeFixture(t)
	f.clients.app.Type = client.TypeSPA
	f.clients.creds = client.Credentials{}

	r := httptest.NewRequest(http.MethodPost, "/oauth/introspect",
		strings.NewReader(url.Values{"token": {"x"}, "client_id": {lifeClientID}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	f.handler.Introspect(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "confidential") {
		t.Errorf("the refusal does not explain itself to an integrator: %s", w.Body.String())
	}
}

// Revocation admits public clients, which RFC 7009 §2.1 contemplates. The
// reasoning differs because the operation does: a caller can only destroy a
// token belonging to its own client, so the worst a forged caller achieves is
// throwing away a credential it already held.
func TestAPublicClientMayRevoke(t *testing.T) {
	f := newLifeFixture(t)
	f.clients.app.Type = client.TypeSPA
	f.clients.creds = client.Credentials{}
	f.verifier.err = signing.ErrInvalidSignature // nothing resolves; 200 regardless

	r := httptest.NewRequest(http.MethodPost, "/oauth/revoke",
		strings.NewReader(url.Values{"token": {"x"}, "client_id": {lifeClientID}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	f.handler.Revoke(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200:\n%s", w.Code, w.Body.String())
	}
}

// --- the uniform negative -----------------------------------------------------------

// DoD item 2. An unknown, an expired and a revoked token must be
// indistinguishable — compared as whole responses, because "both say inactive"
// would pass while a Content-Length differed.
func TestEveryInactiveAnswerIsIdentical(t *testing.T) {
	arrange := map[string]func(*lifeFixture){
		"not a token at all": func(f *lifeFixture) {
			f.verifier.err = errors.New("parsing token: malformed")
		},
		"a bad signature": func(f *lifeFixture) {
			f.verifier.err = signing.ErrInvalidSignature
		},
		"an id_token": func(f *lifeFixture) {
			f.verifier.err = signing.ErrWrongType
		},
		"expired": func(f *lifeFixture) {
			claims := accessClaims(f.now)
			claims["exp"] = float64(f.now.Add(-time.Second).Unix())
			f.verifier.payload = mustMarshal(claims)
		},
		"another issuer": func(f *lifeFixture) {
			claims := accessClaims(f.now)
			claims["iss"] = "https://other.example"
			f.verifier.payload = mustMarshal(claims)
		},
		"another client's token": func(f *lifeFixture) {
			claims := accessClaims(f.now)
			claims["client_id"] = lifeOtherID
			f.verifier.payload = mustMarshal(claims)
		},
		"its session has ended": func(f *lifeFixture) {
			f.handler.Sessions = lifeSessions{live: false}
		},
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
		f := newLifeFixture(t)
		setup(f)
		w := f.introspect(t, token("presented-value"))
		got := response{status: w.Code, headers: lifeHeaders(w), body: w.Body.String()}

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
			t.Errorf("%q and %q differ in their bodies:\n%s\n---\n%s",
				name, refName, got.body, reference.body)
		}
	}

	// The control. Without it the comparison would pass against a handler that
	// answered everything with an empty 500.
	if reference.status != http.StatusOK ||
		strings.TrimSpace(reference.body) != `{"active":false}` {
		t.Fatalf("the shared response is not {\"active\":false}: %d %s",
			reference.status, reference.body)
	}
}

// The other control: an ACTIVE token is distinguishable, or the test above
// would pass against a handler that reported everything inactive.
func TestAnActiveTokenIsDistinguishable(t *testing.T) {
	f := newLifeFixture(t)

	w := f.introspect(t, token("a-real-access-token"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["active"] != true {
		t.Fatalf("active = %v, want true:\n%s", body["active"], w.Body.String())
	}
}

// The negative response carries nothing but `active`. A `reason` field would
// be one convenience and three disclosures: whether the token ever existed,
// whether it has been revoked since, and whether it belongs to the client the
// caller is impersonating.
func TestTheInactiveAnswerSaysNothingElse(t *testing.T) {
	f := newLifeFixture(t)
	f.verifier.err = signing.ErrInvalidSignature
	body := decodeBody(t, f.introspect(t, token("x")))

	if len(body) != 1 {
		t.Errorf("the inactive response carries %d fields: %v", len(body), body)
	}
	if body["active"] != false {
		t.Errorf("active = %v", body["active"])
	}
}

// --- the active response ------------------------------------------------------------

func TestTheActiveResponseCarriesWhatAResourceServerNeeds(t *testing.T) {
	f := newLifeFixture(t)

	body := decodeBody(t, f.introspect(t, token("a-real-access-token")))

	for _, want := range []string{"active", "scope", "client_id", "token_type", "exp", "iat", "sub", "aud", "iss"} {
		if _, present := body[want]; !present {
			t.Errorf("the active response is missing %q", want)
		}
	}
	if body["client_id"] != lifeClientID {
		t.Errorf("client_id = %v", body["client_id"])
	}
	if body["iss"] != lifeIssuer {
		t.Errorf("iss = %v", body["iss"])
	}
}

// RFC 7662 lists `username` and this endpoint does not return it: it is an
// email address, the caller already has `sub`, and a resource server that
// wants the address can ask /oauth/userinfo with the user's own token.
func TestTheActiveResponseCarriesNoPersonalIdentifier(t *testing.T) {
	f := newLifeFixture(t)

	body := decodeBody(t, f.introspect(t, token("a-real-access-token")))

	for _, forbidden := range []string{"username", "email", "name", "org_id"} {
		if _, present := body[forbidden]; present {
			t.Errorf("the active response carries %q, which a resource server does "+
				"not need and routinely logs", forbidden)
		}
	}
}

// --- the ownership rule --------------------------------------------------------------

// The card's second abuse case, at the unit level. A token issued to another
// client is answered as unknown, never as forbidden — a "forbidden" confirms
// it exists and belongs to somebody.
func TestAnotherClientsAccessTokenIsInvisible(t *testing.T) {
	f := newLifeFixture(t)
	claims := accessClaims(f.now)
	claims["client_id"] = lifeOtherID
	f.verifier.payload = mustMarshal(claims)
	w := f.introspect(t, token("someone-elses-token"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with active:false, not a refusal", w.Code)
	}
	body := decodeBody(t, w)
	if body["active"] != false {
		t.Error("a client introspected another client's token")
	}
	if len(body) != 1 {
		t.Errorf("the answer leaked detail about another client's token: %v", body)
	}
}

// --- the token type hint --------------------------------------------------------------

// RFC 7662 §2.1: a hint decides the ORDER and never the outcome. A caller that
// mislabels its own token still holds a real token.
func TestAWrongHintStillResolvesTheToken(t *testing.T) {
	f := newLifeFixture(t)
	body := decodeBody(t, f.introspect(t, url.Values{
		"token":           {"a-real-access-token"},
		"token_type_hint": {HintRefreshToken}, // wrong
	}))

	if body["active"] != true {
		t.Error("a wrong token_type_hint made a real access token report inactive")
	}
}

func TestTheHandlerVerifiesAgainstTheAccessTokenType(t *testing.T) {
	f := newLifeFixture(t)

	f.introspect(t, token("x"))

	if f.verifier.wantType != signing.TypeAccessToken {
		t.Errorf("verified with typ %q, want %q — an id_token would be introspectable",
			f.verifier.wantType, signing.TypeAccessToken)
	}
}

// --- shape -------------------------------------------------------------------------

func TestBothEndpointsRefuseGET(t *testing.T) {
	f := newLifeFixture(t)

	for name, serve := range map[string]http.HandlerFunc{
		"introspect": f.handler.Introspect,
		"revoke":     f.handler.Revoke,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			serve(w, httptest.NewRequest(http.MethodGet, "/oauth/"+name, nil))

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", w.Code)
			}
		})
	}
}

func TestAMissingTokenIsARequestError(t *testing.T) {
	f := newLifeFixture(t)

	w := f.introspect(t, url.Values{})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), ErrInvalidRequest) {
		t.Errorf("error = %s", w.Body.String())
	}
}

func TestNeitherEndpointIsCacheable(t *testing.T) {
	f := newLifeFixture(t)

	for _, w := range []*httptest.ResponseRecorder{
		f.introspect(t, token("x")),
		f.call(t, "/oauth/revoke", token("x"), true),
	} {
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
	}
}

// The metric label must not carry WHY a token was inactive, or the disclosure
// the body refuses to make moves into /metrics.
func TestTheMetricDoesNotSeparateInactiveReasons(t *testing.T) {
	for _, arrange := range []func(*lifeFixture){
		func(f *lifeFixture) { f.verifier.err = signing.ErrInvalidSignature },
		func(f *lifeFixture) { f.handler.Sessions = lifeSessions{live: false} },
		func(f *lifeFixture) {
			claims := accessClaims(f.now)
			claims["client_id"] = lifeOtherID
			f.verifier.payload = mustMarshal(claims)
		},
	} {
		f := newLifeFixture(t)
		arrange(f)
		f.introspect(t, token("x"))

		if len(f.observer.seen) != 1 || f.observer.seen[0] != "introspect:"+OutcomeInactive {
			t.Errorf("observed %v, want exactly [introspect:%s]", f.observer.seen, OutcomeInactive)
		}
	}
}

// --- helpers -------------------------------------------------------------------------

func mustMarshal(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not JSON: %v\n%s", err, w.Body.String())
	}
	return body
}

func lifeHeaders(w *httptest.ResponseRecorder) string {
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

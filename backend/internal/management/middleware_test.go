package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// What a caller can observe: which requests are refused, with what status, and
// what a handler is told about who is calling. The database side — roles read
// per request, RLS confining a query — is exercised against real Postgres.

const issuer = "https://auth.example"

type fakeVerifier struct {
	claims   map[string]any
	err      error
	wantType string
}

func (v *fakeVerifier) Verify(_, wantType string) ([]byte, error) {
	v.wantType = wantType
	if v.err != nil {
		return nil, v.err
	}
	raw, _ := json.Marshal(v.claims)
	return raw, nil
}

type fakeGrants struct {
	grants []Grant
	err    error
	asked  []string
}

func (g *fakeGrants) GrantsFor(_ context.Context, _ *postgres.DB, userID string) ([]Grant, error) {
	g.asked = append(g.asked, userID)
	return g.grants, g.err
}

type fakeSessions struct{ live bool }

func (s fakeSessions) IsLive(context.Context, string, time.Time) bool { return s.live }

func claims(now time.Time) map[string]any {
	return map[string]any{
		"iss":       issuer,
		"sub":       userID,
		"aud":       issuer,
		"exp":       float64(now.Add(10 * time.Minute).Unix()),
		"iat":       float64(now.Unix()),
		"client_id": "55555555-5555-5555-5555-555555555555",
		"org_id":    orgA,
		"sid":       "33333333-3333-3333-3333-333333333333",
	}
}

type fixture struct {
	middleware *Middleware
	verifier   *fakeVerifier
	grants     *fakeGrants
	now        time.Time

	served  bool
	seen    Caller
	seenOrg string
	seenIS  bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	f := &fixture{
		verifier: &fakeVerifier{claims: claims(now)},
		grants:   &fakeGrants{grants: []Grant{{Role: OrgAdmin, ScopeID: orgA}}},
		now:      now,
	}
	f.middleware = &Middleware{
		Issuer:   issuer,
		Verifier: f.verifier,
		Grants:   f.grants,
		Sessions: fakeSessions{live: true},
		Now:      func() time.Time { return now },
	}
	return f
}

// call routes through chi so chi.URLParam works, as it does in the router.
func (f *fixture) call(t *testing.T, req Requirement, path, target string, authenticate bool) *httptest.ResponseRecorder {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.served = true
		f.seen, _ = CallerFrom(r.Context())
		f.seenOrg, f.seenIS = ScopeFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mux := chi.NewRouter()
	mux.Method(http.MethodGet, path, f.middleware.Require(req, handler))

	url := strings.Replace(path, "{org_id}", target, 1)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	if authenticate {
		r.Header.Set("Authorization", "Bearer a.signed.token")
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func orgScoped() Requirement {
	return Requirement{Role: OrgAdmin, Scope: ScopeOrganization}
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not the error envelope: %v\n%s", err, w.Body.String())
	}
	return body.Error
}

// --- authentication ---------------------------------------------------------------

// DoD item 1, in every shape.
func TestUnusableTokensAreRejected(t *testing.T) {
	cases := map[string]struct {
		arrange      func(*fixture)
		authenticate bool
	}{
		"no token":         {func(*fixture) {}, false},
		"a bad signature":  {func(f *fixture) { f.verifier.err = signing.ErrInvalidSignature }, true},
		"an id_token":      {func(f *fixture) { f.verifier.err = signing.ErrWrongType }, true},
		"another issuer":   {func(f *fixture) { f.verifier.claims["iss"] = "https://other.example" }, true},
		"another audience": {func(f *fixture) { f.verifier.claims["aud"] = "https://api.consumer.example" }, true},
		"expired":          {func(f *fixture) { f.verifier.claims["exp"] = float64(f.now.Add(-time.Second).Unix()) }, true},
		"issued in the future": {func(f *fixture) {
			f.verifier.claims["iat"] = float64(f.now.Add(time.Hour).Unix())
		}, true},
		"no subject":       {func(f *fixture) { delete(f.verifier.claims, "sub") }, true},
		"no organization":  {func(f *fixture) { delete(f.verifier.claims, "org_id") }, true},
		"a dead session":   {func(f *fixture) { f.middleware.Sessions = fakeSessions{live: false} }, true},
		"a malformed body": {func(f *fixture) { f.verifier.claims = nil; f.verifier.err = nil }, true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			c.arrange(f)

			w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, c.authenticate)

			if f.served {
				t.Fatal("the handler ran")
			}
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401:\n%s", w.Code, w.Body.String())
			}
			if decode(t, w)["code"] != "UNAUTHENTICATED" {
				t.Errorf("code = %v", decode(t, w)["code"])
			}
			if !strings.Contains(w.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("no bearer challenge: %q", w.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

// A token minted for a consumer's resource server must not administer the
// platform. Called out separately because it is the check that keeps those two
// audiences apart, and it is the one an integrator is most likely to trip over
// legitimately.
func TestATokenForAnotherAudienceCannotAdminister(t *testing.T) {
	f := newFixture(t)
	f.verifier.claims["aud"] = "https://api.consumer.example"

	w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
	if f.served {
		t.Error("a consumer's token reached a management handler")
	}
}

// A client_credentials token has no session and must still work: service
// accounts are half of what this API is for.
func TestAServiceAccountTokenIsAccepted(t *testing.T) {
	f := newFixture(t)
	delete(f.verifier.claims, "sid")
	f.middleware.Sessions = fakeSessions{live: false} // would refuse if consulted

	w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", w.Code, w.Body.String())
	}
	if !f.served {
		t.Error("the handler did not run")
	}
}

func TestTheTokenIsVerifiedAsAnAccessToken(t *testing.T) {
	f := newFixture(t)

	f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)

	if f.verifier.wantType != signing.TypeAccessToken {
		t.Errorf("verified as typ %q, want %q", f.verifier.wantType, signing.TypeAccessToken)
	}
}

// The roles come from the store on every request, not from the token. This is
// the unit-level half; the integration test revokes a role between two
// requests.
func TestRolesAreReadPerRequestAndNotFromTheToken(t *testing.T) {
	f := newFixture(t)
	// A token that CLAIMS a role it does not hold.
	f.verifier.claims["urn:authservice:manager_roles"] = []string{"INSTANCE_OWNER"}
	f.grants.grants = nil

	w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d; a role claimed by the token was honoured", w.Code)
	}
	if len(f.grants.asked) != 1 || f.grants.asked[0] != userID {
		t.Errorf("the store was asked %v, want one read for %s", f.grants.asked, userID)
	}
}

// --- authorization ------------------------------------------------------------------

// DoD item 2: refused regardless of what the console would have shown.
func TestACallerLackingTheRoleIsRefused(t *testing.T) {
	f := newFixture(t)
	f.grants.grants = []Grant{{Role: OrgAdmin, ScopeID: orgA}}

	w := f.call(t, Requirement{Role: OrgOwner, Scope: ScopeOrganization},
		"/v1/organizations/{org_id}/users", orgA, true)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if f.served {
		t.Fatal("the handler ran")
	}
	if decode(t, w)["code"] != "PERMISSION_DENIED" {
		t.Errorf("code = %v", decode(t, w)["code"])
	}
	// The reason stays in the log: told to a caller probing an organization
	// they do not administer, it confirms the organization exists.
	message, _ := decode(t, w)["message"].(string)
	if strings.Contains(message, "ORG_OWNER") || strings.Contains(message, orgA) {
		t.Errorf("the refusal names the role or the organization: %q", message)
	}
}

// Abuse case A-3, through the middleware: a role over one organization does
// not reach another.
func TestAnAdminOfOneOrganizationCannotReachAnother(t *testing.T) {
	f := newFixture(t)
	f.grants.grants = []Grant{{Role: OrgAdmin, ScopeID: orgA}}

	w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgB, true)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d", w.Code)
	}
	if f.served {
		t.Error("the handler ran for another organization")
	}

	// The control.
	own := newFixture(t)
	if w := own.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true); w.Code != http.StatusOK {
		t.Fatalf("the caller was refused their own organization: %d", w.Code)
	}
}

// Abuse case A-9: an endpoint whose requirement was never declared is
// unreachable, not open.
func TestAnEndpointWithNoRequirementIsUnreachableThroughTheMiddleware(t *testing.T) {
	f := newFixture(t)
	f.grants.grants = []Grant{{Role: InstanceOwner, ScopeID: instance}}

	w := f.call(t, Requirement{}, "/v1/organizations/{org_id}/users", orgA, true)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d; an unannotated endpoint was reachable", w.Code)
	}
	if f.served {
		t.Fatal("an unannotated endpoint ran its handler")
	}
}

// An INSTANCE_OWNER reaching another organization is marked, so the handler
// takes the named, logged, audited scope rather than a missing filter.
func TestAnInstanceOwnerIsMarkedWhenActingAcrossTenants(t *testing.T) {
	f := newFixture(t)
	f.grants.grants = []Grant{{Role: InstanceOwner, ScopeID: instance}}

	if w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgB, true); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if f.seenOrg != orgB {
		t.Errorf("the handler was scoped to %q, want the target %q", f.seenOrg, orgB)
	}
	if !f.seenIS {
		t.Error("acting across tenants was not marked instance-scoped")
	}

	own := newFixture(t)
	own.grants.grants = []Grant{{Role: InstanceOwner, ScopeID: instance}}
	own.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)
	if own.seenIS {
		t.Error("acting on their OWN organization was marked instance-scoped, which " +
			"would log and audit a cross-tenant access that did not happen")
	}
}

// The handler is told who is calling, and a handler reached without the
// middleware is told there is nobody — rather than being handed a zero Caller
// that reads as an anonymous but valid request.
func TestTheHandlerLearnsWhoIsCalling(t *testing.T) {
	f := newFixture(t)

	f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)

	if f.seen.UserID != userID {
		t.Errorf("caller = %q", f.seen.UserID)
	}
	if f.seen.OrgID != orgA {
		t.Errorf("caller org = %q", f.seen.OrgID)
	}
	if len(f.seen.Grants) != 1 {
		t.Errorf("grants = %v", f.seen.Grants)
	}

	if _, ok := CallerFrom(context.Background()); ok {
		t.Error("a context with no caller reported one")
	}
}

// A route with no org_id in its path scopes to the caller's own organization,
// which is what an endpoint like /v1/me needs — and what must NOT quietly
// happen for a route that meant to name one.
func TestARouteWithoutAnOrgParameterUsesTheCallersOwn(t *testing.T) {
	f := newFixture(t)

	if w := f.call(t, orgScoped(), "/v1/applications", "", true); w.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", w.Code, w.Body.String())
	}
	if f.seenOrg != orgA {
		t.Errorf("scoped to %q, want the caller's own organization %q", f.seenOrg, orgA)
	}
}

// A store failure is a 500 and not a refusal. Reporting "you lack the role"
// when the roles could not be read would be a lie, and one that sends an
// administrator to the wrong page of documentation.
func TestAFailureToReadRolesIsNotAPermissionDenial(t *testing.T) {
	f := newFixture(t)
	f.grants.err = context.DeadlineExceeded

	w := f.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if decode(t, w)["code"] != "INTERNAL" {
		t.Errorf("code = %v", decode(t, w)["code"])
	}
}

// --- the envelope --------------------------------------------------------------------

// DoD item 5, and the reason the mapping is a table: every response a caller
// can provoke matches docs/PLAN/05's schema.
func TestEveryRefusalMatchesTheErrorSchema(t *testing.T) {
	responses := []*httptest.ResponseRecorder{}

	unauthenticated := newFixture(t)
	responses = append(responses,
		unauthenticated.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, false))

	forbidden := newFixture(t)
	forbidden.grants.grants = nil
	responses = append(responses,
		forbidden.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true))

	broken := newFixture(t)
	broken.grants.err = context.DeadlineExceeded
	responses = append(responses,
		broken.call(t, orgScoped(), "/v1/organizations/{org_id}/users", orgA, true))

	for i, w := range responses {
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("response %d: Content-Type = %q", i, got)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("response %d: Cache-Control = %q; a permission-dependent "+
				"response cached by an intermediary is served to somebody else", i, got)
		}
		body := decode(t, w)
		if body["code"] == nil || body["code"] == "" {
			t.Errorf("response %d has no code", i)
		}
		if body["message"] == nil || body["message"] == "" {
			t.Errorf("response %d has no message", i)
		}
	}
}

func TestTheStatusMappingCoversEveryClass(t *testing.T) {
	for _, class := range []Class{
		Unauthenticated, Forbidden, NotFound, Invalid, Conflict, RateLimited, Internal,
	} {
		if class.Status() == 0 {
			t.Errorf("class %d has no status", class)
		}
		if class.Code() == "" {
			t.Errorf("class %d has no code", class)
		}
		if class.Status() == http.StatusOK {
			t.Errorf("class %d answers 200", class)
		}
	}
}

// An error that is not a Fault must not leak its text. An unexpected error's
// message is written for a developer and routinely names a table, a column or
// a query.
func TestAnUnexpectedErrorDoesNotLeakItsText(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, context.DeadlineExceeded)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), context.DeadlineExceeded.Error()) {
		t.Errorf("the underlying error reached the response: %s", w.Body.String())
	}
}

// Retry-After is rounded UP: rounding down tells a well-behaved client to
// retry fractionally too early, which the limiter then refuses again.
func TestRateLimitedCarriesRetryAfterRoundedUp(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, Fault{
		Class: RateLimited, Message: "Too many requests.",
		RetryAfter: 1500 * time.Millisecond,
	})

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2", got)
	}
}

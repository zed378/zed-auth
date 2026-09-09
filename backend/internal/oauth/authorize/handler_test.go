package authorize

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
)

// --- fakes -------------------------------------------------------------------

const (
	testClientID  = "11111111-1111-1111-1111-111111111111"
	testOrgID     = "22222222-2222-2222-2222-222222222222"
	testRedirect  = "https://app.example.com/callback"
	testChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

type fakeClients struct {
	app client.Application
	err error
}

func (f fakeClients) ByClientID(context.Context, string) (client.Application, error) {
	return f.app, f.err
}

type fakeSessions struct {
	s   session.Session
	err error
}

func (f fakeSessions) Lookup(context.Context, string, session.Policy, time.Time) (session.Session, error) {
	return f.s, f.err
}

// memStore is an in-memory CodeStore for the handler tests.
//
// The real Redis behaviour — atomicity above all — is exercised where it
// lives, against a real container. These tests are about ordering and
// parameter handling, and needing a container to answer "did this redirect"
// would mean the question got asked less often.
type memStore struct {
	codes   map[string]Code
	pending map[string]Request
	failing bool
}

func newStoreForTest(t *testing.T) *memStore {
	t.Helper()
	return &memStore{codes: map[string]Code{}, pending: map[string]Request{}}
}

func (m *memStore) IssueCode(_ context.Context, c Code, _ time.Duration) (string, error) {
	if m.failing {
		return "", errors.New("redis is down")
	}
	code := "code-" + strconv.Itoa(len(m.codes)+1)
	m.codes[code] = c
	return code, nil
}

func (m *memStore) SavePending(_ context.Context, r Request, _ time.Duration) (string, error) {
	if m.failing {
		return "", errors.New("redis is down")
	}
	id := "pending-" + strconv.Itoa(len(m.pending)+1)
	m.pending[id] = r
	return id, nil
}

func (m *memStore) PeekPending(_ context.Context, id string) (Request, error) {
	if m.failing {
		return Request{}, errors.New("redis is down")
	}
	r, ok := m.pending[id]
	if !ok {
		return Request{}, ErrPendingNotFound
	}
	return r, nil
}

// LoadPending consumes, like the real one. The fake would be useless for
// P1-12's single-use test if it did not.
func (m *memStore) LoadPending(_ context.Context, id string) (Request, error) {
	r, err := m.PeekPending(context.Background(), id)
	if err != nil {
		return Request{}, err
	}
	delete(m.pending, id)
	return r, nil
}

func webApp() client.Application {
	return client.Application{
		ID:           testClientID,
		OrgID:        testOrgID,
		Type:         client.TypeWeb,
		RedirectURIs: []string{testRedirect},
		GrantTypes:   []string{client.GrantAuthorizationCode, client.GrantRefreshToken},
	}
}

func liveSession() session.Session {
	now := time.Now()
	return session.Session{
		ID:          "33333333-3333-3333-3333-333333333333",
		UserID:      "44444444-4444-4444-4444-444444444444",
		OrgID:       testOrgID,
		AuthMethods: []string{"pwd"},
		CreatedAt:   now.Add(-time.Minute),
		LastSeenAt:  now,
		ExpiresAt:   now.Add(11 * time.Hour),
	}
}

func handler(t *testing.T, clients Clients, sessions Sessions, store CodeStore) *Handler {
	t.Helper()
	return &Handler{
		Clients:   clients,
		Sessions:  sessions,
		Store:     store,
		LoginPath: "/login",
		Policy:    session.DefaultPolicy,
	}
}

func validQuery() url.Values {
	q := url.Values{}
	q.Set("client_id", testClientID)
	q.Set("redirect_uri", testRedirect)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile")
	q.Set("state", "xyz")
	q.Set("code_challenge", testChallenge)
	q.Set("code_challenge_method", "S256")
	return q
}

func do(h *Handler, q url.Values, cookie string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// --- phase 1: no redirect, ever ------------------------------------------------

// P1-06 DoD item 3, and the most important test in this package.
//
// It asserts the ABSENCE of a Location header, which is an unusual assertion
// and therefore a conspicuous one to break. Reporting these errors by redirect
// would be the open-redirect vulnerability itself, delivered by the code meant
// to prevent it — so "did not redirect" is the property, not "returned 400".
func TestPhase1FailuresNeverRedirect(t *testing.T) {
	unknownClient := fakeClients{err: errors.New("no such client")}
	known := fakeClients{app: webApp()}

	cases := []struct {
		name    string
		clients Clients
		mutate  func(url.Values)
	}{
		{"unknown client_id", unknownClient, func(url.Values) {}},
		{"redirect_uri not registered", known, func(q url.Values) {
			q.Set("redirect_uri", "https://attacker.example/callback")
		}},
		{"redirect_uri differs by a trailing slash", known, func(q url.Values) {
			q.Set("redirect_uri", testRedirect+"/")
		}},
		{"redirect_uri is a prefix", known, func(q url.Values) {
			q.Set("redirect_uri", "https://app.example.com/")
		}},
		{"suffix confusion", known, func(q url.Values) {
			q.Set("redirect_uri", "https://app.example.com.attacker.net/callback")
		}},
		{"client_id missing", known, func(q url.Values) { q.Del("client_id") }},
		{"redirect_uri missing", known, func(q url.Values) { q.Del("redirect_uri") }},
		{"client_id duplicated", known, func(q url.Values) { q["client_id"] = []string{testClientID, "other"} }},
		{"redirect_uri duplicated", known, func(q url.Values) {
			q["redirect_uri"] = []string{testRedirect, "https://attacker.example/cb"}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := validQuery()
			tc.mutate(q)

			rec := do(handler(t, tc.clients, fakeSessions{err: session.ErrNotFound}, nil), q, "")

			if location := rec.Header().Get("Location"); location != "" {
				t.Fatalf("the response carries a Location header (%q). Reporting this error by "+
					"redirect IS the open-redirect vulnerability", location)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if !strings.Contains(rec.Body.String(), "can't complete this sign-in") {
				t.Errorf("no error page was rendered: %s", rec.Body.String())
			}
		})
	}
}

// The control: with everything valid, the handler DOES redirect. Without this,
// a handler that never redirected would pass every case above.
func TestAValidRequestDoesRedirect(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, newStoreForTest(t))

	rec := do(h, validQuery(), "")

	if rec.Header().Get("Location") == "" {
		t.Fatal("a valid request did not redirect; the phase-1 table above proves nothing")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
}

// An unknown client and an unregistered redirect must be indistinguishable, or
// the endpoint enumerates which client ids exist.
func TestPhase1ErrorsDoNotDistinguishTheCause(t *testing.T) {
	unknown := do(handler(t, fakeClients{err: errors.New("nope")}, fakeSessions{}, nil), validQuery(), "")

	q := validQuery()
	q.Set("redirect_uri", "https://attacker.example/cb")
	mismatch := do(handler(t, fakeClients{app: webApp()}, fakeSessions{}, nil), q, "")

	if unknown.Body.String() != mismatch.Body.String() {
		t.Error("an unknown client_id and an unregistered redirect_uri produce different " +
			"pages, which tells a prober which client ids exist")
	}
}

// --- phase 2: errors travel by redirect ----------------------------------------

func TestPhase2FailuresRedirectWithTheOriginalState(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(url.Values)
		wantError string
	}{
		{"no code_challenge", func(q url.Values) { q.Del("code_challenge") }, ErrInvalidRequest},
		{"plain PKCE", func(q url.Values) { q.Set("code_challenge_method", "plain") }, ErrInvalidRequest},
		{"no PKCE method", func(q url.Values) { q.Del("code_challenge_method") }, ErrInvalidRequest},
		{"short challenge", func(q url.Values) { q.Set("code_challenge", "tooshort") }, ErrInvalidRequest},
		{"no state", func(q url.Values) { q.Del("state") }, ErrInvalidRequest},
		{"response_type=token", func(q url.Values) { q.Set("response_type", "token") }, ErrUnsupportedResponseType},
		{"unknown scope", func(q url.Values) { q.Set("scope", "openid wat") }, ErrInvalidScope},
		{"scope without openid", func(q url.Values) { q.Set("scope", "profile") }, ErrInvalidScope},
		{"unknown prompt", func(q url.Values) { q.Set("prompt", "banana") }, ErrInvalidRequest},
		{"prompt=none with another", func(q url.Values) { q.Set("prompt", "none login") }, ErrInvalidRequest},
		{"negative max_age", func(q url.Values) { q.Set("max_age", "-1") }, ErrInvalidRequest},
		{"state duplicated", func(q url.Values) { q["state"] = []string{"a", "b"} }, ErrInvalidRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := validQuery()
			tc.mutate(q)

			rec := do(handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, nil), q, "")

			location := rec.Header().Get("Location")
			if location == "" {
				t.Fatalf("no redirect; phase-2 errors must travel to the validated redirect_uri")
			}

			target, err := url.Parse(location)
			if err != nil {
				t.Fatalf("unparseable Location: %v", err)
			}
			if !strings.HasPrefix(location, testRedirect) {
				t.Errorf("redirected to %q, want the registered URI", location)
			}
			if got := target.Query().Get("error"); got != tc.wantError {
				t.Errorf("error = %q, want %q", got, tc.wantError)
			}
			// The client needs state back to correlate the failure with the
			// request it made — including when state itself was the problem.
			if want := q.Get("state"); want != "" && target.Query().Get("state") != want {
				t.Errorf("state = %q, want %q", target.Query().Get("state"), want)
			}
			// A code must never accompany an error.
			if target.Query().Get("code") != "" {
				t.Error("an error response carries a code")
			}
		})
	}
}

// P1-06 DoD item 1: PKCE is required for EVERY client type, and the DoD calls
// out confidential ones specifically because that is where implementations
// relax it.
func TestPKCEIsRequiredForEveryClientType(t *testing.T) {
	for _, typ := range client.Types {
		if typ == client.TypeSAML {
			continue // no OIDC grants at all
		}

		t.Run(string(typ), func(t *testing.T) {
			app := webApp()
			app.Type = typ

			q := validQuery()
			q.Del("code_challenge")
			q.Del("code_challenge_method")

			rec := do(handler(t, fakeClients{app: app}, fakeSessions{err: session.ErrNotFound}, nil), q, "")

			target, _ := url.Parse(rec.Header().Get("Location"))
			if target.Query().Get("error") != ErrInvalidRequest {
				t.Errorf("a %s client was allowed to omit code_challenge", typ)
			}
			if target.Query().Get("code") != "" {
				t.Errorf("a %s client got a code with no PKCE challenge", typ)
			}
		})
	}
}

// A client that is registered but not permitted the authorization code grant.
func TestClientWithoutTheGrantIsRefused(t *testing.T) {
	app := webApp()
	app.GrantTypes = []string{client.GrantClientCredentials}

	rec := do(handler(t, fakeClients{app: app}, fakeSessions{err: session.ErrNotFound}, nil), validQuery(), "")

	target, _ := url.Parse(rec.Header().Get("Location"))
	if target.Query().Get("error") != ErrUnauthorizedClient {
		t.Errorf("error = %q, want %q", target.Query().Get("error"), ErrUnauthorizedClient)
	}
}

// offline_access is the one per-client scope restriction the data model can
// express today.
func TestOfflineAccessNeedsTheRefreshGrant(t *testing.T) {
	app := webApp()
	app.GrantTypes = []string{client.GrantAuthorizationCode} // no refresh_token

	q := validQuery()
	q.Set("scope", "openid offline_access")

	rec := do(handler(t, fakeClients{app: app}, fakeSessions{err: session.ErrNotFound}, nil), q, "")

	target, _ := url.Parse(rec.Header().Get("Location"))
	if target.Query().Get("error") != ErrInvalidScope {
		t.Errorf("error = %q, want %q", target.Query().Get("error"), ErrInvalidScope)
	}
}

// --- prompt ----------------------------------------------------------------------

// P1-06 DoD item 5. An SPA renewing in a hidden iframe needs a
// machine-readable answer, not a login form it cannot display.
func TestPromptNoneWithoutASessionReturnsLoginRequired(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, nil)

	q := validQuery()
	q.Set("prompt", "none")

	rec := do(h, q, "")

	location := rec.Header().Get("Location")
	target, _ := url.Parse(location)

	if target.Query().Get("error") != ErrLoginRequired {
		t.Errorf("error = %q, want %q", target.Query().Get("error"), ErrLoginRequired)
	}
	if strings.HasPrefix(location, "/login") {
		t.Error("prompt=none rendered the login flow; a hidden iframe cannot show it")
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("prompt=none produced HTML")
	}
}

// Without prompt=none, the same request goes to the login page.
func TestNoSessionGoesToTheLoginPage(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, newStoreForTest(t))

	rec := do(h, validQuery(), "")

	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?") {
		t.Fatalf("Location = %q, want the login page", location)
	}

	target, _ := url.Parse(location)
	if target.Query().Get("request") == "" {
		t.Error("the login redirect carries no pending-request id, so the flow cannot resume")
	}
	// The original parameters must not travel through a URL the user can edit.
	for _, leaked := range []string{"redirect_uri", "client_id", "code_challenge", "state"} {
		if target.Query().Get(leaked) != "" {
			t.Errorf("the login URL carries %s; only the opaque request id should travel", leaked)
		}
	}
}

// prompt=login forces re-authentication even with a live session, and must not
// revoke it — the user asked to prove themselves, not to be logged out of
// everything else.
func TestPromptLoginIgnoresALiveSession(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: liveSession()}, newStoreForTest(t))

	q := validQuery()
	q.Set("prompt", "login")

	token := strings.Repeat("A", 43)
	rec := do(h, q, token)

	if !strings.HasPrefix(rec.Header().Get("Location"), "/login?") {
		t.Errorf("prompt=login did not re-authenticate: %q", rec.Header().Get("Location"))
	}
}

// --- the session ---------------------------------------------------------------------

// A session belonging to a different organization is not a session for this
// client, however live it is.
func TestASessionFromAnotherOrganizationIsNotUsed(t *testing.T) {
	other := liveSession()
	other.OrgID = "99999999-9999-9999-9999-999999999999"

	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: other}, newStoreForTest(t))

	rec := do(h, validQuery(), strings.Repeat("A", 43))

	if !strings.HasPrefix(rec.Header().Get("Location"), "/login?") {
		t.Errorf("a session from another organization was accepted: %q", rec.Header().Get("Location"))
	}
}

// max_age asks how recently the user actually authenticated, which is not how
// recently they were active.
func TestMaxAgeForcesReauthentication(t *testing.T) {
	old := liveSession()
	old.CreatedAt = time.Now().Add(-time.Hour)
	old.LastSeenAt = time.Now()

	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: old}, newStoreForTest(t))

	q := validQuery()
	q.Set("max_age", "60") // one minute

	rec := do(h, q, strings.Repeat("A", 43))

	if !strings.HasPrefix(rec.Header().Get("Location"), "/login?") {
		t.Errorf("max_age did not force re-authentication: %q", rec.Header().Get("Location"))
	}
}

// --- issuing a code -------------------------------------------------------------

// P1-06 DoD item 4: with a session, a code comes back with no interaction.
func TestSilentSSOIssuesACode(t *testing.T) {
	store := newStoreForTest(t)
	current := liveSession()
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: current}, store)

	rec := do(h, validQuery(), strings.Repeat("A", 43))

	target, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("unparseable Location: %v", err)
	}
	if !strings.HasPrefix(target.String(), testRedirect) {
		t.Fatalf("redirected to %q, want the registered URI", target)
	}

	code := target.Query().Get("code")
	if code == "" {
		t.Fatal("no code was issued on the silent path")
	}
	if target.Query().Get("state") != "xyz" {
		t.Errorf("state = %q, want the original", target.Query().Get("state"))
	}
	if target.Query().Get("error") != "" {
		t.Errorf("a code and an error together: %s", target.RawQuery)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("the silent path rendered HTML")
	}

	// The code must bind everything P1-07 will check. A code that bound only
	// the user would be redeemable by any client, against any redirect URI,
	// with any verifier.
	bound := store.codes[code]
	if bound.ClientID != testClientID {
		t.Errorf("code.ClientID = %q, want %q", bound.ClientID, testClientID)
	}
	if bound.RedirectURI != testRedirect {
		t.Errorf("code.RedirectURI = %q, want %q", bound.RedirectURI, testRedirect)
	}
	if bound.CodeChallenge != testChallenge {
		t.Errorf("code.CodeChallenge = %q, want the presented challenge", bound.CodeChallenge)
	}
	if bound.SessionID != current.ID || bound.UserID != current.UserID {
		t.Errorf("the code is not bound to the session that authorised it: %+v", bound)
	}
	if len(bound.AuthMethods) == 0 {
		t.Error("the code carries no auth_methods; P1-07 builds amr from them")
	}
}

// A code that cannot be stored must not be issued: the client would receive one
// that could never be redeemed.
func TestAnUnstorableCodeIsNotIssued(t *testing.T) {
	store := newStoreForTest(t)
	store.failing = true

	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: liveSession()}, store)

	rec := do(h, validQuery(), strings.Repeat("A", 43))

	target, _ := url.Parse(rec.Header().Get("Location"))
	if target.Query().Get("code") != "" {
		t.Error("a code was issued even though it could not be stored")
	}
	if target.Query().Get("error") != ErrServerError {
		t.Errorf("error = %q, want %q", target.Query().Get("error"), ErrServerError)
	}
}

// The nonce travels into the code, for the id_token claim P1-07 builds.
func TestNonceIsCarriedIntoTheCode(t *testing.T) {
	store := newStoreForTest(t)
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: liveSession()}, store)

	q := validQuery()
	q.Set("nonce", "n-0S6_WzA2Mj")

	rec := do(h, q, strings.Repeat("A", 43))
	target, _ := url.Parse(rec.Header().Get("Location"))

	bound := store.codes[target.Query().Get("code")]
	if bound.Nonce != "n-0S6_WzA2Mj" {
		t.Errorf("code.Nonce = %q, want the presented nonce", bound.Nonce)
	}
}

// Nothing sensitive may reach the URL a browser keeps in its history, beyond
// the code itself.
func TestTheRedirectCarriesNothingItShouldNot(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: liveSession()}, newStoreForTest(t))

	q := validQuery()
	q.Set("nonce", "secret-nonce")

	rec := do(h, q, strings.Repeat("A", 43))
	location := rec.Header().Get("Location")

	for _, leaked := range []string{testChallenge, "secret-nonce", strings.Repeat("A", 43)} {
		if strings.Contains(location, leaked) {
			t.Errorf("the redirect URL carries %q, which belongs only on the server", leaked)
		}
	}
}

// --- the seam P1-12 finishes through --------------------------------------------

// Peek reads without consuming, so the login page can render more than once —
// a refresh, a back button, a mistyped password.
func TestPeekDoesNotConsume(t *testing.T) {
	store := newStoreForTest(t)
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, store)

	rec := do(h, validQuery(), "")
	target, _ := url.Parse(rec.Header().Get("Location"))
	id := target.Query().Get("request")
	if id == "" {
		t.Fatal("no pending request was stored")
	}

	for i := range 3 {
		pending, err := h.Peek(context.Background(), id)
		if err != nil {
			t.Fatalf("peek %d: %v", i+1, err)
		}
		if pending.App.ID != testClientID || pending.Request.State != "xyz" {
			t.Errorf("peek %d returned %+v", i+1, pending)
		}
	}
}

func TestPeekOnAnUnknownRequest(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{}, newStoreForTest(t))

	if _, err := h.Peek(context.Background(), "no-such-request"); !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("Peek = %v, want ErrPendingNotFound", err)
	}
}

// A client deleted between the redirect and the login cannot be resumed. There
// is nowhere to send an OAuth error either: the stored redirect_uri was
// validated against a registration that no longer exists, so redirecting to it
// now would be redirecting to an address nothing currently registers.
func TestPeekWhenTheClientIsGone(t *testing.T) {
	store := newStoreForTest(t)
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, store)

	rec := do(h, validQuery(), "")
	target, _ := url.Parse(rec.Header().Get("Location"))
	id := target.Query().Get("request")

	h.Clients = fakeClients{err: errors.New("no such client")}

	if _, err := h.Peek(context.Background(), id); err == nil {
		t.Error("a pending request resolved against a client that no longer exists")
	}
}

// Resume is the single-use point. Exactly one code per pending request, so a
// second tab arriving with the same id gets the expired page rather than a
// second code.
func TestResumeIssuesExactlyOneCode(t *testing.T) {
	store := newStoreForTest(t)
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, store)

	rec := do(h, validQuery(), "")
	target, _ := url.Parse(rec.Header().Get("Location"))
	id := target.Query().Get("request")

	first := httptest.NewRecorder()
	h.Resume(first, httptest.NewRequest(http.MethodPost, "/login", nil), id, liveSession())

	if first.Code != http.StatusFound {
		t.Fatalf("Resume answered %d:\n%s", first.Code, first.Body.String())
	}
	back, _ := url.Parse(first.Header().Get("Location"))
	if back.Query().Get("code") == "" {
		t.Error("no code was issued")
	}
	if got := back.Query().Get("state"); got != "xyz" {
		t.Errorf("state = %q, want the value the client sent", got)
	}

	second := httptest.NewRecorder()
	h.Resume(second, httptest.NewRequest(http.MethodPost, "/login", nil), id, liveSession())

	if second.Code == http.StatusFound {
		t.Fatal("the same pending request issued a second code")
	}
	if second.Header().Get("Location") != "" {
		t.Errorf("the second attempt redirected to %q", second.Header().Get("Location"))
	}
}

// The code Resume issues binds the session that was just established, not the
// one that was absent when the request was stored.
func TestResumeBindsTheNewSession(t *testing.T) {
	store := newStoreForTest(t)
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, store)

	rec := do(h, validQuery(), "")
	target, _ := url.Parse(rec.Header().Get("Location"))
	id := target.Query().Get("request")

	current := liveSession()
	out := httptest.NewRecorder()
	h.Resume(out, httptest.NewRequest(http.MethodPost, "/login", nil), id, current)

	back, _ := url.Parse(out.Header().Get("Location"))
	bound := store.codes[back.Query().Get("code")]

	if bound.UserID != current.UserID || bound.SessionID != current.ID {
		t.Errorf("the code binds %s/%s, want the session just created", bound.UserID, bound.SessionID)
	}
	if bound.CodeChallenge != testChallenge {
		t.Error("the PKCE challenge from the original request was lost")
	}
	if len(bound.AuthMethods) == 0 {
		t.Error("the code carries no auth_methods, so the id_token's amr would be a lie")
	}
}

// A session for another organization is not a session for this client, however
// live it is. Unreachable through the login page — it authenticates against
// the organization it read from this same client — and checked anyway, because
// the cost is one comparison and the failure it prevents is a code issued
// across tenants.
func TestResumeRefusesASessionFromAnotherOrganization(t *testing.T) {
	store := newStoreForTest(t)
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{err: session.ErrNotFound}, store)

	rec := do(h, validQuery(), "")
	target, _ := url.Parse(rec.Header().Get("Location"))
	id := target.Query().Get("request")

	elsewhere := liveSession()
	elsewhere.OrgID = "99999999-9999-9999-9999-999999999999"

	out := httptest.NewRecorder()
	h.Resume(out, httptest.NewRequest(http.MethodPost, "/login", nil), id, elsewhere)

	if out.Code == http.StatusFound {
		t.Fatal("a session from another organization was issued a code")
	}
	if out.Header().Get("Location") != "" {
		t.Errorf("it redirected to %q", out.Header().Get("Location"))
	}
}

// An unknown or expired id is answered without a redirect. There is nothing to
// redirect to: the request that held the validated URI is gone.
func TestResumeOnAnExpiredRequest(t *testing.T) {
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{}, newStoreForTest(t))

	out := httptest.NewRecorder()
	h.Resume(out, httptest.NewRequest(http.MethodPost, "/login", nil), "gone", liveSession())

	if out.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", out.Code)
	}
	if out.Header().Get("Location") != "" {
		t.Errorf("it redirected to %q", out.Header().Get("Location"))
	}
}

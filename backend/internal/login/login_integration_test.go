//go:build integration

package login

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The whole chain, against real Postgres and real Redis.
//
// What is here rather than in handler_test.go is everything a fake cannot
// answer honestly: whether two failure responses are actually identical when a
// real query stands behind one of them and not the other, whether the two
// paths cost the same, and whether RLS confines the lookup to the
// organization the client belongs to.

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const (
	testPassword = "correct horse battery staple"
	testEmail    = "alice@example.test"
)

type stack struct {
	db       *postgres.DB
	rdb      *redis.Client
	factory  *testsupport.Factory
	codes    *authorize.Store
	auth     *authorize.Handler
	sessions *session.Manager
	login    *Handler
	orgID    string
	userID   string
	appID    string
}

// clientLookup adapts the client store to what authorize.Handler needs, the
// same way cmd/authservice does.
type clientLookup struct {
	store *client.Store
	db    *postgres.DB
}

func (c clientLookup) ByClientID(ctx context.Context, clientID string) (client.Application, error) {
	return c.store.ByClientID(ctx, c.db, clientID)
}

func setup(t *testing.T) *stack {
	t.Helper()

	containers := testsupport.Start(t)
	testsupport.Truncate(t, containers)

	factory := testsupport.NewFactory(t, containers)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID, testEmail)

	var projectID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, "billing")

	hash, err := authn.Hash(testPassword)
	if err != nil {
		t.Fatalf("hashing the test password: %v", err)
	}
	factory.Exec(`UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash)

	// The runtime role, not the owner: a grant the service lacks must fail
	// here rather than in production.
	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: containers.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rdb := redis.NewClient(&redis.Options{Addr: containers.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(db, discard(), nil)
	clients := client.NewStore(auditor)

	var app client.Record
	if err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		app, _, err = clients.Create(context.Background(), tx, client.Application{
			OrgID: orgID, ProjectID: projectID, Name: "Billing", Type: client.TypeWeb,
			GrantTypes:   []string{client.GrantAuthorizationCode},
			RedirectURIs: []string{"https://app.example.com/cb"},
		}, "")
		return err
	}); err != nil {
		t.Fatalf("creating the application: %v", err)
	}

	cache := session.NewCache(rdb, nil)
	sessions := session.NewManager(db, cache, auditor, discard())
	codes := authorize.NewStore(rdb)

	authHandler := &authorize.Handler{
		Clients:   clientLookup{store: clients, db: db},
		Sessions:  sessions,
		Store:     codes,
		DB:        db,
		Log:       discard(),
		LoginPath: Path,
		Policy:    session.DefaultPolicy,
	}

	loginHandler := &Handler{
		Authorization: authHandler,
		Sessions:      sessions,
		Users:         authn.NewUserStore(),
		Policies:      authn.NewPolicyStore(discard()),
		Brandings:     NewBrandingStore(),
		DB:            db,
		Audit:         auditor,
		Log:           discard(),
		Policy:        session.DefaultPolicy,
	}

	return &stack{
		db: db, rdb: rdb, factory: factory, codes: codes,
		auth: authHandler, login: loginHandler, sessions: sessions,
		orgID: orgID, userID: userID, appID: app.ID,
	}
}

// begin runs a real authorization request and returns the pending id the
// service redirected the browser to.
func (s *stack) begin(t *testing.T) string {
	t.Helper()

	query := url.Values{
		"client_id":             {s.appID},
		"redirect_uri":          {"https://app.example.com/cb"},
		"response_type":         {"code"},
		"scope":                 {"openid profile"},
		"state":                 {"state-value"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}

	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	s.auth.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("the authorization request answered %d, want a redirect to the login page:\n%s",
			w.Code, w.Body.String())
	}

	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing the redirect: %v", err)
	}
	if target.Path != Path {
		t.Fatalf("redirected to %q, want the login page", target.Path)
	}

	id := target.Query().Get("request")
	if !validPendingID(id) {
		t.Fatalf("the pending id is not the shape this handler accepts: %q", id)
	}
	return id
}

// form renders the login page and returns the id, the CSRF token, and the
// session cookie value if there is one.
func (s *stack) form(t *testing.T, id string) string {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, Path+"?request="+id, nil)
	w := httptest.NewRecorder()
	s.login.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("rendering the form answered %d:\n%s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			return c.Value
		}
	}
	t.Fatal("no CSRF cookie was set")
	return ""
}

// submit posts the form.
func (s *stack) submit(t *testing.T, id, token, email, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{
		"request":  {id},
		csrfField:  {token},
		"email":    {email},
		"password": {password},
	}

	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	for _, c := range cookies {
		r.AddCookie(c)
	}

	w := httptest.NewRecorder()
	s.login.ServeHTTP(w, r)
	return w
}

// --- the whole flow -----------------------------------------------------------------

// DoD: the full journey a person actually takes, through the production code
// path, with no fakes anywhere in it.
func TestASuccessfulLoginCompletesTheAuthorization(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)
	w := s.submit(t, id, token, testEmail, testPassword)

	if w.Code != http.StatusFound {
		t.Fatalf("the login answered %d:\n%s", w.Code, w.Body.String())
	}

	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing the redirect: %v", err)
	}
	if target.Scheme+"://"+target.Host+target.Path != "https://app.example.com/cb" {
		t.Errorf("redirected to %q, want the client's registered URI", target.String())
	}
	if target.Query().Get("code") == "" {
		t.Error("no authorization code was issued")
	}
	if got := target.Query().Get("state"); got != "state-value" {
		t.Errorf("state = %q, want the value the client sent", got)
	}

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie was set, so the next application would prompt again")
	}

	// The cookie is a token, not the row's id (PG-14). A session id in a
	// cookie is a credential on every screen that lists sessions.
	var live bool
	s.factory.QueryRow(&live,
		`SELECT EXISTS (SELECT 1 FROM sessions WHERE user_id = $1 AND id::text = $2)`,
		s.userID, cookie.Value)
	if live {
		t.Error("the cookie carries the session's primary key")
	}

	var stored string
	s.factory.QueryRow(&stored,
		`SELECT token_hash FROM sessions WHERE user_id = $1`, s.userID)
	if stored != session.HashToken(cookie.Value) {
		t.Error("the stored hash is not the hash of the cookie that was issued")
	}
}

// The session established here is the one /oauth/authorize will accept next
// time, which is what makes the second application in an estate silent.
func TestTheNewSessionSatisfiesSilentSignOn(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)
	w := s.submit(t, id, token, testEmail, testPassword)

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}

	query := url.Values{
		"client_id":             {s.appID},
		"redirect_uri":          {"https://app.example.com/cb"},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"state":                 {"second"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie.Value})
	second := httptest.NewRecorder()
	s.auth.ServeHTTP(second, r)

	if second.Code != http.StatusFound {
		t.Fatalf("the second authorization answered %d", second.Code)
	}
	target, _ := url.Parse(second.Header().Get("Location"))
	if target.Path == Path {
		t.Fatal("the second application was sent to the login page; single sign-on is not happening")
	}
	if target.Query().Get("code") == "" {
		t.Errorf("no code on the silent path: %q", second.Header().Get("Location"))
	}
}

// --- the uniform answer, for real ------------------------------------------------

// DoD item 2, tested the only way that means anything: the SAME address is
// submitted twice with the same wrong password, and between the two the
// account is removed. Any difference in the two responses is a difference the
// existence of the account caused.
//
// Comparing two different addresses would not do — the page echoes what was
// typed, so the bodies would differ for a reason that discloses nothing, and
// the comparison would have to be loosened until it stopped testing anything.
func TestAWrongPasswordAndAnUnknownAccountAreByteIdentical(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)

	withAccount := s.submit(t, id, token, testEmail, "the wrong password")

	// The account is gone; the address submitted is unchanged.
	s.factory.Exec(`DELETE FROM sessions WHERE user_id = $1`, s.userID)
	s.factory.Exec(`DELETE FROM events WHERE actor_user_id = $1`, s.userID)
	s.factory.Exec(`DELETE FROM users WHERE id = $1`, s.userID)

	withoutAccount := s.submit(t, id, token, testEmail, "the wrong password")

	if withAccount.Code != withoutAccount.Code {
		t.Errorf("status differs: %d with an account, %d without",
			withAccount.Code, withoutAccount.Code)
	}
	if a, b := dumpHeaders(withAccount), dumpHeaders(withoutAccount); a != b {
		t.Errorf("headers differ:\n--- with an account ---\n%s\n--- without ---\n%s", a, b)
	}
	if withAccount.Body.String() != withoutAccount.Body.String() {
		t.Error("the bodies differ; an attacker can tell which addresses have accounts")
	}

	// The control. Without this, a handler that answered both with an empty
	// 500 would pass every assertion above.
	if withAccount.Code != http.StatusOK ||
		!strings.Contains(withAccount.Body.String(), MsgCredentials) {
		t.Fatalf("the shared response is not the login form with the credential "+
			"message; the comparison above is over the wrong pages:\n%d\n%s",
			withAccount.Code, withAccount.Body.String())
	}
}

// A locked account must be indistinguishable from a wrong password, and this
// is the case where the password is CORRECT — so nothing but the status field
// separates the two, and it is the easiest one to leak by being helpful.
func TestALockedAccountIsIndistinguishableFromAWrongPassword(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)

	wrongPassword := s.submit(t, id, token, testEmail, "the wrong password")

	s.factory.Exec(`UPDATE users SET status = 'locked' WHERE id = $1`, s.userID)
	locked := s.submit(t, id, token, testEmail, testPassword)

	if locked.Code != wrongPassword.Code {
		t.Errorf("status differs: %d locked, %d wrong password", locked.Code, wrongPassword.Code)
	}
	if a, b := dumpHeaders(locked), dumpHeaders(wrongPassword); a != b {
		t.Errorf("headers differ:\n--- locked ---\n%s\n--- wrong password ---\n%s", a, b)
	}
	if locked.Body.String() != wrongPassword.Body.String() {
		t.Error("a locked account is distinguishable from a wrong password, which " +
			"confirms the address has an account")
	}
	if !strings.Contains(locked.Body.String(), MsgCredentials) {
		t.Fatal("neither response carries the credential message; the comparison " +
			"is over the wrong pages")
	}
}

// Abuse case A-3. The not-found path must cost what a real verification costs,
// or the response time is a registered-address oracle — which is a
// password-reset list, a phishing list, and confirmation of where somebody
// works.
//
// authn.VerifyDummy does the work; this asserts that the login path actually
// calls it, which is the part that can be lost by an early return.
func TestTheUnknownAddressPathCostsTheSame(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)

	measure := func(email string) time.Duration {
		const samples = 7
		durations := make([]time.Duration, 0, samples)
		for range samples {
			start := time.Now()
			s.submit(t, id, token, email, "a password that is wrong")
			durations = append(durations, time.Since(start))
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		return durations[len(durations)/2]
	}

	// Warm the connection pool and the query plan, so the first sample of the
	// first case does not carry the setup cost of both.
	s.submit(t, id, token, testEmail, "warm")

	known := measure(testEmail)
	unknown := measure("nobody-here@example.test")

	ratio := float64(unknown) / float64(known)
	if ratio < 0.6 || ratio > 1.6 {
		t.Errorf("an unknown address takes %s and a known one %s (ratio %.2f); "+
			"the response time says which addresses are registered",
			unknown, known, ratio)
	}
}

// --- the pending request ---------------------------------------------------------

// A failed attempt must not spend the request, or every account would get
// exactly one try at its password.
func TestAFailedAttemptCanBeRetried(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)

	for i := range 3 {
		w := s.submit(t, id, token, testEmail, "wrong")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), MsgCredentials) {
			t.Fatalf("attempt %d was not offered the form again: %d", i+1, w.Code)
		}
	}

	if w := s.submit(t, id, token, testEmail, testPassword); w.Code != http.StatusFound {
		t.Fatalf("the correct password after three failures was refused: %d\n%s",
			w.Code, w.Body.String())
	}
}

// A SUCCESS spends it, and exactly once. A resumable-twice request would let
// one login satisfy two authorization flows — a code issued for a flow nobody
// re-authorised.
func TestASuccessConsumesThePendingRequest(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)

	if w := s.submit(t, id, token, testEmail, testPassword); w.Code != http.StatusFound {
		t.Fatalf("the first login was refused: %d", w.Code)
	}

	second := s.submit(t, id, token, testEmail, testPassword)
	if second.Code == http.StatusFound {
		t.Fatal("the pending request was resumable twice; one login issued two codes")
	}
	if second.Header().Get("Location") != "" {
		t.Errorf("the second attempt redirected to %q", second.Header().Get("Location"))
	}
}

// --- tenancy ------------------------------------------------------------------------

// The organization comes from the client the authorization request named, not
// from anything the person typing chose. A user with the same address in
// another organization is not this client's user, and RLS is what enforces it
// rather than a predicate somebody has to remember to write.
func TestAUserInAnotherOrganizationCannotSignIn(t *testing.T) {
	s := setup(t)

	// The same address, in a different tenant, with the same password.
	other := s.factory.Organization(s.factory.Instance())
	otherUser := s.factory.User(other, testEmail)
	hash, err := authn.Hash(testPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	s.factory.Exec(`UPDATE users SET password_hash = $2 WHERE id = $1`, otherUser, hash)

	// Remove this client's own user, so the only row matching the address
	// belongs to the other tenant.
	s.factory.Exec(`DELETE FROM users WHERE id = $1`, s.userID)

	id := s.begin(t)
	token := s.form(t, id)
	w := s.submit(t, id, token, testEmail, testPassword)

	if w.Code == http.StatusFound {
		t.Fatal("a user from another organization signed in through this client")
	}
	if !strings.Contains(w.Body.String(), MsgCredentials) {
		t.Errorf("the refusal was not the uniform one:\n%s", w.Body.String())
	}
}

// --- what is never stored --------------------------------------------------------

// CLAUDE.md: never log tokens or passwords. The audit log has a 24-month
// retention, which makes it the worst place for either.
func TestNoPasswordReachesTheAuditLog(t *testing.T) {
	s := setup(t)

	id := s.begin(t)
	token := s.form(t, id)

	s.submit(t, id, token, testEmail, testPassword)
	s.submit(t, id, token, testEmail, "a-distinctive-wrong-password")

	// Read as the owner. The service's own role is tenant-scoped, so reading
	// events through it returns nothing outside a transaction — which would
	// make this test pass by seeing no rows at all. The first version did
	// exactly that, and its control assertion is what caught it.
	var payloads string
	s.factory.QueryRow(&payloads,
		`SELECT COALESCE(string_agg(payload::text, '|'), '') FROM events`)

	if payloads == "" {
		t.Fatal("no audit events were written at all, so this test proves nothing")
	}
	for _, forbidden := range []string{
		testPassword, "a-distinctive-wrong-password", token, testEmail,
	} {
		if strings.Contains(payloads, forbidden) {
			t.Errorf("an audit payload contains %q: %s", forbidden, payloads)
		}
	}
}

// --- branding, end to end ------------------------------------------------------------

// PG-16: the login page reads settings.branding. Nothing writes it until
// P2-14, so this is the first and only demonstration that the read works
// against a real settings document.
func TestBrandingIsReadFromSettings(t *testing.T) {
	s := setup(t)

	s.factory.Exec(`
		UPDATE organizations
		   SET settings = settings || $2::jsonb
		 WHERE id = $1`,
		s.orgID,
		`{"branding":{"accent_color":"#046c4e","logo_url":"https://cdn.example/logo.svg"}}`)

	id := s.begin(t)

	r := httptest.NewRequest(http.MethodGet, Path+"?request="+id, nil)
	w := httptest.NewRecorder()
	s.login.ServeHTTP(w, r)

	body := w.Body.String()
	if !strings.Contains(body, "--color-accent:#046c4e") {
		t.Error("the organization's accent did not reach the page")
	}
	if !strings.Contains(body, "https://cdn.example/logo.svg") {
		t.Error("the organization's logo did not reach the page")
	}
	// The password policy shares the same settings document and must survive
	// the branding key being added to it.
	if !strings.Contains(body, `name="password"`) {
		t.Error("the form did not render")
	}
}

// --- helper ------------------------------------------------------------------------

// dumpHeaders renders every header in a stable order, so two responses are
// compared including the ones a hand-written list would forget —
// Content-Length and Set-Cookie in particular.
func dumpHeaders(w *httptest.ResponseRecorder) string {
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

// --- P1-10: logout ---------------------------------------------------------------

// logout builds the handler against everything real, sharing this stack's
// session manager so revocation and lookup see the same rows and the same
// cache.
func (s *stack) logout(t *testing.T) *LogoutHandler {
	t.Helper()

	return &LogoutHandler{
		Issuer:    "https://auth.example",
		Clients:   clientLookup{store: client.NewStore(audit.NewWriter(s.db, discard(), nil)), db: s.db},
		Sessions:  s.sessions,
		Refresh:   token.NewRefreshStore(),
		Verifier:  nil, // no hint in these tests; the confirmation path is what they exercise
		Brandings: NewBrandingStore(),
		DB:        s.db,
		Audit:     audit.NewWriter(s.db, discard(), nil),
		Log:       discard(),
		Policy:    session.DefaultPolicy,
	}
}

// signOut drives the confirmation: render the interstitial, then submit it.
func (s *stack) signOut(
	t *testing.T, h *LogoutHandler, cookie *http.Cookie, everywhere bool,
) *httptest.ResponseRecorder {
	t.Helper()

	rendered := httptest.NewRequest(http.MethodGet, LogoutPath, nil)
	rendered.AddCookie(cookie)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, rendered)

	if first.Code != http.StatusOK {
		t.Fatalf("the confirmation did not render: %d\n%s", first.Code, first.Body.String())
	}

	var csrf string
	for _, c := range first.Result().Cookies() {
		if c.Name == CSRFCookieName {
			csrf = c.Value
		}
	}
	if csrf == "" {
		t.Fatal("the confirmation set no CSRF cookie")
	}

	form := url.Values{csrfField: {csrf}}
	if everywhere {
		form.Set("everywhere", "1")
	}

	r := httptest.NewRequest(http.MethodPost, LogoutPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
	r.AddCookie(cookie)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// signIn completes a login and returns the session cookie it produced.
func (s *stack) signIn(t *testing.T) *http.Cookie {
	t.Helper()

	id := s.begin(t)
	csrf := s.form(t, id)
	w := s.submit(t, id, csrf, testEmail, testPassword)
	if w.Code != http.StatusFound {
		t.Fatalf("the login failed: %d\n%s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName {
			return &http.Cookie{Name: session.CookieName, Value: c.Value}
		}
	}
	t.Fatal("the login set no session cookie")
	return nil
}

// DoD items 1 and 2, and the reason this test asserts a CONSEQUENCE rather
// than a column: what the card asks is that the user has to sign in again, and
// a test on `revoked_at` would pass against a system that revokes the row and
// keeps honouring the cookie from a cache.
func TestAfterLogoutTheSameCookieRequiresFullReauthentication(t *testing.T) {
	s := setup(t)
	h := s.logout(t)

	cookie := s.signIn(t)

	// The control: before logout, the cookie authorises silent SSO.
	if target := s.silentAuthorize(t, cookie); target.Path == Path {
		t.Fatal("the cookie did not authorise silent SSO before logout; this test would prove nothing")
	}

	if w := s.signOut(t, h, cookie, false); w.Code != http.StatusOK {
		t.Fatalf("the sign-out answered %d:\n%s", w.Code, w.Body.String())
	}

	// The same cookie, replayed. It must now land on the login page.
	target := s.silentAuthorize(t, cookie)
	if target.Path != Path {
		t.Fatalf("a cookie captured before logout still authorises: %s", target.String())
	}
	if target.Query().Get("code") != "" {
		t.Error("a code was issued to a logged-out session")
	}
}

// silentAuthorize sends an authorization request carrying a cookie and returns
// where the service sent the browser.
func (s *stack) silentAuthorize(t *testing.T, cookie *http.Cookie) *url.URL {
	t.Helper()

	query := url.Values{
		"client_id":             {s.appID},
		"redirect_uri":          {"https://app.example.com/cb"},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"state":                 {"after-logout"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}

	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.auth.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("the authorization request answered %d:\n%s", w.Code, w.Body.String())
	}
	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing the redirect: %v", err)
	}
	return target
}

// DoD item 4. Every session AND the refresh tokens — without the second half
// an application holding one mints a fresh access token minutes later.
func TestSignOutEverywhereEndsEverySessionAndRevokesRefreshTokens(t *testing.T) {
	s := setup(t)
	h := s.logout(t)

	first := s.signIn(t)
	second := s.signIn(t)

	// A refresh token on one of those sessions, issued the way P1-07 does.
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

	var liveBefore int
	s.factory.QueryRow(&liveBefore,
		`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, s.userID)
	if liveBefore < 2 {
		t.Fatalf("expected two live sessions before signing out, got %d", liveBefore)
	}

	s.signOut(t, h, first, true)

	var liveAfter, liveTokens int
	s.factory.QueryRow(&liveAfter,
		`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, s.userID)
	s.factory.QueryRow(&liveTokens,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND NOT revoked`, s.userID)

	if liveAfter != 0 {
		t.Errorf("%d session(s) survived signing out everywhere", liveAfter)
	}
	if liveTokens != 0 {
		t.Errorf("%d refresh token(s) survived; an application holding one would "+
			"mint a fresh access token minutes later", liveTokens)
	}

	// And the OTHER browser's cookie is dead too, which is the whole point.
	if target := s.silentAuthorize(t, second); target.Path != Path {
		t.Error("the second session still authorises after signing out everywhere")
	}
}

// DoD item 5.
func TestALogoutIsAuditedAgainstARealDatabase(t *testing.T) {
	s := setup(t)
	h := s.logout(t)

	cookie := s.signIn(t)
	s.signOut(t, h, cookie, false)

	var payloads string
	s.factory.QueryRow(&payloads,
		`SELECT COALESCE(string_agg(payload::text, '|'), '') FROM events WHERE event_type = 'user.logout'`)

	if payloads == "" {
		t.Fatal("the logout was not audited")
	}
	// Postgres renders jsonb with a space after the colon, so the assertion is
	// on the value rather than on a formatting the database chooses.
	if !strings.Contains(payloads, `"session"`) {
		t.Errorf("the entry does not record what was ended: %s", payloads)
	}
	if strings.Contains(payloads, cookie.Value) {
		t.Errorf("the session cookie was written to the audit log: %s", payloads)
	}
}

// Idempotent. A logout link clicked twice, or clicked after the session
// expired, is not an error.
func TestLoggingOutTwiceIsNotAnError(t *testing.T) {
	s := setup(t)
	h := s.logout(t)

	cookie := s.signIn(t)

	first := s.signOut(t, h, cookie, false)

	// The second attempt has no live session, so the interstitial renders and
	// the confirmation lands on the signed-out page rather than failing.
	rendered := httptest.NewRequest(http.MethodGet, LogoutPath, nil)
	rendered.AddCookie(cookie)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, rendered)

	if second.Code != http.StatusOK {
		t.Errorf("the second sign-out answered %d, want the confirmation:\n%s",
			second.Code, second.Body.String())
	}
	if first.Code != http.StatusOK {
		t.Errorf("the first sign-out answered %d", first.Code)
	}
}

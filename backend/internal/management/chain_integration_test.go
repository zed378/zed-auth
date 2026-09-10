//go:build integration

// The /v1 chain, end to end (P1-15).
//
// This is the file that answers the card's Definition of Done, and it answers
// it the only way the DoD can honestly be answered: with a real signing key, a
// real token minted by P1-07's own claim builder, real manager_roles read from
// PostgreSQL, a real Redis counter, and the same Chain that main.go assembles.
//
// There are no endpoints yet — P1-16 onward add them — so the chain is
// exercised over a test handler. That is deliberate rather than a shortcut: the
// chain is the unit P1-15 delivers, and shipping a fake endpoint so a test
// could call something would put a route in production that exists for a test.
package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

const liveIssuer = "https://auth.example.test"

type v1 struct {
	db      *postgres.DB
	factory *testsupport.Factory
	signer  *signing.Signer
	rdb     *redis.Client

	chain    *Chain
	recorder *audit.Writer
	guard    *countingAudit

	orgA, orgB string
	userID     string
	clientID   string

	// handled counts how many times the test handler actually ran, which is
	// what most of the assertions below are really about. Guarded, because the
	// concurrency test calls the handler from several goroutines: an
	// unsynchronised counter there is a data race that -race reports and that
	// silently under-counts without it — which would make the test PASS for the
	// wrong reason.
	mu      sync.Mutex
	handled int
	// audits, when true, makes the test handler write an audit event.
	audits bool
	// status is what the test handler answers.
	status int
}

func setupV1(t *testing.T) *v1 {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)
	userID := factory.User(orgA)

	var projectID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, orgA, "console")
	var clientID string
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, projectID, orgA)

	db := openApp(t, stack)
	keys := signingKeys(t, stack)

	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	s := &v1{
		db: db, factory: factory, signer: signing.NewSigner(keys), rdb: rdb,
		recorder: audit.NewWriter(db, discard(), nil),
		guard:    &countingAudit{},
		orgA:     orgA, orgB: orgB, userID: userID, clientID: clientID,
		status: http.StatusCreated,
	}

	s.chain = &Chain{
		Auth: &Middleware{
			Issuer:   liveIssuer,
			Verifier: signing.NewVerifier(keys),
			Grants:   NewRoleStore(),
			DB:       db,
			Log:      discard(),
		},
		RateLimit: &RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 5, Window: time.Minute}),
		},
		Idempotency: &Idempotency{Claims: NewDBClaims(db), Log: discard()},
		Audit:       &AuditGuard{Log: discard(), Observer: s.guard},
	}
	return s
}

// handler is the endpoint P1-16 will eventually replace.
func (s *v1) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.handled++
		ran := s.handled
		s.mu.Unlock()

		if s.audits {
			err := s.chain.Auth.InScope(r.Context(), func(tx *postgres.Tx) error {
				return Audit(r.Context(), s.recorder, tx, audit.Event{Type: audit.EventUserCreated})
			})
			if err != nil {
				WriteError(w, err)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(`{"id":"created-` + strconv.Itoa(ran) + `"}`))
	})
}

// handledCount reports how many times the handler ran.
func (s *v1) handledCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handled
}

// router mounts the handler under a real chi route, so URL parameters and route
// patterns behave as they will in production.
func (s *v1) router(req Requirement) http.Handler {
	r := chi.NewRouter()
	r.Method(http.MethodPost, "/v1/organizations/{org_id}/users", s.chain.Handle(req, s.handler()))
	r.Method(http.MethodGet, "/v1/organizations/{org_id}/users", s.chain.Handle(req, s.handler()))
	return r
}

// --- tokens -------------------------------------------------------------------------------

func signingKeys(t *testing.T, containers *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a signing key: %v", err)
	}

	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key file: %v", err)
	}

	owner, err := sql.Open("pgx", containers.OwnerDSN)
	if err != nil {
		t.Fatalf("opening the owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("inserting the signing key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("promoting the signing key: %v", err)
	}

	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

type mint struct {
	audience string
	issuer   string
	issuedAt time.Time
	typ      string
	clientID string
}

// token mints one through P1-07's own claim builder, so this test consumes what
// the token endpoint actually produces rather than an approximation of it.
func (s *v1) token(t *testing.T, opts ...func(*mint)) string {
	t.Helper()

	m := mint{audience: liveIssuer, issuer: liveIssuer, issuedAt: time.Now(),
		typ: signing.TypeAccessToken, clientID: s.clientID}
	for _, o := range opts {
		o(&m)
	}

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer:   m.issuer,
		Audience: m.audience,
		ClientID: m.clientID,
		OrgID:    s.orgA,
		UserID:   s.userID,
		Scope:    []string{"openid"},
	}, m.issuedAt)
	if err != nil {
		t.Fatalf("building claims: %v", err)
	}

	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding claims: %v", err)
	}

	signed, err := s.signer.SignWithType(payload, m.typ)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

// grant gives the test user a manager role over a scope.
func (s *v1) grant(role Role, scopeID string) {
	s.factory.Exec(
		`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, $2, $3)`,
		s.userID, string(role), scopeID)
}

func (s *v1) revoke(role Role) {
	s.factory.Exec(`DELETE FROM manager_roles WHERE user_id = $1 AND role = $2`,
		s.userID, string(role))
}

type callOpt func(*http.Request)

func bearerToken(v string) callOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+v) }
}

func idempotencyKey(v string) callOpt {
	return func(r *http.Request) { r.Header.Set("Idempotency-Key", v) }
}

func (s *v1) post(t *testing.T, h http.Handler, org, body string, opts ...callOpt) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/v1/organizations/"+org+"/users",
		strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for _, o := range opts {
		o(r)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func orgRequirement() Requirement {
	return Requirement{Role: OrgAdmin, Scope: ScopeOrganization}
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("the response is not docs/PLAN/05's envelope: %s", w.Body.String())
	}
	if envelope.Error.Message == "" {
		t.Errorf("the envelope carries no message: %s", w.Body.String())
	}
	return envelope.Error.Code
}

// --- DoD 1: authentication --------------------------------------------------------------

// "A request with no token, an expired token, or a wrong-audience token is
// rejected with the correct status."
//
// Every case is 401 with the same message, and that uniformity is the control:
// a caller holding a captured token must not be able to ask this API which of
// its properties was wrong.
func TestAnUnusableTokenIsRejected(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())

	cases := map[string][]callOpt{
		"no token":       nil,
		"not a token":    {bearerToken("not-a-jwt")},
		"empty bearer":   {bearerToken("")},
		"another issuer": {bearerToken(s.token(t, func(m *mint) { m.issuer = "https://evil.test" }))},
		"another audience": {bearerToken(s.token(t, func(m *mint) {
			m.audience = "https://a-consumers-api.test"
		}))},
		"expired": {bearerToken(s.token(t, func(m *mint) {
			m.issuedAt = time.Now().Add(-24 * time.Hour)
		}))},
		"an id token": {bearerToken(s.token(t, func(m *mint) { m.typ = signing.TypeJWT }))},
	}

	// Two buckets, because they are two different facts and only one of them
	// has to be uniform.
	//
	// A request carrying NO usable credential — no header, or a `Bearer ` with
	// nothing after it — is told authentication is required. That is not a
	// leak: the caller sent nothing and learns nothing they did not already
	// know. An empty bearer belongs here rather than with the bad tokens
	// precisely because an empty string is not a token that failed a check.
	//
	// A request carrying a token that FAILED is told the token is not valid,
	// and every such refusal must be word for word identical.
	noCredential := map[string]bool{"no token": true, "empty bearer": true}

	var presented, absent []string
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			w := s.post(t, h, s.orgA, `{}`, opts...)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", w.Code)
			}
			if code := errorCode(t, w); code != "UNAUTHENTICATED" {
				t.Errorf("code = %q, want UNAUTHENTICATED", code)
			}
			// RFC 6750's challenge, so an OAuth client library refreshes
			// rather than giving up.
			if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "invalid_token") {
				t.Errorf("WWW-Authenticate = %q", got)
			}
			if noCredential[name] {
				absent = append(absent, w.Body.String())
			} else {
				presented = append(presented, w.Body.String())
			}
		})
	}

	if s.handledCount() != 0 {
		t.Errorf("the handler ran %d times behind a refused token", s.handledCount())
	}

	// Every refusal of a PRESENTED credential is identical.
	//
	// "No token" is deliberately excluded and is allowed to differ: a caller who
	// sent nothing learns only that authentication is required, which they
	// already knew. What must not differ is one BAD token from another —
	// expired, forged, wrong audience, wrong type — or a caller holding a
	// captured token could ask this API which property was wrong and work
	// towards one that is not.
	for _, m := range presented[1:] {
		if m != presented[0] {
			t.Errorf("two refusals of a presented token differ, which says WHICH property was wrong:\n  %s\n  %s",
				presented[0], m)
		}
	}
	// And the two no-credential cases answer identically to each other, so a
	// caller cannot distinguish a missing header from an empty one either.
	for _, m := range absent[1:] {
		if m != absent[0] {
			t.Errorf("a missing header and an empty one answer differently:\n  %s\n  %s",
				absent[0], m)
		}
	}

	// A count, so that a future edit which drops cases turns this from a real
	// uniformity check into a comparison of one string with itself.
	if len(presented) < 5 || len(absent) < 2 {
		t.Fatalf("%d presented-token and %d no-credential cases ran; the uniformity checks are nearly vacuous",
			len(presented), len(absent))
	}
}

// A valid token reaches the handler, which is the control that makes every
// refusal above mean something.
func TestAValidTokenReachesTheHandler(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)

	w := s.post(t, s.router(orgRequirement()), s.orgA, `{}`, bearerToken(s.token(t)))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	if s.handledCount() != 1 {
		t.Errorf("the handler ran %d times", s.handledCount())
	}
}

// --- DoD 2: permissions -----------------------------------------------------------------

// "A caller lacking the required manager role is rejected regardless of what
// the console would have shown them."
func TestACallerWithoutTheRoleIsRejected(t *testing.T) {
	s := setupV1(t)
	// No grant at all: authenticated, and holding nothing.
	h := s.router(orgRequirement())

	w := s.post(t, h, s.orgA, `{}`, bearerToken(s.token(t)))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if code := errorCode(t, w); code != "PERMISSION_DENIED" {
		t.Errorf("code = %q", code)
	}
	if s.handledCount() != 0 {
		t.Error("the handler ran without the role")
	}
	// The reason stays in the log. "you are an ORG_ADMIN and this needs
	// ORG_OWNER" told to a caller probing confirms what they hold.
	if strings.Contains(w.Body.String(), "ORG_ADMIN") {
		t.Errorf("the refusal names a role: %s", w.Body.String())
	}
}

// **The role is read from the database on every request, never from the token.**
//
// An access token lives ten minutes, so a role revoked one minute ago is still
// asserted by a token in a caller's hands. For "delete this organization" that
// is the difference between revocation and a promise of revocation.
//
// The SAME token is used for both calls, which is what makes this a test of
// where the role is read from rather than of token expiry.
func TestARoleRevokedBetweenTwoRequestsTakesEffectOnTheSecond(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())

	bearer := bearerToken(s.token(t))

	if w := s.post(t, h, s.orgA, `{}`, bearer); w.Code != http.StatusCreated {
		t.Fatalf("the first call = %d, want 201: %s", w.Code, w.Body.String())
	}

	s.revoke(OrgAdmin)

	w := s.post(t, h, s.orgA, `{}`, bearer)
	if w.Code != http.StatusForbidden {
		t.Fatalf("the second call with the same token = %d, want 403 — the role came from the token",
			w.Code)
	}
	if s.handledCount() != 1 {
		t.Errorf("the handler ran %d times, want 1", s.handledCount())
	}
}

// "IDOR: accessing another organization's resource by id."
//
// 404, not 403. A 403 would confirm the organization exists, which is abuse
// case A-3's disclosure.
func TestAnotherOrganizationIsNotFound(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)

	w := s.post(t, s.router(orgRequirement()), s.orgB, `{}`, bearerToken(s.token(t)))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if code := errorCode(t, w); code != "NOT_FOUND" {
		t.Errorf("code = %q", code)
	}
	if s.handledCount() != 0 {
		t.Error("the handler ran on another organization's resource")
	}
}

// An INSTANCE_OWNER may act across tenants — the capability the role exists
// for, and the control that proves the 404 above is about permission rather
// than about the route being broken.
func TestAnInstanceOwnerReachesAnotherOrganization(t *testing.T) {
	s := setupV1(t)

	var instanceID string
	s.factory.QueryRow(&instanceID, `SELECT instance_id FROM organizations WHERE id = $1`, s.orgA)
	s.grant(InstanceOwner, instanceID)

	w := s.post(t, s.router(orgRequirement()), s.orgB, `{}`, bearerToken(s.token(t)))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

// An endpoint that declares no requirement is unreachable, not open. The
// failure mode of a default-open design is one endpoint nobody annotated, and
// it is invisible until it is exploited.
func TestAnUnannotatedRouteRefusesEvenAnInstanceOwner(t *testing.T) {
	s := setupV1(t)

	var instanceID string
	s.factory.QueryRow(&instanceID, `SELECT instance_id FROM organizations WHERE id = $1`, s.orgA)
	s.grant(InstanceOwner, instanceID) // the most powerful role there is

	w := s.post(t, s.router(Requirement{}), s.orgA, `{}`, bearerToken(s.token(t)))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — an unannotated endpoint is open", w.Code)
	}
	if s.handledCount() != 0 {
		t.Error("an unannotated endpoint reached its handler")
	}
}

// --- DoD 4: idempotency ------------------------------------------------------------------

// "Replaying a POST with the same Idempotency-Key returns the original result
// without creating a duplicate."
func TestAReplayReturnsTheOriginalAndCreatesNothing(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	first := s.post(t, h, s.orgA, `{"email":"a@b.c"}`, bearer, idempotencyKey("key-1"))
	if first.Code != http.StatusCreated {
		t.Fatalf("the first call = %d: %s", first.Code, first.Body.String())
	}

	second := s.post(t, h, s.orgA, `{"email":"a@b.c"}`, bearer, idempotencyKey("key-1"))

	if s.handledCount() != 1 {
		t.Fatalf("the handler ran %d times, want 1 — the retry created a duplicate", s.handledCount())
	}
	if second.Code != first.Code {
		t.Errorf("the replay answered %d, the original %d", second.Code, first.Code)
	}
	if second.Body.String() != first.Body.String() {
		t.Errorf("the replay differs:\n  original %s\n  replay   %s",
			first.Body.String(), second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Error("a client cannot tell the replay from a fresh execution")
	}
}

// "The same key with a different body is 409."
func TestTheSameKeyWithADifferentBodyIsAConflict(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	s.post(t, h, s.orgA, `{"email":"a@b.c"}`, bearer, idempotencyKey("key-1"))

	w := s.post(t, h, s.orgA, `{"email":"somebody-else@b.c"}`, bearer, idempotencyKey("key-1"))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "CONFLICT" {
		t.Errorf("code = %q", code)
	}
	if s.handledCount() != 1 {
		t.Errorf("the handler ran %d times, want 1", s.handledCount())
	}
}

// Concurrent duplicates run the handler exactly once. This is the case the
// header exists for and the one no sequential test can show.
func TestConcurrentDuplicatesRunTheHandlerOnce(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	// The quota must not be what refuses the racers, or this test would pass
	// for the wrong reason.
	s.chain.RateLimit = nil
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	const racers = 6

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		codes = map[int]int{}
	)

	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := s.post(t, h, s.orgA, `{"email":"a@b.c"}`, bearer, idempotencyKey("race"))
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	// The only thing that must be true, and the whole reason the header exists.
	if got := s.handledCount(); got != 1 {
		t.Fatalf("the handler ran %d times for %d concurrent duplicates", got, racers)
	}

	// Every racer got a coherent answer. A 201 is correct for the one that ran
	// AND for any that arrived after it finished — a replay returns the stored
	// 201, which is the point. A 409 is correct for one that arrived while the
	// first was still in flight. Anything else is not.
	//
	// An earlier version of this test demanded exactly one 201 and failed with
	// six. That was the test being wrong rather than the code: all six were
	// served from a single execution, which is exactly what was wanted.
	for code, n := range codes {
		if code != http.StatusCreated && code != http.StatusConflict {
			t.Errorf("%d racers got %d, which is neither a replay nor an in-flight conflict", n, code)
		}
	}
	if codes[http.StatusCreated] == 0 {
		t.Error("no racer got the created response")
	}
}

// --- DoD 5: the error envelope ------------------------------------------------------------

// "Every error response matches docs/PLAN/05's schema." Every refusal the chain
// can produce, checked against the envelope and against being cacheable.
func TestEveryRefusalUsesTheEnvelope(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	// One request to claim a key, so the conflict case has something to hit.
	s.post(t, h, s.orgA, `{"a":1}`, bearer, idempotencyKey("taken"))

	refusals := map[string]func() *httptest.ResponseRecorder{
		"401": func() *httptest.ResponseRecorder { return s.post(t, h, s.orgA, `{}`) },
		"404": func() *httptest.ResponseRecorder { return s.post(t, h, s.orgB, `{}`, bearer) },
		"409": func() *httptest.ResponseRecorder {
			return s.post(t, h, s.orgA, `{"a":2}`, bearer, idempotencyKey("taken"))
		},
		"400": func() *httptest.ResponseRecorder {
			return s.post(t, h, s.orgA, `{}`, bearer, idempotencyKey("has a space"))
		},
	}

	for name, call := range refusals {
		t.Run(name, func(t *testing.T) {
			w := call()

			if got := strconv.Itoa(w.Code); got != name {
				t.Errorf("status = %s, want %s: %s", got, name, w.Body.String())
			}
			if code := errorCode(t, w); code == "" {
				t.Errorf("no code in the envelope: %s", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q", ct)
			}
			// Per-caller and permission-dependent, so nothing in between may
			// cache one and serve it to somebody whose permissions differ.
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
		})
	}
}

// --- DoD 6: audit --------------------------------------------------------------------------

// "Every mutating call writes an audit event."
//
// Asserted at BOTH ends: the row is really in the events table, and the guard
// really reports a handler that writes nothing. Only the first would pass
// against a guard that never fires; only the second would pass against a writer
// that never writes.
func TestAMutatingCallWritesAnAuditEvent(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	s.audits = true
	h := s.router(orgRequirement())

	w := s.post(t, h, s.orgA, `{}`, bearerToken(s.token(t)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	var (
		count int
		actor string
		org   string
	)
	s.factory.QueryRow(&count, `SELECT count(*) FROM events WHERE event_type = $1`,
		string(audit.EventUserCreated))
	if count != 1 {
		t.Fatalf("%d audit events, want 1", count)
	}
	s.factory.QueryRow(&actor, `SELECT actor_user_id FROM events WHERE event_type = $1`,
		string(audit.EventUserCreated))
	s.factory.QueryRow(&org, `SELECT org_id FROM events WHERE event_type = $1`,
		string(audit.EventUserCreated))

	if actor != s.userID {
		t.Errorf("actor = %q, want the caller %q", actor, s.userID)
	}
	if org != s.orgA {
		t.Errorf("org = %q, want %q", org, s.orgA)
	}
	if len(s.guard.missed) != 0 {
		t.Errorf("an audited mutation was reported as unaudited: %v", s.guard.missed)
	}
}

func TestAMutatingCallThatAuditsNothingIsReported(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	s.audits = false // the handler that forgot
	h := s.router(orgRequirement())

	if w := s.post(t, h, s.orgA, `{}`, bearerToken(s.token(t))); w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	if len(s.guard.missed) != 1 {
		t.Fatalf("the guard reported %d unaudited mutations, want 1", len(s.guard.missed))
	}
	if !strings.Contains(s.guard.missed[0], "{org_id}") {
		t.Errorf("the metric label is %q, not a route pattern", s.guard.missed[0])
	}
}

// A replay is not reported as an unaudited mutation. The original request
// already wrote the event, and reporting the retry would make the metric fire
// in normal operation — after which nobody would alert on it.
func TestAReplayIsNotReportedAsUnaudited(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	s.audits = true
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	s.post(t, h, s.orgA, `{"a":1}`, bearer, idempotencyKey("key-1"))
	s.post(t, h, s.orgA, `{"a":1}`, bearer, idempotencyKey("key-1"))

	if s.handledCount() != 1 {
		t.Fatalf("the handler ran %d times", s.handledCount())
	}
	if len(s.guard.missed) != 0 {
		t.Errorf("a replay was reported as an unaudited mutation: %v", s.guard.missed)
	}
}

// --- DoD 8 (card step 8): rate limiting ------------------------------------------------------

// The bound is real, the headers are on every response, and a refused request
// never reaches the handler.
func TestTheClientQuotaIsEnforcedAndReported(t *testing.T) {
	s := setupV1(t) // the fixture's quota is 5 a minute
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	for i := range 5 {
		w := s.post(t, h, s.orgA, `{}`, bearer)
		if w.Code != http.StatusCreated {
			t.Fatalf("request %d = %d: %s", i+1, w.Code, w.Body.String())
		}
		if got := w.Header().Get("X-RateLimit-Remaining"); got != strconv.Itoa(4-i) {
			t.Errorf("request %d reported %q remaining, want %d", i+1, got, 4-i)
		}
	}

	w := s.post(t, h, s.orgA, `{}`, bearer)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("the 6th request = %d, want 429", w.Code)
	}
	if code := errorCode(t, w); code != "RATE_LIMITED" {
		t.Errorf("code = %q", code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on a 429 — the caller is a machine and reads it")
	}
	if s.handledCount() != 5 {
		t.Errorf("the handler ran %d times, want 5", s.handledCount())
	}
}

// "Rate-limit bypass across rotated client credentials" (abuse case A-5).
//
// Rotating a secret must not reset the bound. The client id is precisely the
// part that does not change when a secret does, which is why it is the key.
func TestRotatingAClientSecretDoesNotResetTheBound(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	h := s.router(orgRequirement())

	for range 5 {
		s.post(t, h, s.orgA, `{}`, bearerToken(s.token(t)))
	}
	if w := s.post(t, h, s.orgA, `{}`, bearerToken(s.token(t))); w.Code != http.StatusTooManyRequests {
		t.Fatalf("the client is not over its bound; the test proves nothing (got %d)", w.Code)
	}

	// The rotation: a new secret, the same application.
	s.factory.Exec(
		`UPDATE applications SET client_secret_hash = $2, updated_at = now() WHERE id = $1`,
		s.clientID, "$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	// A freshly minted token, after the rotation.
	w := s.post(t, h, s.orgA, `{}`, bearerToken(s.token(t)))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d after a secret rotation, want 429 — the bound reset", w.Code)
	}
}

// A read is not charged an idempotency claim and is not guarded for audit, but
// it IS counted against the quota. A limit that only bounds writes leaves an
// enumeration run unbounded.
func TestAReadIsCountedAgainstTheQuota(t *testing.T) {
	s := setupV1(t)
	s.grant(OrgAdmin, s.orgA)
	s.status = http.StatusOK
	h := s.router(orgRequirement())
	bearer := bearerToken(s.token(t))

	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/v1/organizations/"+s.orgA+"/users", nil)
		bearer(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	for i := range 5 {
		if w := get(); w.Code != http.StatusOK {
			t.Fatalf("read %d = %d: %s", i+1, w.Code, w.Body.String())
		}
	}
	if w := get(); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 6th read = %d, want 429 — reads are not counted", w.Code)
	}
	// And no read was reported as an unaudited mutation.
	if len(s.guard.missed) != 0 {
		t.Errorf("a read was reported as an unaudited mutation: %v", s.guard.missed)
	}
}

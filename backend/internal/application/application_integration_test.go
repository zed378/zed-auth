//go:build integration

// The application endpoints end to end (P1-18).
//
// The claims worth testing here are all about a credential: that it is shown
// once, that a public client never gets one, that rotation keeps the old one
// working exactly as long as it says, and that none of the other five
// responses can be made to carry one.
//
// Everything runs through the real generated router and P1-15's real chain,
// with tokens from P1-07's own claim builder — so a permission boundary
// asserted here is the one a caller meets.
package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/auditlog"
	"github.com/zed378/zed-auth/backend/internal/authz"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/grant"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/organization"
	"github.com/zed378/zed-auth/backend/internal/project"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/role"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
	"github.com/zed378/zed-auth/backend/internal/user"
)

const issuer = "https://auth.example.test"

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	handler http.Handler
	signer  *signing.Signer

	orgA, orgB         string
	projectA, projectB string
	otherProjectA      string
	userID             string
	clientID           string

	// Every mutation in this package must be visible to P1-15's guard, so the
	// fixture always watches rather than only the one test that asserts it.
	observer *countingObserver
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)
	userID := factory.User(orgA)

	var projectA, otherProjectA, projectB, clientID string
	factory.QueryRow(&projectA,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgA)
	// A second project in the SAME organization. RLS cannot separate these —
	// they share a tenant — so this is what the project predicate is for.
	factory.QueryRow(&otherProjectA,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'billing') RETURNING id`, orgA)
	factory.QueryRow(&projectB,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'theirs') RETURNING id`, orgB)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, projectA, orgA)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keys := signingKeys(t, stack)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(db, discard(), nil)
	observer := &countingObserver{}
	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 400, Window: time.Minute}),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard(), Observer: observer},
		BufferBody:  true,
	}

	srv := httpserver.New(config.HTTPConfig{
		Addr: "127.0.0.1:0", ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second,
	}, httpserver.Deps{
		Logger: discard(),
		Health: &httpserver.Health{},
		V1:     chain,
		Organizations: &organization.Handler{
			Store: organization.NewStore(), DB: db, Audit: auditor, Log: discard(),
		},
		ProjectAPI:     &project.Handler{Store: project.NewStore(), DB: db, Audit: auditor, Log: discard()},
		ApplicationAPI: New(db, auditor, discard()),
		RoleAPI:        role.New(db, auditor, discard()),
		GrantAPI:       grant.New(db, auditor, discard()),
		AuthzAPI:       &authz.Handler{DB: db, Log: discard()},
		// Fourth of four. Nothing here calls it; httpserver.New refuses a /v1
		// chain with any half of the Management API missing, and the handler
		// refuses a deactivation it cannot make real rather than panicking on
		// a nil revoker.
		UserAPI:  &user.Handler{Store: user.NewStore(), DB: db, Audit: auditor, Log: discard()},
		AuditAPI: &auditlog.Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, factory: factory, handler: srv.Handler(), signer: signing.NewSigner(keys),
		orgA: orgA, orgB: orgB,
		projectA: projectA, otherProjectA: otherProjectA, projectB: projectB,
		userID: userID, clientID: clientID, observer: observer,
	}
}

func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("inserting the key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("rotating: %v", err)
	}
	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

func (f *fixture) token(t *testing.T) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: f.orgA, UserID: f.userID, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	signed, err := f.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func (f *fixture) grant(role management.Role, scopeID string) {
	f.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, $2, $3)`,
		f.userID, string(role), scopeID)
}

func (f *fixture) call(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token(t))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) apps(orgID, projectID string) string {
	return "/v1/organizations/" + orgID + "/projects/" + projectID + "/applications"
}

func mustStatus(t *testing.T, w *httptest.ResponseRecorder, want int) *httptest.ResponseRecorder {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
	}
	return w
}

func decodeCreated(t *testing.T, w *httptest.ResponseRecorder) api.ApplicationCreated {
	t.Helper()
	var a api.ApplicationCreated
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatalf("not an ApplicationCreated: %s", w.Body.String())
	}
	return a
}

func decodeApp(t *testing.T, w *httptest.ResponseRecorder) api.Application {
	t.Helper()
	var a api.Application
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatalf("not an Application: %s", w.Body.String())
	}
	return a
}

func envelope(t *testing.T, w *httptest.ResponseRecorder) (string, []api.ErrorDetail) {
	t.Helper()
	var e struct {
		Error struct {
			Code    string            `json:"code"`
			Details []api.ErrorDetail `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("not docs/PLAN/05's envelope: %s", w.Body.String())
	}
	return e.Error.Code, e.Error.Details
}

// createConfidential registers a web client and returns it with its secret.
func (f *fixture) createConfidential(t *testing.T, name string) (api.ApplicationCreated, string) {
	t.Helper()
	body := fmt.Sprintf(
		`{"name":%q,"type":"web","redirect_uris":["https://app.example.test/callback"]}`, name)
	created := decodeCreated(t,
		mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), body), http.StatusCreated))
	if created.ClientSecret == nil || *created.ClientSecret == "" {
		t.Fatal("a confidential client was created with no secret")
	}
	return created, *created.ClientSecret
}

// --- the secret is shown once -----------------------------------------------------------------

// **The card's first DoD item, and the one this whole task exists for.**
//
// Not "the read response has no client_secret field" — that would pass against
// a service that returns the secret under a different key, or inside the name.
// The assertion is that the secret's CHARACTERS do not appear anywhere in any
// subsequent response body.
func TestTheSecretAppearsOnceAndNeverAgain(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created, secret := f.createConfidential(t, "Billing portal")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	for _, probe := range []struct {
		what   string
		method string
		target string
		body   string
	}{
		{"the read", http.MethodGet, one, ""},
		{"the list", http.MethodGet, f.apps(f.orgA, f.projectA), ""},
		{"an update", http.MethodPatch, one, `{"name":"Billing portal v2"}`},
	} {
		w := mustStatus(t, f.call(t, probe.method, probe.target, probe.body), http.StatusOK)
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s carried the client secret", probe.what)
		}
		// And the flag that replaces it is still true, so the absence above is
		// not simply an application that lost its secret.
		if !strings.Contains(w.Body.String(), `"has_secret":true`) {
			t.Errorf("%s does not report has_secret: %s", probe.what, w.Body.String())
		}
	}
}

// The hash is not a substitute for the secret, and neither is a prefix of it.
func TestNoResponseCarriesTheSecretHashOrAPrefix(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, secret := f.createConfidential(t, "Prefix probe")

	var storedHash string
	f.factory.QueryRow(&storedHash,
		`SELECT client_secret_hash FROM applications WHERE id = $1`, created.Id.String())
	if storedHash == "" {
		t.Fatal("no hash was stored, so this test proves nothing")
	}

	w := mustStatus(t, f.call(t, http.MethodGet,
		f.apps(f.orgA, f.projectA)+"/"+created.Id.String(), ""), http.StatusOK)
	body := w.Body.String()

	if strings.Contains(body, storedHash) {
		t.Error("the read carried the stored hash")
	}
	// Half the secret is still most of the search space gone.
	if half := secret[:len(secret)/2]; strings.Contains(body, half) {
		t.Error("the read carried a prefix of the secret")
	}
}

// A public client gets no secret at all, and says so.
func TestAPublicClientIsCreatedWithoutASecret(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created := decodeCreated(t, mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		`{"name":"Console SPA","type":"spa","redirect_uris":["https://console.example.test/cb"]}`),
		http.StatusCreated))

	if created.ClientSecret != nil {
		t.Error("a public client was issued a secret")
	}
	if created.HasSecret {
		t.Error("has_secret is true for a public client")
	}

	// The database agrees, which is the claim that matters — the response
	// omitting it would look identical if a secret had been stored anyway.
	var hash sql.NullString
	f.factory.QueryRow(&hash, `SELECT client_secret_hash FROM applications WHERE id = $1`,
		created.Id.String())
	if hash.Valid {
		t.Error("a secret hash was stored for a public client")
	}
}

// --- rotation ---------------------------------------------------------------------------------

// **The card's third DoD item.** The overlap is the part worth testing: a
// rotation that invalidated the old secret immediately would pass any test
// that only checks the new one works.
func TestRotationIssuesANewSecretAndKeepsTheOldOneForTheOverlap(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, first := f.createConfidential(t, "Rotating")
	id := created.Id.String()

	w := mustStatus(t, f.call(t, http.MethodPost,
		f.apps(f.orgA, f.projectA)+"/"+id+"/rotate-secret?overlap_hours=24", ""), http.StatusOK)

	var rotated api.RotatedSecret
	if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("not a RotatedSecret: %s", w.Body.String())
	}
	second := rotated.ClientSecret

	if second == "" || second == first {
		t.Fatal("rotation did not issue a different secret")
	}
	expires, err := rotated.PreviousSecretExpiresAt.Get()
	if err != nil || expires.IsZero() {
		t.Fatalf("no previous_secret_expires_at: %s", w.Body.String())
	}
	if until := time.Until(expires); until < 23*time.Hour || until > 25*time.Hour {
		t.Errorf("the overlap ends in %v, want about 24h", until)
	}

	// Both authenticate, through the store's own verification rather than
	// through a comparison this test invents.
	creds := f.credentials(t, id)
	if !creds.Verify(second, time.Now()) {
		t.Error("the new secret does not authenticate")
	}
	if !creds.Verify(first, time.Now()) {
		t.Error("the previous secret stopped working inside its overlap")
	}
	// And the old one is dead once the window closes.
	if creds.Verify(first, expires.Add(time.Second)) {
		t.Error("the previous secret still works after its overlap expired")
	}
}

// overlap_hours: 0 is the compromised-secret path and must survive being zero.
//
// A handler treating 0 as "unset" and substituting the 24-hour default is the
// obvious bug here, and it would leave a leaked credential live for a day.
func TestAZeroOverlapRetiresThePreviousSecretImmediately(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, first := f.createConfidential(t, "Compromised")
	id := created.Id.String()

	mustStatus(t, f.call(t, http.MethodPost,
		f.apps(f.orgA, f.projectA)+"/"+id+"/rotate-secret?overlap_hours=0", ""), http.StatusOK)

	creds := f.credentials(t, id)
	if creds.Verify(first, time.Now()) {
		t.Error("a zero overlap left the previous secret working")
	}
}

// A public client has no secret to rotate.
func TestRotatingAPublicClientIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created := decodeCreated(t, mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		`{"name":"Public","type":"native","redirect_uris":["http://127.0.0.1:9999/cb"]}`),
		http.StatusCreated))

	w := f.call(t, http.MethodPost,
		f.apps(f.orgA, f.projectA)+"/"+created.Id.String()+"/rotate-secret", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestAnOverlapBeyondTheBoundIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.createConfidential(t, "Too long")
	w := f.call(t, http.MethodPost,
		f.apps(f.orgA, f.projectA)+"/"+created.Id.String()+"/rotate-secret?overlap_hours=10000", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// credentials reads an application's stored credential state.
func (f *fixture) credentials(t *testing.T, id string) client.Credentials {
	t.Helper()

	var creds client.Credentials
	var previousHash sql.NullString
	var previousUntil sql.NullTime
	f.factory.QueryRow(&creds.Hash,
		`SELECT client_secret_hash FROM applications WHERE id = $1`, id)
	f.factory.QueryRow(&previousHash,
		`SELECT coalesce(previous_client_secret_hash, '') FROM applications WHERE id = $1`, id)
	f.factory.QueryRow(&previousUntil,
		`SELECT previous_client_secret_expires_at FROM applications WHERE id = $1`, id)

	creds.PreviousHash = previousHash.String
	if previousUntil.Valid {
		creds.PreviousExpiresAt = previousUntil.Time
	}
	return creds
}

// --- redirect URI validation, on both paths -----------------------------------------------------

// **The card's second DoD item.** The same table on create and on update,
// driven through the API rather than by reading the code — a validator called
// from one path and not the other is exactly the bug this rules out.
func TestRedirectValidationIsIdenticalOnCreateAndUpdate(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	base, _ := f.createConfidential(t, "Validation subject")
	one := f.apps(f.orgA, f.projectA) + "/" + base.Id.String()

	for _, uri := range []string{
		"https://*.example.test/cb",            // a wildcard
		"http://app.example.test/cb",           // plaintext for a web client
		"https://app.example.test/cb#fragment", // a fragment
		"not-a-uri",                            // not a URI at all
	} {
		create := fmt.Sprintf(`{"name":"Rejected","type":"web","redirect_uris":[%q]}`, uri)
		w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), create)
		createStatus := w.Code

		update := fmt.Sprintf(`{"redirect_uris":[%q]}`, uri)
		w = f.call(t, http.MethodPatch, one, update)
		updateStatus := w.Code

		if createStatus != http.StatusBadRequest {
			t.Errorf("create with %q = %d, want 400", uri, createStatus)
		}
		if updateStatus != http.StatusBadRequest {
			t.Errorf("update with %q = %d, want 400 — a URI refused on create was accepted on update",
				uri, updateStatus)
		}
	}

	// The control: a URI that IS valid is accepted on both paths, so the two
	// 400s above are about the value rather than about the endpoints being
	// broken.
	ok := `{"name":"Accepted","type":"web","redirect_uris":["https://ok.example.test/cb"]}`
	mustStatus(t, f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), ok), http.StatusCreated)
	mustStatus(t, f.call(t, http.MethodPatch, one,
		`{"redirect_uris":["https://ok.example.test/cb"]}`), http.StatusOK)
}

// A forbidden grant type is refused for every application type.
func TestTheRemovedGrantsAreRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for _, grant := range []string{"implicit", "password"} {
		body := fmt.Sprintf(
			`{"name":"Legacy","type":"web","redirect_uris":["https://a.example.test/cb"],"grant_types":["%s"]}`,
			grant)
		w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", grant, w.Code, w.Body.String())
		}
	}
}

// --- type is immutable --------------------------------------------------------------------------

// **The card's fourth DoD item.** An update naming `type` is refused, not
// ignored — and the stored type is unchanged either way, which is the half
// that would still hold if the refusal were missing. Both are asserted, so the
// test cannot pass on the second alone.
func TestAnUpdateNamingTypeIsRefusedAndChangesNothing(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, secret := f.createConfidential(t, "Immutable")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	w := f.call(t, http.MethodPatch, one, `{"name":"Renamed","type":"spa"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	code, details := envelope(t, w)
	if code != "VALIDATION_ERROR" {
		t.Errorf("code = %q", code)
	}
	if len(details) == 0 || details[0].Field != "type" {
		t.Errorf("the refusal does not name the field: %+v", details)
	}

	// Nothing changed: not the type, not the name it was bundled with, and
	// not the secret.
	var storedType, storedName string
	f.factory.QueryRow(&storedType, `SELECT type FROM applications WHERE id = $1`, created.Id.String())
	f.factory.QueryRow(&storedName, `SELECT name FROM applications WHERE id = $1`, created.Id.String())
	if storedType != "web" {
		t.Errorf("type = %q, want web", storedType)
	}
	if storedName != "Immutable" {
		t.Errorf("name = %q — the refused update was partially applied", storedName)
	}
	if creds := f.credentials(t, created.Id.String()); !creds.Verify(secret, time.Now()) {
		t.Error("the secret changed on a refused update")
	}
}

func TestAnUpdateNamingAnIdentityFieldIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.createConfidential(t, "Identity")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	for _, body := range []string{
		`{"project_id":"00000000-0000-0000-0000-0000000000ff"}`,
		`{"org_id":"00000000-0000-0000-0000-0000000000ff"}`,
		`{"client_secret":"chosen-by-the-caller"}`,
	} {
		w := f.call(t, http.MethodPatch, one, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", body, w.Code, w.Body.String())
		}
	}
}

// An omitted field is left alone. A PATCH of the name that cleared every
// redirect URI would be an outage delivered by a rename.
func TestAnOmittedFieldIsNotCleared(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.createConfidential(t, "Partial")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	updated := decodeApp(t, mustStatus(t,
		f.call(t, http.MethodPatch, one, `{"name":"Partial v2"}`), http.StatusOK))

	if updated.Name != "Partial v2" {
		t.Errorf("name = %q", updated.Name)
	}
	if len(updated.RedirectUris) != 1 {
		t.Errorf("redirect_uris = %v — a rename cleared them", updated.RedirectUris)
	}
	if len(updated.GrantTypes) == 0 {
		t.Error("grant_types were cleared by a rename")
	}
}

// --- the two boundaries -------------------------------------------------------------------------

// The tenant boundary, which RLS holds.
func TestAnotherTenantsApplicationIsUnreachable(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	var theirs string
	f.factory.QueryRow(&theirs,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'theirs', 'web') RETURNING id`, f.projectB, f.orgB)

	// Their application, our organization and our project in the path: the
	// application layer allows the request, so only RLS is left.
	w := f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA)+"/"+theirs, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}

	// The control: our own application through the same route is readable.
	ours, _ := f.createConfidential(t, "Ours")
	mustStatus(t, f.call(t, http.MethodGet,
		f.apps(f.orgA, f.projectA)+"/"+ours.Id.String(), ""), http.StatusOK)
}

// **The project boundary, which RLS cannot hold** — both projects are in the
// same tenant, so `org_id = current_org_id()` is true for both rows. This is
// the test that fails if the project predicate is dropped.
func TestAnApplicationInAnotherProjectOfTheSameOrganizationIsUnreachable(t *testing.T) {
	f := setup(t)
	// ORG_OWNER, not ORG_ADMIN: DELETE needs it, and an admin would be refused
	// by the permission check before the project was ever considered — the
	// test would pass on a 403 that says nothing about project scoping.
	f.grant(management.OrgOwner, f.orgA)

	created, _ := f.createConfidential(t, "In project A")
	id := created.Id.String()

	// Same organization, same caller, same role — a different project.
	elsewhere := f.apps(f.orgA, f.otherProjectA) + "/" + id

	for _, probe := range []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"name":"Moved"}`},
		{http.MethodDelete, ""},
		{http.MethodPost, ""},
	} {
		target := elsewhere
		if probe.method == http.MethodPost {
			target = elsewhere + "/rotate-secret"
		}
		w := f.call(t, probe.method, target, probe.body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s through the wrong project = %d, want 404: %s",
				probe.method, w.Code, w.Body.String())
		}
	}

	// The control: every one of those works through the RIGHT project.
	right := f.apps(f.orgA, f.projectA) + "/" + id
	mustStatus(t, f.call(t, http.MethodGet, right, ""), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodPatch, right, `{"name":"Still here"}`), http.StatusOK)
}

// A project that does not exist is a 404, not an empty page.
func TestListingANonexistentProjectIsNotFound(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	ghost := "00000000-0000-0000-0000-0000000000ff"
	w := f.call(t, http.MethodGet, f.apps(f.orgA, ghost), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}

	// And another tenant's project, which exists but not here.
	w = f.call(t, http.MethodGet, f.apps(f.orgA, f.projectB), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("another tenant's project = %d, want 404", w.Code)
	}
}

// A caller with no role over the organization in the path gets nothing.
func TestACallerWithNoRoleOverTheTargetIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	w := f.call(t, http.MethodGet, f.apps(f.orgB, f.projectB), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

// --- permissions ----------------------------------------------------------------------------------

func TestAnOrgAdminCanDoEverythingButDelete(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	created, _ := f.createConfidential(t, "Admin's")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	mustStatus(t, f.call(t, http.MethodGet, one, ""), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodPatch, one, `{"name":"Admin's v2"}`), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodPost, one+"/rotate-secret", ""), http.StatusOK)

	w := f.call(t, http.MethodDelete, one, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("delete = %d, want 403: %s", w.Code, w.Body.String())
	}
	if code, _ := envelope(t, w); code != "PERMISSION_DENIED" {
		t.Errorf("code = %q", code)
	}
}

func TestAnOrgOwnerCanDelete(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created, _ := f.createConfidential(t, "Doomed")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	mustStatus(t, f.call(t, http.MethodDelete, one, ""), http.StatusNoContent)
	if w := f.call(t, http.MethodGet, one, ""); w.Code != http.StatusNotFound {
		t.Errorf("the deleted application reads %d, want 404", w.Code)
	}
}

func TestHoldingProjectOwnerGrantsNothingYet(t *testing.T) {
	f := setup(t)
	f.grant(management.ProjectOwner, f.orgA)

	w := f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA), "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}

	f.grant(management.OrgAdmin, f.orgA)
	mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA), ""), http.StatusOK)
}

// --- audit -----------------------------------------------------------------------------------------

func TestApplicationLifecycleEventsAreAudited(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created, secret := f.createConfidential(t, "Audited")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()

	mustStatus(t, f.call(t, http.MethodPatch, one,
		`{"redirect_uris":["https://widened.example.test/cb","https://app.example.test/callback"]}`),
		http.StatusOK)
	mustStatus(t, f.call(t, http.MethodPost, one+"/rotate-secret", ""), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodDelete, one, ""), http.StatusNoContent)

	for _, kind := range []audit.EventType{
		audit.EventApplicationCreated, audit.EventApplicationUpdated,
		audit.EventApplicationSecretRotated, audit.EventApplicationDeleted,
	} {
		var count int
		f.factory.QueryRow(&count,
			`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
			f.orgA, string(kind))
		if count != 1 {
			t.Errorf("%d %s events, want 1", count, kind)
		}
	}

	// A widened redirect URI is the highest-value change anybody can make to a
	// registration, and this is the only place it shows up afterwards.
	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE org_id = $1 AND event_type = $2`,
		f.orgA, string(audit.EventApplicationUpdated))
	if !strings.Contains(payload, "widened.example.test") {
		t.Errorf("the update event does not record the new redirect URI: %s", payload)
	}

	// No event carries the secret.
	var all string
	f.factory.QueryRow(&all,
		`SELECT coalesce(string_agg(payload::text, ' '), '') FROM events WHERE org_id = $1`, f.orgA)
	if strings.Contains(all, secret) {
		t.Error("an audit payload carries the client secret")
	}
}

// P1-15's guard counts a successful mutation that recorded nothing. The store
// writes its own events through audit.Writer, which does not mark the guard's
// per-request trail — so without the adapter every one of these would be
// reported, and a metric that is supposed to be permanently zero would be
// noise from the first request.
func TestMutationsAreVisibleToTheAuditGuard(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgOwner, f.orgA)

	created, _ := f.createConfidential(t, "Guarded")
	one := f.apps(f.orgA, f.projectA) + "/" + created.Id.String()
	mustStatus(t, f.call(t, http.MethodPatch, one, `{"name":"Guarded v2"}`), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodPost, one+"/rotate-secret", ""), http.StatusOK)
	mustStatus(t, f.call(t, http.MethodDelete, one, ""), http.StatusNoContent)

	if f.observer.count > 0 {
		t.Errorf("%d mutations were reported as unaudited: %v", f.observer.count, f.observer.routes)
	}

	// **The control.** An observer that never fires proves nothing — a
	// disconnected one would pass the assertion above forever. So make it
	// fire: the same guard, wrapping a handler that writes no event.
	silent := &countingObserver{}
	guard := &management.AuditGuard{Log: discard(), Observer: silent}
	guard.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/anything", nil))

	if silent.count != 1 {
		t.Fatalf("the guard did not report an unaudited mutation, so the assertion above measures nothing")
	}
}

type countingObserver struct {
	count  int
	routes []string
}

func (o *countingObserver) MutationNotAudited(route string) {
	o.count++
	o.routes = append(o.routes, route)
}

// --- naming and pagination ----------------------------------------------------------------------

func TestABlankNameIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for _, body := range []string{
		`{"name":"","type":"web","redirect_uris":["https://a.example.test/cb"]}`,
		`{"name":"   ","type":"web","redirect_uris":["https://a.example.test/cb"]}`,
	} {
		w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA), body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, w.Code)
		}
	}
}

func TestAnUnknownTypeIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	w := f.call(t, http.MethodPost, f.apps(f.orgA, f.projectA),
		`{"name":"Odd","type":"mainframe","redirect_uris":["https://a.example.test/cb"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestWalkingTheApplicationListSeesEachOnce(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	for i := range 9 {
		f.factory.Exec(
			`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, $3, 'api')`,
			f.projectA, f.orgA, fmt.Sprintf("app-%02d", i))
	}

	seen := map[string]int{}
	target := f.apps(f.orgA, f.projectA) + "?page_size=4"

	for range 10 {
		w := mustStatus(t, f.call(t, http.MethodGet, target, ""), http.StatusOK)

		var list api.ApplicationList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("not a list: %s", w.Body.String())
		}
		for _, a := range list.Applications {
			seen[a.Id.String()]++
		}
		if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
			break
		}
		target = f.apps(f.orgA, f.projectA) + "?page_size=4&page_token=" +
			url.QueryEscape(*list.PageInfo.NextPageToken)
	}

	// Nine plus the fixture's 'console' client.
	if len(seen) != 10 {
		t.Errorf("saw %d distinct applications, want 10", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times", id, n)
		}
	}
}

// The list never contains another project's applications.
func TestTheListIsScopedToItsProject(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	f.factory.Exec(
		`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'elsewhere', 'api')`,
		f.otherProjectA, f.orgA)

	w := mustStatus(t, f.call(t, http.MethodGet, f.apps(f.orgA, f.projectA), ""), http.StatusOK)
	if strings.Contains(w.Body.String(), "elsewhere") {
		t.Error("the list crossed a project boundary")
	}
}

// --- idempotency ------------------------------------------------------------------------------------

// A retried create makes one application, and the replay returns the same
// secret — the caller who retried because they never saw the first response
// must still be able to get it.
func TestAReplayedCreateMakesOneApplication(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	body := `{"name":"Once","type":"web","redirect_uris":["https://once.example.test/cb"]}`
	key := "p118-create-once"

	first := f.callWithKey(t, http.MethodPost, f.apps(f.orgA, f.projectA), body, key)
	mustStatus(t, first, http.StatusCreated)

	second := f.callWithKey(t, http.MethodPost, f.apps(f.orgA, f.projectA), body, key)
	mustStatus(t, second, http.StatusCreated)

	if first.Body.String() != second.Body.String() {
		t.Error("the replay returned a different body")
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Error("the replay is not marked")
	}

	var count int
	f.factory.QueryRow(&count,
		`SELECT count(*) FROM applications WHERE org_id = $1 AND name = 'Once'`, f.orgA)
	if count != 1 {
		t.Errorf("%d applications named Once, want 1", count)
	}
}

func (f *fixture) callWithKey(t *testing.T, method, target, body, key string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token(t))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

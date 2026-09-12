//go:build integration

package token

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const testIssuer = "https://auth.example"

type fixture struct {
	db        *postgres.DB
	rdb       *redis.Client
	factory   *testsupport.Factory
	handler   *Handler
	codes     *authorize.Store
	clients   *client.Store
	orgID     string
	userID    string
	appID     string
	projectID string
	sessionID string
	secret    client.Secret
	verifier  string
	keys      *signing.Cache
}

// setup builds the whole chain the token endpoint sits at the end of: a real
// signing key from P1-03's store, an application from P1-05, a session from
// P1-11, and codes from P1-06. Everything is the production path.
func setup(t *testing.T) fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID)

	var projectID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, "billing")

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	auditor := audit.NewWriter(db, discard(), nil)
	clients := client.NewStore(auditor)

	// A confidential client, so client authentication is exercised rather than
	// skipped.
	var app client.Record
	var secret client.Secret
	if err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		app, secret, err = clients.Create(context.Background(), tx, client.Application{
			OrgID: orgID, ProjectID: projectID, Name: "billing", Type: client.TypeWeb,
			GrantTypes: []string{
				client.GrantAuthorizationCode, client.GrantRefreshToken, client.GrantClientCredentials,
			},
			RedirectURIs: []string{"https://app.example.com/cb"},
		}, "")
		return err
	}); err != nil {
		t.Fatalf("creating the application: %v", err)
	}

	keys := signingKeys(t, stack)

	// A REAL session, and the manager wired in.
	//
	// The fixture used to pin SessionID to "" and leave Sessions nil, which
	// meant two things were never exercised: the `sid` claim, and the refresh
	// grant's session-liveness check. P1-08 made the first one matter —
	// /oauth/userinfo looks the session up by `sid`, so an access token
	// without one is refused there — and a test written against the old
	// fixture would have passed while every real token failed at userinfo.
	sessions := session.NewManager(db, session.NewCache(rdb, nil), auditor, discard())

	var browser session.Session
	if err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		browser, _, err = sessions.Create(context.Background(), tx, session.New{
			UserID: userID, OrgID: orgID, AuthMethods: []string{"pwd"}, IP: "192.0.2.1",
		}, session.DefaultPolicy, time.Now())
		return err
	}); err != nil {
		t.Fatalf("creating the session: %v", err)
	}

	f := fixture{
		db:        db,
		rdb:       rdb,
		factory:   factory,
		codes:     authorize.NewStore(rdb),
		clients:   clients,
		orgID:     orgID,
		userID:    userID,
		appID:     app.ID,
		projectID: projectID,
		secret:    secret,
		verifier:  "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		keys:      keys,

		sessionID: browser.ID,
	}

	f.handler = &Handler{
		Issuer:   testIssuer,
		Clients:  clientAdapter{store: clients, db: db},
		Codes:    f.codes,
		Refresh:  NewRefreshStore(),
		Signer:   signing.NewSigner(keys),
		Sessions: liveness{sessions: sessions},
		DB:       db,
		Audit:    auditor,
		Log:      discard(),

		// P2-04. A local reader rather than `grant.NewTokenClaims`, because
		// importing `internal/grant` here would be a cycle: its own tests
		// import this package to mint tokens. The query is the same one.
		Roles: rolesFromDB{db: db},
	}
	return f
}

// rolesFromDB is the production reader's query, inlined to avoid an import
// cycle between this package's tests and `internal/grant`.
type rolesFromDB struct{ db *postgres.DB }

func (r rolesFromDB) ForToken(ctx context.Context, orgID, userID, projectID string) (RoleClaims, error) {
	var out RoleClaims
	err := r.db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT
			  coalesce((SELECT role_keys FROM user_grants
			             WHERE user_id = $1 AND project_id = $2), '{}'),
			  coalesce((SELECT array_agg(role ORDER BY role) FROM manager_roles
			             WHERE user_id = $1), '{}')`,
			userID, projectID,
		).Scan(pq.Array(&out.Keys), pq.Array(&out.Manager))
	})
	return out, err
}

// signingKeys creates a real key through P1-03's store, so the token is signed
// by the same path production uses — including the secret resolver.
func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a signing key: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key file: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("opening the owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	// The owner connection, because signing_keys is instance-level: it has no
	// org_id and no RLS policy, and key management is an operator action.
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

// liveness is what main.go wires as sessionLiveness: the refresh grant refuses
// a token whose session has ended. Reproduced here so the test exercises the
// same seam rather than leaving Sessions nil and skipping the check.
type liveness struct{ sessions *session.Manager }

func (l liveness) IsLive(ctx context.Context, sessionID string, now time.Time) bool {
	return l.sessions.IsLive(ctx, sessionID, now)
}

// clientAdapter is what main.go wires; reproduced here so the test exercises
// the same seam.
type clientAdapter struct {
	store *client.Store
	db    *postgres.DB
}

func (c clientAdapter) ByClientID(ctx context.Context, id string) (client.Application, error) {
	return c.store.ByClientID(ctx, c.db, id)
}

func (c clientAdapter) CredentialsFor(ctx context.Context, app client.Application) (client.Credentials, error) {
	return c.store.CredentialsFor(ctx, c.db, app)
}

// issueCode puts a code in Redis exactly as /oauth/authorize would.
func (f fixture) issueCode(t *testing.T, mutate func(*authorize.Code)) string {
	t.Helper()

	code := authorize.Code{
		ClientID:      f.appID,
		RedirectURI:   "https://app.example.com/cb",
		UserID:        f.userID,
		OrgID:         f.orgID,
		SessionID:     f.sessionID,
		Scope:         []string{"openid", "profile", "offline_access"},
		CodeChallenge: challengeFor(f.verifier),
		AuthMethods:   []string{"pwd"},
		AuthTime:      time.Now().Add(-time.Minute),
		IssuedAt:      time.Now(),
	}
	if mutate != nil {
		mutate(&code)
	}

	value, err := f.codes.IssueCode(context.Background(), code, authorize.CodeTTL)
	if err != nil {
		t.Fatalf("issuing a code: %v", err)
	}
	return value
}

func (f fixture) post(t *testing.T, form url.Values, useSecret bool) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if useSecret {
		r.SetBasicAuth(url.QueryEscape(f.appID), url.QueryEscape(f.secret.Reveal()))
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

func (f fixture) exchange(t *testing.T, code string) *httptest.ResponseRecorder {
	t.Helper()
	return f.post(t, url.Values{
		"grant_type":    {GrantAuthorizationCode},
		"code":          {code},
		"redirect_uri":  {"https://app.example.com/cb"},
		"code_verifier": {f.verifier},
	}, true)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the response: %v\n%s", err, rec.Body.String())
	}
	return body
}

// --- the happy path -------------------------------------------------------------

// P1-07 DoD item 1, and the moment the flow finally closes: P1-06 has been
// issuing codes that nothing could redeem.
func TestAuthorizationCodeExchange(t *testing.T) {
	f := setup(t)

	rec := f.exchange(t, f.issueCode(t, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	body := decode(t, rec)
	for _, required := range []string{"access_token", "token_type", "expires_in", "id_token", "refresh_token"} {
		if body[required] == nil || body[required] == "" {
			t.Errorf("the response is missing %q: %s", required, rec.Body.String())
		}
	}
	if body["token_type"] != "Bearer" {
		t.Errorf("token_type = %v", body["token_type"])
	}

	// P1-07 DoD item 7 / RFC 6749 § 5.1. A cached token response is a token
	// handed to whoever asks the cache next.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// The loop from P1-03's signing, through P1-04's publication, to this
// endpoint's issuance — closed for the first time. This is the property a
// consumer actually depends on: it fetches the JWKS and verifies locally.
func TestTheIssuedIDTokenVerifiesAgainstTheJWKS(t *testing.T) {
	f := setup(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	idToken, _ := body["id_token"].(string)
	if idToken == "" {
		t.Fatal("no id_token was issued")
	}

	// Verified through the same Verifier a consumer's library models, against
	// the published key set rather than the private key.
	payload, err := signing.NewVerifier(f.keys).Verify(idToken, signing.TypeJWT)
	if err != nil {
		t.Fatalf("the issued id_token does not verify against the published keys: %v", err)
	}

	// And it does NOT verify as an access token. P1-08 relies on exactly this
	// to refuse an id_token presented as a bearer credential (abuse case A-5),
	// so the property is asserted where the tokens are actually issued rather
	// than only where they are consumed.
	if _, err := signing.NewVerifier(f.keys).Verify(idToken, signing.TypeAccessToken); err == nil {
		t.Error("the issued id_token verifies as an access token; typ is not distinguishing them")
	}

	accessToken, _ := body["access_token"].(string)
	if accessToken == "" {
		t.Fatal("no access_token was issued")
	}
	if _, err := signing.NewVerifier(f.keys).Verify(accessToken, signing.TypeAccessToken); err != nil {
		t.Errorf("the issued access token does not verify as at+jwt: %v", err)
	}
	if _, err := signing.NewVerifier(f.keys).Verify(accessToken, signing.TypeJWT); err == nil {
		t.Error("the issued access token verifies as an id_token; typ is not distinguishing them")
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decoding claims: %v", err)
	}

	if claims["iss"] != testIssuer {
		t.Errorf("iss = %v, want %q", claims["iss"], testIssuer)
	}
	if claims["aud"] != f.appID {
		t.Errorf("aud = %v, want the client id", claims["aud"])
	}
	if claims["sub"] != f.userID {
		t.Errorf("sub = %v, want the user id", claims["sub"])
	}
	amr, _ := claims["amr"].([]any)
	if len(amr) != 1 || amr[0] != "pwd" {
		t.Errorf("amr = %v, want [pwd]", claims["amr"])
	}
}

// --- the code's bindings ------------------------------------------------------------

// P1-07 DoD item 3, plus the property that makes it safe: the code is consumed
// before the verifier is checked, so a wrong guess cannot be retried.
func TestAWrongVerifierIsRejectedAndBurnsTheCode(t *testing.T) {
	f := setup(t)
	code := f.issueCode(t, nil)

	rec := f.post(t, url.Values{
		"grant_type":    {GrantAuthorizationCode},
		"code":          {code},
		"redirect_uri":  {"https://app.example.com/cb"},
		"code_verifier": {"M25iVXpKU3puUjFaYWg3T1NDTDQtcW1ROUY5YXlwalNoc0hhakxifmZHag"},
	}, true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if decode(t, rec)["error"] != ErrInvalidGrant {
		t.Errorf("error = %v, want %q", decode(t, rec)["error"], ErrInvalidGrant)
	}

	// The code is gone. Leaving it alive would turn a single-use credential
	// into an oracle a verifier could be brute-forced against.
	retry := f.exchange(t, code)
	if retry.Code == http.StatusOK {
		t.Error("the code survived a failed verifier; it can be retried against")
	}
}

// Abuse case A-3.
func TestACodeCannotBeRedeemedByAnotherClient(t *testing.T) {
	f := setup(t)

	// A code issued to a different client.
	code := f.issueCode(t, func(c *authorize.Code) {
		c.ClientID = "99999999-9999-9999-9999-999999999999"
	})

	rec := f.exchange(t, code)
	if rec.Code == http.StatusOK {
		t.Error("a code issued to another client was redeemed")
	}
	if decode(t, rec)["error"] != ErrInvalidGrant {
		t.Errorf("error = %v, want %q", decode(t, rec)["error"], ErrInvalidGrant)
	}
}

func TestTheRedirectURIMustMatchTheCode(t *testing.T) {
	f := setup(t)

	rec := f.post(t, url.Values{
		"grant_type":    {GrantAuthorizationCode},
		"code":          {f.issueCode(t, nil)},
		"redirect_uri":  {"https://app.example.com/cb2"},
		"code_verifier": {f.verifier},
	}, true)

	if rec.Code == http.StatusOK {
		t.Error("a code was redeemed against a different redirect_uri")
	}
}

// Abuse case A-1.
func TestACodeRedeemsOnlyOnce(t *testing.T) {
	f := setup(t)
	code := f.issueCode(t, nil)

	if rec := f.exchange(t, code); rec.Code != http.StatusOK {
		t.Fatalf("the first exchange failed: %s", rec.Body.String())
	}
	if rec := f.exchange(t, code); rec.Code == http.StatusOK {
		t.Error("a code was redeemed twice")
	}
}

// P1-07 DoD item 4: concurrent redemption yields exactly one success.
//
// The same property P1-06 proves at the store; proved again here through the
// whole endpoint, because that is where a second lookup or a retry could have
// been introduced between the redemption and the response.
func TestConcurrentExchangeYieldsExactlyOneTokenSet(t *testing.T) {
	f := setup(t)

	const racers = 16
	for round := range 5 {
		code := f.issueCode(t, nil)

		var (
			wg        sync.WaitGroup
			mu        sync.Mutex
			successes int
		)
		start := make(chan struct{})

		for range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rec := f.exchange(t, code)
				if rec.Code == http.StatusOK {
					mu.Lock()
					successes++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()

		if successes != 1 {
			t.Fatalf("round %d: %d concurrent exchanges succeeded, want exactly 1", round, successes)
		}
	}
}

// --- client authentication ------------------------------------------------------------

// Abuse case A-4, end to end.
func TestAConfidentialClientCannotRedeemWithoutItsSecret(t *testing.T) {
	f := setup(t)

	rec := f.post(t, url.Values{
		"grant_type":    {GrantAuthorizationCode},
		"client_id":     {f.appID},
		"code":          {f.issueCode(t, nil)},
		"redirect_uri":  {"https://app.example.com/cb"},
		"code_verifier": {f.verifier},
	}, false) // no secret

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	if decode(t, rec)["error"] != ErrInvalidClient {
		t.Errorf("error = %v, want %q", decode(t, rec)["error"], ErrInvalidClient)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" && !strings.Contains(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}

// --- refresh ---------------------------------------------------------------------------

// P1-07 DoD item 6.
func TestNoRefreshTokenIsStoredInPlaintext(t *testing.T) {
	f := setup(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)
	if refresh == "" {
		t.Fatal("no refresh token was issued")
	}

	// The whole row as text, so a leak into any column is caught.
	var dump string
	f.factory.QueryRow(&dump, `SELECT refresh_tokens::text FROM refresh_tokens LIMIT 1`)

	if strings.Contains(dump, refresh) {
		t.Error("the refresh token appears in the stored row")
	}
	if !strings.Contains(dump, HashRefresh(refresh)) {
		t.Error("the stored row does not contain the token's hash")
	}
}

func TestRefreshGrant(t *testing.T) {
	f := setup(t)

	first := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := first["refresh_token"].(string)

	rec := f.post(t, url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
	}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("the refresh failed: %d %s", rec.Code, rec.Body.String())
	}

	body := decode(t, rec)
	if body["access_token"] == nil || body["access_token"] == "" {
		t.Error("the refresh returned no access token")
	}
	// No id_token on a refresh: nothing was authenticated just now, and an
	// id_token asserting an authentication that did not happen would be a
	// false statement with a fresh timestamp on it.
	if body["id_token"] != nil {
		t.Error("a refresh returned an id_token")
	}
	// PG-15: the scope was recorded at issuance, so the refresh can reproduce it.
	if !strings.Contains(body["scope"].(string), "openid") {
		t.Errorf("the refreshed scope lost what was granted: %v", body["scope"])
	}
}

// Widening would make the refresh token more powerful than the consent that
// created it.
func TestARefreshCannotWidenScope(t *testing.T) {
	f := setup(t)

	first := decode(t, f.exchange(t, f.issueCode(t, func(c *authorize.Code) {
		c.Scope = []string{"openid", "offline_access"}
	})))
	refresh, _ := first["refresh_token"].(string)

	rec := f.post(t, url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
		"scope":         {"openid profile"},
	}, true)

	if rec.Code == http.StatusOK {
		t.Error("a refresh widened its scope")
	}
	if decode(t, rec)["error"] != ErrInvalidScope {
		t.Errorf("error = %v, want %q", decode(t, rec)["error"], ErrInvalidScope)
	}
}

// --- client_credentials -----------------------------------------------------------------

func TestClientCredentialsGrant(t *testing.T) {
	f := setup(t)

	rec := f.post(t, url.Values{"grant_type": {GrantClientCredentials}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	body := decode(t, rec)
	if body["access_token"] == nil || body["access_token"] == "" {
		t.Fatal("no access token")
	}
	// No user, so no assertion about one — and no refresh token, because the
	// client can authenticate again whenever it likes.
	if body["id_token"] != nil {
		t.Error("client_credentials returned an id_token; there is no user to assert about")
	}
	if body["refresh_token"] != nil {
		t.Error("client_credentials returned a refresh token")
	}
}

func TestClientCredentialsRefusesOpenID(t *testing.T) {
	f := setup(t)

	rec := f.post(t, url.Values{
		"grant_type": {GrantClientCredentials},
		"scope":      {"openid"},
	}, true)

	if rec.Code == http.StatusOK {
		t.Error("client_credentials was granted the openid scope with no user")
	}
}

// --- refused grants ------------------------------------------------------------------------

func TestRefusedGrantsAtTheEndpoint(t *testing.T) {
	f := setup(t)

	for _, grant := range []string{"password", "implicit"} {
		t.Run(grant, func(t *testing.T) {
			rec := f.post(t, url.Values{"grant_type": {grant}}, true)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if decode(t, rec)["error"] != ErrUnsupportedGrantType {
				t.Errorf("error = %v, want %q", decode(t, rec)["error"], ErrUnsupportedGrantType)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Error("an error response is cacheable")
			}
		})
	}
}

// P1-08 depends on this, and nothing asserted it until P1-08 was written.
//
// The `sid` claim is what /oauth/userinfo looks the session up by, so an
// access token without one is refused there. A fresh token obviously carries
// it; the one worth testing is the REFRESHED token, because the refresh path
// builds its own Subject and dropping SessionID from that struct is a one-line
// edit that breaks nothing here and makes every refreshed token fail at
// userinfo with `invalid_token` — which reads like a signature problem and is
// very hard to trace back.
func TestSidSurvivesARefresh(t *testing.T) {
	f := setup(t)

	first := decode(t, f.exchange(t, f.issueCode(t, nil)))
	original := claimsOf(t, f, first["access_token"].(string))

	if original["sid"] == nil || original["sid"] == "" {
		t.Fatal("a freshly issued access token carries no sid; userinfo cannot " +
			"check the session behind it")
	}

	refresh, _ := first["refresh_token"].(string)
	rec := f.post(t, url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
	}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("the refresh failed: %d %s", rec.Code, rec.Body.String())
	}

	refreshed := claimsOf(t, f, decode(t, rec)["access_token"].(string))

	if refreshed["sid"] != original["sid"] {
		t.Errorf("sid = %v after a refresh, want the original %v — a refreshed "+
			"token that loses its sid is refused by /oauth/userinfo",
			refreshed["sid"], original["sid"])
	}
}

// A client_credentials token has no user and must carry no sid: `sub` is the
// client, and userinfo refuses it partly on that absence.
func TestClientCredentialsCarriesNoSession(t *testing.T) {
	f := setup(t)

	rec := f.post(t, url.Values{"grant_type": {GrantClientCredentials}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("client_credentials failed: %d %s", rec.Code, rec.Body.String())
	}

	claims := claimsOf(t, f, decode(t, rec)["access_token"].(string))

	if _, present := claims["sid"]; present {
		t.Errorf("a client_credentials token carries sid=%v; there is no session "+
			"and no user behind it", claims["sid"])
	}
	if claims["sub"] != f.appID {
		t.Errorf("sub = %v, want the client id", claims["sub"])
	}
}

// claimsOf verifies a token against the published key set and returns its
// claims — the same path a resource server takes.
func claimsOf(t *testing.T, f fixture, compact string) map[string]any {
	t.Helper()

	payload, err := signing.NewVerifier(f.keys).Verify(compact, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("verifying the access token: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decoding claims: %v", err)
	}
	return claims
}

// --- P1-09: introspection and revocation ---------------------------------------------

// lifecycle builds the handler against everything real: the same key set, the
// same client store, the same refresh store, the same database.
func (f fixture) lifecycle(t *testing.T) *LifecycleHandler {
	t.Helper()

	return &LifecycleHandler{
		Issuer:   testIssuer,
		Clients:  clientAdapter{store: f.clients, db: f.db},
		Verifier: signing.NewVerifier(f.keys),
		Refresh:  boundRefresh{store: NewRefreshStore(), db: f.db},
		// The session check is wired, because it is the difference between an
		// access token that resolves and one reported inactive — leaving it
		// nil made the access-token revocation test fail for a reason that
		// looked like the revocation being broken.
		Sessions: liveness{sessions: session.NewManager(
			f.db, session.NewCache(f.rdb, nil), audit.NewWriter(f.db, discard(), nil), discard())},
		Tenant: f.db,
		Audit:  audit.NewWriter(f.db, discard(), nil),
		Log:    discard(),
	}
}

// boundRefresh is the adapter cmd/authservice wires, reproduced so the test
// exercises the same seam.
type boundRefresh struct {
	store *RefreshStore
	db    *postgres.DB
}

func (r boundRefresh) Lookup(ctx context.Context, presented string, now time.Time) (Refresh, error) {
	return r.store.Lookup(ctx, r.db, presented, now)
}

func (r boundRefresh) RevokeFamily(ctx context.Context, tx *postgres.Tx, familyID string) (int64, error) {
	return r.store.RevokeFamily(ctx, tx, familyID)
}

func (r boundRefresh) RevokeForSessionAndClient(
	ctx context.Context, tx *postgres.Tx, sessionID, clientID string,
) (int64, error) {
	return r.store.RevokeForSessionAndClient(ctx, tx, sessionID, clientID)
}

// post sends an authenticated form to one of the two lifecycle endpoints.
func (f fixture) lifecyclePost(
	t *testing.T, h *LifecycleHandler, path string, form url.Values, clientID, secret string,
) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))

	w := httptest.NewRecorder()
	if strings.Contains(path, "introspect") {
		h.Introspect(w, r)
	} else {
		h.Revoke(w, r)
	}
	return w
}

// secondClient registers another confidential application in the same
// organization, which is what the ownership tests need: same tenant, so RLS is
// not what refuses the request — the ownership rule is.
func (f fixture) secondClient(t *testing.T) (client.Record, client.Secret) {
	t.Helper()

	var projectID string
	f.factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, f.orgID, "other")

	var (
		app    client.Record
		secret client.Secret
	)
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		app, secret, err = f.clients.Create(context.Background(), tx, client.Application{
			OrgID: f.orgID, ProjectID: projectID, Name: "other", Type: client.TypeWeb,
			GrantTypes:   []string{GrantAuthorizationCode, GrantRefreshToken},
			RedirectURIs: []string{"https://other.example.com/cb"},
		}, "")
		return err
	}); err != nil {
		t.Fatalf("creating the second application: %v", err)
	}
	return app, secret
}

func TestIntrospectingALiveRefreshToken(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)
	if refresh == "" {
		t.Fatal("no refresh token was issued")
	}

	w := f.lifecyclePost(t, h, "/oauth/introspect",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", w.Code, w.Body.String())
	}
	got := decode(t, w)
	if got["active"] != true {
		t.Fatalf("active = %v for a token issued moments ago:\n%s", got["active"], w.Body.String())
	}
	if got["client_id"] != f.appID {
		t.Errorf("client_id = %v", got["client_id"])
	}
	if got["sub"] != f.userID {
		t.Errorf("sub = %v", got["sub"])
	}
	// The address is not here, and the resource server does not need it.
	for _, forbidden := range []string{"username", "email"} {
		if _, present := got[forbidden]; present {
			t.Errorf("the response carries %q", forbidden)
		}
	}
}

// The card's second abuse case, against two real clients in the same
// organization — so RLS is not what refuses this, the ownership rule is.
func TestOneClientCannotIntrospectAnothersToken(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)

	other, otherSecret := f.secondClient(t)

	// The control: the owner can see it.
	if got := decode(t, f.lifecyclePost(t, h, "/oauth/introspect",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())); got["active"] != true {
		t.Fatal("the owning client cannot introspect its own token; the assertion below proves nothing")
	}

	w := f.lifecyclePost(t, h, "/oauth/introspect",
		url.Values{"token": {refresh}}, other.ID, otherSecret.Reveal())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with active:false rather than a refusal:\n%s",
			w.Code, w.Body.String())
	}
	got := decode(t, w)
	if got["active"] != false {
		t.Error("one client introspected another client's refresh token")
	}
	if len(got) != 1 {
		t.Errorf("the answer leaked detail about another client's token: %v", got)
	}
}

func TestOneClientCannotRevokeAnothersToken(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)

	other, otherSecret := f.secondClient(t)

	w := f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {refresh}}, other.ID, otherSecret.Reveal())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 regardless:\n%s", w.Code, w.Body.String())
	}

	// It must still work. A 200 that quietly destroyed somebody else's token
	// would be the abuse case succeeding silently.
	rec := f.post(t, url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
	}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("another client revoked this token: the refresh now fails %d %s",
			rec.Code, rec.Body.String())
	}
}

// DoD item 3, and RFC 7009 section 2.1: revoking a refresh token invalidates
// tokens derived from it, not just the row presented.
func TestRevokingARefreshTokenKillsItsFamily(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)

	var familyID string
	f.factory.QueryRow(&familyID, `SELECT family_id FROM refresh_tokens LIMIT 1`)

	// A sibling in the same family — what a rotation will produce once P3-06
	// lands, and what "based on the same authorization grant" means.
	var sibling RefreshToken
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		sibling, _, err = NewRefreshStore().Issue(context.Background(), tx, Refresh{
			UserID: f.userID, ClientID: f.appID, OrgID: f.orgID,
			SessionID: f.sessionID, Scope: []string{"openid"},
			ExpiresAt: time.Now().Add(RefreshTokenLifetime),
		}, familyID, time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing a sibling: %v", err)
	}

	w := f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	for name, presented := range map[string]string{
		"the revoked token": refresh,
		"its sibling":       sibling.Reveal(),
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.post(t, url.Values{
				"grant_type":    {GrantRefreshToken},
				"refresh_token": {presented},
			}, true)
			if rec.Code == http.StatusOK {
				t.Error("still redeemable after the family was revoked")
			}
		})
	}
}

// RFC 7009 section 2.2. Revocation is idempotent, and a client retrying a
// request it is not sure landed must not be told which attempt worked.
func TestRevocationIsIdempotent(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)

	first := f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())
	second := f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())

	if first.Code != second.Code {
		t.Errorf("the second revocation answered %d and the first %d", second.Code, first.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Error("the two revocations differ in their bodies")
	}
}

// A revoked refresh token introspects as inactive, which is the same answer an
// unknown one gets.
func TestARevokedTokenIntrospectsAsInactive(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)

	revoked := f.lifecyclePost(t, h, "/oauth/introspect",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())
	if decode(t, revoked)["active"] != true {
		t.Fatal("the token was not active before revocation")
	}

	f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())

	after := f.lifecyclePost(t, h, "/oauth/introspect",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())
	unknown := f.lifecyclePost(t, h, "/oauth/introspect",
		url.Values{"token": {"a-token-that-never-existed"}}, f.appID, f.secret.Reveal())

	if after.Body.String() != unknown.Body.String() {
		t.Errorf("a revoked token is distinguishable from an unknown one:\n%s\n---\n%s",
			after.Body.String(), unknown.Body.String())
	}
}

// DoD item 4, and the other half of it: the token itself must not be in the
// entry. The audit log has a 24-month retention, which makes it the worst
// place in the system for a credential.
func TestARevocationIsAuditedWithoutTheToken(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	refresh, _ := body["refresh_token"].(string)

	f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {refresh}}, f.appID, f.secret.Reveal())

	var payloads string
	f.factory.QueryRow(&payloads,
		`SELECT COALESCE(string_agg(payload::text, '|'), '') FROM events WHERE event_type = 'token.revoked'`)

	if payloads == "" {
		t.Fatal("the revocation was not audited")
	}
	if !strings.Contains(payloads, f.appID) {
		t.Errorf("the entry does not name the client: %s", payloads)
	}
	for _, forbidden := range []string{refresh, HashRefresh(refresh)} {
		if strings.Contains(payloads, forbidden) {
			t.Errorf("the audit entry contains the token or its hash: %s", payloads)
		}
	}
}

// An access token cannot be revoked, and presenting one revokes the refresh
// tokens behind it instead — RFC 7009 section 2.1's SHOULD. The important half
// is the scoping: session AND client, never session alone, or one client's
// logout would throw away every other application's refresh token in the same
// single sign-on session.
func TestRevokingAnAccessTokenRevokesTheRefreshBehindIt(t *testing.T) {
	f := setup(t)
	h := f.lifecycle(t)

	body := decode(t, f.exchange(t, f.issueCode(t, nil)))
	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)

	// A second client, with its own refresh token on the SAME session.
	other, otherSecret := f.secondClient(t)
	var othersToken RefreshToken
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		var err error
		othersToken, _, err = NewRefreshStore().Issue(context.Background(), tx, Refresh{
			UserID: f.userID, ClientID: other.ID, OrgID: f.orgID,
			SessionID: f.sessionID, Scope: []string{"openid"},
			ExpiresAt: time.Now().Add(RefreshTokenLifetime),
		}, "", time.Now())
		return err
	}); err != nil {
		t.Fatalf("issuing the other client's token: %v", err)
	}

	w := f.lifecyclePost(t, h, "/oauth/revoke",
		url.Values{"token": {access}}, f.appID, f.secret.Reveal())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", w.Code, w.Body.String())
	}

	// Ours is gone.
	if rec := f.post(t, url.Values{
		"grant_type":    {GrantRefreshToken},
		"refresh_token": {refresh},
	}, true); rec.Code == http.StatusOK {
		t.Error("the refresh token behind the access token survived")
	}

	// Theirs is not. This is the assertion that would fail if the predicate
	// were session alone.
	var live bool
	f.factory.QueryRow(&live,
		`SELECT NOT revoked FROM refresh_tokens WHERE token_hash = $1`,
		HashRefresh(othersToken.Reveal()))
	if !live {
		t.Error("revoking one client's access token destroyed another client's " +
			"refresh token on the same session")
	}

	_ = otherSecret
}

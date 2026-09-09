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

	"github.com/redis/go-redis/v9"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const testIssuer = "https://auth.example"

type fixture struct {
	db       *postgres.DB
	rdb      *redis.Client
	factory  *testsupport.Factory
	handler  *Handler
	codes    *authorize.Store
	clients  *client.Store
	orgID    string
	userID   string
	appID    string
	secret   client.Secret
	verifier string
	keys     *signing.Cache
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

	f := fixture{
		db:       db,
		rdb:      rdb,
		factory:  factory,
		codes:    authorize.NewStore(rdb),
		clients:  clients,
		orgID:    orgID,
		userID:   userID,
		appID:    app.ID,
		secret:   secret,
		verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		keys:     keys,
	}

	f.handler = &Handler{
		Issuer:  testIssuer,
		Clients: clientAdapter{store: clients, db: db},
		Codes:   f.codes,
		Refresh: NewRefreshStore(),
		Signer:  signing.NewSigner(keys),
		DB:      db,
		Audit:   auditor,
		Log:     discard(),
	}
	return f
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
		SessionID:     "",
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
	payload, err := signing.NewVerifier(f.keys).Verify(idToken)
	if err != nil {
		t.Fatalf("the issued id_token does not verify against the published keys: %v", err)
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

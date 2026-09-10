//go:build integration

package userinfo

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The chain a real request travels: a token signed by a real key, verified
// against the published key set, resolved to a real user through a
// tenant-scoped query that also checks the session is live.
//
// What is here rather than in handler_test.go is everything a fake cannot
// answer honestly — above all that revoking a session stops the token working,
// which is the one claim in the spec that costs something and is therefore the
// one most worth proving.

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const liveIssuer = "https://auth.example"

type stack struct {
	db        *postgres.DB
	factory   *testsupport.Factory
	handler   *Handler
	signer    *signing.Signer
	keys      *signing.Cache
	sessions  *session.Manager
	orgID     string
	userID    string
	sessionID string
}

func setup(t *testing.T) *stack {
	t.Helper()

	containers := testsupport.Start(t)
	testsupport.Truncate(t, containers)

	factory := testsupport.NewFactory(t, containers)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID, "alice@example.test")
	factory.Exec(
		`UPDATE users SET display_name = $2, username = $3 WHERE id = $1`,
		userID, "Alice Example", "alice")

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: containers.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keys := signingKeys(t, containers)
	auditor := audit.NewWriter(db, discard(), nil)
	rdb := redis.NewClient(&redis.Options{Addr: containers.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	sessions := session.NewManager(db, session.NewCache(rdb, nil), auditor, discard())

	// A real session, created through P1-11 rather than inserted, so the row
	// this endpoint checks liveness against is the row login would produce.
	var created session.Session
	if err := db.WithTenant(context.Background(), orgID, func(tx *postgres.Tx) error {
		var err error
		created, _, err = sessions.Create(context.Background(), tx, session.New{
			UserID: userID, OrgID: orgID, AuthMethods: []string{"pwd"}, IP: "192.0.2.1",
		}, session.DefaultPolicy, time.Now())
		return err
	}); err != nil {
		t.Fatalf("creating the session: %v", err)
	}

	return &stack{
		db: db, factory: factory, signer: signing.NewSigner(keys), keys: keys, sessions: sessions,
		orgID: orgID, userID: userID, sessionID: created.ID,
		handler: &Handler{
			Issuer:   liveIssuer,
			Verifier: signing.NewVerifier(keys),
			Subjects: NewStore(),
			DB:       db,
			Log:      discard(),
		},
	}
}

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

// accessToken mints one through P1-07's own claim builder and signer, so this
// test consumes what the token endpoint actually produces rather than a
// hand-built approximation of it.
func (s *stack) accessToken(t *testing.T, scope []string) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer:    liveIssuer,
		Audience:  liveIssuer,
		ClientID:  "11111111-1111-1111-1111-111111111111",
		OrgID:     s.orgID,
		UserID:    s.userID,
		SessionID: s.sessionID,
		Scope:     scope,
	}, time.Now())
	if err != nil {
		t.Fatalf("building claims: %v", err)
	}

	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding claims: %v", err)
	}

	signed, err := s.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func (s *stack) call(t *testing.T, bearer string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	return w
}

// --- the happy path ------------------------------------------------------------

func TestARealTokenReturnsTheRealUser(t *testing.T) {
	s := setup(t)

	w := s.call(t, s.accessToken(t, []string{"openid", "profile", "email"}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", w.Code, w.Body.String())
	}

	var claims map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &claims); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}

	if claims["sub"] != s.userID {
		t.Errorf("sub = %v, want the user id", claims["sub"])
	}
	if claims["email"] != "alice@example.test" {
		t.Errorf("email = %v", claims["email"])
	}
	if claims["name"] != "Alice Example" {
		t.Errorf("name = %v", claims["name"])
	}
	if claims["preferred_username"] != "alice" {
		t.Errorf("preferred_username = %v", claims["preferred_username"])
	}
	if _, present := claims["updated_at"]; !present {
		t.Error("updated_at is absent")
	}
}

func TestScopesAreHonouredAgainstRealData(t *testing.T) {
	s := setup(t)

	w := s.call(t, s.accessToken(t, []string{"openid"}))

	var claims map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &claims); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(claims) != 1 {
		t.Errorf("claims = %v, want only sub", claims)
	}
	// The row HAS an email and a display name. The scope is what withholds
	// them — which is the whole point, and a mapping tested only against an
	// empty row would not show it.
	if strings.Contains(w.Body.String(), "alice@example.test") {
		t.Error("the email was returned without the email scope")
	}
	if strings.Contains(w.Body.String(), "Alice Example") {
		t.Error("the display name was returned without the profile scope")
	}
}

// --- the check that costs something ------------------------------------------------

// DoD item 4, and the reason the store's query carries a liveness predicate at
// all. Without it a user who logs out keeps being described for up to ten
// minutes — the access token's remaining life — which for a call an SPA makes
// on every page load is the difference between logging out and appearing to.
func TestRevokingTheSessionInvalidatesTheTokenImmediately(t *testing.T) {
	s := setup(t)
	bearer := s.accessToken(t, []string{"openid", "profile", "email"})

	// The control: it works first.
	if w := s.call(t, bearer); w.Code != http.StatusOK {
		t.Fatalf("the token did not work before revocation: %d", w.Code)
	}

	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		_, err := s.sessions.Revoke(context.Background(), tx, s.sessionID,
			session.ReasonLogout, s.userID, time.Now())
		return err
	}); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	w := s.call(t, bearer)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("the same token still works after logout: %d\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Errorf("WWW-Authenticate = %q", w.Header().Get("WWW-Authenticate"))
	}
	// And it says nothing about WHY — a caller holding a captured token must
	// not be able to ask this endpoint whether the owner has logged out.
	if strings.Contains(strings.ToLower(w.Body.String()), "revok") ||
		strings.Contains(strings.ToLower(w.Body.String()), "session") {
		t.Errorf("the response explains the revocation:\n%s", w.Body.String())
	}
}

func TestAnExpiredSessionInvalidatesTheToken(t *testing.T) {
	s := setup(t)
	bearer := s.accessToken(t, []string{"openid"})

	// created_at moves too: the schema has a sessions_expires_after_creation
	// check constraint, so a row cannot expire before it began. Pushing only
	// expires_at into the past violates it — which is the constraint doing its
	// job, and a reminder that "expired" here means aged out rather than
	// retroactively invalid.
	s.factory.Exec(`
		UPDATE sessions
		   SET created_at = now() - interval '2 hours',
		       expires_at = now() - interval '1 hour'
		 WHERE id = $1`, s.sessionID)

	if w := s.call(t, bearer); w.Code != http.StatusUnauthorized {
		t.Errorf("a token for an expired session still works: %d", w.Code)
	}
}

func TestADeactivatedUserStopsBeingDescribed(t *testing.T) {
	for _, status := range []string{"locked", "deactivated", "invited"} {
		t.Run(status, func(t *testing.T) {
			s := setup(t)
			bearer := s.accessToken(t, []string{"openid", "email"})

			if w := s.call(t, bearer); w.Code != http.StatusOK {
				t.Fatalf("the token did not work while active: %d", w.Code)
			}

			s.factory.Exec(`UPDATE users SET status = $2 WHERE id = $1`, s.userID, status)

			if w := s.call(t, bearer); w.Code != http.StatusUnauthorized {
				t.Errorf("a %s user is still described: %d", status, w.Code)
			}
		})
	}
}

// --- tenancy ----------------------------------------------------------------------

// The organization comes from the token, and the read is scoped to it. A token
// naming another organization must find nothing, because RLS confines the
// query rather than a predicate somebody remembers to write.
func TestATokenCannotReachAcrossTenants(t *testing.T) {
	s := setup(t)

	other := s.factory.Organization(s.factory.Instance())

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer:    liveIssuer,
		Audience:  liveIssuer,
		ClientID:  "11111111-1111-1111-1111-111111111111",
		OrgID:     other, // another tenant
		UserID:    s.userID,
		SessionID: s.sessionID,
		Scope:     []string{"openid", "email"},
	}, time.Now())
	if err != nil {
		t.Fatalf("building claims: %v", err)
	}
	payload, _ := claims.Encode()
	forged, err := s.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	w := s.call(t, forged)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("a token scoped to another organization read the user: %d\n%s",
			w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "alice@example.test") {
		t.Error("the response leaked the address across tenants")
	}
}

// --- token type -------------------------------------------------------------------

// Abuse case A-5 end to end: an id_token signed by this very service, with
// claims for this very user, presented as a bearer credential.
//
// TWO independent controls refuse it, and this test deliberately does not try
// to isolate one. `typ` is `JWT` rather than `at+jwt`, and an id_token's `aud`
// is the client rather than the issuer — so removing either check still leaves
// the other. That was found by mutation: making the handler verify with
// `TypeJWT` did not break this test, because the audience check caught it.
//
// Saying so was the right response, rather than weakening the design to make
// one test sharper. The handler's use of `typ` is proven separately and
// sharply by TestTheHandlerAsksForAnAccessToken here, and by
// TestATokenOfTheWrongTypeIsRefused in `signing`.
//
// What THIS test establishes is the property a consumer cares about — an
// id_token gets nothing from this endpoint — and then, below, that the two
// things making it distinguishable are still TRUE OF THE TOKEN. Those
// assertions are about what P1-07 issues, not about what this handler checks:
// if a future change ever typed an id_token `at+jwt`, or addressed it to the
// issuer, this endpoint's defence would quietly rest on one control instead of
// two, and nothing else in the suite would notice.
func TestAnIDTokenIsRefusedAsABearerCredential(t *testing.T) {
	s := setup(t)

	claims, err := token.IDTokenClaims(token.Subject{
		Issuer:      liveIssuer,
		Audience:    liveIssuer,
		ClientID:    "11111111-1111-1111-1111-111111111111",
		OrgID:       s.orgID,
		UserID:      s.userID,
		SessionID:   s.sessionID,
		AuthMethods: []string{"pwd"},
		AuthTime:    time.Now(),
		Scope:       []string{"openid", "email"},
	}, time.Now())
	if err != nil {
		t.Fatalf("building claims: %v", err)
	}
	payload, _ := claims.Encode()

	idToken, err := s.signer.SignWithType(payload, signing.TypeJWT)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	w := s.call(t, idToken)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("an id_token was accepted as an access token: %d\n%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "alice@example.test") {
		t.Error("the response leaked the address to an id_token holder")
	}

	// Each half on its own, against the real key set, so that losing one later
	// fails here rather than silently leaning on the other.
	if _, err := signing.NewVerifier(s.keys).Verify(idToken, signing.TypeAccessToken); err == nil {
		t.Error("the id_token verifies as an access token; the typ control is gone")
	}

	verified, err := signing.NewVerifier(s.keys).Verify(idToken, signing.TypeJWT)
	if err != nil {
		t.Fatalf("the id_token does not verify as itself: %v", err)
	}
	var idClaims map[string]any
	if err := json.Unmarshal(verified, &idClaims); err != nil {
		t.Fatalf("decoding the id_token: %v", err)
	}
	if idClaims["aud"] == liveIssuer {
		t.Error("the id_token's audience is the issuer; the audience control is gone")
	}
}

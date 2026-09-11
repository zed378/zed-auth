//go:build integration

package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The whole flow, in one test (`P1-27` step 2, `docs/PLAN/11` § Integration).
//
//	create organization → create project → create application
//	→ create user → sign in → verify the token's claims
//
// Every stage of this is covered somewhere by a narrower test. What none of
// them can answer is whether the stages FIT: whether an application registered
// through one package resolves in another, whether a user created without a
// password can be given one and then authenticate, and whether the token that
// comes out the end says what the consumer at the other side needs.
//
// `console/e2e` drives the same path through a browser and a real HTTP
// service. This one runs against the packages directly, so a failure names the
// layer instead of a screen — and it runs in seconds rather than minutes,
// which is what makes it the layer that catches the break first.

func TestTheWholeFlowFitsTogether(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	ctx := context.Background()

	// As the RUNTIME role, like every test in this package. Registering an
	// application as the owner would pass against a schema where auth_app
	// cannot touch `applications` at all.
	db := appPool(t, stack)

	// --- an organization, and a project inside it --------------------------

	orgID := factory.Organization(factory.Instance())

	var projectID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, "billing")

	// --- an application, registered the way an operator registers one ------

	clients := client.NewStore(audit.NewWriter(db, discardLogger(), nil))

	registration := client.Application{
		OrgID:                  orgID,
		ProjectID:              projectID,
		Name:                   "billing-portal",
		Type:                   client.TypeWeb,
		GrantTypes:             []string{client.GrantAuthorizationCode, client.GrantRefreshToken},
		RedirectURIs:           []string{"https://billing.example.com/callback"},
		PostLogoutRedirectURIs: []string{"https://billing.example.com/"},
		AllowedOrigins:         []string{"https://billing.example.com"},
	}

	var registered client.Record
	var secret client.Secret
	if err := db.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		var err error
		registered, secret, err = clients.Create(ctx, tx, registration, "")
		return err
	}); err != nil {
		t.Fatalf("registering the application: %v", err)
	}

	if secret.IsZero() {
		t.Fatal("a confidential client was registered without a secret")
	}

	// **Resolved through the bootstrap path**, which is the one /oauth/authorize
	// uses before any tenant is known. A registration that only reads back
	// through the tenant-scoped path is a registration no authorization
	// request can find.
	resolved, err := clients.ByClientID(ctx, db, registered.ID)
	if err != nil {
		t.Fatalf("resolving the client_id an authorization request would carry: %v", err)
	}
	if resolved.OrgID != orgID {
		t.Errorf("the client resolved to organization %q, not %q", resolved.OrgID, orgID)
	}
	if !resolved.MatchesRedirectURI("https://billing.example.com/callback") {
		t.Error("the registered redirect URI does not match itself after a round trip")
	}
	if !resolved.MatchesOrigin("https://billing.example.com") {
		t.Error("the registered origin does not match itself after a round trip (P1-29)")
	}

	// --- a user, created with no password at all ---------------------------

	userID := factory.User(orgID, "budi@example.test")

	// `P1-19`: a user is created with no password, and there is no API that
	// sets one for somebody else. The password arrives through a link the
	// person receives, and this is the equivalent — the hash written by the
	// set-password page.
	hash, err := authn.Hash("Correct-Horse-Battery-Staple-Flow")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	factory.Exec(`UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash)

	// --- the sign-in -------------------------------------------------------

	var storedHash string
	factory.QueryRow(&storedHash, `SELECT password_hash FROM users WHERE id = $1`, userID)

	result, err := authn.Verify(storedHash, "Correct-Horse-Battery-Staple-Flow")
	if err != nil {
		t.Fatalf("verifying the password: %v", err)
	}
	if !result.Match {
		t.Fatal("the password set through the invitation does not authenticate")
	}

	// And the negative, in the same breath. A verifier that returned true for
	// everything would satisfy the line above.
	wrong, err := authn.Verify(storedHash, "not-the-password")
	if err != nil {
		t.Fatalf("verifying a wrong password: %v", err)
	}
	if wrong.Match {
		t.Fatal("SECURITY: the wrong password authenticated")
	}

	// --- the token, and what it says ---------------------------------------

	const issuer = "https://auth.example.test"
	now := time.Now()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer:      issuer,
		UserID:      userID,
		OrgID:       orgID,
		ClientID:    registered.ID,
		ProjectID:   projectID,
		Audience:    issuer,
		Scope:       []string{"openid"},
		AuthMethods: []string{"pwd"},
		AuthTime:    now,
	}, now)
	if err != nil {
		t.Fatalf("assembling the access token claims: %v", err)
	}

	// Each of these is something a consumer at the far end relies on, and each
	// is a different package's responsibility to have got right.
	for field, want := range map[string]any{
		"iss":       issuer,
		"sub":       userID,
		"org_id":    orgID,
		"client_id": registered.ID,
		"aud":       issuer,
	} {
		if got := claims[field]; got != want {
			t.Errorf("claim %q is %v, want %v", field, got, want)
		}
	}

	// The role claim namespace, reserved and empty until `P2-04`. Present
	// rather than absent, so a consumer reading it today does not change shape
	// when roles arrive — and a consumer that would have crashed on a missing
	// key never gets written.
	namespace := token.RoleClaimNamespace(projectID)
	roles, present := claims[namespace]
	if !present {
		t.Errorf("the reserved role claim %q is absent; a consumer written today "+
			"would have to change shape when P2-04 fills it", namespace)
	}
	if roles != nil {
		if assigned, isMap := roles.(map[string]any); !isMap || len(assigned) != 0 {
			t.Errorf("the reserved role claim carries %v before P2-04 fills it", roles)
		}
	}

	// --- and the token verifies, as the thing that signed it --------------

	keys := signingKeys(t, stack)
	signed, err := signing.NewSigner(keys).SignWithType(mustJSON(t, claims), signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing the access token: %v", err)
	}

	payload, err := signing.NewVerifier(keys).Verify(signed, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("the token this service just issued does not verify: %v", err)
	}
	if len(payload) == 0 {
		t.Error("the verified payload is empty")
	}

	// Abuse case A-5, at the end of the happy path rather than in a file of
	// its own: the same token, asked for as the other kind.
	if _, err := signing.NewVerifier(keys).Verify(signed, signing.TypeJWT); err == nil {
		t.Error("SECURITY: an access token was accepted where an ID token belongs")
	}
}

// --- helpers ----------------------------------------------------------------

// appPool opens a pooled connection as the runtime role.
//
// `appConn` in this package hands back a `*sql.DB` for the raw-SQL isolation
// tests. The stores take a `*postgres.DB`, because tenant scoping is a
// property of the transaction rather than of the query.
func appPool(t *testing.T, stack *testsupport.Stack) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN:             stack.AppDSN,
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	}, discardLogger())
	if err != nil {
		t.Fatalf("opening the application connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// signingKeys generates a key, stores it, and returns a cache over it.
//
// The private half goes to a file the resolver will accept, because that is
// how the service reads it — a test that injected the key directly would pass
// against a deployment where the reference resolves for nothing.
func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a signing key: %v", err)
	}

	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key file: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("opening the owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("storing the signing key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("promoting the signing key: %v", err)
	}

	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return raw
}

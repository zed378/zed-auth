//go:build integration

package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// openApp connects as the runtime role, not the owner.
//
// The service registers applications as auth_app, so the test does too. Run as
// the owner it would pass against a schema where the runtime role cannot touch
// `applications` at all, and the first symptom would be production.
func openApp(t *testing.T, stack *testsupport.Stack) *postgres.DB {
	t.Helper()

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN:             stack.AppDSN,
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type fixture struct {
	db      *postgres.DB
	store   *Store
	factory *testsupport.Factory
	orgID   string
	project string
}

func setup(t *testing.T) fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())

	var projectID string
	factory.QueryRow(&projectID,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, "billing")

	db := openApp(t, stack)

	return fixture{
		db:      db,
		store:   NewStore(audit.NewWriter(db, discard(), nil)),
		factory: factory,
		orgID:   orgID,
		project: projectID,
	}
}

func (f fixture) tx(t *testing.T, fn func(*postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), f.orgID, fn)
}

func (f fixture) webApp(name string) Application {
	return Application{
		OrgID:        f.orgID,
		ProjectID:    f.project,
		Name:         name,
		Type:         TypeWeb,
		GrantTypes:   []string{GrantAuthorizationCode, GrantRefreshToken},
		RedirectURIs: []string{"https://app.example.com/cb"},
	}
}

// --- the secret never reaches the row ---------------------------------------

// P1-05 DoD item 2: only the hash exists in the database, verified by direct
// inspection.
//
// The inspection is the point. Asserting that Get() does not return a secret
// only tests the read path; reading the raw column is what proves the
// plaintext was never written in the first place.
func TestOnlyTheHashIsStored(t *testing.T) {
	f := setup(t)

	var rec Record
	var secret Secret
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rec, secret, err = f.store.Create(context.Background(), tx, f.webApp("billing"), "")
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	plaintext := secret.Reveal()
	if plaintext == "" {
		t.Fatal("no secret was issued for a confidential client")
	}

	// The whole row, as text. Not just the secret column: if the plaintext
	// ever leaked into `name`, or into a future column, this catches it.
	var dump string
	f.factory.QueryRow(&dump, `SELECT applications::text FROM applications WHERE id = $1`, rec.ID)

	if strings.Contains(dump, plaintext) {
		t.Error("the plaintext secret appears somewhere in the stored row")
	}
	if !strings.Contains(dump, Hash(plaintext)) {
		t.Error("the stored row does not contain the hash of the issued secret")
	}

	// And the audit event, which is the other place a secret classically ends
	// up — written by the same transaction, so it is on disk by now.
	var payloads string
	f.factory.QueryRow(&payloads,
		`SELECT COALESCE(string_agg(payload::text, ' '), '') FROM events WHERE org_id = $1`, f.orgID)

	if strings.Contains(payloads, plaintext) {
		t.Error("the plaintext secret appears in an audit payload")
	}
	if strings.Contains(payloads, Hash(plaintext)) {
		t.Error("the secret hash appears in an audit payload")
	}
}

// The control: the assertion above would pass if Create silently issued no
// secret, so the issued one must actually authenticate.
func TestTheIssuedSecretAuthenticates(t *testing.T) {
	f := setup(t)

	var rec Record
	var secret Secret
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rec, secret, err = f.store.Create(context.Background(), tx, f.webApp("billing"), "")
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.store.Authenticate(context.Background(), tx, rec.ID, secret.Reveal(), time.Now())
		return err
	}); err != nil {
		t.Fatalf("the issued secret does not authenticate: %v", err)
	}

	// A wrong secret fails, so authentication is not simply returning success.
	other, _, _ := Generate()
	err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.store.Authenticate(context.Background(), tx, rec.ID, other.Reveal(), time.Now())
		return err
	})
	if err == nil {
		t.Error("an unrelated secret authenticated")
	}
}

// --- public clients ---------------------------------------------------------

// P1-05 DoD item 4, and abuse case A-4. Belt and braces: the code refuses to
// issue one, and P0-07's CHECK constraint refuses to store one.
func TestPublicClientsGetNoSecret(t *testing.T) {
	f := setup(t)

	for _, typ := range []Type{TypeSPA, TypeNative} {
		t.Run(string(typ), func(t *testing.T) {
			app := f.webApp("public-" + string(typ))
			app.Type = typ
			if typ == TypeNative {
				app.RedirectURIs = []string{"http://127.0.0.1:8080/cb"}
			}

			var rec Record
			var secret Secret
			if err := f.tx(t, func(tx *postgres.Tx) error {
				var err error
				rec, secret, err = f.store.Create(context.Background(), tx, app, "")
				return err
			}); err != nil {
				t.Fatalf("Create: %v", err)
			}

			if !secret.IsZero() {
				t.Error("a secret was issued to a public client")
			}
			if rec.Credentials.HasSecret() {
				t.Error("a public client was stored with a secret hash")
			}

			// Rotation must refuse too, rather than quietly issuing the secret
			// the database would then reject.
			err := f.tx(t, func(tx *postgres.Tx) error {
				_, _, err := f.store.RotateSecret(
					context.Background(), tx, rec.ID, DefaultRotationOverlap, time.Now(), "")
				return err
			})
			if !errors.Is(err, ErrPublicClientSecret) {
				t.Errorf("rotating a public client's secret = %v, want ErrPublicClientSecret", err)
			}
		})
	}
}

// The database is the second line, and it must actually hold.
func TestTheDatabaseRefusesAPublicClientSecret(t *testing.T) {
	f := setup(t)

	_, hash, _ := Generate()
	err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO applications (org_id, project_id, name, type, client_secret_hash)
			VALUES ($1, $2, 'sneaky', 'spa', $3)`, f.orgID, f.project, hash)
		return err
	})
	if err == nil {
		t.Error("the database accepted a secret on an spa client; the CHECK constraint is not holding")
	}
}

// --- rotation through the database ------------------------------------------

func TestRotationThroughTheStore(t *testing.T) {
	f := setup(t)
	now := time.Now()

	var rec Record
	var first Secret
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rec, first, err = f.store.Create(context.Background(), tx, f.webApp("billing"), "")
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var second Secret
	var expires time.Time
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		second, expires, err = f.store.RotateSecret(
			context.Background(), tx, rec.ID, time.Hour, now, "")
		return err
	}); err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}

	auth := func(secret string, at time.Time) error {
		return f.tx(t, func(tx *postgres.Tx) error {
			_, err := f.store.Authenticate(context.Background(), tx, rec.ID, secret, at)
			return err
		})
	}

	if err := auth(second.Reveal(), now); err != nil {
		t.Errorf("the new secret does not authenticate: %v", err)
	}
	if err := auth(first.Reveal(), now.Add(30*time.Minute)); err != nil {
		t.Errorf("the previous secret does not authenticate during its overlap: %v", err)
	}
	if err := auth(first.Reveal(), expires.Add(time.Second)); err == nil {
		t.Error("the previous secret still authenticates after the overlap expired")
	}
	if err := auth(second.Reveal(), expires.Add(time.Hour)); err != nil {
		t.Errorf("the new secret stopped working after the overlap: %v", err)
	}
}

// --- tenant isolation --------------------------------------------------------

// Abuse case A-11. Not-found rather than forbidden, because a "forbidden"
// confirms the row exists.
func TestApplicationsAreNotReadableAcrossTenants(t *testing.T) {
	f := setup(t)

	// A second tenant in the same instance, with its own application.
	var otherInstance string
	f.factory.QueryRow(&otherInstance, `SELECT instance_id FROM organizations WHERE id = $1`, f.orgID)
	otherOrg := f.factory.Organization(otherInstance)

	var otherProject string
	f.factory.QueryRow(&otherProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, otherOrg, "theirs")

	var theirs Record
	if err := f.db.WithTenant(context.Background(), otherOrg, func(tx *postgres.Tx) error {
		app := Application{
			OrgID: otherOrg, ProjectID: otherProject, Name: "theirs", Type: TypeWeb,
			GrantTypes: []string{GrantAuthorizationCode}, RedirectURIs: []string{"https://theirs.example/cb"},
		}
		var err error
		theirs, _, err = f.store.Create(context.Background(), tx, app, "")
		return err
	}); err != nil {
		t.Fatalf("creating the other tenant's application: %v", err)
	}

	// Our scope cannot see it.
	err := f.tx(t, func(tx *postgres.Tx) error {
		_, err := f.store.Get(context.Background(), tx, theirs.ID)
		return err
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("reading another tenant's application = %v, want ErrNotFound", err)
	}

	// The control: it IS readable in its own scope, so the failure above was
	// isolation rather than a missing row or a broken query.
	if err := f.db.WithTenant(context.Background(), otherOrg, func(tx *postgres.Tx) error {
		_, err := f.store.Get(context.Background(), tx, theirs.ID)
		return err
	}); err != nil {
		t.Fatalf("the other tenant cannot read its own application: %v", err)
	}
}

// --- audit -------------------------------------------------------------------

// P1-05 DoD item 5.
func TestLifecycleEventsAreAudited(t *testing.T) {
	f := setup(t)

	var rec Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rec, _, err = f.store.Create(context.Background(), tx, f.webApp("billing"), "")
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		app := f.webApp("billing")
		app.RedirectURIs = []string{"https://app.example.com/cb", "https://app.example.com/cb2"}
		_, err := f.store.Update(context.Background(), tx, rec.ID, app, "")
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, _, err := f.store.RotateSecret(context.Background(), tx, rec.ID, time.Hour, time.Now(), "")
		return err
	}); err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		return f.store.Delete(context.Background(), tx, rec.ID, "")
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var types string
	f.factory.QueryRow(&types,
		`SELECT string_agg(event_type, ',' ORDER BY id) FROM events WHERE org_id = $1`, f.orgID)

	for _, want := range []string{
		"application.created", "application.updated",
		"application.secret_rotated", "application.deleted",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("no %s event was written; got: %s", want, types)
		}
	}
}

// A widened redirect URI must be visible in the audit log WITH its value.
// "redirect_uris changed" cannot answer the question the entry exists for.
func TestAWidenedRedirectURIIsVisibleInTheAuditLog(t *testing.T) {
	f := setup(t)

	var rec Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rec, _, err = f.store.Create(context.Background(), tx, f.webApp("billing"), "")
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		app := f.webApp("billing")
		app.RedirectURIs = []string{"https://app.example.com/cb", "https://attacker.example/cb"}
		_, err := f.store.Update(context.Background(), tx, rec.ID, app, "")
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var payload string
	f.factory.QueryRow(&payload,
		`SELECT payload::text FROM events WHERE org_id = $1 AND event_type = 'application.updated'`,
		f.orgID)

	if !strings.Contains(payload, "attacker.example") {
		t.Errorf("the added redirect URI is not in the audit payload: %s", payload)
	}
	if !strings.Contains(payload, "redirect_uris_before") {
		t.Errorf("the audit payload does not record what the URIs were before: %s", payload)
	}
}

// --- canonicalisation on the way in ------------------------------------------

// Registration stores the canonical form, which is what makes the exact
// comparison at authorization time predictable.
func TestRedirectURIsAreStoredCanonically(t *testing.T) {
	f := setup(t)

	app := f.webApp("billing")
	app.RedirectURIs = []string{"HTTPS://App.Example.COM/CB"}

	var rec Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		rec, _, err = f.store.Create(context.Background(), tx, app, "")
		return err
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	want := "https://app.example.com/CB"
	if len(rec.RedirectURIs) != 1 || rec.RedirectURIs[0] != want {
		t.Fatalf("stored redirect URIs = %v, want [%s]", rec.RedirectURIs, want)
	}
	if !rec.MatchesRedirectURI(want) {
		t.Error("the canonical form does not match itself")
	}
	if rec.MatchesRedirectURI("HTTPS://App.Example.COM/CB") {
		t.Error("the pre-canonical form still matches; comparison is not exact against the stored value")
	}
}

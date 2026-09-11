//go:build integration

package client

import (
	"context"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Allowed origins, against a real database (P1-29, closing PG-17).
//
// The unit tests cover the validator and the matcher. What only a database can
// answer is whether the column, the constraint, the two lookups and the
// application role's grants actually line up — and `PG-24` is the standing
// reminder that a migration reading correctly is not the same as a schema
// being correct.

func TestOriginsSurviveARoundTrip(t *testing.T) {
	f := setup(t)

	app := f.webApp("with-origins")
	app.AllowedOrigins = []string{"HTTPS://App.Example.COM", "http://localhost:5173"}

	var created Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		created, _, err = f.store.Create(context.Background(), tx, app, "")
		return err
	}); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// Canonicalised on the way in, so the exact comparison at request time has
	// a predictable target. This is the same discipline redirect URIs live by.
	want := []string{"https://app.example.com", "http://localhost:5173"}
	if len(created.AllowedOrigins) != len(want) {
		t.Fatalf("stored %v, want %v", created.AllowedOrigins, want)
	}
	for i := range want {
		if created.AllowedOrigins[i] != want[i] {
			t.Errorf("stored[%d] = %q, want %q", i, created.AllowedOrigins[i], want[i])
		}
	}
}

// The default state of every application that existed before this column did,
// and of every one created without mentioning it.
func TestAnApplicationWithNoOriginsReadsAsAnEmptyListNotNull(t *testing.T) {
	f := setup(t)

	var created Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		created, _, err = f.store.Create(context.Background(), tx, f.webApp("no-origins"), "")
		return err
	}); err != nil {
		t.Fatalf("creating: %v", err)
	}

	if created.AllowedOrigins == nil {
		t.Error("allowed_origins reads as nil; a consumer then has to handle two empties")
	}
	if len(created.AllowedOrigins) != 0 {
		t.Errorf("an application registered no origins and has %v", created.AllowedOrigins)
	}
}

// The bootstrap lookup is what the CORS middleware resolves an application
// through, and it runs before a tenant is known. If the migration that added
// the column to its signature did not reach this database, this is where it
// shows — rather than as "CORS does not work" against a service that starts
// cleanly and reports healthy.
func TestTheBootstrapLookupCarriesTheOrigins(t *testing.T) {
	f := setup(t)

	app := f.webApp("bootstrap-origins")
	app.AllowedOrigins = []string{"https://app.example.com"}

	var created Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		created, _, err = f.store.Create(context.Background(), tx, app, "")
		return err
	}); err != nil {
		t.Fatalf("creating: %v", err)
	}

	found, err := f.store.ByClientID(context.Background(), f.db, created.ID)
	if err != nil {
		t.Fatalf("resolving the client_id: %v", err)
	}

	if !found.MatchesOrigin("https://app.example.com") {
		t.Errorf("the bootstrap lookup lost the origins: %v", found.AllowedOrigins)
	}
	if found.MatchesOrigin("https://evil.test") {
		t.Error("the bootstrap lookup matches an origin nobody registered")
	}
}

// The preflight lookup. A preflight carries no credential, so this is the only
// question that can be asked at that moment — and it must still be a question
// with a false answer.
func TestOnlyARegisteredOriginPassesThePreflightLookup(t *testing.T) {
	f := setup(t)

	app := f.webApp("preflight")
	app.AllowedOrigins = []string{"https://app.example.com"}

	if err := f.tx(t, func(tx *postgres.Tx) error {
		_, _, err := f.store.Create(context.Background(), tx, app, "")
		return err
	}); err != nil {
		t.Fatalf("creating: %v", err)
	}

	for _, given := range []struct {
		origin string
		want   bool
	}{
		{"https://app.example.com", true},
		{"https://evil.test", false},
		{"", false},
		// Exactness, at the database layer as well as in Go. A prefix match
		// here would be the open-redirect class of bug wearing a CORS hat.
		{"https://app.example.com.evil.test", false},
		{"https://app.example.com/", false},
	} {
		got, err := f.store.OriginIsRegistered(context.Background(), f.db, given.origin)
		if err != nil {
			t.Fatalf("checking %q: %v", given.origin, err)
		}
		if got != given.want {
			t.Errorf("OriginIsRegistered(%q) = %v, want %v", given.origin, got, given.want)
		}
	}
}

// An update may clear every origin, and that has to reach the row.
//
// Revoking access in a hurry is the one operation on this column that somebody
// will perform under pressure, and "the API accepted it" is not the same as
// "the database has it".
func TestClearingTheOriginsRevokesThemInTheRow(t *testing.T) {
	f := setup(t)

	app := f.webApp("revocable")
	app.AllowedOrigins = []string{"https://app.example.com"}

	var created, updated Record
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		created, _, err = f.store.Create(context.Background(), tx, app, "")
		return err
	}); err != nil {
		t.Fatalf("creating: %v", err)
	}

	cleared := f.webApp("revocable")
	cleared.AllowedOrigins = []string{}
	if err := f.tx(t, func(tx *postgres.Tx) error {
		var err error
		updated, err = f.store.Update(context.Background(), tx, created.ID, cleared, "")
		return err
	}); err != nil {
		t.Fatalf("updating: %v", err)
	}

	if len(updated.AllowedOrigins) != 0 {
		t.Errorf("the origins survived being cleared: %v", updated.AllowedOrigins)
	}

	// And the lookup the middleware uses agrees, which is the one that matters.
	registered, err := f.store.OriginIsRegistered(context.Background(), f.db, "https://app.example.com")
	if err != nil {
		t.Fatalf("checking: %v", err)
	}
	if registered {
		t.Error("a cleared origin still passes the preflight lookup")
	}
}

// The database refuses what the validator would never produce.
//
// Not redundant with the Go validation: seeding scripts write to this table
// directly and `PG-26` says they will keep having to, so the column needs a
// floor that does not depend on going through the API.
func TestTheDatabaseRefusesAnOriginShapeTheValidatorWouldNeverProduce(t *testing.T) {
	f := setup(t)

	for _, bad := range []string{
		"https://app.example.com/callback",
		"https://*.example.com",
		"not-an-origin",
		"",
	} {
		_, err := f.db.SQL().ExecContext(context.Background(),
			`INSERT INTO applications (org_id, project_id, name, type, redirect_uris, grant_types, allowed_origins)
			 VALUES ($1, $2, $3, 'web', ARRAY['https://app.example.com/cb'], ARRAY['authorization_code'], ARRAY[$4])`,
			f.orgID, f.project, "direct-"+bad, bad)
		if err == nil {
			t.Errorf("the database accepted %q as an origin", bad)
		}
	}
}

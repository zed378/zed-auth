//go:build integration

package maintenance

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

// Why these tests exist at all (BL-08).
//
// Five tables had cleanup code that could not work or could not be reached, and
// every one of them had a passing test. The reason is that "did it error?" is
// not the question. Under instance scope `current_org_id()` is NULL, every
// row-level security policy evaluates false, and a DELETE reports success having
// removed nothing — so a test asserting the absence of an error passes on code
// that does nothing at all.
//
// So every test below asserts a COUNT, and one of them deliberately runs the
// broken version to prove the difference is real.

// stack opens an app-role connection, which is the one the service uses and
// therefore the one row-level security applies to. The owner connection would
// bypass RLS and make every assertion here meaningless.
func stack(t *testing.T) (*postgres.DB, *testsupport.Factory) {
	t.Helper()

	s := testsupport.Start(t)
	testsupport.Truncate(t, s)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: s.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db, testsupport.NewFactory(t, s)
}

// seedExpiredAssertion writes one assertion id that expired an hour ago.
func seedExpiredAssertion(t *testing.T, f *testsupport.Factory, orgID, id string) {
	t.Helper()
	f.Exec(`INSERT INTO saml_assertion_ids (assertion_id, org_id, issued_at, expires_at)
	        VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')`, id, orgID)
}

// A bare DELETE under instance scope removes nothing, and reports success.
//
// This is the test the five broken sweepers needed and did not have. It asserts
// the failure mode directly, so the SECURITY DEFINER functions the janitor calls
// are demonstrably necessary rather than a precaution somebody took on faith.
func TestABareDeleteUnderInstanceScopeRemovesNothingAndSaysNothing(t *testing.T) {
	db, f := stack(t)
	instance := f.Instance()
	orgID := f.Organization(instance)

	seedExpiredAssertion(t, f, orgID, "_bare-delete-probe")

	var affected int64
	err := db.WithInstanceScope(context.Background(), "proving a bare DELETE is blind",
		func(tx *postgres.Tx) error {
			result, err := tx.Exec(context.Background(),
				`DELETE FROM saml_assertion_ids WHERE expires_at < now()`)
			if err != nil {
				return err
			}
			affected, err = result.RowsAffected()
			return err
		})

	// No error. That is the whole problem: the statement succeeded.
	if err != nil {
		t.Fatalf("the bare DELETE errored, which is not the failure being demonstrated: %v", err)
	}
	if affected != 0 {
		t.Fatalf("the bare DELETE removed %d row(s) — row-level security is not blind here, "+
			"so the SECURITY DEFINER functions are not needed for the reason claimed", affected)
	}

	// And the row is still there, so the assertion above is not passing because
	// there was nothing to delete.
	var left int
	f.QueryRow(&left, `SELECT count(*) FROM saml_assertion_ids WHERE assertion_id = '_bare-delete-probe'`)
	if left != 1 {
		t.Fatalf("%d row(s) left, want 1 — the seed did not land and this test proved nothing", left)
	}
}

// The janitor removes what expired and keeps what has not.
func TestTheJanitorRemovesExpiredRowsAndKeepsLiveOnes(t *testing.T) {
	db, f := stack(t)
	instance := f.Instance()
	orgID := f.Organization(instance)

	seedExpiredAssertion(t, f, orgID, "_expired")
	f.Exec(`INSERT INTO saml_assertion_ids (assertion_id, org_id, issued_at, expires_at)
	        VALUES ('_live', $1, now(), now() + interval '1 hour')`, orgID)

	removed := sweepAssertions(t, db, time.Now())
	if removed != 1 {
		t.Fatalf("removed %d, want 1", removed)
	}

	// The unexpired row is still there, and it is the only one.
	var left, liveLeft int
	f.QueryRow(&left, `SELECT count(*) FROM saml_assertion_ids`)
	f.QueryRow(&liveLeft, `SELECT count(*) FROM saml_assertion_ids WHERE assertion_id = '_live'`)
	if left != 1 || liveLeft != 1 {
		t.Errorf("%d row(s) left of which %d is _live, want exactly the one unexpired row", left, liveLeft)
	}
}

// The batch bound is real, and the next pass finishes the job.
//
// A janitor that woke up to a month of accumulation and took it all in one
// transaction would hold a lock worth noticing on the login path. The bound is
// what makes catching up boring.
func TestTheJanitorIsBoundedPerPassAndCatchesUpOnTheNext(t *testing.T) {
	db, f := stack(t)
	instance := f.Instance()
	orgID := f.Organization(instance)

	for i := 0; i < 5; i++ {
		seedExpiredAssertion(t, f, orgID, "_batch-"+string(rune('a'+i)))
	}

	now := time.Now()

	first := sweepAssertionsWithLimit(t, db, now, 2)
	if first != 2 {
		t.Fatalf("first pass removed %d, want exactly the limit of 2", first)
	}

	second := sweepAssertionsWithLimit(t, db, now, 2)
	if second != 2 {
		t.Fatalf("second pass removed %d, want 2", second)
	}

	third := sweepAssertionsWithLimit(t, db, now, 2)
	if third != 1 {
		t.Fatalf("third pass removed %d, want the remaining 1", third)
	}

	var left int
	f.QueryRow(&left, `SELECT count(*) FROM saml_assertion_ids`)
	if left != 0 {
		t.Errorf("%d row(s) left after catching up, want 0", left)
	}
}

// --- helpers -----------------------------------------------------------------

func sweepAssertionsRaw(
	ctx context.Context, db *postgres.DB, before time.Time, limit int,
) (int64, error) {
	var removed int64
	err := db.WithInstanceScope(ctx, "sweeping assertion ids in a test",
		func(tx *postgres.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT sweep_saml_assertion_ids($1, $2)`, before, limit).Scan(&removed)
		})
	return removed, err
}

func sweepAssertions(t *testing.T, db *postgres.DB, now time.Time) int64 {
	t.Helper()
	return sweepAssertionsWithLimit(t, db, now, Batch)
}

func sweepAssertionsWithLimit(t *testing.T, db *postgres.DB, now time.Time, limit int) int64 {
	t.Helper()
	removed, err := sweepAssertionsRaw(context.Background(), db, now, limit)
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	return removed
}

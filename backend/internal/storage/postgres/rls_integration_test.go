//go:build integration

// Row-level security tests (P0-08).
//
// These are the tests that matter most in this package. PLAN/08 Part B wants
// cross-tenant isolation to hold "even when the application layer forgets to
// filter", so every test here deliberately issues an UNFILTERED query — no
// WHERE org_id — and asserts the database returns only the current tenant's
// rows. A test that filtered correctly would prove nothing about RLS.
package postgres

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/observability"
)

func osGetenv(k string) string { return os.Getenv(k) }

func testDB(t *testing.T) *DB {
	t.Helper()

	dsn := envOr("AUTH_TEST_APP_DSN", defaultAppDSN)
	log := observability.NewLogger(io.Discard, observability.Options{Level: "error", Format: "json"})

	db, err := Open(context.Background(), config.PostgresConfig{
		DSN:          dsn,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	}, log)
	if err != nil {
		t.Fatalf("PostgreSQL unreachable despite the test stack being up: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func envOr(key, fallback string) string {
	if v := osGetenv(key); v != "" {
		return v
	}
	return fallback
}

// seedTwoOrgs creates two organizations with a user each, using the owner
// connection (which is not subject to RLS), and returns their ids.
func seedTwoOrgs(t *testing.T) (orgA, orgB string) {
	t.Helper()
	owner := ownerDB(t)

	var instanceID string
	if err := owner.QueryRow(
		`INSERT INTO instances (name) VALUES ('rls-test') RETURNING id`).Scan(&instanceID); err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	mk := func(name, email string) string {
		var id string
		if err := owner.QueryRow(
			`INSERT INTO organizations (instance_id, name) VALUES ($1, $2) RETURNING id`,
			instanceID, name).Scan(&id); err != nil {
			t.Fatalf("seed organization %s: %v", name, err)
		}
		if _, err := owner.Exec(
			`INSERT INTO users (org_id, email, status) VALUES ($1, $2, 'active')`, id, email); err != nil {
			t.Fatalf("seed user for %s: %v", name, err)
		}
		if _, err := owner.Exec(
			`INSERT INTO projects (org_id, name) VALUES ($1, $2)`, id, name+"-project"); err != nil {
			t.Fatalf("seed project for %s: %v", name, err)
		}
		return id
	}

	orgA = mk("rls-org-a", "alice@org-a.example.com")
	orgB = mk("rls-org-b", "bob@org-b.example.com")

	t.Cleanup(func() {
		for _, id := range []string{orgA, orgB} {
			owner.Exec(`DELETE FROM projects WHERE org_id = $1`, id)
			owner.Exec(`DELETE FROM users WHERE org_id = $1`, id)
			owner.Exec(`DELETE FROM organizations WHERE id = $1`, id)
		}
		owner.Exec(`DELETE FROM instances WHERE id = $1`, instanceID)
	})

	return orgA, orgB
}

// The core guarantee. An unfiltered SELECT must still return only one tenant's
// rows — that is the whole point of putting isolation in the database.
func TestUnfilteredQueryReturnsOnlyTheCurrentTenant(t *testing.T) {
	db := testDB(t)
	orgA, orgB := seedTwoOrgs(t)

	countUsers := func(orgID string) int {
		var n int
		err := db.WithTenant(context.Background(), orgID, func(tx *Tx) error {
			// Deliberately no WHERE clause. This is the query a developer
			// writes when they forget, and RLS is what makes forgetting safe.
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n)
		})
		if err != nil {
			t.Fatalf("count users for %s: %v", orgID, err)
		}
		return n
	}

	if got := countUsers(orgA); got != 1 {
		t.Errorf("org A sees %d users, want exactly its own 1", got)
	}
	if got := countUsers(orgB); got != 1 {
		t.Errorf("org B sees %d users, want exactly its own 1", got)
	}

	// And the emails must actually differ, not merely the counts.
	email := func(orgID string) string {
		var e string
		if err := db.WithTenant(context.Background(), orgID, func(tx *Tx) error {
			return tx.QueryRow(context.Background(), `SELECT email FROM users`).Scan(&e)
		}); err != nil {
			t.Fatalf("read email for %s: %v", orgID, err)
		}
		return e
	}

	if a, b := email(orgA), email(orgB); a == b {
		t.Errorf("both tenants see the same user %q — isolation is not working", a)
	}
}

// Isolation must hold across every tenant-scoped table, not only the one
// someone remembered to test.
func TestIsolationAppliesToEveryTenantScopedTable(t *testing.T) {
	db := testDB(t)
	orgA, _ := seedTwoOrgs(t)

	for _, table := range []string{"users", "projects", "organizations"} {
		t.Run(table, func(t *testing.T) {
			var total int
			err := db.WithTenant(context.Background(), orgA, func(tx *Tx) error {
				//nolint:gosec // table name is from a fixed literal list, not input
				return tx.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&total)
			})
			if err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			// org A has exactly one of each; org B's rows must be invisible.
			if total != 1 {
				t.Errorf("%s: tenant sees %d rows, want 1 — rows from another organization are visible", table, total)
			}
		})
	}
}

// Fail closed. A query with no tenant context must return nothing, not
// everything — the difference between a bug and a breach.
func TestWithoutTenantContextNoRowsAreVisible(t *testing.T) {
	db := testDB(t)
	seedTwoOrgs(t)

	var n int
	err := db.WithInstanceScope(context.Background(), "test: verify fail-closed behaviour", func(tx *Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n)
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if n != 0 {
		t.Errorf("with no tenant context %d users are visible, want 0. "+
			"A missing context must fail closed: returning every row would turn a forgotten "+
			"SET into a cross-tenant breach", n)
	}
}

// Writes are scoped too. Inserting a row for another tenant must be refused by
// the policy's WITH CHECK, not merely filtered out of later reads.
func TestCannotWriteIntoAnotherTenant(t *testing.T) {
	db := testDB(t)
	orgA, orgB := seedTwoOrgs(t)

	err := db.WithTenant(context.Background(), orgA, func(tx *Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO users (org_id, email, status) VALUES ($1, $2, 'active')`,
			orgB, "smuggled@org-b.example.com")
		return err
	})

	if err == nil {
		t.Fatal("a tenant was able to insert a row belonging to another organization")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "policy") {
		t.Errorf("expected a row-level security policy violation, got: %v", err)
	}
}

// Updating another tenant's row must be impossible even when its id is known —
// this is the IDOR case from SECURITY/02 §2, where an attacker guesses or
// leaks an identifier.
func TestCannotUpdateAnotherTenantsRowEvenWithItsID(t *testing.T) {
	db := testDB(t)
	owner := ownerDB(t)
	orgA, orgB := seedTwoOrgs(t)

	var victimID string
	if err := owner.QueryRow(`SELECT id FROM users WHERE org_id = $1`, orgB).Scan(&victimID); err != nil {
		t.Fatalf("read org B user id: %v", err)
	}

	var affected int64
	err := db.WithTenant(context.Background(), orgA, func(tx *Tx) error {
		res, err := tx.Exec(context.Background(),
			`UPDATE users SET status = 'deactivated' WHERE id = $1`, victimID)
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if affected != 0 {
		t.Errorf("org A modified %d row(s) belonging to org B by id", affected)
	}

	var status string
	if err := owner.QueryRow(`SELECT status FROM users WHERE id = $1`, victimID).Scan(&status); err != nil {
		t.Fatalf("re-read victim: %v", err)
	}
	if status != "active" {
		t.Errorf("victim status is %q, want unchanged 'active'", status)
	}
}

// A Project Grant is the one row two tenants legitimately share — the granting
// organization manages it, the receiving one needs to see which roles it may
// assign (PLAN/08 Part C).
func TestProjectGrantIsVisibleToBothSidesButWritableOnlyByTheGranter(t *testing.T) {
	db := testDB(t)
	owner := ownerDB(t)
	orgA, orgB := seedTwoOrgs(t)

	var projectID string
	if err := owner.QueryRow(`SELECT id FROM projects WHERE org_id = $1`, orgA).Scan(&projectID); err != nil {
		t.Fatalf("read org A project: %v", err)
	}

	var grantID string
	if err := owner.QueryRow(`
		INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
		VALUES ($1, $2, $3, '{viewer}') RETURNING id`, projectID, orgA, orgB).Scan(&grantID); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	t.Cleanup(func() { owner.Exec(`DELETE FROM project_grants WHERE id = $1`, grantID) })

	visible := func(orgID string) int {
		var n int
		if err := db.WithTenant(context.Background(), orgID, func(tx *Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM project_grants`).Scan(&n)
		}); err != nil {
			t.Fatalf("count grants for %s: %v", orgID, err)
		}
		return n
	}

	if visible(orgA) != 1 {
		t.Error("the granting organization cannot see its own grant")
	}
	if visible(orgB) != 1 {
		t.Error("the receiving organization cannot see the grant delegated to it")
	}

	// The receiving side must not be able to create a grant naming itself as
	// the granter — that is how an organization would widen its own delegation
	// (PLAN/09 § Delegation abuse).
	err := db.WithTenant(context.Background(), orgB, func(tx *Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
			VALUES ($1, $2, $3, '{admin}')`, projectID, orgB, orgB)
		return err
	})
	if err == nil {
		t.Error("the receiving organization was able to create a grant — it could widen its own delegation")
	}
}

// The audit log carries who did what across every tenant, so a leak here is
// worse than a leak from any single table.
func TestAuditLogIsTenantIsolated(t *testing.T) {
	db := testDB(t)
	orgA, orgB := seedTwoOrgs(t)
	ctx := context.Background()

	for _, org := range []string{orgA, orgB} {
		if err := db.WithTenant(ctx, org, func(tx *Tx) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO events (org_id, event_type, payload) VALUES ($1, 'user.login.success', '{}'::jsonb)`,
				org)
			return err
		}); err != nil {
			t.Fatalf("write event for %s: %v", org, err)
		}
	}
	t.Cleanup(func() {
		o := ownerDB(t)
		o.Exec(`DELETE FROM events WHERE org_id = ANY($1)`, "{"+orgA+","+orgB+"}")
	})

	var n int
	if err := db.WithTenant(ctx, orgA, func(tx *Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&n)
	}); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 1 {
		t.Errorf("org A sees %d audit events, want only its own 1", n)
	}
}

// SET LOCAL is transaction-scoped. If a plain SET were used, one request's
// tenant would persist on the pooled connection and leak into whichever
// request reused it — intermittently, under concurrency, in production.
func TestTenantContextDoesNotLeakBetweenTransactions(t *testing.T) {
	db := testDB(t)
	orgA, _ := seedTwoOrgs(t)
	ctx := context.Background()

	if err := db.WithTenant(ctx, orgA, func(tx *Tx) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	}); err != nil {
		t.Fatalf("first transaction: %v", err)
	}

	// A later transaction with no tenant must see nothing, even if it happens
	// to reuse the same pooled connection.
	for i := 0; i < 10; i++ {
		var n int
		if err := db.WithInstanceScope(ctx, "test: confirm no context leak", func(tx *Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
		}); err != nil {
			t.Fatalf("instance-scoped read: %v", err)
		}
		if n != 0 {
			t.Fatalf("iteration %d: %d users visible without a tenant — the previous transaction's context leaked", i, n)
		}
	}
}

// A failing callback must roll back rather than leaving a partial write.
func TestErrorRollsBack(t *testing.T) {
	db := testDB(t)
	orgA, _ := seedTwoOrgs(t)
	ctx := context.Background()

	sentinel := errors.New("deliberate failure")

	err := db.WithTenant(ctx, orgA, func(tx *Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (org_id, email, status) VALUES ($1, 'rollback@example.com', 'active')`,
			orgA); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want the callback's error back, got %v", err)
	}

	var n int
	if err := db.WithTenant(ctx, orgA, func(tx *Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'rollback@example.com'`).Scan(&n)
	}); err != nil {
		t.Fatalf("verify rollback: %v", err)
	}
	if n != 0 {
		t.Error("the insert survived a returned error — the transaction did not roll back")
	}
}

func TestWithTenantRejectsAnEmptyOrgID(t *testing.T) {
	db := testDB(t)

	err := db.WithTenant(context.Background(), "", func(*Tx) error { return nil })
	if !errors.Is(err, ErrEmptyOrgID) {
		t.Errorf("want ErrEmptyOrgID, got %v", err)
	}
}

// The instance-scoped path must be uncomfortable to use by accident.
func TestInstanceScopeRequiresAReason(t *testing.T) {
	db := testDB(t)

	if err := db.WithInstanceScope(context.Background(), "", func(*Tx) error { return nil }); err == nil {
		t.Error("instance-scoped access without a reason was allowed")
	}
}

// The startup assertion is what catches AUTH_POSTGRES_DSN being pointed at the
// owner — plausible while debugging, and it silently disables every policy.
func TestAssertRoleIsNotPrivileged(t *testing.T) {
	t.Run("application role passes", func(t *testing.T) {
		db := testDB(t)
		if err := db.AssertRoleIsNotPrivileged(context.Background()); err != nil {
			t.Errorf("the application role should pass: %v", err)
		}
	})

	t.Run("owner role is refused", func(t *testing.T) {
		log := observability.NewLogger(io.Discard, observability.Options{Level: "error", Format: "json"})
		db, err := Open(context.Background(), config.PostgresConfig{
			DSN:          envOr("AUTH_TEST_OWNER_DSN", defaultOwnerDSN),
			MaxOpenConns: 2,
			MaxIdleConns: 1,
		}, log)
		if err != nil {
			t.Fatalf("owner connection unavailable despite the test stack being up: %v", err)
		}
		defer db.Close()

		err = db.AssertRoleIsNotPrivileged(context.Background())
		if err == nil {
			t.Fatal("the owner role was accepted — pointing the service at it would silently disable RLS")
		}
		if !strings.Contains(err.Error(), "superuser") && !strings.Contains(err.Error(), "owns") {
			t.Errorf("the error should explain why this is dangerous, got: %v", err)
		}
	})
}

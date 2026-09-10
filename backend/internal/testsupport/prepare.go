package testsupport

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/migrations"
)

// prepare turns a bare PostgreSQL container into the database the service
// expects: two roles, then every migration.
//
// The role split is not a detail to skip in tests. `docs/PLAN/08` Part B and
// `P0-08` put cross-tenant isolation in the database rather than in the code
// that queries it, and that only holds because the runtime role cannot bypass
// row-level security. A test suite connecting as the owner would find every
// isolation test passing and prove nothing — the exact vacuous pass this
// project hit when `auth_app` did not exist and the check for
// "auth_app is not superuser" was therefore trivially satisfied.
func prepare(ctx context.Context, stack *Stack) error {
	db, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		return fmt.Errorf("open owner connection: %w", err)
	}
	defer db.Close()

	// The ORDER here mirrors production and is load-bearing.
	//
	// deploy/postgres/init/01-roles.sh sets ALTER DEFAULT PRIVILEGES *before*
	// any migration runs, so every table a migration creates picks up DML for
	// auth_app automatically — and the revokes those migrations then perform
	// survive, because nothing grants over them afterwards.
	//
	// The first version of this file granted SELECT/INSERT/UPDATE/DELETE on
	// ALL TABLES after migrating. That re-granted UPDATE and DELETE on the
	// events partitions that migration 000006 had carefully revoked, and broke
	// the append-only guarantee inside the test harness — so a suite that
	// looked like it was testing production was testing a database with a
	// weaker privilege model than production has. TestNewPartitionsAreAppendOnly
	// caught it, which it could only do because the skip that used to hide it
	// was removed in the same change.
	if err := createAppRole(ctx, db); err != nil {
		return err
	}

	if err := applyMigrations(stack.OwnerDSN); err != nil {
		return err
	}

	return nil
}

// createAppRole creates the runtime role with exactly the privileges the
// service has in production, and no others.
func createAppRole(ctx context.Context, db *sql.DB) error {
	// NOSUPERUSER and NOBYPASSRLS are the two that matter. Without them the
	// role can read every tenant's rows and every isolation test becomes a
	// statement about nothing.
	stmts := []string{
		`DO $$
		 BEGIN
		   IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'auth_app') THEN
		     CREATE ROLE auth_app LOGIN PASSWORD '` + testPassword + `'
		       NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
		   END IF;
		 END $$;`,
		`GRANT CONNECT ON DATABASE auth TO auth_app`,
		`GRANT USAGE ON SCHEMA public TO auth_app`,

		// Default privileges, exactly as deploy/postgres/init/01-roles.sh sets
		// them. Applied BEFORE the migrations, so tables they create are
		// covered and the revokes they perform are not undone afterwards.
		`ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner IN SCHEMA public
		   GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO auth_app`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner IN SCHEMA public
		   GRANT USAGE, SELECT ON SEQUENCES TO auth_app`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("creating auth_app: %w", err)
		}
	}
	return nil
}

// applyMigrations runs the same embedded migrations the binary carries.
//
// The same `embed.FS` the service uses (ADR-007), not a copy read from disk:
// a test that migrates from a different source is testing a schema nothing
// will ever run.
func applyMigrations(ownerDSN string) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}

	db, err := sql.Open("pgx", ownerDSN)
	if err != nil {
		return fmt.Errorf("open for migrations: %w", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("applying migrations: %w", err)
	}

	return nil
}

// Truncate empties every table between tests, leaving the schema in place.
//
// This is how tests stay isolated without paying for a container each. `P0-15`
// requires the suite to pass "repeatedly and in random order", which means no
// test may see another's rows — and `go test -shuffle=on` in CI makes an
// accidental ordering dependency fail rather than lurk.
//
// TRUNCATE rather than DELETE: it resets sequences and does not leave dead
// tuples behind for a suite that runs hundreds of times.
func Truncate(t *testing.T, stack *Stack) {
	t.Helper()

	db, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("truncate: open: %v", err)
	}
	defer db.Close()

	// Every table except the migration bookkeeping, which must survive or the
	// next test run would try to migrate an already-migrated schema.
	const stmt = `
		DO $$
		DECLARE
		  tables text;
		BEGIN
		  SELECT string_agg(format('%I.%I', schemaname, tablename), ', ')
		  INTO tables
		  FROM pg_tables
		  WHERE schemaname = 'public'
		    AND tablename <> 'schema_migrations'
		    AND tablename NOT LIKE 'events_%';

		  IF tables IS NOT NULL THEN
		    EXECUTE 'TRUNCATE TABLE ' || tables || ' RESTART IDENTITY CASCADE';
		  END IF;
		END $$;`

	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

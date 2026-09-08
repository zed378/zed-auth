// Command migrate applies and rolls back database migrations.
//
// It is a separate binary from the service on purpose. PLAN/14-DEPLOYMENT.md
// requires migrations to run as a reviewable step before application rollout,
// never implicitly on service startup in production — a service that migrates
// as it boots will, during a rolling update, have several instances racing to
// alter the schema while older instances are still serving traffic against the
// old one.
//
// It connects as the schema owner (AUTH_MIGRATE_DSN), not as the application
// role. The application role is deliberately not the table owner so that
// row-level security applies to it (P0-08).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/zed378/zed-auth/backend/migrations"
)

// migrationsDir is only used by the `new` command, which writes a file the
// developer then commits. Everything else reads from the embedded FS.
const migrationsDir = "migrations"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("a command is required")
	}

	switch args[0] {
	case "new":
		if len(args) < 2 {
			return errors.New("new requires a name: migrate new add_widgets")
		}
		return newMigration(args[1])

	case "up":
		return withMigrator(func(m *migrate.Migrate) error {
			err := m.Up()
			if errors.Is(err, migrate.ErrNoChange) {
				fmt.Println("no pending migrations")
				return nil
			}
			if err != nil {
				return err
			}
			return reportVersion(m, "migrated up")
		})

	case "down":
		steps := 1
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				return fmt.Errorf("down takes a positive step count, got %q", args[1])
			}
			steps = n
		}
		return withMigrator(func(m *migrate.Migrate) error {
			// Deliberately step-limited with no "down to zero" shortcut.
			// An accidental full teardown of an environment's schema is a
			// mistake worth making hard to type.
			if err := m.Steps(-steps); err != nil {
				return err
			}
			return reportVersion(m, "rolled back")
		})

	case "status":
		return withMigrator(func(m *migrate.Migrate) error {
			return reportVersion(m, "current")
		})

	case "force":
		// Clears the dirty flag after a failed migration has been manually
		// repaired. Destructive if used carelessly: it tells the tool the
		// database is at a version without verifying that it is.
		if len(args) < 2 {
			return errors.New("force requires a version")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("force takes an integer version, got %q", args[1])
		}
		return withMigrator(func(m *migrate.Migrate) error {
			if err := m.Force(v); err != nil {
				return err
			}
			fmt.Printf("forced version to %d — verify the schema matches before deploying\n", v)
			return nil
		})

	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `
Usage: migrate <command>

  up              Apply all pending migrations
  down [n]        Roll back n migrations (default 1)
  status          Print the current schema version
  new <name>      Create an up/down migration pair
  force <v>       Clear the dirty flag at version v (after a manual repair)

Connects using AUTH_MIGRATE_DSN, which must be the schema OWNER role —
not the application role, which is deliberately non-owner so that
row-level security applies to it.

`)
}

func withMigrator(fn func(*migrate.Migrate) error) error {
	dsn := strings.TrimSpace(os.Getenv("AUTH_MIGRATE_DSN"))
	if dsn == "" {
		return errors.New("AUTH_MIGRATE_DSN is required and must be the schema owner's connection string")
	}

	// Read from the embedded FS rather than from disk: no filesystem path to
	// get wrong across operating systems, and no way to run a binary against a
	// migrations directory it was not built with.
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer func() {
		// Both errors are reported rather than swallowed: a source that fails
		// to close is usually harmless, but a database connection that does
		// not close during CI is how a test suite runs out of connections.
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			fmt.Fprintf(os.Stderr, "migrate: close: source=%v database=%v\n", srcErr, dbErr)
		}
	}()

	return fn(m)
}

func reportVersion(m *migrate.Migrate, action string) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Printf("%s: no migrations applied\n", action)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read version: %w", err)
	}

	fmt.Printf("%s: version %d\n", action, v)
	if dirty {
		// A dirty database means a migration failed partway. Continuing to
		// deploy against it will fail confusingly, so say so loudly.
		return fmt.Errorf("database is DIRTY at version %d: a migration failed partway. "+
			"Inspect the schema, repair it manually, then run: migrate force <version>", v)
	}
	return nil
}

// newMigration writes an up/down pair with a timestamp prefix.
//
// The template carries the expand/contract reminder because that discipline is
// invisible at the moment someone is writing a migration and expensive to
// discover during a rollback (PLAN/14-DEPLOYMENT.md § Rollback Strategy).
func newMigration(name string) error {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		case r == '-' || r == ' ':
			return '_'
		default:
			return -1
		}
	}, name)

	if safe == "" {
		return fmt.Errorf("migration name %q contains no usable characters", name)
	}

	version := time.Now().UTC().Format("20060102150405")

	files := map[string]string{
		fmt.Sprintf("%s_%s.up.sql", version, safe): fmt.Sprintf(
			`-- %s (up)
--
-- Expand/contract reminder (PLAN/14-DEPLOYMENT.md § Rollback Strategy):
-- this migration must leave the PREVIOUS application version able to run
-- against the new schema, so an application rollback never requires a
-- database rollback. In practice that means: add columns as nullable or with
-- a default, add tables freely, and never drop or rename in the same release
-- that stops using something.

`, safe),
		fmt.Sprintf("%s_%s.down.sql", version, safe): fmt.Sprintf(
			`-- %s (down)
--
-- Must undo the up migration exactly. An untested down migration is not a
-- rollback plan; run it locally before merging.

`, safe),
	}

	for filename, content := range files {
		path := filepath.Join(migrationsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Println("created", path)
	}
	return nil
}

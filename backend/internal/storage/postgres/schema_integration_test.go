//go:build integration

// Integration tests for the schema itself (P0-07).
//
// These run against a real PostgreSQL because the properties under test are
// database properties, not application properties: a privilege revocation, a
// composite unique index, and a partition boundary cannot be verified against
// a mock. docs/PLAN/11-TESTING.md § Integration Testing requires exactly this.
//
// Run with:
//
//	make up && make migrate-up
//	go test -tags=integration ./internal/storage/postgres/...
//
// Two connections are used, and the distinction is the point:
//
//	ownerDSN — the schema owner, used by migrations
//	appDSN   — the application role, which is NOT the owner and NOT BYPASSRLS,
//	           so row-level security applies to it (P0-08)
package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// The stack is started by the tests themselves — no database prepared in
// advance, no environment variables (P0-15).
func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

const (
	defaultOwnerDSN = "postgres://auth_owner:local_dev_only@localhost:5432/auth?sslmode=disable"
	defaultAppDSN   = "postgres://auth_app:local_dev_only@localhost:5432/auth?sslmode=disable"
)

func ownerDB(t *testing.T) *sql.DB { return openDB(t, "AUTH_TEST_OWNER_DSN", defaultOwnerDSN) }
func appDB(t *testing.T) *sql.DB   { return openDB(t, "AUTH_TEST_APP_DSN", defaultAppDSN) }

func openDB(t *testing.T, envKey, fallback string) *sql.DB {
	t.Helper()

	dsn := os.Getenv(envKey)
	if dsn == "" {
		dsn = fallback
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", envKey, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Fatalf("PostgreSQL unreachable despite the test stack being up (%s): %v", envKey, err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// seedOrg creates an organization and returns its id, cleaning up afterwards.
func seedOrg(t *testing.T, db *sql.DB, name string) string {
	t.Helper()

	var instanceID string
	err := db.QueryRow(`
		INSERT INTO instances (name) VALUES ($1)
		RETURNING id`, "test-instance-"+name).Scan(&instanceID)
	if err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	var orgID string
	err = db.QueryRow(`
		INSERT INTO organizations (instance_id, name) VALUES ($1, $2)
		RETURNING id`, instanceID, name).Scan(&orgID)
	if err != nil {
		t.Fatalf("seed organization: %v", err)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM users WHERE org_id = $1`, orgID)
		db.Exec(`DELETE FROM organizations WHERE id = $1`, orgID)
		db.Exec(`DELETE FROM instances WHERE id = $1`, instanceID)
	})

	return orgID
}

// --- P0-08: the application role must not be able to bypass RLS -------------

// If the application connected as the table owner or as a BYPASSRLS role, every
// row-level security policy would be silently disabled while every test still
// passed. docs/PLAN/08 Part B wants cross-tenant isolation to be a database
// property, and this is the precondition for that.
func TestAppRoleCannotBypassRowLevelSecurity(t *testing.T) {
	db := appDB(t)

	var isSuper, bypassRLS bool
	err := db.QueryRow(`
		SELECT rolsuper, rolbypassrls
		FROM pg_roles
		WHERE rolname = current_user`).Scan(&isSuper, &bypassRLS)
	if err != nil {
		t.Fatalf("inspect current role: %v", err)
	}

	if isSuper {
		t.Error("the application role is a superuser: row-level security would be bypassed entirely")
	}
	if bypassRLS {
		t.Error("the application role has BYPASSRLS: row-level security would be silently disabled")
	}
}

func TestAppRoleDoesNotOwnTables(t *testing.T) {
	db := appDB(t)

	rows, err := db.Query(`
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tableowner = current_user`)
	if err != nil {
		t.Fatalf("query owned tables: %v", err)
	}
	defer rows.Close()

	var owned []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		owned = append(owned, name)
	}

	if len(owned) > 0 {
		t.Errorf("the application role owns %v — an owner bypasses RLS on its own tables", owned)
	}
}

// --- P0-07: the audit log is append-only at the database level --------------

// docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §19. A convention holds until
// someone writes an UPDATE; a revoked privilege holds regardless.
func TestEventsAreAppendOnlyForTheApplicationRole(t *testing.T) {
	owner := ownerDB(t)
	app := appDB(t)

	orgID := seedOrg(t, owner, "append-only-test")

	// Row-level security (P0-08) now applies to events, so this INSERT needs a
	// tenant context — the raw connection used elsewhere in this file has none.
	// Setting it inside the transaction mirrors what the storage layer does.
	//
	// The test previously inserted without a context and passed, because RLS
	// did not exist yet. Its failure when RLS landed was the policy working.
	tx, err := app.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // rolled back or committed below

	if _, err := tx.Exec(`SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	var eventID int64
	err = tx.QueryRow(`
		INSERT INTO events (org_id, event_type, payload)
		VALUES ($1, 'user.login.success', '{"note":"seed"}'::jsonb)
		RETURNING id`, orgID).Scan(&eventID)
	if err != nil {
		t.Fatalf("the application role must be able to INSERT audit events: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	t.Cleanup(func() { owner.Exec(`DELETE FROM events WHERE id = $1`, eventID) })

	// Each sub-test sets the tenant context first. Without it RLS would refuse
	// the statement before the privilege check was ever reached, and the test
	// would pass for the wrong reason — proving isolation works rather than
	// proving the audit log is append-only.
	scoped := func(t *testing.T, fn func(*sql.Tx) error) error {
		t.Helper()
		tx, err := app.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback() //nolint:errcheck // read-only or expected to fail
		if _, err := tx.Exec(`SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
			t.Fatalf("set tenant context: %v", err)
		}
		return fn(tx)
	}

	t.Run("UPDATE is refused", func(t *testing.T) {
		err := scoped(t, func(tx *sql.Tx) error {
			_, err := tx.Exec(`UPDATE events SET event_type = 'tampered' WHERE id = $1`, eventID)
			return err
		})
		if err == nil {
			t.Fatal("the application role was able to UPDATE an audit event — the log is not append-only")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Errorf("expected a permission error, got: %v", err)
		}
	})

	t.Run("DELETE is refused", func(t *testing.T) {
		err := scoped(t, func(tx *sql.Tx) error {
			_, err := tx.Exec(`DELETE FROM events WHERE id = $1`, eventID)
			return err
		})
		if err == nil {
			t.Fatal("the application role was able to DELETE an audit event — the log is not append-only")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Errorf("expected a permission error, got: %v", err)
		}
	})

	t.Run("SELECT is still allowed", func(t *testing.T) {
		var got string
		if err := scoped(t, func(tx *sql.Tx) error {
			return tx.QueryRow(`SELECT event_type FROM events WHERE id = $1`, eventID).Scan(&got)
		}); err != nil {
			t.Fatalf("the application role must be able to read the audit log: %v", err)
		}
		if got != "user.login.success" {
			t.Errorf("event_type = %q, want the original value — it must not have been altered", got)
		}
	})
}

func TestEventsIsPartitionedByMonth(t *testing.T) {
	db := ownerDB(t)

	var partitionCount int
	err := db.QueryRow(`
		SELECT count(*)
		FROM pg_inherits i
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'events'`).Scan(&partitionCount)
	if err != nil {
		t.Fatalf("count partitions: %v", err)
	}

	// The current month and the next one are seeded by the migration. Seeding
	// ahead matters: without next month's partition, every audited action fails
	// at midnight on the 1st.
	if partitionCount < 2 {
		t.Errorf("events has %d partitions, want at least 2 (current and next month)", partitionCount)
	}

	var isPartitioned bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_partitioned_table pt
			JOIN pg_class c ON c.oid = pt.partrelid
			WHERE c.relname = 'events'
		)`).Scan(&isPartitioned)
	if err != nil {
		t.Fatalf("check partitioning: %v", err)
	}
	if !isPartitioned {
		t.Error("events is not a partitioned table — retrofitting partitioning later is far more disruptive")
	}
}

// --- P0-07: email uniqueness is per organization, not global ----------------

// docs/PLAN/04 § users. A global constraint would let the first tenant to register
// an address block every other tenant from ever inviting it — two different
// companies may legitimately employ the same person.
func TestUserEmailIsUniquePerOrganizationNotGlobally(t *testing.T) {
	db := ownerDB(t)

	orgA := seedOrg(t, db, "email-scope-a")
	orgB := seedOrg(t, db, "email-scope-b")

	const email = "shared.person@example.com"

	if _, err := db.Exec(`INSERT INTO users (org_id, email) VALUES ($1, $2)`, orgA, email); err != nil {
		t.Fatalf("first user: %v", err)
	}

	t.Run("same email in a different organization is allowed", func(t *testing.T) {
		if _, err := db.Exec(`INSERT INTO users (org_id, email) VALUES ($1, $2)`, orgB, email); err != nil {
			t.Errorf("email must be unique per org, not globally: %v", err)
		}
	})

	t.Run("same email in the same organization is refused", func(t *testing.T) {
		if _, err := db.Exec(`INSERT INTO users (org_id, email) VALUES ($1, $2)`, orgA, email); err == nil {
			t.Error("a duplicate email within one organization must be refused")
		}
	})

	t.Run("case-insensitive within an organization", func(t *testing.T) {
		if _, err := db.Exec(`INSERT INTO users (org_id, email) VALUES ($1, $2)`, orgA, strings.ToUpper(email)); err == nil {
			t.Error("email uniqueness must be case-insensitive, or the same person can register twice")
		}
	})
}

// --- P0-07: credential columns and constraints ------------------------------

// docs/PLAN/02 § Constraints is absolute: no third-party dependency holds the
// private signing key outside this service's own infrastructure. The column
// holds a secret-manager reference, and the constraint refuses the
// "simplification" that would silently violate that.
func TestSigningKeysRefuseInlinePrivateKeyMaterial(t *testing.T) {
	db := ownerDB(t)

	// Assembled at runtime so this repository contains no literal PEM block
	// anywhere — see TestIsSecretMaterial in internal/config for why that
	// matters to the pre-commit hook and to gitleaks.
	dashes := strings.Repeat("-", 5)
	pem := dashes + "BEGIN RSA PRIVATE KEY" + dashes + "\nQUJDREVG\n" + dashes + "END RSA PRIVATE KEY" + dashes

	_, err := db.Exec(`
		INSERT INTO signing_keys (kid, algorithm, public_key, private_key_ref)
		VALUES ('test-kid-inline', 'RS256', 'public-material', $1)`, pem)
	if err == nil {
		db.Exec(`DELETE FROM signing_keys WHERE kid = 'test-kid-inline'`)
		t.Fatal("PEM private key material was accepted into private_key_ref — docs/PLAN/02's constraint is violated silently")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "constraint") {
		t.Errorf("expected a check-constraint violation, got: %v", err)
	}
}

// docs/PLAN/05 Part A: a public client cannot keep a secret confidential in a
// browser or a shipped mobile binary, so holding one implies a false sense of
// security. It must use PKCE instead.
func TestPublicClientsCannotHoldASecret(t *testing.T) {
	db := ownerDB(t)

	orgID := seedOrg(t, db, "public-client-test")

	var projectID string
	if err := db.QueryRow(`
		INSERT INTO projects (org_id, name) VALUES ($1, 'test-project')
		RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM applications WHERE project_id = $1`, projectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, projectID)
	})

	for _, clientType := range []string{"spa", "native"} {
		t.Run(clientType+" with a secret is refused", func(t *testing.T) {
			_, err := db.Exec(`
				INSERT INTO applications (project_id, org_id, name, type, client_secret_hash)
				VALUES ($1, $2, $3, $4, 'argon2id$hash')`,
				projectID, orgID, "app-"+clientType, clientType)
			if err == nil {
				t.Errorf("a %s client was allowed to hold a secret", clientType)
			}
		})
	}

	t.Run("web client may hold a secret", func(t *testing.T) {
		_, err := db.Exec(`
			INSERT INTO applications (project_id, org_id, name, type, client_secret_hash)
			VALUES ($1, $2, 'app-web', 'web', 'argon2id$hash')`, projectID, orgID)
		if err != nil {
			t.Errorf("a confidential client must be able to hold a secret: %v", err)
		}
	})
}

// docs/PLAN/08 § Least Privilege: a grant with no roles grants nothing, so it should
// not exist rather than sit as an empty row that looks like access.
func TestUserGrantsRefuseEmptyRoleSets(t *testing.T) {
	db := ownerDB(t)

	orgID := seedOrg(t, db, "empty-grant-test")

	var projectID, userID string
	if err := db.QueryRow(`INSERT INTO projects (org_id, name) VALUES ($1, 'p') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO users (org_id, email) VALUES ($1, 'grant@example.com') RETURNING id`, orgID).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM user_grants WHERE project_id = $1`, projectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, projectID)
	})

	_, err := db.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, '{}')`, userID, projectID, orgID)
	if err == nil {
		t.Error("a grant with no roles was accepted — it looks like access while granting none")
	}
}

// docs/PLAN/08 Part C: delegating a project to its own owner creates a second,
// confusing path to access the organization already has.
func TestProjectGrantsRefuseSelfGrants(t *testing.T) {
	db := ownerDB(t)

	orgID := seedOrg(t, db, "self-grant-test")

	var projectID string
	if err := db.QueryRow(`INSERT INTO projects (org_id, name) VALUES ($1, 'p') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM projects WHERE id = $1`, projectID) })

	_, err := db.Exec(`
		INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
		VALUES ($1, $2, $2, '{admin}')`, projectID, orgID)
	if err == nil {
		t.Error("an organization was allowed to grant a project to itself")
	}
}

// --- P0-07: the schema matches docs/PLAN/04 --------------------------------------

func TestEveryTableFromThePlanExists(t *testing.T) {
	db := ownerDB(t)

	// Phase 0 and Phase 1 tables. Later-phase tables (user_mfa_factors,
	// user_identities, webhook_endpoints, user_attributes, policies) are
	// deliberately deferred to the phase that uses them, per P0-07 step 2.
	want := []string{
		"instances", "organizations", "users",
		"projects", "applications", "roles",
		"user_grants", "project_grants", "manager_roles",
		"sessions", "refresh_tokens", "signing_keys", "user_tokens",
		"events",
	}

	for _, table := range want {
		t.Run(table, func(t *testing.T) {
			var exists bool
			err := db.QueryRow(`
				SELECT EXISTS (
					SELECT 1 FROM information_schema.tables
					WHERE table_schema = 'public' AND table_name = $1
				)`, table).Scan(&exists)
			if err != nil {
				t.Fatalf("check table: %v", err)
			}
			if !exists {
				t.Errorf("table %q from docs/PLAN/04-DATA-MODEL.md does not exist", table)
			}
		})
	}
}

// docs/PLAN/04 § users: nullable because login can also happen via social or
// passwordless only. A NOT NULL here would force a fake hash for every
// federated user, which is worse than an honest NULL.
func TestNullableColumnsThePlanRequiresToBeNullable(t *testing.T) {
	db := ownerDB(t)

	cases := []struct{ table, column string }{
		{"users", "password_hash"},
		{"users", "username"},
		{"applications", "client_secret_hash"},
		{"user_grants", "project_grant_id"},
		{"events", "actor_user_id"},
		{"sessions", "revoked_at"},
	}

	for _, c := range cases {
		t.Run(c.table+"."+c.column, func(t *testing.T) {
			var nullable string
			err := db.QueryRow(`
				SELECT is_nullable FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`,
				c.table, c.column).Scan(&nullable)
			if err != nil {
				t.Fatalf("inspect column: %v", err)
			}
			if nullable != "YES" {
				t.Errorf("%s.%s must be nullable per docs/PLAN/04", c.table, c.column)
			}
		})
	}
}

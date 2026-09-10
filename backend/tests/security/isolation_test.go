//go:build integration

// Package security holds the abuse-case tests from docs/PLAN/11 § Security Testing,
// kept apart from the feature tests that live beside the code they exercise.
//
// The separation is `P0-15` step 4, and its reason is that these tests are a
// checklist as much as a suite. `docs/PLAN/11` lists seven scenarios drawn from
// `docs/PLAN/10-THREAT-MODEL.md`; scattered across packages they become seven tests
// nobody can enumerate, and "have we covered the threat model" stops being a
// question anyone can answer by looking. Here, `go test ./tests/security/...`
// is the answer.
//
// Each test names the scenario it comes from. When a scenario has no test yet,
// it is listed in the coverage map below rather than left implicit — an
// unwritten test that nobody knows is unwritten is worse than a failing one.
//
// # Coverage against docs/PLAN/11 § Security Testing
//
//	Row-level security prevents cross-org leaks, independent of
//	  application-layer filtering .......................... TestCrossTenantReadsAreEmpty
//	                                                         TestUnfilteredQueryCannotSeeAnotherTenant
//	The runtime role cannot bypass RLS ..................... TestRuntimeRoleCannotBypassRLS
//	The audit log is append-only .......................... TestAuditLogCannotBeAltered
//	User enumeration by login timing ...................... internal/authn:
//	                                                         TestNonexistentUserCostsTheSameAsARealOne
//	                                                         (lives there because it needs the package's
//	                                                          own cost parameters; verified to fail when
//	                                                          the not-found path short-circuits)
//
//	Not yet testable — the feature does not exist:
//	  Token with wrong `aud` rejected ...................... P1-07
//	  Non-exact-match `redirect_uri` rejected .............. P1-06
//	  Rotated refresh token cannot be reused ............... P3-02
//	  Rate limiting triggers under brute force ............. P1-13
//	  Receiving org cannot assign a role outside its grant .. P4-01
//	  Revoked Project Grant invalidates access immediately .. P4-01
package security

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.RunTests(m)) }

// appConn opens a connection as the RUNTIME role.
//
// Every test in this file must use it. Connecting as the owner would bypass
// row-level security and turn each of these into an assertion about nothing —
// which is not hypothetical: this project shipped a check for
// "auth_app is not superuser" that passed because the role did not exist.
func appConn(t *testing.T, stack *testsupport.Stack) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", stack.AppDSN)
	if err != nil {
		t.Fatalf("open app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("app connection unusable: %v", err)
	}
	return db
}

// withTenant runs fn inside a transaction scoped to one organization, the way
// the service does it (`docs/PLAN/08` Part B): `set_config` on a transaction-local
// setting that the RLS policies read.
func withTenant(t *testing.T, db *sql.DB, orgID string, fn func(tx *sql.Tx)) {
	t.Helper()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		t.Fatalf("set tenant: %v", err)
	}

	fn(tx)
}

// docs/PLAN/08 Part B: isolation must hold "even when the application layer forgets
// to filter". So this issues a query with NO WHERE clause on org_id — the
// query a careless caller writes — and asserts the database returns only the
// current tenant's rows.
//
// A test that filtered correctly would prove nothing: it would pass with RLS
// disabled.
func TestUnfilteredQueryCannotSeeAnotherTenant(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	f := testsupport.NewFactory(t, stack)
	orgA, orgB := f.TwoOrganizations()

	userA := f.User(orgA, "a@example.test")
	userB := f.User(orgB, "b@example.test")

	db := appConn(t, stack)

	withTenant(t, db, orgA, func(tx *sql.Tx) {
		// Deliberately unfiltered.
		rows, err := tx.Query(`SELECT id FROM users`)
		if err != nil {
			t.Fatalf("select users: %v", err)
		}
		defer rows.Close()

		var seen []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			seen = append(seen, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}

		if len(seen) != 1 || seen[0] != userA {
			t.Errorf("tenant A saw %v, want exactly [%s]", seen, userA)
		}
		for _, id := range seen {
			if id == userB {
				t.Errorf("CROSS-TENANT LEAK: tenant A read tenant B's user %s", userB)
			}
		}
	})
}

// The control for the test above.
//
// "Tenant A saw one user" is only evidence of isolation if the row it did not
// see was actually there. This connects as the OWNER — which bypasses
// row-level security — and asserts it sees both users from the same
// unfiltered query.
//
// Without this, every isolation test in the file would still pass if the
// factory silently failed to create the second user, or if a botched
// TRUNCATE left the table empty. That is exactly the shape of vacuous pass
// this project has hit four times, and a control is the cheapest way to
// close it: one test that proves the others are measuring something.
func TestIsolationTestsAreNotVacuous(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	f := testsupport.NewFactory(t, stack)
	orgA, orgB := f.TwoOrganizations()
	userA := f.User(orgA)
	userB := f.User(orgB)

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("open owner connection: %v", err)
	}
	defer owner.Close()

	rows, err := owner.Query(`SELECT id FROM users ORDER BY email`)
	if err != nil {
		t.Fatalf("select as owner: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen[id] = true
	}

	if !seen[userA] || !seen[userB] {
		t.Fatalf("the owner should see BOTH users (saw %d); if it does not, the "+
			"isolation tests are passing because the rows are missing rather "+
			"than because RLS is working", len(seen))
	}
}

// Reading another tenant's row by its exact id must also return nothing.
//
// Distinct from the test above: that one covers a forgotten filter, this one
// covers an attacker who already knows the identifier — the IDOR case in
// docs/SECURITY/02 §2. An implementation that filtered lists but honoured a direct
// lookup would pass the first test and fail this one.
func TestCrossTenantReadsAreEmpty(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	f := testsupport.NewFactory(t, stack)
	orgA, orgB := f.TwoOrganizations()
	_ = f.User(orgA)
	userB := f.User(orgB)

	db := appConn(t, stack)

	withTenant(t, db, orgA, func(tx *sql.Tx) {
		var id string
		err := tx.QueryRow(`SELECT id FROM users WHERE id = $1`, userB).Scan(&id)

		if err == nil {
			t.Fatalf("CROSS-TENANT LEAK: tenant A read tenant B's user by id (%s)", id)
		}
		if err != sql.ErrNoRows {
			t.Fatalf("want ErrNoRows, got %v", err)
		}
	})
}

// The isolation above is only worth anything because the runtime role cannot
// turn it off. This asserts the property directly rather than inferring it.
func TestRuntimeRoleCannotBypassRLS(t *testing.T) {
	stack := testsupport.Start(t)
	db := appConn(t, stack)

	var (
		current   string
		superuser bool
		bypassRLS bool
	)
	err := db.QueryRow(`
		SELECT current_user, rolsuper, rolbypassrls
		FROM pg_roles WHERE rolname = current_user`,
	).Scan(&current, &superuser, &bypassRLS)
	if err != nil {
		t.Fatalf("querying role attributes: %v", err)
	}

	// The guard against the vacuous pass. If the connection is not actually
	// the runtime role, every other assertion in this file is meaningless, and
	// this is where that gets caught.
	if current != "auth_app" {
		t.Fatalf("connected as %q, expected auth_app — the security suite must "+
			"run as the runtime role or it proves nothing", current)
	}
	if superuser {
		t.Error("auth_app is a superuser; row-level security does not apply to it")
	}
	if bypassRLS {
		t.Error("auth_app has BYPASSRLS; every isolation policy is inert")
	}
}

// docs/PLAN/09 § Audit and P0-12: the audit log is append-only, enforced by
// privilege rather than by a trigger or by convention in the writing code.
//
// A privilege cannot be forgotten by a future code path, which is the whole
// argument for doing it this way — so the test asserts the privilege holds
// rather than that some function declines to call UPDATE.
func TestAuditLogCannotBeAltered(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	f := testsupport.NewFactory(t, stack)
	instance := f.Instance()
	org := f.Organization(instance)

	f.Exec(`INSERT INTO events (org_id, event_type, payload)
	        VALUES ($1, 'test.event', '{}'::jsonb)`, org)

	db := appConn(t, stack)

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"UPDATE", `UPDATE events SET event_type = 'tampered'`},
		{"DELETE", `DELETE FROM events`},
		{"TRUNCATE", `TRUNCATE events`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTenant(t, db, org, func(tx *sql.Tx) {
				_, err := tx.Exec(tc.query)
				if err == nil {
					t.Fatalf("%s on events succeeded; the audit log is not append-only", tc.name)
				}
				if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
					t.Errorf("%s failed for the wrong reason: %v\n"+
						"Expected a privilege error — if this is now blocked by a "+
						"trigger or a policy instead, the guarantee has moved and "+
						"this test should say so", tc.name, err)
				}
			})
		})
	}
}

//go:build integration

package security

import (
	"database/sql"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// A second organization works with no migration.
//
// `docs/PLAN/02` § Assumptions promised this, and `P2-08` step 1 asks for the
// promise to be verified rather than assumed. The test creates a complete
// second tenant — organization, project, application, role, user, grant — and
// asserts every row landed. If a migration were needed, something here would
// fail to insert.
func TestASecondOrganizationNeedsNoMigration(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	build := func(name string) string {
		org := factory.Organization(instance)

		var project string
		factory.QueryRow(&project,
			`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, name)
		factory.Exec(`INSERT INTO applications (project_id, org_id, name, type)
			VALUES ($1, $2, $3, 'web')`, project, org, name)
		factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
			VALUES ($1, $2, 'admin', 'Admin', $3)`, org, project, pq.Array([]string{"thing:do"}))

		user := factory.User(org)
		factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
			VALUES ($1, $2, $3, $4)`, user, project, org, pq.Array([]string{"admin"}))
		return org
	}

	first := build("first")
	second := build("second")

	if first == second {
		t.Fatal("the two organizations are the same row")
	}

	var orgs, projects, apps, roles, grants int
	factory.QueryRow(&orgs, `SELECT count(*) FROM organizations WHERE deleted_at IS NULL`)
	factory.QueryRow(&projects, `SELECT count(*) FROM projects`)
	factory.QueryRow(&apps, `SELECT count(*) FROM applications`)
	factory.QueryRow(&roles, `SELECT count(*) FROM roles`)
	factory.QueryRow(&grants, `SELECT count(*) FROM user_grants`)

	for name, got := range map[string]int{
		"organizations": orgs, "projects": projects, "applications": apps,
		"roles": roles, "user_grants": grants,
	} {
		if got != 2 {
			t.Errorf("%s has %d rows, want 2 — one per organization", name, got)
		}
	}
}

// A row can never reference a project in another organization.
//
// `P2-08` step 4. Four tables carry both an organization and a project, and
// until this task two of them had nothing checking that the two agree — most
// importantly `applications`, which holds every OIDC client and has been
// writable since Phase 0.
//
// The failure is quiet: both ids are real, so nothing rejects the write, and
// row-level security then HIDES the row from the organization that actually
// owns the project. An application nobody can see; a login nobody can account
// for.
func TestNoRowCanReferenceAnotherOrganizationsProject(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	var projectB string
	factory.QueryRow(&projectB,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'beta') RETURNING id`, orgB)
	factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name)
		VALUES ($1, $2, 'admin', 'Admin')`, orgB, projectB)

	userA := factory.User(orgA)

	// Each of these says "I am in organization A" while pointing at
	// organization B's project.
	misfiled := []struct {
		table string
		query string
		args  []any
	}{
		{
			"applications",
			`INSERT INTO applications (project_id, org_id, name, type) VALUES ($1, $2, 'sneaky', 'web')`,
			[]any{projectB, orgA},
		},
		{
			"roles",
			`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'sneaky', 'Sneaky')`,
			[]any{orgA, projectB},
		},
		{
			"user_grants",
			`INSERT INTO user_grants (user_id, project_id, org_id, role_keys) VALUES ($1, $2, $3, $4)`,
			[]any{userA, projectB, orgA, pq.Array([]string{"admin"})},
		},
		{
			// Phase 4's table, given the rule before it has any rows — a
			// delegation filed under the wrong organization is the worst case
			// of this bug, because crossing an organization boundary is what
			// the row is FOR, so a mistake looks like the feature working.
			"project_grants",
			`INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys, status)
			 VALUES ($1, $2, $3, $4, 'active')`,
			[]any{projectB, orgA, orgB, pq.Array([]string{"admin"})},
		},
	}

	for _, m := range misfiled {
		if err := factory.TryExec(m.query, m.args...); err == nil {
			t.Errorf("%s accepted a row pointing at another organization's project", m.table)
		}
	}
}

// Row-level security holds with real multi-organization data, read through the
// RUNTIME role — `P2-08` step 2 asks for this rather than the synthetic
// single-table check `P0-08` wrote.
func TestEveryTenantScopedTableIsInvisibleAcrossOrganizations(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	seed := func(org, name string) {
		var project string
		factory.QueryRow(&project,
			`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, name)
		factory.Exec(`INSERT INTO applications (project_id, org_id, name, type)
			VALUES ($1, $2, $3, 'web')`, project, org, name)
		factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name)
			VALUES ($1, $2, 'admin', 'Admin')`, org, project)
		user := factory.User(org)
		factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
			VALUES ($1, $2, $3, $4)`, user, project, org, pq.Array([]string{"admin"}))
	}
	seed(orgA, "alpha")
	seed(orgB, "beta")

	conn := appConn(t, stack)

	// Scoped to one organization, every one of these tables must show exactly
	// that organization's row — not two, and not zero.
	//
	// The "not zero" half matters as much as the other: a policy that hid
	// everything would pass an isolation test that only counted what leaked.
	count := func(org, table string) int {
		var n int
		withTenant(t, conn, org, func(tx *sql.Tx) {
			// Deliberately unfiltered: docs/PLAN/08 Part B requires isolation
			// to hold "even when the application layer forgets to filter", so
			// this is the query a careless caller writes.
			if err := tx.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
				t.Fatalf("%s in %s: %v", table, org, err)
			}
		})
		return n
	}

	for _, table := range []string{"projects", "applications", "roles", "user_grants"} {
		if got := count(orgA, table); got != 1 {
			t.Errorf("%s: organization A sees %d rows, want exactly its own 1", table, got)
		}
		if got := count(orgB, table); got != 1 {
			t.Errorf("%s: organization B sees %d rows, want exactly its own 1", table, got)
		}
	}
}

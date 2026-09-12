//go:build integration

package security

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

// Row level security does not degrade query plans as tenants multiply
// (P2-16 step 5).
//
// The isolation tests next door prove RLS **works**. This one asks a different
// question: whether it keeps working at a price anybody would pay.
//
// The failure it guards against is specific. An RLS policy is a predicate the
// planner ANDs into every query, and a predicate the planner cannot push into
// an index turns a keyed lookup into a sequential scan over every tenant's
// rows. Nothing breaks. Correctness is untouched — the wrong rows are filtered
// out, just after they have been read. The symptom is a service that is fine
// with three organizations, fine with thirty, and unusable at three hundred,
// and by then the cause is invisible because every test still passes.
//
// So this asserts on the **plan**, not on the clock. A timing assertion on a
// loaded CI machine is a flaky test that gets deleted; a plan assertion is
// deterministic and names the thing that actually went wrong.

// seedTenants builds `count` organizations, each with a project, a role, a
// user and a grant, and returns one organization id to query as.
func seedTenants(t *testing.T, factory *testsupport.Factory, instance string, count int) string {
	t.Helper()

	var subject string
	for i := 0; i < count; i++ {
		org := factory.Organization(instance)

		var project string
		factory.QueryRow(&project,
			`INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`,
			org, fmt.Sprintf("project-%d", i))

		factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
			VALUES ($1, $2, 'cashier', 'Cashier', $3)`,
			org, project, pq.Array([]string{"sale:create"}))

		user := factory.User(org)
		factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
			VALUES ($1, $2, $3, $4)`,
			user, project, org, pq.Array([]string{"cashier"}))

		// The middle one, so the row being looked for is neither first nor
		// last — a sequential scan that stops early on the first row would
		// otherwise look fast for the wrong reason.
		if i == count/2 {
			subject = org
		}
	}
	return subject
}

// explain runs EXPLAIN inside the tenant scope and returns the plan text.
//
// Through `withTenant`, the same `set_config` the service uses — because the
// RLS predicate only exists when that setting does, and a plan taken outside
// the tenant scope is a plan for a query the service never issues.
func explain(t *testing.T, db *sql.DB, orgID, query string, args ...any) string {
	t.Helper()

	var plan []string
	withTenant(t, db, orgID, func(tx *sql.Tx) {
		rows, err := tx.Query("EXPLAIN (COSTS OFF) "+query, args...)
		if err != nil {
			t.Fatalf("explain: %v\nquery: %s", err, query)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("reading the plan: %v", err)
			}
			plan = append(plan, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("reading the plan: %v", err)
		}
	})

	if len(plan) == 0 {
		t.Fatalf("EXPLAIN returned nothing for: %s", query)
	}
	return strings.Join(plan, "\n")
}

func TestRowLevelSecurityDoesNotForceSequentialScansAtScale(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	// Enough tenants that the planner's own estimates stop rounding a
	// sequential scan and an index scan to the same cost. Below about a
	// hundred rows Postgres will legitimately choose a sequential scan
	// because it is genuinely cheaper, and a test that failed on that would
	// be testing the planner rather than the schema.
	const tenants = 400
	orgID := seedTenants(t, factory, instance, tenants)

	factory.Exec("ANALYZE")
	db := appConn(t, stack)

	for _, c := range []struct {
		what  string
		query string
		args  []any
	}{
		{
			what:  "a project lookup inside one tenant",
			query: `SELECT id, name FROM projects WHERE org_id = $1`,
			args:  []any{orgID},
		},
		{
			what:  "a role lookup inside one tenant",
			query: `SELECT key FROM roles WHERE org_id = $1`,
			args:  []any{orgID},
		},
		{
			what:  "a grant lookup inside one tenant",
			query: `SELECT role_keys FROM user_grants WHERE org_id = $1`,
			args:  []any{orgID},
		},
	} {
		plan := explain(t, db, orgID, c.query, c.args...)

		if strings.Contains(plan, "Seq Scan") {
			t.Errorf("%s plans a sequential scan across %d tenants.\n"+
				"Correctness is unaffected — the other tenants' rows are read and then "+
				"discarded — which is exactly why this is invisible until the service "+
				"is slow for a reason nobody can find.\n\n%s",
				c.what, tenants, plan)
		}

		// And the plan must actually be about something. An EXPLAIN that came
		// back empty would pass the assertion above having checked nothing.
		if !strings.Contains(plan, "Scan") {
			t.Errorf("%s produced a plan with no scan node at all, so this proves nothing:\n%s",
				c.what, plan)
		}
	}
}

// The tenant predicate is what the index is used for, not merely present.
//
// A plan can name an index and still read every row of it. The distinguishing
// feature is whether `org_id` appears as an **Index Cond** — pushed into the
// index — rather than only as a Filter applied after the fact.
func TestTheTenantPredicateReachesTheIndex(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	const tenants = 400
	orgID := seedTenants(t, factory, instance, tenants)
	factory.Exec("ANALYZE")
	db := appConn(t, stack)

	plan := explain(t, db, orgID,
		`SELECT id, name FROM projects WHERE org_id = $1`, orgID)

	if !strings.Contains(plan, "Index Cond") {
		t.Errorf("the tenant predicate is not an index condition — it is being applied "+
			"after rows are read rather than to choose which rows to read:\n\n%s", plan)
	}
}

// The guard that makes the two tests above mean something.
//
// If `seedTenants` ever stopped inserting — a factory change, a constraint, a
// truncate in the wrong order — every plan above would be over an empty table,
// every assertion would pass, and the suite would report that isolation scales
// having measured nothing.
func TestTheScaleTestsActuallyHaveScale(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	const tenants = 50
	seedTenants(t, factory, instance, tenants)

	for table, want := range map[string]int{
		"organizations": tenants,
		"projects":      tenants,
		"roles":         tenants,
		"user_grants":   tenants,
	} {
		var got int
		factory.QueryRow(&got, "SELECT count(*) FROM "+table)
		if got < want {
			t.Errorf("%s holds %d rows after seeding %d tenants; the plan tests would "+
				"be measuring an empty table", table, got, want)
		}
	}
}

// The detector above can fail.
//
// A plan assertion that never sees a sequential scan is indistinguishable from
// one that cannot recognise one.
//
// Finding a tenant-scoped query that plans a sequential scan turns out to be
// hard, and the reason is the thing being tested: the RLS predicate itself is
// `org_id = current_org_id()`, which is indexed, so it steers the planner onto
// an index even when the caller's own predicate cannot. That is the property
// these tests assert, arrived at from the other direction.
//
// So the detector is exercised against a table with no policy and a query with
// no predicate, where a scan is the only possible plan.
func TestTheSequentialScanDetectorWorks(t *testing.T) {
	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()

	const tenants = 200
	orgID := seedTenants(t, factory, instance, tenants)
	factory.Exec("ANALYZE")
	db := appConn(t, stack)

	// `manager_roles` carries no org_id and no policy — its scope spans
	// organizations by design — and a bare count has nothing to key on.
	plan := explain(t, db, orgID, `SELECT count(*) FROM manager_roles`)

	if !strings.Contains(plan, "Seq Scan") {
		t.Errorf("a query that can only be answered by a scan did not plan one, so the "+
			"assertions in this file may be unable to recognise one:%s", plan)
	}
}

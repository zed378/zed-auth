package testsupport

import (
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
)

// A test-data factory: each test states only what it actually cares about.
//
// `P0-15` step 5 asks for this, and the reason is legibility rather than
// convenience. A test that opens with twenty lines of INSERT statements buries
// its own subject — a reader cannot tell which of those rows the assertion
// depends on and which are scaffolding. When every value that does not matter
// has a default, the ones a test sets are exactly the ones it is about.
//
// Everything here seeds through the OWNER connection, which bypasses
// row-level security. That is correct for arranging a fixture and wrong for
// asserting one: a test that reads back through the owner proves nothing about
// isolation. Read through the app connection.

// counter makes generated names unique within a test binary without a random
// source, so a failure message is the same on a re-run.
var counter atomic.Int64

func next() int64 { return counter.Add(1) }

// Factory seeds fixtures into a running stack.
type Factory struct {
	t     *testing.T
	db    *sql.DB
	stack *Stack
}

// NewFactory opens an owner connection for the lifetime of the test.
func NewFactory(t *testing.T, stack *Stack) *Factory {
	t.Helper()

	db, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("factory: open owner connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &Factory{t: t, db: db, stack: stack}
}

// Instance creates an instance and returns its id.
func (f *Factory) Instance(name ...string) string {
	f.t.Helper()

	label := fmt.Sprintf("instance-%d", next())
	if len(name) > 0 {
		label = name[0]
	}

	var id string
	err := f.db.QueryRow(
		`INSERT INTO instances (name) VALUES ($1) RETURNING id`, label,
	).Scan(&id)
	if err != nil {
		f.t.Fatalf("factory: create instance: %v", err)
	}
	return id
}

// Organization creates an organization within an instance.
//
// The organization is the tenant boundary (`docs/PLAN/04`), so almost every
// integration test needs at least one and most isolation tests need two.
func (f *Factory) Organization(instanceID string, name ...string) string {
	f.t.Helper()

	label := fmt.Sprintf("org-%d", next())
	if len(name) > 0 {
		label = name[0]
	}

	var id string
	err := f.db.QueryRow(
		`INSERT INTO organizations (instance_id, name) VALUES ($1, $2) RETURNING id`,
		instanceID, label,
	).Scan(&id)
	if err != nil {
		f.t.Fatalf("factory: create organization: %v", err)
	}
	return id
}

// TwoOrganizations is the shape isolation tests need: two tenants in one
// instance, which must not be able to see each other.
//
// Named rather than assembled inline in each test because it is the setup for
// every cross-tenant assertion, and a reader seeing this call knows
// immediately what the test is about.
func (f *Factory) TwoOrganizations() (orgA, orgB string) {
	f.t.Helper()

	instance := f.Instance()
	return f.Organization(instance), f.Organization(instance)
}

// User creates a user in an organization.
func (f *Factory) User(orgID string, email ...string) string {
	f.t.Helper()

	address := fmt.Sprintf("user-%d@example.test", next())
	if len(email) > 0 {
		address = email[0]
	}

	var id string
	err := f.db.QueryRow(
		`INSERT INTO users (org_id, email, status) VALUES ($1, $2, 'active') RETURNING id`,
		orgID, address,
	).Scan(&id)
	if err != nil {
		f.t.Fatalf("factory: create user: %v", err)
	}
	return id
}

// Exec runs an arbitrary statement as the owner, for the cases a factory
// method would be over-fitting.
//
// Present so a test needing one unusual row does not have to grow a method
// nothing else uses — and deliberately blunt, so reaching for it reads as a
// choice rather than as the normal path.
func (f *Factory) Exec(query string, args ...any) {
	f.t.Helper()

	if _, err := f.db.Exec(query, args...); err != nil {
		f.t.Fatalf("factory: exec: %v\nquery: %s", err, query)
	}
}

// QueryRow reads a single value on the OWNER connection, for assertions that
// must see the raw row rather than what an application read path returns.
//
// The owner bypasses row-level security, which is exactly why this is the
// right tool for "prove the plaintext never reached the column" and the wrong
// tool for anything about isolation. An isolation test that used this would
// pass against a schema with no policies at all (P0-08's lesson), so it is
// documented as an inspection helper and named to read like one.
func (f *Factory) QueryRow(dest any, query string, args ...any) {
	f.t.Helper()

	if err := f.db.QueryRow(query, args...).Scan(dest); err != nil {
		f.t.Fatalf("factory: query: %v\nquery: %s", err, query)
	}
}

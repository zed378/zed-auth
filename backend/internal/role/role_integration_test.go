//go:build integration

// The role store against a real database (P2-01).
//
// Every rule in this package exists twice — once in Go, where it produces a
// field-level error a form can point at, and once in the schema, where it holds
// against writers this package does not mediate. The Go half is covered by
// `role_test.go`. This file covers the schema half, and one test covers the
// agreement between them, because two validators that disagree are worse than
// one: the API accepts what the database then refuses, or the database accepts
// what the API believes it prevented.
package role

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db      *postgres.DB
	store   *Store
	factory *testsupport.Factory

	orgA, orgB          string
	projectA, projectA2 string
	projectB            string
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)

	project := func(org, name string) string {
		var id string
		factory.QueryRow(&id, `INSERT INTO projects (org_id, name) VALUES ($1, $2) RETURNING id`, org, name)
		return id
	}

	// The connection is the RUNTIME role, always. Connecting as the owner would
	// bypass row-level security and turn the isolation tests below into
	// assertions about nothing — which is not hypothetical: this project once
	// shipped a check for "auth_app is not superuser" that passed because the
	// role did not exist.
	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &fixture{
		db: db, store: NewStore(), factory: factory,
		orgA: orgA, orgB: orgB,
		projectA: project(orgA, "alpha"), projectA2: project(orgA, "alpha-two"),
		projectB: project(orgB, "beta"),
	}
}

// in runs fn inside the given organization's tenant scope.
func (f *fixture) in(t *testing.T, orgID string, fn func(tx *postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), orgID, fn)
}

func (f *fixture) mustCreate(t *testing.T, orgID, projectID, key string, perms ...string) Role {
	t.Helper()
	var created Role
	err := f.in(t, orgID, func(tx *postgres.Tx) error {
		var err error
		created, err = f.store.Create(context.Background(), tx, projectID, key, key+" role", perms)
		return err
	})
	if err != nil {
		t.Fatalf("creating %q: %v", key, err)
	}
	return created
}

// --- the scoping that is the whole point ------------------------------------

// `docs/PLAN/08` Part A opens with this: "admin" in Project A must never imply
// "admin" in Project B.
//
// The assertion is deliberately about the SECOND project's permissions rather
// than the first's. A test that only checks Project A's role exists would pass
// against a schema with no scoping at all.
func TestARoleKeyRepeatsAcrossProjectsAndCarriesDifferentPermissions(t *testing.T) {
	f := setup(t)

	f.mustCreate(t, f.orgA, f.projectA, "admin", "user:read")
	f.mustCreate(t, f.orgA, f.projectA2, "admin", "billing:write")

	var inA, inA2 Role
	if err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		var err error
		if inA, err = f.store.GetByKey(context.Background(), tx, f.projectA, "admin"); err != nil {
			return err
		}
		inA2, err = f.store.GetByKey(context.Background(), tx, f.projectA2, "admin")
		return err
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if inA.ID == inA2.ID {
		t.Fatal("the two projects share one role row")
	}
	if got := strings.Join(inA.PermissionKeys, ","); got != "user:read" {
		t.Errorf("project A's admin carries %q", got)
	}
	// The half that matters: Project A's permission is absent from Project B's
	// identically-named role.
	for _, k := range inA2.PermissionKeys {
		if k == "user:read" {
			t.Fatal("the admin role in the second project carries the first project's permission")
		}
	}
}

func TestARoleKeyIsUniqueWithinItsProject(t *testing.T) {
	f := setup(t)
	f.mustCreate(t, f.orgA, f.projectA, "admin")

	err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		_, err := f.store.Create(context.Background(), tx, f.projectA, "admin", "Another", nil)
		return err
	})
	if err == nil {
		t.Fatal("a duplicate key within one project was accepted")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
}

// --- tenancy ----------------------------------------------------------------

// A caller inside organization A naming organization B's project. Without the
// trigger the row is written with A's org_id and B's project_id — and then RLS
// HIDES it from B, the organization that actually owns the project. A silently
// misfiled permission is worse than a rejected write.
func TestARoleCannotBeWrittenIntoAnotherOrganizationsProject(t *testing.T) {
	f := setup(t)

	err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		_, err := f.store.Create(context.Background(), tx, f.projectB, "sneaky", "Sneaky", nil)
		return err
	})
	if err == nil {
		t.Fatal("a role was written into another organization's project")
	}
	// Not found, never forbidden: a 403 would confirm the project id is real.
	if err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}

	// And nothing landed.
	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM roles WHERE key = 'sneaky'`)
	if count != 0 {
		t.Errorf("%d sneaky role(s) exist", count)
	}
}

func TestRolesAreInvisibleAcrossTenants(t *testing.T) {
	f := setup(t)
	created := f.mustCreate(t, f.orgA, f.projectA, "admin", "user:read")

	// Organization B, holding A's role id.
	err := f.in(t, f.orgB, func(tx *postgres.Tx) error {
		_, err := f.store.Get(context.Background(), tx, created.ID)
		return err
	})
	if err != ErrNotFound {
		t.Errorf("organization B read organization A's role: %v", err)
	}

	// And a list in B does not contain it.
	var found []Role
	if err := f.in(t, f.orgB, func(tx *postgres.Tx) error {
		var err error
		found, err = f.store.List(context.Background(), tx, f.projectA, "", 50)
		return err
	}); err != nil {
		t.Fatalf("listing in B: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("organization B listed %d of organization A's roles", len(found))
	}
}

// --- built-in immutability --------------------------------------------------

func TestABuiltinRoleCannotBeDeletedOrReKeyed(t *testing.T) {
	f := setup(t)
	created := f.mustCreate(t, f.orgA, f.projectA, "platform", "user:read")

	// Marked built-in directly: nothing in the API creates one, which is the
	// point — `is_builtin` is for roles the SERVICE owns.
	f.factory.Exec(`UPDATE roles SET is_builtin = true WHERE id = $1`, created.ID)

	err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		return f.store.Delete(context.Background(), tx, created.ID)
	})
	if err != ErrBuiltin {
		t.Errorf("deleting a built-in role gave %v, want ErrBuiltin", err)
	}

	// The store refuses first, so the trigger underneath is exercised directly:
	// a writer that never came through this package must be refused too.
	dbErr := f.factory.TryExec(`DELETE FROM roles WHERE id = $1`, created.ID)
	if dbErr == nil {
		t.Error("the database allowed a built-in role to be deleted")
	}
	dbErr = f.factory.TryExec(`UPDATE roles SET key = 'renamed' WHERE id = $1`, created.ID)
	if dbErr == nil {
		t.Error("the database allowed a built-in role to be re-keyed")
	}
	// Clearing the flag would be a two-step deletion.
	dbErr = f.factory.TryExec(`UPDATE roles SET is_builtin = false WHERE id = $1`, created.ID)
	if dbErr == nil {
		t.Error("the database allowed a built-in role to stop being built-in")
	}

	// Its display name is still editable: what a grant references is the key.
	if err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		_, err := f.store.Update(context.Background(), tx, created.ID, "Platform Team", []string{"user:read"})
		return err
	}); err != nil {
		t.Errorf("a built-in role's display name could not be corrected: %v", err)
	}
}

// --- delete refusal ---------------------------------------------------------

// `P2-01` step 5: refused, or cascaded with explicit confirmation — "silent
// cascade is how permissions vanish mysteriously".
func TestDeletingAReferencedRoleIsRefused(t *testing.T) {
	f := setup(t)
	created := f.mustCreate(t, f.orgA, f.projectA, "billing-admin", "billing:write")
	userID := f.factory.User(f.orgA)

	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		userID, f.projectA, f.orgA, pq.Array([]string{"billing-admin"}))

	err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		return f.store.Delete(context.Background(), tx, created.ID)
	})
	var inUse ErrInUse
	if !asErr(err, &inUse) {
		t.Fatalf("want ErrInUse, got %v", err)
	}
	if inUse.Grants != 1 {
		t.Errorf("reported %d grants, want 1", inUse.Grants)
	}

	// The role survived.
	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM roles WHERE id = $1`, created.ID)
	if count != 1 {
		t.Error("the role was deleted despite the refusal")
	}

	// Once the grant is gone, the delete succeeds — so the refusal is about the
	// reference and not about deletes being broken.
	f.factory.Exec(`DELETE FROM user_grants WHERE user_id = $1`, userID)
	if err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		return f.store.Delete(context.Background(), tx, created.ID)
	}); err != nil {
		t.Errorf("deleting an unreferenced role failed: %v", err)
	}
}

func TestGrantCountsAreAccurate(t *testing.T) {
	f := setup(t)
	f.mustCreate(t, f.orgA, f.projectA, "reader", "user:read")
	f.mustCreate(t, f.orgA, f.projectA, "writer", "user:write")
	f.mustCreate(t, f.orgA, f.projectA, "unused", "user:delete")

	for i := 0; i < 3; i++ {
		userID := f.factory.User(f.orgA)
		keys := []string{"reader"}
		if i == 0 {
			keys = append(keys, "writer")
		}
		f.factory.Exec(`
			INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
			VALUES ($1, $2, $3, $4)`, userID, f.projectA, f.orgA, pq.Array(keys))
	}

	var counts map[string]int
	if err := f.in(t, f.orgA, func(tx *postgres.Tx) error {
		var err error
		counts, err = f.store.GrantCounts(context.Background(), tx, f.projectA)
		return err
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}

	if counts["reader"] != 3 {
		t.Errorf("reader counted %d, want 3", counts["reader"])
	}
	if counts["writer"] != 1 {
		t.Errorf("writer counted %d, want 1", counts["writer"])
	}
	// Absent rather than zero — and asserted, because a map that silently
	// contains every key with a zero would make the console warn about
	// deleting a role nobody holds.
	if _, present := counts["unused"]; present {
		t.Errorf("an unreferenced role appears in the counts as %d", counts["unused"])
	}
}

// --- the two validators must agree -------------------------------------------

// The Go validator and the database constraint are two implementations of one
// rule, written in different languages, in different files, by hand.
//
// This feeds one table of cases to both. Without it, the pair drifts the first
// time somebody widens one side to accept a customer's permission key — and
// the drift is silent in the direction that matters: Go accepts, the database
// refuses, and the caller gets a 500 for input the API said was fine.
func TestTheDatabaseAgreesWithTheValidator(t *testing.T) {
	f := setup(t)

	cases := []string{
		"user:read", "billing.invoice:write", "a:b", "user_profile:read_all",
		"", "user", "user:", ":read", "User:read", "user:Read", "user:read:write",
		"user..profile:read", "1user:read", "user-profile:read", "user:*", "*",
		"user:read ", "user:read;DROP", strings.Repeat("a", 200) + ":read",
	}

	for _, key := range cases {
		goAccepts := ValidatePermissionKey(key) == nil

		var dbAccepts bool
		f.factory.QueryRow(&dbAccepts,
			`SELECT permission_keys_are_well_formed(ARRAY[$1]::text[])`, key)

		// The length bound is Go's and the API's, not the database's — the
		// column has no per-element length CHECK — so a key that is only too
		// long is allowed to differ.
		if len(key) > MaxPermissionKeyLength && !goAccepts && dbAccepts {
			continue
		}

		if goAccepts != dbAccepts {
			t.Errorf("%q: Go accepts=%v, database accepts=%v", key, goAccepts, dbAccepts)
		}
	}
}

// And the constraint is actually attached to the column — a function that is
// never called validates nothing.
func TestTheConstraintIsAttachedToTheTable(t *testing.T) {
	f := setup(t)

	err := f.factory.TryExec(`
		INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, 'direct', 'Direct', ARRAY['not a permission key'])`,
		f.orgA, f.projectA)
	if err == nil {
		t.Fatal("the database accepted a malformed permission key written directly")
	}
	if !strings.Contains(err.Error(), "roles_permission_keys_well_formed") {
		t.Errorf("refused, but by something else: %v", err)
	}

	err = f.factory.TryExec(`
		INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, 'dupes', 'Dupes', ARRAY['user:read','user:read'])`,
		f.orgA, f.projectA)
	if err == nil {
		t.Fatal("the database accepted duplicate permission keys")
	}

	over := make([]string, MaxPermissionKeys+1)
	for i := range over {
		over[i] = fmt.Sprintf("r%d:read", i)
	}
	err = f.factory.TryExec(`
		INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, 'many', 'Many', $3)`,
		f.orgA, f.projectA, pq.Array(over))
	if err == nil {
		t.Fatalf("the database accepted %d permission keys", len(over))
	}
}

func asErr[T error](err error, target *T) bool {
	if e, ok := err.(T); ok {
		*target = e
		return true
	}
	return false
}

//go:build integration

// Delegated access through `/v1/authz/check` (P4-04).
//
// `P4-02` writes a delegated `user_grants` row; until this card nothing read it
// as access. These tests are the other half of threat review T4-2: the reader
// resolves the row against its Project Grant on every request, so a revoked or
// narrowed delegation stops working immediately rather than when a cache entry
// or a token expires.
package authz

import (
	"context"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// delegate creates an active grant from organization A's project to
// organization B, and assigns `keys` to a B user through it. It returns the
// grant id and the user.
//
// Written from the owner connection on purpose: this file is about the READ
// path, and the write path has its own suite in `internal/projectgrant`. The
// triggers still run — the row is refused unless the grant is active, names
// this organization, and carries a subset — so the fixture cannot set up a
// state the API would not have allowed.
func (f *fixture) delegate(t *testing.T, roleKey string, granted []string, assigned []string) (string, string) {
	t.Helper()

	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, $3, $3, $4)`,
		f.orgA, f.projectA, roleKey, pq.Array([]string{"purchase_request:approve"}))

	var grantID string
	f.factory.QueryRow(&grantID, `
		INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		f.projectA, f.orgA, f.orgB, pq.Array(granted))

	partner := f.factory.User(f.orgB)
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys, project_grant_id)
		VALUES ($1, $2, $3, $4, $5)`,
		partner, f.projectA, f.orgB, pq.Array(assigned), grantID)

	return grantID, partner
}

// The delegation works: organization A asks about a user who belongs to
// organization B, in A's own project, and gets an answer.
//
// Note what is NOT required for this: the subject is invisible in A's tenant,
// because `users` is tenant-scoped and the user belongs to B. The decision does
// not depend on the subject existing locally — `exists` feeds a log line only —
// and the delegated row is visible to A through the grant it made itself.
func TestADelegatedRoleIsHonouredInTheGrantingOrganizationsProject(t *testing.T) {
	f := setup(t)
	_, partner := f.delegate(t, "approver", []string{"approver"}, []string{"approver"})

	got := f.check(t, partner, "approve", "purchase_request")
	if !got.Allowed {
		t.Fatalf("a delegated role was not honoured: %+v", got)
	}
	if got.MatchedPolicy != "approver" {
		t.Errorf("matched_policy = %q, want the delegated role key", got.MatchedPolicy)
	}
}

// A-1: revocation is honoured on the next check, not when something expires.
func TestRevokingTheGrantEndsDelegatedAccessImmediately(t *testing.T) {
	f := setupCached(t)
	grantID, partner := f.delegate(t, "approver", []string{"approver"}, []string{"approver"})

	if got := f.check(t, partner, "approve", "purchase_request"); !got.Allowed {
		t.Fatalf("precondition: the delegated role was not honoured: %+v", got)
	}

	// Revoked exactly as the API revokes it, plus the invalidation the handler
	// issues after the transaction commits.
	f.factory.Exec(`UPDATE project_grants SET status = 'revoked', revoked_at = now() WHERE id = $1`, grantID)
	f.cache.InvalidateGrant(context.Background(), grantID)

	if got := f.check(t, partner, "approve", "purchase_request"); got.Allowed {
		t.Error("a revoked Project Grant still granted access")
	}
}

// A-2: the answer is the intersection with what the grant delegates, computed
// against the grant on every read — so a grant that no longer carries a role
// stops conferring it even though the assignment row still names it.
func TestADelegatedRoleOutsideTheGrantIsNotHonoured(t *testing.T) {
	f := setupCached(t)
	grantID, partner := f.delegate(t, "approver", []string{"approver"}, []string{"approver"})

	if got := f.check(t, partner, "approve", "purchase_request"); !got.Allowed {
		t.Fatalf("precondition: %+v", got)
	}

	// The grant narrows to a role the assignment does not name. Only the owner
	// connection can do this — the API refuses it (P4-01's immutability
	// trigger) — and that is the point: the reader must not trust the
	// assignment row's own list.
	f.factory.Exec(`ALTER TABLE project_grants DISABLE TRIGGER project_grants_only_narrow`)
	t.Cleanup(func() {
		f.factory.Exec(`ALTER TABLE project_grants ENABLE TRIGGER project_grants_only_narrow`)
	})
	f.factory.Exec(`UPDATE project_grants SET granted_role_keys = '{something-else}' WHERE id = $1`, grantID)
	f.factory.Exec(`ALTER TABLE project_grants ENABLE TRIGGER project_grants_only_narrow`)
	f.cache.InvalidateGrant(context.Background(), grantID)

	if got := f.check(t, partner, "approve", "purchase_request"); got.Allowed {
		t.Error("a role the grant no longer delegates was still honoured")
	}
}

// A-5: the cache cannot outlive the grant it depended on. One INCR retires
// every entry that came through the grant, whoever holds it.
func TestOneInvalidationRetiresEveryDecisionThatCameThroughAGrant(t *testing.T) {
	// The cached fixture: this test is about what the cache records and when it
	// stops serving it.
	f := setupCached(t)
	grantID, partner := f.delegate(t, "approver", []string{"approver"}, []string{"approver"})
	ctx := context.Background()

	if got := f.check(t, partner, "approve", "purchase_request"); !got.Allowed {
		t.Fatalf("precondition: %+v", got)
	}
	keys, via, hit := f.cache.RoleKeys(ctx, f.orgA, partner, f.projectA)
	if !hit || via != grantID {
		t.Fatalf("cached entry: keys=%v via=%q hit=%v — want the grant recorded", keys, via, hit)
	}

	f.cache.InvalidateGrant(ctx, grantID)

	if _, _, hit := f.cache.RoleKeys(ctx, f.orgA, partner, f.projectA); hit {
		t.Error("the entry survived its grant's revocation; the TTL would be the revocation window")
	}
}

// A-3 and A-4: the read the granting organization gained is bounded to grants
// it made. A bystander organization sees nothing at all.
func TestTheGrantingSideReadIsBoundedToItsOwnGrants(t *testing.T) {
	f := setup(t)
	grantID, partner := f.delegate(t, "approver", []string{"approver"}, []string{"approver"})

	ctx := context.Background()
	count := func(org string) int {
		var n int
		if err := f.db.WithTenant(ctx, org, func(tx *postgres.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM user_grants WHERE project_grant_id = $1`, grantID).Scan(&n)
		}); err != nil {
			t.Fatalf("reading as %s: %v", org, err)
		}
		return n
	}

	if got := count(f.orgA); got != 1 {
		t.Errorf("the granting organization sees %d rows of its own grant, want 1", got)
	}
	if got := count(f.orgB); got != 1 {
		t.Errorf("the receiving organization sees %d of its own rows, want 1", got)
	}

	// A third organization, with a grant of its own to the same partner, must
	// still see nothing of this one.
	instance := f.factory.Instance()
	orgC := f.factory.Organization(instance)
	var projectC string
	f.factory.QueryRow(&projectC, `INSERT INTO projects (org_id, name) VALUES ($1, 'c') RETURNING id`, orgC)
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'approver', 'approver')`, orgC, projectC)
	f.factory.Exec(`INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
	                VALUES ($1, $2, $3, '{approver}')`, projectC, orgC, f.orgB)

	if got := count(orgC); got != 0 {
		t.Errorf("an unrelated granting organization sees %d rows of somebody else's grant, want 0", got)
	}
	_ = partner
}

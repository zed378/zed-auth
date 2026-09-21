//go:build integration

// The token claim for a delegated grant (P4-04).
//
// `docs/PLAN/08` Part A nests an `org_id` inside every role claim, for a reason
// that only arrives with delegation: the same role name becomes reachable from
// two organizational contexts, and a consumer must be able to tell them apart.
// These tests pin the value that field finally carries, and that a revoked
// delegation claims nothing.
//
// No live path issues such a token yet — a session from another organization is
// still refused at `/oauth/authorize`, and cross-organization sign-in is its own
// decision (ADR-025). The claim code is written and tested now so that the day
// sign-in lands, the claim is already right rather than approximately right.
package grant

import (
	"context"
	"testing"

	"github.com/lib/pq"
)

// delegated sets up: a role in organization A's project, an active grant of it
// to organization B, and a B user assigned that role through the grant.
func (f *fixture) delegated(t *testing.T, granted, assigned []string) (grantID, partner string) {
	t.Helper()

	for _, key := range []string{"cashier", "manager"} {
		f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name)
			VALUES ($1, $2, $3, $3)`, f.orgA, f.projectA, key)
	}
	f.factory.QueryRow(&grantID, `
		INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		f.projectA, f.orgA, f.orgB, pq.Array(granted))

	partner = f.factory.User(f.orgB)
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys, project_grant_id)
		VALUES ($1, $2, $3, $4, $5)`,
		partner, f.projectA, f.orgB, pq.Array(assigned), grantID)
	return grantID, partner
}

func TestADelegatedRoleIsClaimedWithTheDelegatingOrganization(t *testing.T) {
	f := setup(t)
	_, partner := f.delegated(t, []string{"cashier", "manager"}, []string{"cashier"})

	// Read in the GRANTING organization's tenant, which is the context a token
	// for its application is issued in.
	claims, err := NewTokenClaims(f.db).ForToken(context.Background(), f.orgA, partner, f.projectA)
	if err != nil {
		t.Fatalf("reading claims: %v", err)
	}

	if len(claims.Keys) != 1 || claims.Keys[0] != "cashier" {
		t.Errorf("claimed keys = %v, want the one delegated role the user holds", claims.Keys)
	}
	if claims.RoleOrgID != f.orgA {
		t.Errorf("role org = %q, want the delegating organization %q — a consumer reading the "+
			"receiving organization's id would treat a partner's role as one of its own",
			claims.RoleOrgID, f.orgA)
	}
}

func TestADirectGrantClaimsNoSeparateOrganization(t *testing.T) {
	f := setup(t)
	subject := f.factory.User(f.orgA)
	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name) VALUES ($1, $2, 'cashier', 'cashier')`, f.orgA, f.projectA)
	f.factory.Exec(`INSERT INTO user_grants (user_id, project_id, org_id, role_keys) VALUES ($1, $2, $3, '{cashier}')`,
		subject, f.projectA, f.orgA)

	claims, err := NewTokenClaims(f.db).ForToken(context.Background(), f.orgA, subject, f.projectA)
	if err != nil {
		t.Fatalf("reading claims: %v", err)
	}
	if claims.RoleOrgID != "" {
		t.Errorf("role org = %q for a direct grant, want empty — the token's own organization is the answer", claims.RoleOrgID)
	}
	if len(claims.Keys) != 1 {
		t.Errorf("claimed keys = %v", claims.Keys)
	}
}

// A-1, on the claim side: the token reader resolves the row against the grant
// exactly as `/v1/authz/check` does, so a revoked delegation claims nothing.
func TestARevokedDelegationClaimsNothing(t *testing.T) {
	f := setup(t)
	grantID, partner := f.delegated(t, []string{"cashier"}, []string{"cashier"})

	f.factory.Exec(`UPDATE project_grants SET status = 'revoked', revoked_at = now() WHERE id = $1`, grantID)

	claims, err := NewTokenClaims(f.db).ForToken(context.Background(), f.orgA, partner, f.projectA)
	if err != nil {
		t.Fatalf("reading claims: %v", err)
	}
	if len(claims.Keys) != 0 {
		t.Errorf("a revoked delegation still claimed %v", claims.Keys)
	}
}

// The subset is applied on the read, not trusted from the assignment row.
func TestOnlyTheIntersectionWithTheGrantIsClaimed(t *testing.T) {
	f := setup(t)
	grantID, partner := f.delegated(t, []string{"cashier", "manager"}, []string{"cashier", "manager"})

	// Narrowed from the owner connection, which is the only writer that can:
	// the API refuses it (P4-01's immutability trigger).
	f.factory.Exec(`ALTER TABLE project_grants DISABLE TRIGGER project_grants_only_narrow`)
	f.factory.Exec(`UPDATE project_grants SET granted_role_keys = '{cashier}' WHERE id = $1`, grantID)
	f.factory.Exec(`ALTER TABLE project_grants ENABLE TRIGGER project_grants_only_narrow`)

	claims, err := NewTokenClaims(f.db).ForToken(context.Background(), f.orgA, partner, f.projectA)
	if err != nil {
		t.Fatalf("reading claims: %v", err)
	}
	if len(claims.Keys) != 1 || claims.Keys[0] != "cashier" {
		t.Errorf("claimed keys = %v, want only the role the grant still delegates", claims.Keys)
	}
}

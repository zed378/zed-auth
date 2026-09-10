package management

import (
	"context"
	"fmt"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Reading a caller's manager roles.
//
// **On every request, from the database, never from the token.**
//
// docs/PLAN/08 Part A illustrates a token carrying
// `"urn:authservice:manager_roles": ["ORG_ADMIN"]`, and that claim is real and
// will exist — P2-04 populates it so a consumer application can hide a button
// without a round trip. It is NOT what this reads.
//
// A role in a token is a snapshot taken when the token was issued. An access
// token lives ten minutes (P1-07), so an administrator whose ORG_ADMIN was
// revoked one minute ago still holds a token asserting it. For a consumer
// deciding whether to grey out a menu item, ten minutes of staleness is
// nothing. For THIS API — where the actions are "delete this organization",
// "assign this role", "rotate this client's secret" — it is the difference
// between revocation and a promise of revocation.
//
// docs/PLAN/08 Part C already sets the precedent in the strongest terms for the
// analogous case: a project grant's roles are validated server-side as a subset
// of granted_role_keys "on every single request, not just at grant-creation
// time". Manager roles govern who administers the service itself.

// RoleStore reads manager_roles.
type RoleStore struct{}

func NewRoleStore() *RoleStore { return &RoleStore{} }

// GrantsFor reads every manager role a user holds.
//
// **Instance-scoped, deliberately, and this is the one read in the request
// path that has to be.**
//
// manager_roles has no org_id column — it has a `scope_id` whose meaning
// depends on the role, and an INSTANCE_OWNER's scope is the instance rather
// than any organization. A tenant-scoped read could therefore never see the
// grant that spans tenants, and scoping it to the caller's own organization
// would make an INSTANCE_OWNER unable to administer anybody else: the exact
// capability the role exists for.
//
// It runs before the request's own tenant scope is chosen, because WHICH scope
// to choose is what this read decides.
func (s *RoleStore) GrantsFor(ctx context.Context, db *postgres.DB, userID string) ([]Grant, error) {
	if userID == "" {
		// A token with no subject. Refused here rather than producing a query
		// that matches nothing and reads like a caller with no roles.
		return nil, fmt.Errorf("management: cannot read grants without a subject")
	}

	var grants []Grant

	err := db.WithInstanceScope(ctx,
		"reading a caller's manager roles, whose scope spans organizations by design",
		func(tx *postgres.Tx) error {
			rows, err := tx.Query(ctx,
				`SELECT role, scope_id FROM manager_roles WHERE user_id = $1`, userID)
			if err != nil {
				return fmt.Errorf("management: reading manager roles: %w", err)
			}
			defer func() { _ = rows.Close() }()

			for rows.Next() {
				var g Grant
				if err := rows.Scan(&g.Role, &g.ScopeID); err != nil {
					return fmt.Errorf("management: reading manager roles: %w", err)
				}
				grants = append(grants, g)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, err
	}

	return grants, nil
}

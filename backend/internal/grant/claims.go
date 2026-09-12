package grant

import (
	"context"
	"fmt"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// TokenClaims reads what an access token should say about a user's roles
// (P2-04).
//
// It lives here rather than in `internal/oauth/token` because the token
// package should not know how grants are stored — it declares an interface and
// this satisfies it, the same arrangement `Clients`, `Codes` and `Sessions`
// already use there.
//
// # Why this is one query and not two round trips
//
// It runs on the token endpoint, which `docs/PLAN/12` makes the most
// latency-sensitive path in the service and which the `P1-28` load test found
// to be the first thing that saturates. Two extra queries per token would be
// two more round trips on the endpoint that already sets the ceiling.
type TokenClaims struct {
	DB *postgres.DB
}

func NewTokenClaims(db *postgres.DB) *TokenClaims { return &TokenClaims{DB: db} }

// ForToken reads both sets in one round trip.
//
// The project roles are read inside the user's tenant, because `user_grants`
// is org-scoped and row-level security is what confines it. The manager roles
// are read OUTSIDE any tenant — `manager_roles` has no `org_id`, only a
// `scope_id` whose meaning depends on the role, and an INSTANCE_OWNER's scope
// is the instance rather than any organization. The same reasoning
// `management.RoleStore` already documents; the difference here is that both
// reads happen in one statement, so the union is one round trip.
func (t *TokenClaims) ForToken(ctx context.Context, orgID, userID, projectID string) (token.RoleClaims, error) {
	if t == nil || t.DB == nil {
		// A handler wired without this reads as a user with no roles, which
		// would be a silent authorization downgrade. Refused loudly instead.
		return token.RoleClaims{}, fmt.Errorf("grant: token claims were requested with no database")
	}
	if userID == "" {
		return token.RoleClaims{}, nil
	}

	var out token.RoleClaims

	// One statement, two sources. The project half goes through the tenant
	// scope; the manager half deliberately does not, so it is selected by
	// user_id alone and cannot be hidden by the tenant the request happens to
	// be acting in.
	err := t.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT
			  coalesce((SELECT role_keys FROM user_grants
			             WHERE user_id = $1 AND project_id = $2), '{}'),
			  coalesce((SELECT array_agg(role ORDER BY role) FROM manager_roles
			             WHERE user_id = $1), '{}')`,
			userID, projectID,
		).Scan(pq.Array(&out.Keys), pq.Array(&out.Manager))
	})
	if err != nil {
		return token.RoleClaims{}, fmt.Errorf("grant: reading token roles: %w", err)
	}

	if out.Keys == nil {
		out.Keys = []string{}
	}
	if out.Manager == nil {
		out.Manager = []string{}
	}
	return out, nil
}

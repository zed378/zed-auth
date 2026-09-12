package organization

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// `GET /v1/me/organizations` — the organizations the caller administers (P2-13).
//
// The one route in `management.Policy` that demands no role. It reports which
// organizations the caller holds a manager role over, derived from their own
// `manager_roles` rows, so it can only ever describe access they already have.
// An endpoint that required a role in order to report which roles you hold is
// one nobody can bootstrap from.
//
// It exists because the console's organization switcher has to offer exactly
// the organizations the caller may act in, and `docs/UI-UX/08` § Cross-Screen
// Requirements is explicit that the switcher is UI, not a control — so what it
// offers must come from where the API's own refusals come from.
//
// **Not from the token.** `urn:authservice:manager_roles` carries role names
// without their scopes, so it cannot name an organization at all; and a role in
// a token is a snapshot up to ten minutes stale, which
// `internal/management/store.go` explains at length for this API in particular.

// AdministeredOrganization is one row of the switcher's list.
//
// Deliberately smaller than Organization: no settings, no status, no instance.
// An endpoint that requires no permission should return the least it can, and
// an organization's settings are readable through `GET /v1/organizations/{org_id}`,
// which requires ORG_ADMIN over it.
type AdministeredOrganization struct {
	ID        string
	Name      string
	Roles     []string
	CreatedAt time.Time
}

// AdministeredPosition is the sort position of a row, for `P1-15`'s cursor.
//
// The same `(created_at, id)` keyset every other list uses, so a page token
// from this endpoint has the same shape and the same guarantees.
func AdministeredPosition(o AdministeredOrganization) management.Cursor {
	return management.Cursor{After: o.CreatedAt, ID: o.ID}
}

// ListAdministered reads the organizations one user administers.
//
// Runs under instance scope through a SECURITY DEFINER function, for the same
// reason `List` does: `organizations` is the one table whose RLS policy keys on
// `id` rather than `org_id`, so under instance scope a plain SELECT matches
// nothing, and under tenant scope it matches exactly the caller's own row —
// neither of which is the question being asked.
//
// The function takes a **user**, never an organization id, so no argument can
// steer it toward an organization the caller does not administer.
func (s *Store) ListAdministered(
	ctx context.Context, db *postgres.DB, userID string, after management.Cursor, size int,
) ([]AdministeredOrganization, error) {
	if userID == "" {
		// A token with no subject. Refused rather than turned into a query
		// that matches nothing and reads like "administers nothing".
		return nil, fmt.Errorf("organization: cannot list administered organizations without a subject")
	}

	var out []AdministeredOrganization

	err := db.WithInstanceScope(ctx,
		"listing the organizations one caller administers, which spans tenants by definition",
		func(tx *postgres.Tx) error {
			rows, err := tx.Query(ctx,
				`SELECT id, name, roles, created_at
				   FROM organizations_administered_by($1, $2, $3, $4)`,
				userID, nullTime(after), nullID(after), size+1)
			if err != nil {
				return fmt.Errorf("organization: listing administered: %w", err)
			}
			defer func() { _ = rows.Close() }()

			for rows.Next() {
				var row AdministeredOrganization
				// `pq.Array` for the text[]: database/sql hands an array
				// column back as the driver's own string and will not decode
				// it into []string on its own. Without it every row fails to
				// scan, the read returns an error, and the endpoint answers
				// 500 — which decodes as an empty list and looks exactly like
				// a caller who administers nothing.
				if err := rows.Scan(&row.ID, &row.Name, pq.Array(&row.Roles), &row.CreatedAt); err != nil {
					return fmt.Errorf("organization: listing administered: %w", err)
				}
				out = append(out, row)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListAdministeredOrganizations implements the generated operation.
func (h *Handler) ListAdministeredOrganizations(
	ctx context.Context, request api.ListAdministeredOrganizationsRequestObject,
) (api.ListAdministeredOrganizationsResponseObject, error) {
	caller, ok := management.CallerFrom(ctx)
	if !ok {
		// The middleware puts one there for every /v1 route. Its absence is a
		// wiring fault rather than an unauthenticated request — that would
		// already have been refused with a 401 before reaching here.
		return nil, fmt.Errorf("organization: no caller in context on a self-scoped route")
	}

	size := management.PageSize(pageSizeParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(pageTokenParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	rows, err := h.Store.ListAdministered(ctx, h.DB, caller.UserID, cursor, size)
	if err != nil {
		return nil, err
	}

	page, err := management.Paginate(rows, size, AdministeredPosition)
	if err != nil {
		return nil, err
	}

	out := api.AdministeredOrganizationList{
		Organizations: make([]api.AdministeredOrganization, 0, len(page.Items)),
	}
	for _, row := range page.Items {
		id, err := uuid.Parse(row.ID)
		if err != nil {
			return nil, fmt.Errorf("organization: an administered id is not a uuid: %w", err)
		}
		out.Organizations = append(out.Organizations, api.AdministeredOrganization{
			Id:    id,
			Name:  row.Name,
			Roles: rolesHighestFirst(row.Roles),
		})
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}

	return api.ListAdministeredOrganizations200JSONResponse(out), nil
}

// rolesHighestFirst orders by the role hierarchy, not the alphabet.
//
// "ORG_ADMIN, ORG_OWNER" is alphabetical and reads as though the first is the
// stronger one. `Role.Reach` is the hierarchy itself rather than a restatement
// of it, so this cannot end up sorted by a rule that used to be true.
func rolesHighestFirst(roles []string) []api.AdministeredOrganizationRoles {
	out := make([]api.AdministeredOrganizationRoles, 0, len(roles))
	for _, role := range roles {
		out = append(out, api.AdministeredOrganizationRoles(role))
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Descending reach: the role that satisfies the most requirements is
		// the strongest. An undefined role has a reach of zero and sorts last,
		// rather than being presented as the caller's most powerful role.
		return management.Role(out[i]).Reach() > management.Role(out[j]).Reach()
	})
	return out
}

package projectgrant

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// ListReceivedProjectGrants is the receiving side's own list: the projects
// another organization has lent to this one (P4-06).
//
// The granting side has `GET …/projects/{project_id}/grants`; until this route
// there was no way for the organization that RECEIVED a grant to discover it —
// it could assign delegated roles only if it already knew the grant id.
//
// `ORG_ADMIN` over the organization in the path, so `inScope` is that
// organization, and the store filters on `granted_org_id`. RLS shows this
// tenant both sides of its own delegations; a receiving-side route must not
// list the grants it MADE.
func (h *Handler) ListReceivedProjectGrants(
	ctx context.Context, request api.ListReceivedProjectGrantsRequestObject,
) (api.ListReceivedProjectGrantsResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))
	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	var rows []Received
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		var err error
		rows, err = h.Grants.ListReceived(ctx, tx, orgID, cursor, size)
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	page, err := management.Paginate(rows, size, func(r Received) management.Cursor {
		return management.Cursor{After: r.CreatedAt, ID: r.ID}
	})
	if err != nil {
		return nil, err
	}
	out := api.ReceivedGrantList{Grants: make([]api.ReceivedGrant, 0, len(page.Items))}
	for _, r := range page.Items {
		rendered, err := renderReceived(r)
		if err != nil {
			return nil, err
		}
		out.Grants = append(out.Grants, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}
	return api.ListReceivedProjectGrants200JSONResponse(out), nil
}

func renderReceived(r Received) (api.ReceivedGrant, error) {
	parse := func(label, v string) (uuid.UUID, error) {
		id, err := uuid.Parse(v)
		if err != nil {
			return uuid.UUID{}, fmt.Errorf("projectgrant: rendering %s %q: %w", label, v, err)
		}
		return id, nil
	}
	id, err := parse("id", r.ID)
	if err != nil {
		return api.ReceivedGrant{}, err
	}
	project, err := parse("project", r.ProjectID)
	if err != nil {
		return api.ReceivedGrant{}, err
	}
	granting, err := parse("granting org", r.GrantingOrgID)
	if err != nil {
		return api.ReceivedGrant{}, err
	}

	out := api.ReceivedGrant{
		Id:              id,
		ProjectId:       project,
		ProjectName:     r.ProjectName,
		GrantingOrgId:   granting,
		GrantingOrgName: r.GrantingOrgName,
		GrantedRoleKeys: append(make([]string, 0, len(r.GrantedRoleKeys)), r.GrantedRoleKeys...),
		HolderCount:     r.HolderCount,
		Status:          api.ReceivedGrantStatus(r.Status),
		CreatedAt:       r.CreatedAt,
	}
	if r.RevokedAt != nil {
		out.RevokedAt.Set(*r.RevokedAt)
	} else {
		out.RevokedAt.SetNull()
	}
	return out, nil
}

package projectgrant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// PROJECT_GRANT_OWNER (P4-03).
//
// Specification: MEMORY/specs/P4-03-project-grant-owner.md.
//
// The narrowest manager role: the delegated roles of ONE grant, for the users
// of the organization it was granted to. Appointed by that organization's
// administrators — never by the granting organization (threat review T4-4) and
// never by another grant owner.
//
// `manager_roles` has no RLS and `scope_id` no foreign key, so every statement
// here names the role and the scope together, and the grant is validated under
// the receiving tenant before a row naming it is written.

// OwnerRole is the manager_roles value this file reads and writes.
const OwnerRole = string(management.ProjectGrantOwner)

// ErrAlreadyOwner is an appointment that already exists.
var ErrAlreadyOwner = errors.New("projectgrant: already an owner of this grant")

// Owner is one PROJECT_GRANT_OWNER of a grant.
type Owner struct {
	UserID    string
	CreatedAt time.Time
}

// Owners lists a grant's owners who are users of the transaction's organization.
func (s *Store) Owners(ctx context.Context, tx *postgres.Tx, grantID string) ([]Owner, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.user_id, m.created_at
		  FROM manager_roles m
		  JOIN users u ON u.id = m.user_id
		 WHERE m.role = $1 AND m.scope_id = $2
		 ORDER BY m.created_at, m.user_id`, OwnerRole, grantID)
	if err != nil {
		return nil, fmt.Errorf("projectgrant: listing owners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Owner{}
	for rows.Next() {
		var o Owner
		if err := rows.Scan(&o.UserID, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("projectgrant: listing owners: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// AddOwner appoints a user, who the caller has checked is a member.
func (s *Store) AddOwner(ctx context.Context, tx *postgres.Tx, grantID, userID string) (Owner, error) {
	o := Owner{UserID: userID}
	err := tx.QueryRow(ctx, `
		INSERT INTO manager_roles (user_id, role, scope_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, role, scope_id) DO NOTHING
		RETURNING created_at`, userID, OwnerRole, grantID).Scan(&o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, ErrAlreadyOwner
	}
	if err != nil {
		return Owner{}, fmt.Errorf("projectgrant: appointing an owner: %w", err)
	}
	return o, nil
}

// RemoveOwner removes an appointment, for a user of the transaction's
// organization only — RLS on users bounds it, since manager_roles has none.
func (s *Store) RemoveOwner(ctx context.Context, tx *postgres.Tx, grantID, userID string) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM manager_roles
		 WHERE user_id = $1 AND role = $2 AND scope_id = $3
		   AND EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID, OwnerRole, grantID)
	if err != nil {
		return fmt.Errorf("projectgrant: removing an owner: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// requireStanding refuses a caller who reaches a received grant ONLY as its
// PROJECT_GRANT_OWNER once that grant is revoked (spec F-3): the role works
// while the grant does. Organization administrators keep list and cleanup.
func requireStanding(ctx context.Context, g Grant) error {
	caller, ok := management.CallerFrom(ctx)
	if !ok {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a grant operation ran with no caller",
		}
	}
	if management.HoldsOrganizationRole(caller, management.OrgAdmin, g.GrantedOrgID) {
		return nil
	}
	if g.Status != StatusActive {
		return ErrNotFound
	}
	return nil
}

// --- handlers ---------------------------------------------------------------

func (h *Handler) ListProjectGrantOwners(
	ctx context.Context, request api.ListProjectGrantOwnersRequestObject,
) (api.ListProjectGrantOwnersResponseObject, error) {
	var owners []Owner
	var g Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		var err error
		if g, err = h.Grants.Received(ctx, tx, orgID, request.GrantId.String()); err != nil {
			return err
		}
		owners, err = h.Grants.Owners(ctx, tx, g.ID)
		return err
	}); err != nil {
		return nil, delegatedFault(err)
	}

	out := api.ProjectGrantOwnerList{Owners: make([]api.ProjectGrantOwner, 0, len(owners))}
	for _, o := range owners {
		rendered, err := renderOwner(o, g.ID)
		if err != nil {
			return nil, err
		}
		out.Owners = append(out.Owners, rendered)
	}
	return api.ListProjectGrantOwners200JSONResponse(out), nil
}

func (h *Handler) AssignProjectGrantOwner(
	ctx context.Context, request api.AssignProjectGrantOwnerRequestObject,
) (api.AssignProjectGrantOwnerResponseObject, error) {
	if request.Body == nil {
		return nil, management.Fault{Class: management.Invalid, Message: "A request body is required.", Reason: "empty body"}
	}
	userID := request.Body.UserId.String()
	if err := refuseSelf(ctx, userID); err != nil {
		return nil, err
	}

	var (
		appointed Owner
		g         Grant
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		var err error
		if g, err = h.Grants.Received(ctx, tx, orgID, request.GrantId.String()); err != nil {
			return err
		}
		if g.Status != StatusActive {
			return ErrRevoked
		}
		if err := requireMember(ctx, tx, userID); err != nil {
			return err
		}
		if appointed, err = h.Grants.AddOwner(ctx, tx, g.ID, userID); err != nil {
			return err
		}
		// Elevated visibility: the first API in the service that writes a
		// manager role (docs/SECURITY/02 §3 expects these to be rare).
		payload := delegatedPayload(g, userID)
		payload["role"] = OwnerRole
		return h.audit(ctx, tx, audit.EventManagerRoleAssigned, payload)
	}); err != nil {
		if errors.Is(err, ErrAlreadyOwner) {
			return nil, management.Fault{
				Class: management.Conflict, Message: "This user is already an owner of this Project Grant.",
				Reason: "duplicate PROJECT_GRANT_OWNER appointment",
			}
		}
		return nil, delegatedFault(err)
	}

	rendered, err := renderOwner(appointed, g.ID)
	if err != nil {
		return nil, err
	}
	return api.AssignProjectGrantOwner201JSONResponse(rendered), nil
}

func (h *Handler) RemoveProjectGrantOwner(
	ctx context.Context, request api.RemoveProjectGrantOwnerRequestObject,
) (api.RemoveProjectGrantOwnerResponseObject, error) {
	userID := request.UserId.String()
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		g, err := h.Grants.Received(ctx, tx, orgID, request.GrantId.String())
		if err != nil {
			return err
		}
		if err := h.Grants.RemoveOwner(ctx, tx, g.ID, userID); err != nil {
			return err
		}
		payload := delegatedPayload(g, userID)
		payload["role"] = OwnerRole
		return h.audit(ctx, tx, audit.EventManagerRoleRevoked, payload)
	}); err != nil {
		return nil, delegatedFault(err)
	}
	return api.RemoveProjectGrantOwner204Response{}, nil
}

func renderOwner(o Owner, grantID string) (api.ProjectGrantOwner, error) {
	userID, err := uuid.Parse(o.UserID)
	if err != nil {
		return api.ProjectGrantOwner{}, fmt.Errorf("projectgrant: rendering owner %q: %w", o.UserID, err)
	}
	grant, err := uuid.Parse(grantID)
	if err != nil {
		return api.ProjectGrantOwner{}, fmt.Errorf("projectgrant: rendering grant %q: %w", grantID, err)
	}
	return api.ProjectGrantOwner{UserId: userID, GrantId: grant, CreatedAt: o.CreatedAt}, nil
}

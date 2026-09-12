package grant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Handler implements the generated grant operations (P2-03).
type Handler struct {
	Grants *Store
	DB     *postgres.DB
	Log    *slog.Logger
	Audit  management.Recorder
}

func New(db *postgres.DB, recorder management.Recorder, log *slog.Logger) *Handler {
	return &Handler{Grants: NewStore(), DB: db, Log: log, Audit: recorder}
}

// --- list -------------------------------------------------------------------

func (h *Handler) ListUserGrants(
	ctx context.Context, request api.ListUserGrantsRequestObject,
) (api.ListUserGrantsResponseObject, error) {
	userID := request.UserId.String()

	var grants []Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireUser(ctx, tx, userID); err != nil {
			return err
		}
		var err error
		grants, err = h.Grants.ForUser(ctx, tx, userID)
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	// Not paginated, deliberately. A user's grants are bounded by the number
	// of projects in one organization, and a page token on a list that is
	// almost always shorter than one page is a contract nobody can remove
	// later.
	out := api.GrantList{Grants: make([]api.Grant, 0, len(grants))}
	for _, g := range grants {
		rendered, err := render(g)
		if err != nil {
			return nil, err
		}
		out.Grants = append(out.Grants, rendered)
	}
	return api.ListUserGrants200JSONResponse(out), nil
}

// --- grant ------------------------------------------------------------------

func (h *Handler) GrantRolesToUser(
	ctx context.Context, request api.GrantRolesToUserRequestObject,
) (api.GrantRolesToUserResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}

	userID := request.UserId.String()
	projectID := request.Body.ProjectId.String()

	if err := refuseSelfService(ctx, userID); err != nil {
		return nil, err
	}

	var created Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireUser(ctx, tx, userID); err != nil {
			return err
		}
		var err error
		created, err = h.Grants.Create(ctx, tx, userID, projectID, request.Body.RoleKeys)
		if err != nil {
			return err
		}
		return h.audit(ctx, tx, audit.EventRoleAssigned, map[string]any{
			"subject_user_id": created.UserID,
			"project_id":      created.ProjectID,
			"roles_added":     created.RoleKeys,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(created)
	if err != nil {
		return nil, err
	}
	return api.GrantRolesToUser201JSONResponse(rendered), nil
}

// --- replace ----------------------------------------------------------------

func (h *Handler) ReplaceUserGrant(
	ctx context.Context, request api.ReplaceUserGrantRequestObject,
) (api.ReplaceUserGrantResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}

	userID := request.UserId.String()
	projectID := request.ProjectId.String()

	if err := refuseSelfService(ctx, userID); err != nil {
		return nil, err
	}

	var updated Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireUser(ctx, tx, userID); err != nil {
			return err
		}

		before, err := h.Grants.Get(ctx, tx, userID, projectID)
		if err != nil {
			return err
		}

		updated, err = h.Grants.Replace(ctx, tx, userID, projectID, request.Body.RoleKeys)
		if err != nil {
			return err
		}

		added, removed := difference(before.RoleKeys, updated.RoleKeys)
		return h.audit(ctx, tx, audit.EventRoleAssigned, map[string]any{
			"subject_user_id": updated.UserID,
			"project_id":      updated.ProjectID,
			"roles_added":     added,
			"roles_removed":   removed,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(updated)
	if err != nil {
		return nil, err
	}
	return api.ReplaceUserGrant200JSONResponse(rendered), nil
}

// --- revoke -----------------------------------------------------------------

func (h *Handler) RevokeUserGrant(
	ctx context.Context, request api.RevokeUserGrantRequestObject,
) (api.RevokeUserGrantResponseObject, error) {
	userID := request.UserId.String()
	projectID := request.ProjectId.String()

	// Revoking your own access is allowed where granting is not: it can only
	// reduce what you hold, and refusing it would mean an administrator who
	// wants to drop a privilege has to ask somebody else.
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireUser(ctx, tx, userID); err != nil {
			return err
		}

		revoked, err := h.Grants.Revoke(ctx, tx, userID, projectID)
		if err != nil {
			return err
		}

		// The keys it carried, because after the row is gone this is the only
		// record of what the user could do — which is what an investigation
		// asks about.
		return h.audit(ctx, tx, audit.EventRoleRevoked, map[string]any{
			"subject_user_id": revoked.UserID,
			"project_id":      revoked.ProjectID,
			"roles_removed":   revoked.RoleKeys,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	return api.RevokeUserGrant204Response{}, nil
}

// --- helpers ----------------------------------------------------------------

// refuseSelfService stops a caller granting roles to themselves.
//
// No scope expresses this. An ORG_ADMIN legitimately administers every user in
// the organization, and they are one of those users — so the permission model
// says yes and the answer should be no. `docs/SECURITY/02` §3's vertical
// escalation is exactly this: the administrator who gives themselves one more
// role than they were given.
//
// Revocation is deliberately not covered: it can only reduce what somebody
// holds.
func refuseSelfService(ctx context.Context, subjectUserID string) error {
	caller, ok := management.CallerFrom(ctx)
	if !ok {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a grant operation ran with no caller",
		}
	}
	if caller.UserID == subjectUserID {
		return management.Fault{
			Class:   management.Forbidden,
			Message: "You cannot change your own roles. Ask another administrator.",
			Reason:  "self-service grant refused",
		}
	}
	return nil
}

func render(g Grant) (api.Grant, error) {
	userID, err := uuid.Parse(g.UserID)
	if err != nil {
		return api.Grant{}, fmt.Errorf("grant: rendering user %q: %w", g.UserID, err)
	}
	projectID, err := uuid.Parse(g.ProjectID)
	if err != nil {
		return api.Grant{}, fmt.Errorf("grant: rendering project %q: %w", g.ProjectID, err)
	}

	keys := make([]string, 0, len(g.RoleKeys))
	keys = append(keys, g.RoleKeys...)

	out := api.Grant{UserId: userID, ProjectId: projectID, RoleKeys: keys}
	if g.CreatedAt.Valid {
		created := g.CreatedAt.Time
		out.CreatedAt = &created
	}
	if g.UpdatedAt.Valid {
		updated := g.UpdatedAt.Time
		out.UpdatedAt = &updated
	}
	return out, nil
}

func difference(before, after []string) (added, removed []string) {
	had := make(map[string]struct{}, len(before))
	for _, k := range before {
		had[k] = struct{}{}
	}
	has := make(map[string]struct{}, len(after))
	for _, k := range after {
		has[k] = struct{}{}
	}

	added, removed = []string{}, []string{}
	for _, k := range after {
		if _, existed := had[k]; !existed {
			added = append(added, k)
		}
	}
	for _, k := range before {
		if _, remains := has[k]; !remains {
			removed = append(removed, k)
		}
	}
	return added, removed
}

func (h *Handler) audit(ctx context.Context, tx *postgres.Tx, kind audit.EventType, payload map[string]any) error {
	if h.Audit == nil {
		return nil
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{Type: kind, Payload: payload})
}

func (h *Handler) requireUser(ctx context.Context, tx *postgres.Tx, userID string) error {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists)
	switch {
	case err != nil:
		return fmt.Errorf("grant: checking the user: %w", err)
	case !exists:
		// Also the answer for another tenant's user, which RLS makes invisible.
		return ErrNotFound
	}
	return nil
}

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a grant operation ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

func faultFrom(err error) error {
	var fault management.Fault
	if errors.As(err, &fault) {
		return err
	}

	var unknown ErrUnknownRole
	if errors.As(err, &unknown) {
		return management.Fault{
			Class:   management.Invalid,
			Message: fmt.Sprintf("There is no role %q in that project.", unknown.Key),
			Details: []api.ErrorDetail{{Field: "role_keys", Issue: "no such role in this project: " + unknown.Key}},
			Reason:  "grant names a role that does not exist",
		}
	}

	switch {
	case errors.Is(err, ErrNotFound):
		return management.Fault{
			Class: management.NotFound, Message: "The requested resource was not found.",
			Reason: "no such grant, user or project in this organization",
		}
	case errors.Is(err, ErrDelegationNotImplemented):
		return management.Fault{
			Class:   management.Invalid,
			Message: "Delegated grants are not available yet.",
			Reason:  "project_grant_id is closed until P4-01",
		}
	}
	return err
}

func missingBody() error {
	return management.Fault{
		Class: management.Invalid, Message: "A request body is required.",
		Reason: "empty body",
	}
}

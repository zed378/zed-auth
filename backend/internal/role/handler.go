package role

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

// Handler implements the generated role operations (P2-02).
//
// The store already owns every rule about what a role may be; this adds the
// HTTP surface, the project in the path, and the audit events — which land
// here rather than in the store for a specific reason. An audit row needs an
// actor, and the store has no caller: `P1-14` found a lockout event that was
// never written because it was emitted where no organization was in scope, and
// writing one here with a nil actor would be the same mistake on purpose.
type Handler struct {
	Roles *Store
	DB    *postgres.DB
	Log   *slog.Logger

	// Audit is P1-15's guarded recorder. Every mutation writes through it, so
	// the per-request trail the audit guard checks afterwards is marked.
	Audit management.Recorder
}

func New(db *postgres.DB, recorder management.Recorder, log *slog.Logger) *Handler {
	return &Handler{Roles: NewStore(), DB: db, Log: log, Audit: recorder}
}

// --- list -------------------------------------------------------------------

func (h *Handler) ListRoles(
	ctx context.Context, request api.ListRolesRequestObject,
) (api.ListRolesResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	projectID := request.ProjectId.String()

	var (
		rows   []Role
		counts map[string]int
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		if rows, err = h.Roles.List(ctx, tx, projectID, cursor.Key, size); err != nil {
			return err
		}
		// One query for the whole page rather than a count per row: the grants
		// table grows with every user, and an N+1 there is the list getting
		// slower for every account the organization adds.
		counts, err = h.Roles.GrantCounts(ctx, tx, projectID)
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	page, err := management.Paginate(rows, size, position)
	if err != nil {
		return nil, err
	}

	out := api.RoleList{Roles: make([]api.Role, 0, len(page.Items))}
	for _, r := range page.Items {
		rendered, err := render(r, counts[r.Key])
		if err != nil {
			return nil, err
		}
		out.Roles = append(out.Roles, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}
	return api.ListRoles200JSONResponse(out), nil
}

// --- create -----------------------------------------------------------------

func (h *Handler) CreateRole(
	ctx context.Context, request api.CreateRoleRequestObject,
) (api.CreateRoleResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}

	projectID := request.ProjectId.String()
	permissions := permissionsOf(request.Body.PermissionKeys)

	var created Role
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		created, err = h.Roles.Create(ctx, tx, projectID,
			string(request.Body.Key), request.Body.DisplayName, permissions)
		if err != nil {
			return err
		}
		return h.audit(ctx, tx, audit.EventRoleCreated, created, map[string]any{
			"project_id":      created.ProjectID,
			"key":             created.Key,
			"permission_keys": created.PermissionKeys,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	// grant_count is 0 by construction: the role did not exist a moment ago,
	// so nothing can reference it.
	rendered, err := render(created, 0)
	if err != nil {
		return nil, err
	}
	return api.CreateRole201JSONResponse(rendered), nil
}

// --- read -------------------------------------------------------------------

func (h *Handler) GetRole(
	ctx context.Context, request api.GetRoleRequestObject,
) (api.GetRoleResponseObject, error) {
	projectID := request.ProjectId.String()

	var (
		found  Role
		counts map[string]int
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		if found, err = h.Roles.Get(ctx, tx, request.RoleId.String()); err != nil {
			return err
		}
		// A role id from another project in the same organization is visible to
		// RLS but is not under the path that was asked for. Answered as not
		// found, so the path means what it says.
		if found.ProjectID != projectID {
			return ErrNotFound
		}
		counts, err = h.Roles.GrantCounts(ctx, tx, projectID)
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(found, counts[found.Key])
	if err != nil {
		return nil, err
	}
	return api.GetRole200JSONResponse(rendered), nil
}

// --- update -----------------------------------------------------------------

func (h *Handler) UpdateRole(
	ctx context.Context, request api.UpdateRoleRequestObject,
) (api.UpdateRoleResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}

	projectID := request.ProjectId.String()
	roleID := request.RoleId.String()
	permissions := permissionsOf(&request.Body.PermissionKeys)

	var (
		updated Role
		counts  map[string]int
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}

		// Read first, so the audit event can record what CHANGED rather than
		// only the new state. "who gave this role billing access" is then one
		// row to read instead of two rows and a comparison.
		before, err := h.Roles.Get(ctx, tx, roleID)
		if err != nil {
			return err
		}
		if before.ProjectID != projectID {
			return ErrNotFound
		}

		updated, err = h.Roles.Update(ctx, tx, roleID, request.Body.DisplayName, permissions)
		if err != nil {
			return err
		}

		added, removed := difference(before.PermissionKeys, updated.PermissionKeys)
		if err := h.audit(ctx, tx, audit.EventRoleUpdated, updated, map[string]any{
			"project_id":          updated.ProjectID,
			"key":                 updated.Key,
			"permissions_added":   added,
			"permissions_removed": removed,
		}); err != nil {
			return err
		}

		counts, err = h.Roles.GrantCounts(ctx, tx, projectID)
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(updated, counts[updated.Key])
	if err != nil {
		return nil, err
	}
	return api.UpdateRole200JSONResponse(rendered), nil
}

// --- delete -----------------------------------------------------------------

func (h *Handler) DeleteRole(
	ctx context.Context, request api.DeleteRoleRequestObject,
) (api.DeleteRoleResponseObject, error) {
	projectID := request.ProjectId.String()
	roleID := request.RoleId.String()

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}

		existing, err := h.Roles.Get(ctx, tx, roleID)
		if err != nil {
			return err
		}
		if existing.ProjectID != projectID {
			return ErrNotFound
		}

		if err := h.Roles.Delete(ctx, tx, roleID); err != nil {
			return err
		}

		// Audited with the permissions it carried. After the row is gone, the
		// audit log is the only record of what the role could do — and "what
		// did the role we deleted last month grant" is exactly the question an
		// incident asks.
		return h.audit(ctx, tx, audit.EventRoleDeleted, existing, map[string]any{
			"project_id":      existing.ProjectID,
			"key":             existing.Key,
			"permission_keys": existing.PermissionKeys,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	return api.DeleteRole204Response{}, nil
}

// --- helpers ----------------------------------------------------------------

func render(r Role, grants int) (api.Role, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return api.Role{}, fmt.Errorf("role: rendering %q: %w", r.ID, err)
	}
	projectID, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return api.Role{}, fmt.Errorf("role: rendering project %q: %w", r.ProjectID, err)
	}

	// A fresh slice, never the store's. Handing the caller's serialiser the
	// same backing array the store returned is how a later change to one
	// becomes a change to the other.
	keys := make([]string, 0, len(r.PermissionKeys))
	keys = append(keys, r.PermissionKeys...)

	return api.Role{
		Id:             id,
		ProjectId:      projectID,
		Key:            r.Key,
		DisplayName:    r.DisplayName,
		PermissionKeys: keys,
		IsBuiltin:      r.IsBuiltin,
		GrantCount:     grants,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}, nil
}

// difference reports what a permission set gained and lost.
func difference(before, after []string) (added, removed []string) {
	had := make(map[string]struct{}, len(before))
	for _, k := range before {
		had[k] = struct{}{}
	}
	has := make(map[string]struct{}, len(after))
	for _, k := range after {
		has[k] = struct{}{}
	}

	added = []string{}
	removed = []string{}
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

// permissionsOf normalises the optional array. A nil and an empty array mean
// the same thing here — no permissions — and a role with none is a label.
func permissionsOf(in *[]api.PermissionKey) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, 0, len(*in))
	out = append(out, *in...)
	return out
}

func (h *Handler) audit(
	ctx context.Context, tx *postgres.Tx, kind audit.EventType, r Role, payload map[string]any,
) error {
	if h.Audit == nil {
		return nil
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{Type: kind, Payload: payload})
}

func (h *Handler) requireProject(ctx context.Context, tx *postgres.Tx, projectID string) error {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1)`, projectID).Scan(&exists)
	switch {
	case err != nil:
		return fmt.Errorf("role: checking the project: %w", err)
	case !exists:
		// Also the answer for another tenant's project, which RLS makes
		// invisible — so this cannot be used to ask whether an id is real.
		return ErrNotFound
	}
	return nil
}

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a role operation ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

func position(r Role) management.Cursor {
	// Ordered by key, so the position IS the key — not the id, and not a
	// timestamp. Putting the key in the id field looked equivalent and was
	// not: `DecodeCursor` requires a time alongside an id, so the second page
	// was refused as an invalid token. Found by a pagination test, which is
	// the only kind of test that reaches a second page.
	return management.KeyCursor(r.Key)
}

// faultFrom translates the store's errors into the API's.
func faultFrom(err error) error {
	var fault management.Fault
	if errors.As(err, &fault) {
		return err
	}

	switch {
	case errors.Is(err, ErrNotFound):
		return management.Fault{
			Class:   management.NotFound,
			Message: "The requested resource was not found.",
			Reason:  "no such role in this project",
		}
	case errors.Is(err, ErrBuiltin):
		return management.Fault{
			Class:   management.Conflict,
			Message: "A built-in role cannot be deleted or re-keyed.",
			Reason:  "built-in role immutability",
		}
	}

	var inUse ErrInUse
	if errors.As(err, &inUse) {
		return management.Fault{
			Class: management.Conflict,
			Message: fmt.Sprintf(
				"This role is still assigned to %d user grant(s). Remove those assignments first.", inUse.Grants),
			Reason: "role delete refused: still referenced",
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

// intParam renders an optional page size the way management.PageSize parses
// it — as the string the query carried, so "0" and "absent" stay different
// things.
func intParam(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

func stringParam(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

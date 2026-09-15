package projectgrant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Handler implements the Project Grants operations (P4-01).
//
// Every route is the GRANTING organization's, at PROJECT_OWNER over the project
// (the policy table). The organization in the path is the one the transaction is
// scoped to, and every query filters on it as the granting side — RLS alone would
// also show a receiving organization the row, and visibility is not authority.
type Handler struct {
	Grants *Store
	DB     *postgres.DB
	Audit  management.Recorder
	Log    *slog.Logger
	Now    func() time.Time
}

func New(db *postgres.DB, recorder management.Recorder, log *slog.Logger) *Handler {
	return &Handler{Grants: NewStore(), DB: db, Audit: recorder, Log: log}
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// --- list -------------------------------------------------------------------

func (h *Handler) ListProjectGrants(
	ctx context.Context, request api.ListProjectGrantsRequestObject,
) (api.ListProjectGrantsResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))
	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}
	projectID := request.ProjectId.String()

	var rows []Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		if err := requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		rows, err = h.Grants.List(ctx, tx, orgID, projectID, cursor, size)
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	page, err := management.Paginate(rows, size, func(g Grant) management.Cursor {
		return management.Cursor{After: g.CreatedAt, ID: g.ID}
	})
	if err != nil {
		return nil, err
	}
	out := api.ProjectGrantList{Grants: make([]api.ProjectGrant, 0, len(page.Items))}
	for _, g := range page.Items {
		rendered, err := render(g)
		if err != nil {
			return nil, err
		}
		out.Grants = append(out.Grants, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}
	return api.ListProjectGrants200JSONResponse(out), nil
}

// --- create -----------------------------------------------------------------

func (h *Handler) CreateProjectGrant(
	ctx context.Context, request api.CreateProjectGrantRequestObject,
) (api.CreateProjectGrantResponseObject, error) {
	if request.Body == nil {
		return nil, management.Fault{Class: management.Invalid, Message: "A request body is required.", Reason: "empty body"}
	}
	projectID := request.ProjectId.String()
	grantedOrgID := request.Body.GrantedOrgId.String()

	keys, err := roleKeys(request.Body.RoleKeys)
	if err != nil {
		return nil, err
	}

	var created Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		if err := requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		if created, err = h.Grants.Create(ctx, tx, orgID, projectID, grantedOrgID, keys); err != nil {
			return err
		}
		// Elevated visibility (docs/PLAN/08 § Least Privilege): who delegated
		// what, to whom — the complete contract, because nothing about a grant
		// can change afterwards and this row is the record of its terms.
		return h.audit(ctx, tx, audit.EventProjectGrantCreated, map[string]any{
			"grant_id":          created.ID,
			"project_id":        created.ProjectID,
			"granted_org_id":    created.GrantedOrgID,
			"granted_role_keys": created.GrantedRoleKeys,
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(created)
	if err != nil {
		return nil, err
	}
	return api.CreateProjectGrant201JSONResponse(rendered), nil
}

// --- read -------------------------------------------------------------------

func (h *Handler) GetProjectGrant(
	ctx context.Context, request api.GetProjectGrantRequestObject,
) (api.GetProjectGrantResponseObject, error) {
	projectID := request.ProjectId.String()

	var found Grant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		if err := requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		found, err = h.Grants.Get(ctx, tx, orgID, projectID, request.GrantId.String())
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(found)
	if err != nil {
		return nil, err
	}
	return api.GetProjectGrant200JSONResponse(rendered), nil
}

// --- revoke -----------------------------------------------------------------

func (h *Handler) RevokeProjectGrant(
	ctx context.Context, request api.RevokeProjectGrantRequestObject,
) (api.RevokeProjectGrantResponseObject, error) {
	projectID := request.ProjectId.String()

	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		if err := requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		revoked, changed, err := h.Grants.Revoke(ctx, tx, orgID, projectID, request.GrantId.String(), h.now())
		if err != nil {
			return err
		}
		if !changed {
			// Already revoked: a success that changes nothing, and so writes
			// nothing. The audit guard is told that is deliberate.
			management.Unchanged(ctx)
			return nil
		}
		// The role keys the delegation carried go in the event. After P4-02,
		// "what could that partner assign until last Tuesday" is answered here.
		return h.audit(ctx, tx, audit.EventProjectGrantRevoked, map[string]any{
			"grant_id":          revoked.ID,
			"project_id":        revoked.ProjectID,
			"granted_org_id":    revoked.GrantedOrgID,
			"granted_role_keys": revoked.GrantedRoleKeys,
			"revoked_at":        revoked.RevokedAt.UTC().Format(time.RFC3339),
		})
	}); err != nil {
		return nil, faultFrom(err)
	}
	return api.RevokeProjectGrant204Response{}, nil
}

// --- helpers ----------------------------------------------------------------

func (h *Handler) audit(ctx context.Context, tx *postgres.Tx, kind audit.EventType, payload map[string]any) error {
	if h.Audit == nil {
		return nil
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{Type: kind, Payload: payload})
}

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx, string) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a project grant operation ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error { return fn(tx, orgID) })
}

// requireProject answers a project that is not this organization's — including
// another tenant's, which RLS hides — as not found.
func requireProject(ctx context.Context, tx *postgres.Tx, projectID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1)`, projectID).Scan(&exists); err != nil {
		return fmt.Errorf("projectgrant: checking the project: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// roleKeys normalises the requested subset: sorted and de-duplicated, so a grant
// stores one canonical form and an audit event compares cleanly. The schema
// already refuses duplicates and bounds the count; this repeats both because a
// generated validator is not the last line.
func roleKeys(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, invalidKeys("At least one role is required.")
	}
	if len(in) > MaxRoleKeys {
		return nil, invalidKeys(fmt.Sprintf("A grant may delegate at most %d roles.", MaxRoleKeys))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, k := range in {
		if seen[k] {
			return nil, invalidKeys("The role keys must not repeat.")
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func invalidKeys(issue string) error {
	return management.Fault{
		Class: management.Invalid, Message: issue,
		Details: []api.ErrorDetail{{Field: "role_keys", Issue: issue}},
		Reason:  "invalid role_keys",
	}
}

func render(g Grant) (api.ProjectGrant, error) {
	parse := func(label, v string) (uuid.UUID, error) {
		id, err := uuid.Parse(v)
		if err != nil {
			return uuid.UUID{}, fmt.Errorf("projectgrant: rendering %s %q: %w", label, v, err)
		}
		return id, nil
	}
	id, err := parse("id", g.ID)
	if err != nil {
		return api.ProjectGrant{}, err
	}
	project, err := parse("project", g.ProjectID)
	if err != nil {
		return api.ProjectGrant{}, err
	}
	granting, err := parse("granting org", g.GrantingOrgID)
	if err != nil {
		return api.ProjectGrant{}, err
	}
	granted, err := parse("granted org", g.GrantedOrgID)
	if err != nil {
		return api.ProjectGrant{}, err
	}

	keys := append(make([]string, 0, len(g.GrantedRoleKeys)), g.GrantedRoleKeys...)
	out := api.ProjectGrant{
		Id:              id,
		ProjectId:       project,
		GrantingOrgId:   granting,
		GrantedOrgId:    granted,
		GrantedOrgName:  g.GrantedOrgName,
		GrantedRoleKeys: keys,
		HolderCount:     g.HolderCount,
		Status:          api.ProjectGrantStatus(g.Status),
		CreatedAt:       g.CreatedAt,
	}
	if g.RevokedAt != nil {
		out.RevokedAt.Set(*g.RevokedAt)
	} else {
		out.RevokedAt.SetNull()
	}
	return out, nil
}

func faultFrom(err error) error {
	var fault management.Fault
	if errors.As(err, &fault) {
		return err
	}
	var unknown UnknownRoles
	if errors.As(err, &unknown) {
		issue := "Not a role of this project: " + strings.Join(unknown.Keys, ", ")
		return management.Fault{
			Class: management.Invalid, Message: issue,
			Details: []api.ErrorDetail{{Field: "role_keys", Issue: issue}},
			Reason:  "grant names roles the project does not define",
		}
	}
	if errors.Is(err, ErrNotFound) {
		return management.Fault{
			Class: management.NotFound, Message: "The requested resource was not found.",
			Reason: "no such project grant under this project and organization",
		}
	}
	return err
}

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

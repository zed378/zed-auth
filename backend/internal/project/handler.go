package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The five project operations (P1-17).
//
// Every one of them runs inside the transaction scoped to the organization in
// the path, so cross-organization access is impossible in two independent ways:
// RLS confines the query, and P1-15's middleware already refused a caller with
// no role over that organization.
//
// The card asks for both, "verified at both the RLS and application layers",
// and they really are independent — the integration test removes the caller's
// grant to check the application layer, and reads another tenant's project by
// id through a correctly scoped transaction to check RLS.

// Handler implements the generated project operations.
type Handler struct {
	Store *Store
	DB    *postgres.DB
	Audit management.Recorder
	Log   *slog.Logger
}

// --- list -------------------------------------------------------------------------------

func (h *Handler) ListProjects(
	ctx context.Context, request api.ListProjectsRequestObject,
) (api.ListProjectsResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	var rows []Project
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		rows, err = h.Store.List(ctx, tx, cursor, size)
		return err
	}); err != nil {
		return nil, err
	}

	page, err := management.Paginate(rows, size, Position)
	if err != nil {
		return nil, err
	}

	out := api.ProjectList{Projects: make([]api.Project, 0, len(page.Items))}
	for _, p := range page.Items {
		rendered, err := render(p)
		if err != nil {
			return nil, err
		}
		out.Projects = append(out.Projects, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}

	return api.ListProjects200JSONResponse(out), nil
}

// --- create -----------------------------------------------------------------------------

func (h *Handler) CreateProject(
	ctx context.Context, request api.CreateProjectRequestObject,
) (api.CreateProjectResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}

	name, err := validName(request.Body.Name)
	if err != nil {
		return nil, err
	}

	var created Project
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		if created, err = h.Store.Create(ctx, tx, name); err != nil {
			return err
		}
		return h.write(ctx, tx, audit.EventProjectCreated, map[string]any{
			"project_id": created.ID,
			"name":       created.Name,
		})
	}); err != nil {
		return nil, err
	}

	rendered, err := render(created)
	if err != nil {
		return nil, err
	}
	return api.CreateProject201JSONResponse(rendered), nil
}

// --- read -------------------------------------------------------------------------------

func (h *Handler) GetProject(
	ctx context.Context, request api.GetProjectRequestObject,
) (api.GetProjectResponseObject, error) {
	var found Project

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		found, err = h.Store.Get(ctx, tx, request.ProjectId.String())
		return err
	}); err != nil {
		return nil, notFoundOr(err)
	}

	rendered, err := render(found)
	if err != nil {
		return nil, err
	}
	return api.GetProject200JSONResponse(rendered), nil
}

// --- update -----------------------------------------------------------------------------

func (h *Handler) UpdateProject(
	ctx context.Context, request api.UpdateProjectRequestObject,
) (api.UpdateProjectResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}
	if request.Body.Name == nil {
		// Nothing to do, and answered as the current state rather than as an
		// error: a PATCH that changes nothing is not a malformed request.
		return h.readAsUpdate(ctx, request.ProjectId.String())
	}

	name, err := validName(*request.Body.Name)
	if err != nil {
		return nil, err
	}

	var (
		before  Project
		updated Project
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		if before, err = h.Store.Get(ctx, tx, request.ProjectId.String()); err != nil {
			return err
		}
		if updated, err = h.Store.Rename(ctx, tx, request.ProjectId.String(), name); err != nil {
			return err
		}
		if before.Name == updated.Name {
			// A rename to the same name changed nothing. No event, because an
			// audit log full of "renamed Billing to Billing" is an audit log
			// nobody reads.
			return nil
		}
		return h.write(ctx, tx, audit.EventProjectUpdated, map[string]any{
			"project_id": updated.ID,
			"name":       updated.Name,
			"was":        before.Name,
		})
	}); err != nil {
		return nil, notFoundOr(err)
	}

	rendered, err := render(updated)
	if err != nil {
		return nil, err
	}
	return api.UpdateProject200JSONResponse(rendered), nil
}

func (h *Handler) readAsUpdate(ctx context.Context, id string) (api.UpdateProjectResponseObject, error) {
	var found Project
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		found, err = h.Store.Get(ctx, tx, id)
		return err
	}); err != nil {
		return nil, notFoundOr(err)
	}

	rendered, err := render(found)
	if err != nil {
		return nil, err
	}
	return api.UpdateProject200JSONResponse(rendered), nil
}

// --- delete -----------------------------------------------------------------------------

func (h *Handler) DeleteProject(
	ctx context.Context, request api.DeleteProjectRequestObject,
) (api.DeleteProjectResponseObject, error) {
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		found, err := h.Store.Get(ctx, tx, request.ProjectId.String())
		if err != nil {
			return err
		}
		if err := h.Store.Delete(ctx, tx, request.ProjectId.String()); err != nil {
			return err
		}
		return h.write(ctx, tx, audit.EventProjectDeleted, map[string]any{
			"project_id": found.ID,
			"name":       found.Name,
		})
	}); err != nil {
		return nil, blockedOr(notFoundOr(err))
	}

	return api.DeleteProject204Response{}, nil
}

// blockedOr turns "something is still attached" into an actionable refusal.
//
// **The counts are in the response.** "This project still has things in it" is
// not actionable; "4 applications and 2 roles" tells an administrator what to
// go and look at, and neither number discloses anything they cannot already
// list.
func blockedOr(err error) error {
	var blocked ErrHasDependents
	if !errors.As(err, &blocked) {
		return err
	}

	var details []api.ErrorDetail
	if blocked.Applications > 0 {
		details = append(details, api.ErrorDetail{
			Field: "applications",
			Issue: fmt.Sprintf("%d still belong to this project", blocked.Applications),
		})
	}
	if blocked.Roles > 0 {
		details = append(details, api.ErrorDetail{
			Field: "roles",
			Issue: fmt.Sprintf("%d still belong to this project", blocked.Roles),
		})
	}
	if len(details) == 0 {
		// The race path: something attached between the count and the delete.
		details = append(details, api.ErrorDetail{
			Field: "project",
			Issue: "something was attached to this project while it was being deleted",
		})
	}

	return management.Fault{
		Class:   management.Conflict,
		Message: "This project still has resources in it. Delete or move them first.",
		Details: details,
		Reason:  blocked.Error(),
	}
}

// --- shared ------------------------------------------------------------------------------

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a project operation ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

func (h *Handler) write(ctx context.Context, tx *postgres.Tx, kind audit.EventType, payload map[string]any) error {
	if h.Audit == nil {
		return nil
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{Type: kind, Payload: payload})
}

func missingBody() error {
	return management.Fault{
		Class: management.Invalid, Message: "A request body is required.", Reason: "no body",
	}
}

func notFoundOr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return management.Fault{
			Class:   management.NotFound,
			Message: "The requested resource was not found.",
			Reason:  "no such project in this organization",
		}
	}
	return err
}

func validName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return "", management.Fault{
			Class: management.Invalid, Message: "The project name is required.",
			Details: []api.ErrorDetail{{Field: "name", Issue: "must not be blank"}},
			Reason:  "blank name",
		}
	case len(trimmed) > MaxNameLength:
		return "", management.Fault{
			Class: management.Invalid, Message: "The project name is too long.",
			Details: []api.ErrorDetail{{
				Field: "name",
				Issue: fmt.Sprintf("must be at most %d characters", MaxNameLength),
			}},
			Reason: "name over the length bound",
		}
	}
	return trimmed, nil
}

func render(p Project) (api.Project, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return api.Project{}, fmt.Errorf("project: %q is not a uuid: %w", p.ID, err)
	}
	return api.Project{
		Id: id, Name: p.Name, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}, nil
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

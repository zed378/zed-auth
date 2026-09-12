package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The five organization operations (P1-16).
//
// Each is short, and that is the point: authentication, permissions, tenant
// scoping, the error envelope, idempotency, rate limiting and the audit guard
// all happened before the handler ran (P1-15). What is left here is the thing
// the endpoint is actually about.
//
// Errors are RETURNED rather than rendered. The strict handler's
// ResponseErrorHandlerFunc turns a management.Fault into docs/PLAN/05's
// envelope, so a handler never picks a status code — which is what stops two
// endpoints answering the same condition differently.

// Handler implements the generated organization operations.
type Handler struct {
	Store *Store
	DB    *postgres.DB
	Audit management.Recorder
	Log   *slog.Logger

	// Sessions revokes everything belonging to an organization being deleted.
	// Optional: nil means deletion does not revoke, which is logged loudly
	// rather than silently accepted.
	Sessions Revoker

	Now func() time.Time
}

// Revoker ends every session and refresh token in an organization.
//
// A deleted tenant whose users keep working until their tokens expire is a
// tenant that is only deleted in the console (abuse case A-8).
type Revoker interface {
	RevokeOrganization(ctx context.Context, tx *postgres.Tx, orgID string) (int64, error)
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Handler) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

// --- list ----------------------------------------------------------------------------

func (h *Handler) ListOrganizations(
	ctx context.Context, request api.ListOrganizationsRequestObject,
) (api.ListOrganizationsResponseObject, error) {
	size := management.PageSize(pageSizeParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(pageTokenParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	rows, err := h.Store.List(ctx, h.DB, cursor, size)
	if err != nil {
		return nil, err
	}

	page, err := management.Paginate(rows, size, Position)
	if err != nil {
		return nil, err
	}

	out := api.OrganizationList{Organizations: make([]api.Organization, 0, len(page.Items))}
	for _, o := range page.Items {
		rendered, err := render(o)
		if err != nil {
			return nil, err
		}
		out.Organizations = append(out.Organizations, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}

	return api.ListOrganizations200JSONResponse(out), nil
}

// --- create ----------------------------------------------------------------------------

func (h *Handler) CreateOrganization(
	ctx context.Context, request api.CreateOrganizationRequestObject,
) (api.CreateOrganizationResponseObject, error) {
	if request.Body == nil {
		return nil, management.Fault{
			Class: management.Invalid, Message: "A request body is required.",
			Reason: "no body",
		}
	}

	name, err := validName(request.Body.Name)
	if err != nil {
		return nil, err
	}

	settings, err := settingsFrom(ctx)
	if err != nil {
		return nil, err
	}

	created, err := h.Store.Create(ctx, h.DB, NewOrganization{
		Name:     name,
		Domain:   nullableToPointer(request.Body.Domain),
		Settings: settings,
	})
	if err != nil {
		return nil, err
	}

	if err := h.record(ctx, created.ID, audit.EventOrganizationCreated, map[string]any{
		"organization_id": created.ID,
		"name":            created.Name,
	}); err != nil {
		return nil, err
	}

	rendered, err := render(created)
	if err != nil {
		return nil, err
	}
	return api.CreateOrganization201JSONResponse(rendered), nil
}

// --- read ------------------------------------------------------------------------------

func (h *Handler) GetOrganization(
	ctx context.Context, _ api.GetOrganizationRequestObject,
) (api.GetOrganizationResponseObject, error) {
	// The id in the path is NOT used to look the row up.
	//
	// P1-15's middleware already scoped the transaction to it, and the row RLS
	// lets this transaction see IS the one addressed. Passing the id again
	// would be a second, independent way to name the target — and two ways to
	// name a target is how one of them ends up not being checked.
	var found Organization

	err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		found, err = h.Store.Get(ctx, tx)
		return err
	})
	if err != nil {
		return nil, notFoundOr(err)
	}

	rendered, err := render(found)
	if err != nil {
		return nil, err
	}
	return api.GetOrganization200JSONResponse(rendered), nil
}

// --- update ----------------------------------------------------------------------------

func (h *Handler) UpdateOrganization(
	ctx context.Context, request api.UpdateOrganizationRequestObject,
) (api.UpdateOrganizationResponseObject, error) {
	if request.Body == nil {
		return nil, management.Fault{
			Class: management.Invalid, Message: "A request body is required.",
			Reason: "no body",
		}
	}

	changes := Changes{}

	if request.Body.Name != nil {
		name, err := validName(*request.Body.Name)
		if err != nil {
			return nil, err
		}
		changes.Name = &name
	}

	if request.Body.Domain.IsSpecified() {
		domain := nullableToPointer(request.Body.Domain)
		changes.Domain = &domain
	}

	if request.Body.Status != nil {
		// **The field-level rule.** Suspending an organization locks out every
		// user in it, so it needs INSTANCE_OWNER — which the route's own
		// requirement (ORG_OWNER) does not imply.
		//
		// Route-level authorization cannot express "depends what is in the
		// body", so it is checked here. Through the same Authorize the
		// middleware uses, so there is one implementation of "does this caller
		// satisfy this role" rather than two that can drift.
		if err := requireInstanceOwner(ctx); err != nil {
			return nil, err
		}
		status := string(*request.Body.Status)
		changes.Status = &status
	}

	settings, err := settingsFrom(ctx)
	if err != nil {
		return nil, err
	}
	changes.Settings = settings

	var (
		before  Organization
		updated Organization
	)
	err = h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		if before, err = h.Store.Get(ctx, tx); err != nil {
			return err
		}
		if updated, err = h.Store.Update(ctx, tx, changes); err != nil {
			return err
		}
		return h.write(ctx, tx, eventFor(before, updated), changedPayload(before, updated))
	})
	if err != nil {
		return nil, notFoundOr(err)
	}

	rendered, err := render(updated)
	if err != nil {
		return nil, err
	}
	return api.UpdateOrganization200JSONResponse(rendered), nil
}

// --- delete ----------------------------------------------------------------------------

func (h *Handler) DeleteOrganization(
	ctx context.Context, request api.DeleteOrganizationRequestObject,
) (api.DeleteOrganizationResponseObject, error) {
	var revoked int64

	err := h.inScope(ctx, func(tx *postgres.Tx) error {
		current, err := h.Store.Get(ctx, tx)
		if err != nil {
			return err
		}

		// **The confirmation, checked on the server.**
		//
		// docs/UI-UX/07 specifies typed confirmation in the console, and a UI
		// affordance is not a control: a script that deletes the wrong
		// organization never goes near the console. Compared after trimming
		// only — case must match, because "acme corp" and "Acme Corp" being
		// interchangeable defeats the point of typing it back.
		if strings.TrimSpace(request.Params.ConfirmName) != current.Name {
			return management.Fault{
				Class:   management.Invalid,
				Message: "The confirmation does not match the organization's name.",
				Details: []api.ErrorDetail{{
					Field: "confirm_name",
					Issue: "must exactly match the organization's current name",
				}},
				Reason: "delete confirmation mismatch",
			}
		}

		if h.Sessions != nil {
			// Otherwise a deleted tenant's users keep working until their
			// tokens expire, which is a tenant deleted only in the console
			// (abuse case A-8).
			if revoked, err = h.Sessions.RevokeOrganization(ctx, tx, current.ID); err != nil {
				return err
			}
		} else {
			h.log().Error("no session revoker is configured; a deleted organization's users keep working until their tokens expire",
				"organization_id", current.ID)
		}

		if err := h.Store.SoftDelete(ctx, tx, h.now()); err != nil {
			return err
		}

		return h.write(ctx, tx, audit.EventOrganizationDeleted, map[string]any{
			"organization_id":  current.ID,
			"name":             current.Name,
			"sessions_revoked": revoked,
			"domain_released":  current.Domain != nil,
		})
	})
	if err != nil {
		return nil, notFoundOr(err)
	}

	return api.DeleteOrganization204Response{}, nil
}

// --- shared ------------------------------------------------------------------------------

// inScope runs fn in the transaction the request is scoped to.
func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a single-organization operation ran with no scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

// record opens a transaction of its own to write an event.
//
// Used only by Create, whose work happened in a SECURITY DEFINER function
// rather than in a transaction this handler holds. Every other operation writes
// its event INSIDE the transaction that made the change, so the change and its
// record commit together.
func (h *Handler) record(ctx context.Context, orgID string, kind audit.EventType, payload map[string]any) error {
	if h.Audit == nil {
		return nil
	}
	return h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return management.Audit(ctx, h.Audit, tx, audit.Event{
			OrgID: orgID, Type: kind, Payload: payload,
		})
	})
}

func (h *Handler) write(ctx context.Context, tx *postgres.Tx, kind audit.EventType, payload map[string]any) error {
	if h.Audit == nil {
		return nil
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{Type: kind, Payload: payload})
}

// requireInstanceOwner checks a field-level permission.
func requireInstanceOwner(ctx context.Context) error {
	caller, ok := management.CallerFrom(ctx)
	if !ok {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "no caller in context",
		}
	}

	decision := management.Authorize(caller,
		management.Requirement{Role: management.InstanceOwner, Scope: management.ScopeInstance},
		management.Target{})
	if decision.Allowed {
		return nil
	}

	// PERMISSION_DENIED rather than NOT_FOUND: the caller has already been let
	// through the route's own check, so they can see this organization. Hiding
	// it now would be incoherent.
	return management.Fault{
		Class:   management.Forbidden,
		Message: "Changing an organization's status requires instance-level permission.",
		Reason:  "status change without INSTANCE_OWNER: " + decision.Reason,
	}
}

// settingsFrom pulls the `settings` document out of the RAW request body.
//
// Not from the decoded struct, and that is the whole reason RawBody exists:
// `additionalProperties: false` is not enforced at runtime, so json.Unmarshal
// silently discards `mfa_requried` and the caller would be told their policy
// was saved.
func settingsFrom(ctx context.Context) (json.RawMessage, error) {
	raw, ok := management.RawBody(ctx)
	if !ok {
		return nil, management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "the request body was not buffered, so unknown settings keys cannot be detected",
		}
	}
	if len(raw) == 0 {
		return nil, nil
	}

	var body struct {
		Settings json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, management.Fault{
			Class: management.Invalid, Message: "The request body could not be read.",
			Reason: "body did not decode: " + err.Error(),
		}
	}
	if len(body.Settings) == 0 {
		return nil, nil
	}

	if _, err := ValidateSettings(body.Settings); err != nil {
		return nil, err
	}
	return body.Settings, nil
}

func validName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return "", management.Fault{
			Class: management.Invalid, Message: "The organization name is required.",
			Details: []api.ErrorDetail{{Field: "name", Issue: "must not be blank"}},
			Reason:  "blank name",
		}
	case len(trimmed) > MaxNameLength:
		return "", management.Fault{
			Class: management.Invalid, Message: "The organization name is too long.",
			Details: []api.ErrorDetail{{
				Field: "name",
				Issue: fmt.Sprintf("must be at most %d characters", MaxNameLength),
			}},
			Reason: "name over the length bound",
		}
	}
	return trimmed, nil
}

// notFoundOr turns the store's not-found into the API's.
func notFoundOr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return management.Fault{
			Class:   management.NotFound,
			Message: "The requested resource was not found.",
			Reason:  "no live organization in scope",
		}
	}
	return err
}

// eventFor picks the event a change deserves.
//
// A suspension and a reactivation are recorded as themselves rather than as
// updates, because "who un-suspended this tenant, and when" is asked during an
// incident and answering it should not require diffing two updates.
func eventFor(before, after Organization) audit.EventType {
	switch {
	case before.Status != after.Status && after.Status == StatusSuspended:
		return audit.EventOrganizationSuspended
	case before.Status != after.Status && after.Status == StatusActive:
		return audit.EventOrganizationReactivated
	default:
		return audit.EventOrganizationUpdated
	}
}

// changedPayload names what changed, and for settings says what it changed TO.
//
// The same exception EventApplicationUpdated makes for redirect URIs, and for
// the same reason: "settings changed" cannot answer the question the log exists
// for. If somebody with admin access turns mfa_required off, this is the only
// place that shows it.
func changedPayload(before, after Organization) map[string]any {
	payload := map[string]any{"organization_id": after.ID}

	var changed []string
	if before.Name != after.Name {
		changed = append(changed, "name")
		payload["name"] = after.Name
	}
	if !sameDomain(before.Domain, after.Domain) {
		changed = append(changed, "domain")
		payload["domain"] = after.Domain
	}
	if before.Status != after.Status {
		changed = append(changed, "status")
		payload["status"] = after.Status
	}
	if string(before.Settings) != string(after.Settings) {
		changed = append(changed, "settings")
		payload["settings"] = json.RawMessage(after.Settings)
	}

	payload["changed"] = changed
	return payload
}

func sameDomain(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// --- rendering -----------------------------------------------------------------------------

func render(o Organization) (api.Organization, error) {
	var settings api.OrganizationSettings
	if len(o.Settings) > 0 {
		if err := json.Unmarshal(o.Settings, &settings); err != nil {
			return api.Organization{}, fmt.Errorf("organization: rendering settings: %w", err)
		}
	}

	// The database hands back a canonical UUID string; the contract types it as
	// a UUID. A parse failure here would mean the column stopped being a uuid,
	// which is worth an error rather than a zero value that renders as
	// 00000000-0000-0000-0000-000000000000 and looks like a real id.
	id, err := uuid.Parse(o.ID)
	if err != nil {
		return api.Organization{}, fmt.Errorf("organization: %q is not a uuid: %w", o.ID, err)
	}

	out := api.Organization{
		Id:        id,
		Name:      o.Name,
		Status:    api.OrganizationStatus(o.Status),
		Settings:  settings,
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}
	if o.Domain != nil {
		out.Domain = nullable.NewNullableWithValue(*o.Domain)
	} else {
		out.Domain = nullable.NewNullNullable[string]()
	}
	return out, nil
}

func nullableToPointer(n nullable.Nullable[string]) *string {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v, err := n.Get()
	if err != nil {
		return nil
	}
	return &v
}

func pageSizeParam(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

func pageTokenParam(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

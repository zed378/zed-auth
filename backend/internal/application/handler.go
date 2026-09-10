// Package application implements the Management API's application endpoints
// (P1-18).
//
// An application is a registered OIDC client. Almost nothing about one is
// decided here: P1-05 already owns secret generation and hashing, redirect URI
// canonicalisation, the grant-type rules and the audit events. What this
// package adds is the HTTP surface and two boundaries the store cannot know
// about on its own — the project in the path, and P1-15's audit guard.
package application

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
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// MaxNameLength bounds a display name, matching the contract.
const MaxNameLength = 200

// MaxOverlapHours bounds a rotation window, matching the contract.
//
// A week. Long enough for any deployment cadence; short enough that a window
// opened because a secret leaked cannot be left open for a quarter.
const MaxOverlapHours = 168

// Handler implements the generated application operations.
type Handler struct {
	Clients *client.Store
	DB      *postgres.DB
	Log     *slog.Logger
}

// New builds a handler whose store audits through P1-15's guard.
//
// The store is constructed here rather than injected, because *which recorder
// it holds* is the security-relevant part and this is where that decision
// belongs. A store handed in from elsewhere might be the one the token
// endpoint uses, which writes straight at the audit writer — correct there,
// and wrong here, because P1-15's guard would then report every successful
// application mutation as unaudited.
func New(db *postgres.DB, recorder management.Recorder, log *slog.Logger) *Handler {
	return &Handler{
		Clients: client.NewStore(guarded{recorder}),
		DB:      db,
		Log:     log,
	}
}

// guarded routes the store's own audit events through management.Audit, which
// fills in the actor, organization, request id and client IP from the request
// and — the part that matters — marks the per-request trail P1-15's guard
// checks afterwards.
type guarded struct{ inner management.Recorder }

func (g guarded) Write(ctx context.Context, tx *postgres.Tx, e audit.Event) error {
	if g.inner == nil {
		return nil
	}
	return management.Audit(ctx, g.inner, tx, e)
}

// --- list -------------------------------------------------------------------------------

func (h *Handler) ListApplications(
	ctx context.Context, request api.ListApplicationsRequestObject,
) (api.ListApplicationsResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))

	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	projectID := request.ProjectId.String()

	var rows []client.Record
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		rows, err = h.Clients.List(ctx, tx, projectID, cursor.After, cursor.ID, size)
		return err
	}); err != nil {
		return nil, err
	}

	page, err := management.Paginate(rows, size, position)
	if err != nil {
		return nil, err
	}

	out := api.ApplicationList{Applications: make([]api.Application, 0, len(page.Items))}
	for _, rec := range page.Items {
		rendered, err := render(rec)
		if err != nil {
			return nil, err
		}
		out.Applications = append(out.Applications, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}

	return api.ListApplications200JSONResponse(out), nil
}

// --- create -----------------------------------------------------------------------------

func (h *Handler) CreateApplication(
	ctx context.Context, request api.CreateApplicationRequestObject,
) (api.CreateApplicationResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}

	name, err := validName(request.Body.Name)
	if err != nil {
		return nil, err
	}

	kind := client.Type(request.Body.Type)
	if !kind.Valid() {
		return nil, management.Fault{
			Class: management.Invalid, Message: "That application type is not recognised.",
			Details: []api.ErrorDetail{{Field: "type", Issue: "must be one of web, native, spa, api, saml"}},
			Reason:  "unknown application type",
		}
	}

	projectID := request.ProjectId.String()
	app := client.Application{
		ProjectID:              projectID,
		Name:                   name,
		Type:                   kind,
		RedirectURIs:           values(request.Body.RedirectUris),
		PostLogoutRedirectURIs: values(request.Body.PostLogoutRedirectUris),
		GrantTypes:             defaultGrants(values(request.Body.GrantTypes)),
	}

	var (
		created client.Record
		secret  client.Secret
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if err := h.requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		// The organization comes from the transaction's own scope, never from
		// the body — P1-17's rule, and it holds for every nested resource.
		app.OrgID = tx.OrgID()

		var err error
		created, secret, err = h.Clients.Create(ctx, tx, app, actor(ctx))
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	body, err := render(created)
	if err != nil {
		return nil, err
	}

	out := api.ApplicationCreated{
		Id: body.Id, ProjectId: body.ProjectId, Name: body.Name, Type: body.Type,
		RedirectUris: body.RedirectUris, PostLogoutRedirectUris: body.PostLogoutRedirectUris,
		GrantTypes: body.GrantTypes, HasSecret: body.HasSecret,
		PreviousSecretExpiresAt: body.PreviousSecretExpiresAt,
		CreatedAt:               body.CreatedAt, UpdatedAt: body.UpdatedAt,
	}
	// The one place in the service where a secret is revealed, alongside
	// rotation. Reveal() is an explicit call for exactly this reason: every
	// other path holds a Secret that renders as [REDACTED].
	if !secret.IsZero() {
		plaintext := secret.Reveal()
		out.ClientSecret = &plaintext
	}

	return api.CreateApplication201JSONResponse(out), nil
}

// --- read -------------------------------------------------------------------------------

func (h *Handler) GetApplication(
	ctx context.Context, request api.GetApplicationRequestObject,
) (api.GetApplicationResponseObject, error) {
	rec, err := h.resolve(ctx, request.ProjectId.String(), request.ApplicationId.String())
	if err != nil {
		return nil, err
	}

	rendered, err := render(rec)
	if err != nil {
		return nil, err
	}
	return api.GetApplication200JSONResponse(rendered), nil
}

// --- update -----------------------------------------------------------------------------

func (h *Handler) UpdateApplication(
	ctx context.Context, request api.UpdateApplicationRequestObject,
) (api.UpdateApplicationResponseObject, error) {
	if request.Body == nil {
		return nil, missingBody()
	}
	if err := refuseImmutableFields(ctx); err != nil {
		return nil, err
	}

	projectID := request.ProjectId.String()
	id := request.ApplicationId.String()

	var updated client.Record
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		before, err := h.Clients.GetInProject(ctx, tx, id, projectID)
		if err != nil {
			return err
		}

		// An omitted field keeps its stored value. Passing the zero value
		// instead would make a PATCH of the name silently clear every
		// redirect URI, which is an outage delivered by a rename.
		app := client.Application{
			Name:                   before.Name,
			RedirectURIs:           before.RedirectURIs,
			PostLogoutRedirectURIs: before.PostLogoutRedirectURIs,
			GrantTypes:             before.GrantTypes,
		}
		if request.Body.Name != nil {
			if app.Name, err = validName(*request.Body.Name); err != nil {
				return err
			}
		}
		if request.Body.RedirectUris != nil {
			app.RedirectURIs = *request.Body.RedirectUris
		}
		if request.Body.PostLogoutRedirectUris != nil {
			app.PostLogoutRedirectURIs = *request.Body.PostLogoutRedirectUris
		}
		if request.Body.GrantTypes != nil {
			app.GrantTypes = *request.Body.GrantTypes
		}

		updated, err = h.Clients.Update(ctx, tx, id, app, actor(ctx))
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	rendered, err := render(updated)
	if err != nil {
		return nil, err
	}
	return api.UpdateApplication200JSONResponse(rendered), nil
}

// refuseImmutableFields rejects a body naming something that cannot change.
//
// From the RAW body, not the decoded struct: `additionalProperties: false` is
// documentation and json.Unmarshal discards what it does not recognise, so a
// caller sending {"type":"spa"} would get a 200 and a response still saying
// "web" — told that a reclassification happened when it did not (P1-16).
func refuseImmutableFields(ctx context.Context) error {
	raw, ok := management.RawBody(ctx)
	if !ok {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "the request body was not buffered, so immutable fields cannot be detected",
		}
	}
	if len(raw) == 0 {
		return nil
	}

	var sent map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sent); err != nil {
		return management.Fault{
			Class: management.Invalid, Message: "The request body could not be read.",
			Reason: "body did not decode: " + err.Error(),
		}
	}

	// Every field of an application that a PATCH may not touch. `id` and
	// `project_id` are here for the same reason `type` is: silently ignoring
	// them tells a caller a move happened.
	var refused []api.ErrorDetail
	for _, field := range []string{"type", "id", "project_id", "org_id", "client_secret", "has_secret"} {
		if _, present := sent[field]; present {
			refused = append(refused, api.ErrorDetail{
				Field: field,
				Issue: "cannot be changed after the application is registered",
			})
		}
	}
	if len(refused) == 0 {
		return nil
	}

	return management.Fault{
		Class: management.Invalid,
		Message: "An application's type and identity cannot be changed. " +
			"Delete it and register a new one.",
		Details: refused,
		Reason:  "immutable field in an update body",
	}
}

// --- delete -----------------------------------------------------------------------------

func (h *Handler) DeleteApplication(
	ctx context.Context, request api.DeleteApplicationRequestObject,
) (api.DeleteApplicationResponseObject, error) {
	projectID := request.ProjectId.String()
	id := request.ApplicationId.String()

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if _, err := h.Clients.GetInProject(ctx, tx, id, projectID); err != nil {
			return err
		}
		return h.Clients.Delete(ctx, tx, id, actor(ctx))
	}); err != nil {
		return nil, faultFrom(err)
	}

	return api.DeleteApplication204Response{}, nil
}

// --- rotate -----------------------------------------------------------------------------

func (h *Handler) RotateApplicationSecret(
	ctx context.Context, request api.RotateApplicationSecretRequestObject,
) (api.RotateApplicationSecretResponseObject, error) {
	overlap := client.DefaultRotationOverlap
	if request.Params.OverlapHours != nil {
		hours := *request.Params.OverlapHours
		if hours < 0 || hours > MaxOverlapHours {
			return nil, management.Fault{
				Class: management.Invalid, Message: "That overlap is out of range.",
				Details: []api.ErrorDetail{{
					Field: "overlap_hours",
					Issue: fmt.Sprintf("must be between 0 and %d", MaxOverlapHours),
				}},
				Reason: "overlap outside the permitted window",
			}
		}
		// Zero is meaningful and must survive being zero: it retires the
		// previous secret immediately, which is the whole compromised-
		// credential path. Treating it as "unset" and substituting the
		// 24-hour default would leave a leaked secret live for a day.
		overlap = time.Duration(hours) * time.Hour
	}

	projectID := request.ProjectId.String()
	id := request.ApplicationId.String()

	var (
		secret  client.Secret
		expires time.Time
	)
	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		if _, err := h.Clients.GetInProject(ctx, tx, id, projectID); err != nil {
			return err
		}
		var err error
		secret, expires, err = h.Clients.RotateSecret(ctx, tx, id, overlap, time.Now(), actor(ctx))
		return err
	}); err != nil {
		return nil, faultFrom(err)
	}

	plaintext := secret.Reveal()
	out := api.RotatedSecret{ClientSecret: plaintext}
	if !expires.IsZero() {
		out.PreviousSecretExpiresAt = nullable.NewNullableWithValue(expires)
	}

	return api.RotateApplicationSecret200JSONResponse(out), nil
}

// --- shared ------------------------------------------------------------------------------

// resolve reads one application, enforcing both containers in the path.
func (h *Handler) resolve(ctx context.Context, projectID, id string) (client.Record, error) {
	var rec client.Record
	err := h.inScope(ctx, func(tx *postgres.Tx) error {
		var err error
		rec, err = h.Clients.GetInProject(ctx, tx, id, projectID)
		return err
	})
	if err != nil {
		return client.Record{}, faultFrom(err)
	}
	return rec, nil
}

// requireProject turns a project that is not in this organization into a 404.
//
// Without it, listing a nonexistent project's applications answers an empty
// page — a 200 saying "there are none here" for a container that does not
// exist, which reads as a working endpoint and is how a typo in an id becomes
// twenty minutes of confusion.
//
// A bare existence query rather than a call into the project package. That is
// not a shortcut: **this package depending on that one produced an import
// cycle** the moment P1-17's in-package test harness needed a handler from
// here, and the dependency bought nothing — the question is "is there a row",
// which RLS already scopes to the caller's tenant, not "give me a Project".
func (h *Handler) requireProject(ctx context.Context, tx *postgres.Tx, projectID string) error {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1)`, projectID).Scan(&exists)
	switch {
	case err != nil:
		return fmt.Errorf("application: checking the project: %w", err)
	case !exists:
		// Also the answer for another tenant's project, which RLS makes
		// invisible — so this cannot be used to ask whether an id is real.
		return notFound("no such project in this organization")
	}
	return nil
}

func (h *Handler) inScope(ctx context.Context, fn func(*postgres.Tx) error) error {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "an application operation ran with no tenant scope",
		}
	}
	return h.DB.WithTenant(ctx, orgID, fn)
}

func actor(ctx context.Context) string {
	caller, _ := management.CallerFrom(ctx)
	return caller.UserID
}

func position(rec client.Record) management.Cursor {
	return management.Cursor{After: rec.CreatedAt, ID: rec.ID}
}

// faultFrom translates the store's errors into the API's.
func faultFrom(err error) error {
	var fault management.Fault
	switch {
	case err == nil:
		return nil
	case errors.As(err, &fault):
		return err
	case errors.Is(err, client.ErrNotFound):
		return notFound("no such application in this project")

	case errors.Is(err, client.ErrInvalid):
		// A caller mistake: a wildcard redirect URI, a grant type OAuth 2.1
		// removed, a blank name. It is a 400 carrying the reason, not a 500
		// that hides it.
		//
		// **By sentinel.** The first version of this matched on the error
		// text, and every one of these arrived as a 500 — a string check
		// standing in for a type, which is what P1-05 now has instead.
		return management.Fault{
			Class: management.Invalid, Message: "The application could not be saved.",
			Details: []api.ErrorDetail{{Field: "application", Issue: cleanup(err)}},
			Reason:  err.Error(),
		}

	case errors.Is(err, client.ErrSecretNotSet), errors.Is(err, client.ErrPublicClientSecret):
		return management.Fault{
			Class: management.Conflict,
			Message: "This application has no client secret to rotate. " +
				"Public clients use PKCE instead.",
			Reason: "rotation requested for a client with no secret",
		}
	}
	return err
}

// cleanup strips the sentinel's own text, leaving the part that tells the
// caller what to change.
func cleanup(err error) string {
	return strings.TrimPrefix(err.Error(), client.ErrInvalid.Error()+": ")
}

func notFound(reason string) error {
	return management.Fault{
		Class:   management.NotFound,
		Message: "The requested resource was not found.",
		Reason:  reason,
	}
}

func missingBody() error {
	return management.Fault{
		Class: management.Invalid, Message: "A request body is required.", Reason: "no body",
	}
}

func validName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return "", management.Fault{
			Class: management.Invalid, Message: "The application name is required.",
			Details: []api.ErrorDetail{{Field: "name", Issue: "must not be blank"}},
			Reason:  "blank name",
		}
	case len(trimmed) > MaxNameLength:
		return "", management.Fault{
			Class: management.Invalid, Message: "The application name is too long.",
			Details: []api.ErrorDetail{{
				Field: "name",
				Issue: fmt.Sprintf("must be at most %d characters", MaxNameLength),
			}},
			Reason: "name over the length bound",
		}
	}
	return trimmed, nil
}

// defaultGrants supplies the contract's default when the caller sends none.
func defaultGrants(grants []string) []string {
	if len(grants) == 0 {
		return []string{client.GrantAuthorizationCode, client.GrantRefreshToken}
	}
	return grants
}

// render turns a stored record into the API resource.
//
// **There is no branch here that could emit a secret.** Record carries only
// hashes — client.Credentials has no plaintext field — so this is a property of
// the types rather than a rule this function follows.
func render(rec client.Record) (api.Application, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return api.Application{}, fmt.Errorf("application: %q is not a uuid: %w", rec.ID, err)
	}
	projectID, err := uuid.Parse(rec.ProjectID)
	if err != nil {
		return api.Application{}, fmt.Errorf("application: %q is not a uuid: %w", rec.ProjectID, err)
	}

	out := api.Application{
		Id:        id,
		ProjectId: projectID,
		Name:      rec.Name,
		Type:      api.ApplicationType(rec.Type),
		// Never nil: a nil slice marshals as null, and a caller reading
		// redirect_uris as null rather than [] has to handle two empties.
		RedirectUris:           nonNil(rec.RedirectURIs),
		PostLogoutRedirectUris: nonNil(rec.PostLogoutRedirectURIs),
		GrantTypes:             nonNil(rec.GrantTypes),
		HasSecret:              rec.Credentials.HasSecret(),
		CreatedAt:              rec.CreatedAt,
		UpdatedAt:              rec.UpdatedAt,
	}
	if !rec.Credentials.PreviousExpiresAt.IsZero() {
		out.PreviousSecretExpiresAt = nullable.NewNullableWithValue(rec.Credentials.PreviousExpiresAt)
	}
	return out, nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func values(in *[]string) []string {
	if in == nil {
		return nil
	}
	return *in
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

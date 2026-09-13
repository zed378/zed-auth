// Package sessionapi is the Management API's view of sign-in sessions (P3-09).
//
// Specification: MEMORY/specs/P3-09-session-management-api.md.
//
// Five operations over one resource, reached two ways: `/v1/me/sessions` for a
// person's own, and `/v1/organizations/{org_id}/users/{user_id}/sessions` for
// an administrator. The two share every piece of behaviour except where the
// user id comes from — and that difference is the security property, so it is
// the one thing each entry point decides for itself.
package sessionapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/zed378/zed-auth/backend/internal/anomaly"
	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/user"
)

// Sessions is what this API does with sessions. Satisfied by session.Manager.
type Sessions interface {
	ListLive(ctx context.Context, tx *postgres.Tx, userID string, policy session.Policy, now time.Time,
		after session.ListPosition, limit int) ([]session.Session, error)
	RevokeOwned(ctx context.Context, tx *postgres.Tx, sessionID, userID string,
		now time.Time) (session.Revocation, error)
	RevokeOthers(ctx context.Context, tx *postgres.Tx, userID, keep string,
		now time.Time) (session.Revocation, error)
}

// RefreshRevoker ends the refresh tokens issued through sessions. Satisfied by
// token.RefreshStore.
type RefreshRevoker interface {
	RevokeForSessions(ctx context.Context, tx *postgres.Tx, sessionIDs []string) (int64, error)
}

// Members confirms a user exists in the scoped organization. Satisfied by
// user.Store.
type Members interface {
	Get(ctx context.Context, tx *postgres.Tx, id string) (user.User, error)
}

// Tenant runs work inside one organization.
type Tenant interface {
	WithTenant(ctx context.Context, orgID string, fn func(*postgres.Tx) error) error
}

// Handler implements the five generated operations.
type Handler struct {
	Sessions Sessions
	Refresh  RefreshRevoker
	Members  Members
	DB       Tenant

	// Audit records every revocation, through management.Audit so the event
	// carries the actor, the request id and the client address. Required: a
	// revocation nobody can trace is an incident nobody can reconstruct.
	Audit management.Recorder

	// Locator derives a coarse place from a stored address. Nil means no
	// geolocation database (PG-41), and every location is null.
	Locator anomaly.Locator

	// Policy decides which sessions are still usable — the idle timeout in
	// particular, so the list never offers a session Lookup would refuse.
	Policy session.Policy

	Log *slog.Logger
	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// --- the caller's own ------------------------------------------------------------------

// ListMySessions lists the caller's sessions.
//
// The user and the organization come from the token and from nowhere else.
// There is no parameter that names a user, so there is nothing to tamper with.
func (h *Handler) ListMySessions(
	ctx context.Context, request api.ListMySessionsRequestObject,
) (api.ListMySessionsResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}

	list, err := h.list(ctx, caller.OrgID, caller.UserID, caller.SessionID,
		request.Params.PageSize, request.Params.PageToken, false)
	if err != nil {
		return nil, err
	}
	return api.ListMySessions200JSONResponse(list), nil
}

// RevokeMySession ends one of the caller's sessions.
func (h *Handler) RevokeMySession(
	ctx context.Context, request api.RevokeMySessionRequestObject,
) (api.RevokeMySessionResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.revokeOne(ctx, caller.OrgID, caller.UserID, request.SessionId.String(),
		session.ReasonSelfService, caller.UserID); err != nil {
		return nil, err
	}
	return api.RevokeMySession204Response{}, nil
}

// RevokeMyOtherSessions ends every session but the caller's current one.
func (h *Handler) RevokeMyOtherSessions(
	ctx context.Context, _ api.RevokeMyOtherSessionsRequestObject,
) (api.RevokeMyOtherSessionsResponseObject, error) {
	caller, err := self(ctx)
	if err != nil {
		return nil, err
	}

	if caller.SessionID == "" {
		// A token with no session has no "current" to keep. Refused, never
		// read as "end all of them": that is a larger action than the one
		// asked for, and the caller has a separate way to take it.
		return nil, management.Fault{
			Class:   management.Invalid,
			Message: "This token is not tied to a sign-in session, so there is no current session to keep.",
			Reason:  "revoke-others with a token that carries no sid",
		}
	}

	var (
		revocation session.Revocation
		now        = h.now()
	)
	if err := h.DB.WithTenant(ctx, caller.OrgID, func(tx *postgres.Tx) error {
		var err error
		revocation, err = h.Sessions.RevokeOthers(ctx, tx, caller.UserID, caller.SessionID, now)
		if err != nil {
			return err
		}
		refreshRevoked, err := h.Refresh.RevokeForSessions(ctx, tx, revocation.SessionIDs)
		if err != nil {
			return err
		}
		if len(revocation.SessionIDs) == 0 {
			// No other sessions. Nothing changed and nothing is recorded.
			management.Unchanged(ctx)
			return nil
		}
		return h.audit(ctx, tx, caller.OrgID, map[string]any{
			"session_ids":            revocation.SessionIDs,
			"user_id":                caller.UserID,
			"kept":                   caller.SessionID,
			"reason":                 string(session.ReasonLogoutOthers),
			"count":                  len(revocation.SessionIDs),
			"refresh_tokens_revoked": refreshRevoked,
		})
	}); err != nil {
		return nil, internal("revoking other sessions", err)
	}

	if err := invalidate(revocation); err != nil {
		return nil, err
	}
	return api.RevokeMyOtherSessions200JSONResponse{Revoked: len(revocation.SessionIDs)}, nil
}

// --- an administrator's view -------------------------------------------------------------

// ListUserSessions lists a member's sessions.
func (h *Handler) ListUserSessions(
	ctx context.Context, request api.ListUserSessionsRequestObject,
) (api.ListUserSessionsResponseObject, error) {
	orgID, err := scoped(ctx)
	if err != nil {
		return nil, err
	}
	caller, _ := management.CallerFrom(ctx)

	list, err := h.list(ctx, orgID, request.UserId.String(), caller.SessionID,
		request.Params.PageSize, request.Params.PageToken, true)
	if err != nil {
		return nil, err
	}
	return api.ListUserSessions200JSONResponse(list), nil
}

// RevokeUserSession ends one of a member's sessions.
func (h *Handler) RevokeUserSession(
	ctx context.Context, request api.RevokeUserSessionRequestObject,
) (api.RevokeUserSessionResponseObject, error) {
	orgID, err := scoped(ctx)
	if err != nil {
		return nil, err
	}
	caller, _ := management.CallerFrom(ctx)

	if err := h.revokeOne(ctx, orgID, request.UserId.String(), request.SessionId.String(),
		session.ReasonAdmin, caller.UserID); err != nil {
		return nil, err
	}
	return api.RevokeUserSession204Response{}, nil
}

// --- shared ------------------------------------------------------------------------------

// list reads one page. `member` is true on the administrator's route, where a
// user the organization does not have is a 404 rather than an empty list.
func (h *Handler) list(
	ctx context.Context, orgID, userID, currentSessionID string,
	pageSize *int, pageToken *string, member bool,
) (api.SessionList, error) {
	size := management.PageSize(intParam(pageSize))
	cursor, err := management.DecodeCursor(stringParam(pageToken))
	if err != nil {
		return api.SessionList{}, err
	}

	var rows []session.Session
	if err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		if member {
			if _, err := h.Members.Get(ctx, tx, userID); err != nil {
				return err
			}
		}
		var err error
		rows, err = h.Sessions.ListLive(ctx, tx, userID, h.Policy, h.now(),
			session.ListPosition{CreatedAt: cursor.After, ID: cursor.ID}, size+1)
		return err
	}); err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return api.SessionList{}, notFound("no such user in this organization")
		}
		return api.SessionList{}, internal("listing sessions", err)
	}

	page, err := management.Paginate(rows, size, func(s session.Session) management.Cursor {
		return management.Cursor{After: s.CreatedAt, ID: s.ID}
	})
	if err != nil {
		return api.SessionList{}, err
	}

	out := api.SessionList{Sessions: make([]api.Session, 0, len(page.Items))}
	for _, s := range page.Items {
		rendered, err := h.render(s, currentSessionID)
		if err != nil {
			return api.SessionList{}, err
		}
		out.Sessions = append(out.Sessions, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}
	return out, nil
}

// revokeOne ends a session that must belong to userID, with its refresh tokens.
//
// The session and its owner are bound in ONE query (session.Store.RevokeOwned).
// A session id belonging to anybody else is the same 404 as one that does not
// exist, so the endpoint cannot be used to learn which ids are real.
func (h *Handler) revokeOne(
	ctx context.Context, orgID, userID, sessionID string, reason session.Reason, actorUserID string,
) error {
	var revocation session.Revocation
	if err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		var err error
		revocation, err = h.Sessions.RevokeOwned(ctx, tx, sessionID, userID, h.now())
		if err != nil || !revocation.Found {
			return err
		}
		// Every refresh token issued through it, for every client. Revoked in
		// the same transaction, so there is no moment when the session is over
		// and an application's refresh token still mints access tokens.
		refreshRevoked, err := h.Refresh.RevokeForSessions(ctx, tx, []string{sessionID})
		if err != nil {
			return err
		}
		if len(revocation.SessionIDs) == 0 {
			// Already over. Nothing changed and nothing is recorded.
			management.Unchanged(ctx)
			return nil
		}
		return h.audit(ctx, tx, orgID, map[string]any{
			"session_id":             sessionID,
			"user_id":                revocation.UserID,
			"reason":                 string(reason),
			"refresh_tokens_revoked": refreshRevoked,
		})
	}); err != nil {
		return internal("revoking a session", err)
	}

	if !revocation.Found {
		return notFound("no such session for this user")
	}
	return invalidate(revocation)
}

// audit writes `session.revoked` for the request.
//
// The actor comes from the request context (management.Audit), never from a
// parameter, so an event cannot be attributed to anybody but the caller.
func (h *Handler) audit(ctx context.Context, tx *postgres.Tx, orgID string, payload map[string]any) error {
	if h.Audit == nil {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "the sessions API has no audit recorder",
		}
	}
	return management.Audit(ctx, h.Audit, tx, audit.Event{
		OrgID:   orgID,
		Type:    audit.EventSessionRevoked,
		Payload: payload,
	})
}

// render shapes a session for a response.
//
// **What is left out is the point.** No IP address and no user agent string:
// the browser family and OS are enough for a person to recognise "my phone",
// and the city is enough to recognise "not me". The audit log keeps the
// address for an investigation (abuse case A-3).
func (h *Handler) render(s session.Session, currentSessionID string) (api.Session, error) {
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return api.Session{}, fmt.Errorf("sessionapi: a session id is not a uuid: %w", err)
	}

	device := anomaly.ParseDevice(s.UserAgent)
	out := api.Session{
		Id:           id,
		Device:       api.SessionDevice{Browser: nullString(device.Browser), Os: nullString(device.OS)},
		Location:     nullable.NewNullNullable[string](),
		AuthMethods:  s.AuthMethods,
		CreatedAt:    s.CreatedAt,
		LastActiveAt: s.LastSeenAt,
		ExpiresAt:    s.ExpiresAt,
		Current:      currentSessionID != "" && currentSessionID == s.ID,
	}
	if out.AuthMethods == nil {
		out.AuthMethods = []string{}
	}
	if h.Locator != nil {
		if place := h.Locator.Locate(s.IP).Describe(); place != "" {
			out.Location = nullable.NewNullableWithValue(place)
		}
	}
	return out, nil
}

// invalidate removes ended sessions from the cache, after commit.
//
// A failure is an error, as it is for deactivation: the table says revoked and
// the cache would still say valid, and the cache is what the next request reads.
func invalidate(r session.Revocation) error {
	if r.Invalidate == nil {
		return nil
	}
	if err := r.Invalidate(context.Background()); err != nil {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "the session was revoked but the session cache could not be invalidated: " + err.Error(),
		}
	}
	return nil
}

// self returns the caller of a /v1/me route.
func self(ctx context.Context) (management.Caller, error) {
	caller, ok := management.CallerFrom(ctx)
	if !ok || caller.UserID == "" || caller.OrgID == "" {
		// The middleware puts a caller there for every /v1 route, with both
		// fields checked. Absence is a wiring fault.
		return management.Caller{}, management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a self-scoped session route ran with no caller",
		}
	}
	return caller, nil
}

// scoped returns the organization an administrator's route runs in.
func scoped(ctx context.Context) (string, error) {
	orgID, _ := management.ScopeFrom(ctx)
	if orgID == "" {
		return "", management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "an organization session route ran with no tenant scope",
		}
	}
	return orgID, nil
}

func notFound(reason string) error {
	return management.Fault{
		Class: management.NotFound, Message: "The requested resource was not found.", Reason: reason,
	}
}

func internal(doing string, err error) error {
	var fault management.Fault
	if errors.As(err, &fault) {
		return err
	}
	return fmt.Errorf("sessionapi: %s: %w", doing, err)
}

func nullString(v string) nullable.Nullable[string] {
	if v == "" {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(v)
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

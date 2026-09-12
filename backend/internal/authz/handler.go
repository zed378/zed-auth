package authz

import (
	"context"
	"log/slog"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Handler implements the authorization check endpoint (P2-06).
type Handler struct {
	DB  *postgres.DB
	Log *slog.Logger

	// Observer records decision metrics. `docs/PLAN/13` wants latency, the
	// allow/deny ratio and the error rate as three separate series, because an
	// outage that showed up as a deny spike would look like a policy change.
	Observer Observer

	Now func() time.Time
}

// Observer receives what happened, without what it was about.
type Observer interface {
	AuthorizationDecided(allowed bool, took time.Duration)
	AuthorizationFailed(took time.Duration)
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// CheckAuthorization answers one question.
func (h *Handler) CheckAuthorization(
	ctx context.Context, request api.CheckAuthorizationRequestObject,
) (api.CheckAuthorizationResponseObject, error) {
	started := h.now()

	if request.Body == nil {
		return nil, management.Fault{
			Class: management.Invalid, Message: "A request body is required.",
			Reason: "empty body",
		}
	}

	req := Request{
		SubjectUserID: request.Body.Subject.UserId.String(),
		Action:        request.Body.Action,
		ResourceType:  request.Body.Resource.Type,
	}
	if err := req.Validate(); err != nil {
		// A malformed question is the caller's bug, and answering "denied"
		// would hide it behind a plausible result.
		return nil, management.Fault{
			Class:   management.Invalid,
			Message: "The check is not well formed.",
			Details: []api.ErrorDetail{{Field: "action", Issue: err.Error()}},
			Reason:  "malformed authorization check: " + err.Error(),
		}
	}

	// Both scopes come from the token. Neither can be named in the request:
	// given a free choice of project, a caller could map another
	// organization's grants one question at a time.
	orgID, _ := management.ScopeFrom(ctx)
	caller, _ := management.CallerFrom(ctx)
	if orgID == "" || caller.ClientID == "" {
		return nil, management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "an authorization check ran with no caller scope",
		}
	}

	grants, subjectExists, err := h.grantsFor(ctx, orgID, caller.ClientID, req.SubjectUserID)
	if err != nil {
		// Fail closed, and say so. NOT 200 with allowed:false — that claims a
		// decision was reached, which a caller may cache and which makes an
		// outage indistinguishable from a policy change on every dashboard
		// watching the allow/deny ratio.
		h.observeFailure(started)
		if h.Log != nil {
			h.Log.Error("an authorization check could not be completed",
				"error", err.Error(), "client_id", caller.ClientID,
				// The permission, never the resource id and never the
				// attributes (docs/PLAN/13, CLAUDE.md).
				"permission", req.Permission())
		}
		return nil, management.Fault{
			Class:   management.Unavailable,
			Message: "No authorization decision could be reached. Treat this as denied.",
			Reason:  "authorization dependency failure",
		}
	}

	decision := Decide(req, grants)
	h.observeDecision(decision.Allowed, started)

	if h.Log != nil {
		// The distinction the RESPONSE must not carry: whether the subject
		// exists at all. Here, where an operator can see it and a caller
		// cannot.
		h.Log.Info("authorization decided",
			"allowed", decision.Allowed,
			"permission", req.Permission(),
			"subject_known", subjectExists,
			"matched_policy", decision.MatchedPolicy,
			"client_id", caller.ClientID)
	}

	out := api.AuthorizationDecision{
		Allowed: decision.Allowed,
		Reasons: decision.Reasons,
	}
	if decision.MatchedPolicy != "" {
		matched := decision.MatchedPolicy
		out.MatchedPolicy = &matched
	}
	return api.CheckAuthorization200JSONResponse(out), nil
}

// grantsFor reads the subject's roles in the caller's own project, with what
// each role carries.
//
// One query. `P1-28` found Postgres CPU to be the ceiling and this endpoint is
// expected to carry more traffic than everything in Phase 1 combined, so a
// decision that cost three round trips would set a new one.
//
// The second return says whether the subject exists — for the LOG only. The
// response must not distinguish an unknown subject from an unauthorized one.
func (h *Handler) grantsFor(
	ctx context.Context, orgID, clientID, subjectUserID string,
) ([]Grant, bool, error) {
	var (
		out    []Grant
		exists bool
	)

	err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		// Does the subject exist in this tenant? Read in the same round trip
		// as the grants, because it is only ever used for a log line and does
		// not deserve a query of its own.
		rows, err := tx.Query(ctx, `
			WITH subject AS (
			  SELECT id FROM users WHERE id = $1
			),
			client_project AS (
			  SELECT project_id FROM applications WHERE id = $2
			)
			SELECT
			  (SELECT count(*) FROM subject) > 0,
			  r.key,
			  r.permission_keys
			  FROM user_grants g
			  JOIN client_project cp ON cp.project_id = g.project_id
			  JOIN roles r ON r.project_id = g.project_id AND r.key = ANY(g.role_keys)
			 WHERE g.user_id = $1`,
			subjectUserID, clientID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var g Grant
			if err := rows.Scan(&exists, &g.RoleKey, pq.Array(&g.PermissionKeys)); err != nil {
				return err
			}
			out = append(out, g)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// No rows means no grants, which says nothing about whether the
		// subject exists — so that has to be asked separately in exactly the
		// case where the join produced nothing.
		if len(out) == 0 {
			return tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, subjectUserID).Scan(&exists)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, exists, nil
}

func (h *Handler) observeDecision(allowed bool, started time.Time) {
	if h.Observer != nil {
		h.Observer.AuthorizationDecided(allowed, h.now().Sub(started))
	}
}

func (h *Handler) observeFailure(started time.Time) {
	if h.Observer != nil {
		h.Observer.AuthorizationFailed(h.now().Sub(started))
	}
}

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

	// Cache holds the decision's INPUTS (P2-07). Optional: nil means every
	// check reads the database, which is slower and never wrong.
	Cache *Cache

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
// Two reads, both cacheable independently (`P2-07`): what the user holds, and
// what the project's roles carry. They change for different reasons and are
// invalidated by different events — combining them would mean one role edit
// invalidating every user who holds it, which is a scan or a guess and neither
// belongs in a request path.
//
// A cache miss, or a cache that cannot be reached, falls through to the
// database. Never to an allow.
//
// The second return says whether the subject exists — for the LOG only. The
// response must not distinguish an unknown subject from an unauthorized one.
func (h *Handler) grantsFor(
	ctx context.Context, orgID, clientID, subjectUserID string,
) ([]Grant, bool, error) {
	projectID, err := h.projectOf(ctx, orgID, clientID)
	if err != nil {
		return nil, false, err
	}

	roleKeys, cachedKeys := h.Cache.RoleKeys(ctx, orgID, subjectUserID, projectID)
	definitions, cachedRoles := h.Cache.Roles(ctx, orgID, projectID)

	// `exists` is only ever used for a log line, so it is not worth a query of
	// its own — and not worth caching either. It is read alongside the grants
	// when the grants are read, and assumed true when they came from a cache
	// (an entry only exists for a user who was looked up).
	exists := cachedKeys

	if !cachedKeys || !cachedRoles {
		err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
			if !cachedKeys {
				var err error
				roleKeys, exists, err = readRoleKeys(ctx, tx, subjectUserID, projectID)
				if err != nil {
					return err
				}
				h.Cache.PutRoleKeys(ctx, orgID, subjectUserID, projectID, roleKeys)
			}
			if !cachedRoles {
				var err error
				definitions, err = readRoleDefinitions(ctx, tx, projectID)
				if err != nil {
					return err
				}
				h.Cache.PutRoles(ctx, orgID, projectID, definitions)
			}
			return nil
		})
		if err != nil {
			return nil, false, err
		}
	}

	out := make([]Grant, 0, len(roleKeys))
	for _, key := range roleKeys {
		// A role key with no definition is dropped rather than treated as
		// carrying nothing in particular. `P2-03`'s trigger makes it
		// impossible to write one, and if one ever exists it must not become a
		// grant that quietly matches an empty permission set.
		permissions, defined := definitions[key]
		if !defined {
			continue
		}
		out = append(out, Grant{RoleKey: key, PermissionKeys: permissions})
	}
	return out, exists, nil
}

// projectOf resolves the caller's client to its project.
//
// Not cached: it is a single indexed lookup by primary key, and caching an
// application's project would add an invalidation path for a value that
// changes when an application is created and never again.
func (h *Handler) projectOf(ctx context.Context, orgID, clientID string) (string, error) {
	var projectID string
	err := h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT project_id FROM applications WHERE id = $1`, clientID).Scan(&projectID)
	})
	if err != nil {
		return "", err
	}
	return projectID, nil
}

func readRoleKeys(
	ctx context.Context, tx *postgres.Tx, userID, projectID string,
) ([]string, bool, error) {
	var (
		keys   []string
		exists bool
	)
	err := tx.QueryRow(ctx, `
		SELECT
		  EXISTS (SELECT 1 FROM users WHERE id = $1),
		  coalesce((SELECT role_keys FROM user_grants
		             WHERE user_id = $1 AND project_id = $2), '{}')`,
		userID, projectID,
	).Scan(&exists, pq.Array(&keys))
	if err != nil {
		return nil, false, err
	}
	if keys == nil {
		keys = []string{}
	}
	return keys, exists, nil
}

func readRoleDefinitions(
	ctx context.Context, tx *postgres.Tx, projectID string,
) (map[string][]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT key, permission_keys FROM roles WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]string{}
	for rows.Next() {
		var key string
		var permissions []string
		if err := rows.Scan(&key, pq.Array(&permissions)); err != nil {
			return nil, err
		}
		if permissions == nil {
			permissions = []string{}
		}
		out[key] = permissions
	}
	return out, rows.Err()
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

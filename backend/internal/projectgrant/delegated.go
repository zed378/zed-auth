package projectgrant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Delegated user grants: the receiving organization's side (P4-02).
//
// Specification: MEMORY/specs/P4-02-delegated-user-grants.md.
//
// The rule every plan document states independently: the roles assigned
// through a Project Grant must be a subset of its granted_role_keys, checked on
// EVERY request against the grant as it stands then. Each write here reads the
// grant FOR SHARE in its own transaction, so a revocation (FOR UPDATE, in
// Store.Revoke) either committed first and is seen, or waits for this write.
// The database trigger `user_grants_delegation_closed` repeats every rule for
// any other writer.
//
// These rows confer no access yet: no reader of user_grants can see them until
// P4-04 adds the reader-side join (threat review T4-2).

// MaxDelegatedRoleKeys matches grant.MaxRoleKeys and the OpenAPI schema.
const MaxDelegatedRoleKeys = 64

// UserGrant is one delegated user_grants row.
//
// Its own type rather than UserGrant: internal/grant's integration tests
// wire this package into a server, so importing grant from here is a cycle.
// The API shape is the same generated api.Grant either way.
type UserGrant struct {
	ID, UserID, ProjectID, ProjectGrantID string
	RoleKeys                              []string
	CreatedAt, UpdatedAt                  sql.NullTime
}

func renderUserGrant(g UserGrant) (api.Grant, error) {
	userID, err := uuid.Parse(g.UserID)
	if err != nil {
		return api.Grant{}, fmt.Errorf("projectgrant: rendering user %q: %w", g.UserID, err)
	}
	projectID, err := uuid.Parse(g.ProjectID)
	if err != nil {
		return api.Grant{}, fmt.Errorf("projectgrant: rendering project %q: %w", g.ProjectID, err)
	}
	out := api.Grant{UserId: userID, ProjectId: projectID, RoleKeys: append([]string{}, g.RoleKeys...)}
	if g.ProjectGrantID == "" {
		out.ProjectGrantId.SetNull()
	} else {
		via, err := uuid.Parse(g.ProjectGrantID)
		if err != nil {
			return api.Grant{}, fmt.Errorf("projectgrant: rendering project grant %q: %w", g.ProjectGrantID, err)
		}
		out.ProjectGrantId.Set(via)
	}
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

// ErrRevoked is a write through a grant that is no longer active.
var ErrRevoked = errors.New("projectgrant: the grant is revoked")

// NotDelegated carries the requested keys the grant does not delegate.
type NotDelegated struct {
	Keys      []string
	Delegated []string
}

func (e NotDelegated) Error() string {
	return "projectgrant: not delegated: " + strings.Join(e.Keys, ", ")
}

const delegatedColumns = `id, user_id, project_id, coalesce(project_grant_id::text, ''), role_keys, created_at, updated_at`

func scanDelegated(row interface{ Scan(...any) error }) (UserGrant, error) {
	var g UserGrant
	err := row.Scan(&g.ID, &g.UserID, &g.ProjectID, &g.ProjectGrantID,
		pq.Array(&g.RoleKeys), &g.CreatedAt, &g.UpdatedAt)
	if g.RoleKeys == nil {
		g.RoleKeys = []string{}
	}
	return g, err
}

// Received reads a grant as the organization it was granted TO, locking it
// against revocation for the rest of the transaction.
//
// Filtered on granted_org_id although RLS already bounds the row: the
// two-sided policy shows a grant to its granting organization too, and a
// granting administrator must not reach the receiving side's routes by putting
// their own organization in the path (spec §10, threat review T4-4).
func (s *Store) Received(ctx context.Context, tx *postgres.Tx, grantedOrgID, id string) (Grant, error) {
	var g Grant
	err := scan(tx.QueryRow(ctx, `
		SELECT `+columns+`
		  FROM project_grants
		 WHERE id = $1 AND granted_org_id = $2
		   FOR SHARE`,
		id, grantedOrgID), &g)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, fmt.Errorf("projectgrant: reading a received grant: %w", err)
	}
	return g, nil
}

// requireAssignable is the per-request check: active, and a subset.
func requireAssignable(g Grant, keys []string) error {
	if g.Status != StatusActive {
		return ErrRevoked
	}
	delegated := make(map[string]bool, len(g.GrantedRoleKeys))
	for _, k := range g.GrantedRoleKeys {
		delegated[k] = true
	}
	var missing []string
	for _, k := range keys {
		if !delegated[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return NotDelegated{Keys: missing, Delegated: append([]string(nil), g.GrantedRoleKeys...)}
	}
	return nil
}

// requireMember answers a user outside the receiving organization — RLS hides
// them — as not found.
func requireMember(ctx context.Context, tx *postgres.Tx, userID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("projectgrant: checking the user: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// AssignDelegated writes a delegated grant, replacing one the user holds for
// the same project under a grant that has since been revoked (spec F-8). It
// returns that superseded grant's id, or "".
func (s *Store) AssignDelegated(
	ctx context.Context, tx *postgres.Tx, g Grant, userID string, keys []string,
) (UserGrant, string, error) {
	var (
		existingVia    string
		existingStatus sql.NullString
	)
	err := tx.QueryRow(ctx, `
		SELECT coalesce(ug.project_grant_id::text, ''), pg.status
		  FROM user_grants ug
		  LEFT JOIN project_grants pg ON pg.id = ug.project_grant_id
		 WHERE ug.user_id = $1 AND ug.project_id = $2
		   FOR UPDATE OF ug`,
		userID, g.ProjectID).Scan(&existingVia, &existingStatus)

	superseded := ""
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return UserGrant{}, "", fmt.Errorf("projectgrant: reading the user's current grant: %w", err)
	case existingVia == g.ID:
		return UserGrant{}, "", management.Fault{
			Class:   management.Conflict,
			Message: "This user already holds roles through this Project Grant. Replace them instead.",
			Reason:  "second delegated grant under the same project grant",
		}
	case existingVia != "" && existingStatus.String == StatusRevoked:
		if _, err := tx.Exec(ctx,
			`DELETE FROM user_grants WHERE user_id = $1 AND project_id = $2`, userID, g.ProjectID); err != nil {
			return UserGrant{}, "", fmt.Errorf("projectgrant: replacing a grant under a revoked grant: %w", err)
		}
		superseded = existingVia
	default:
		// A row for this user and project that is neither under this grant nor
		// under a revoked one. One active grant per project and organization
		// makes this unreachable; refused rather than overwritten if it is not.
		return UserGrant{}, "", management.Fault{
			Class:   management.Conflict,
			Message: "This user already holds roles in that project.",
			Reason:  "existing user grant for the project under another path",
		}
	}

	created, err := scanDelegated(tx.QueryRow(ctx, `
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys, project_grant_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+delegatedColumns,
		userID, g.ProjectID, g.GrantedOrgID, pq.Array(keys), g.ID))
	if err != nil {
		return UserGrant{}, "", delegatedConstraint(err, "assigning")
	}
	return created, superseded, nil
}

// ReplaceDelegated sets a user's delegated roles under this grant.
func (s *Store) ReplaceDelegated(
	ctx context.Context, tx *postgres.Tx, grantID, userID string, keys []string,
) (before, after UserGrant, err error) {
	before, err = scanDelegated(tx.QueryRow(ctx, `
		SELECT `+delegatedColumns+` FROM user_grants
		 WHERE user_id = $1 AND project_grant_id = $2
		   FOR UPDATE`, userID, grantID))
	if errors.Is(err, sql.ErrNoRows) {
		return UserGrant{}, UserGrant{}, ErrNotFound
	}
	if err != nil {
		return UserGrant{}, UserGrant{}, fmt.Errorf("projectgrant: reading a delegated grant: %w", err)
	}
	after, err = scanDelegated(tx.QueryRow(ctx, `
		UPDATE user_grants SET role_keys = $3, updated_at = now()
		 WHERE user_id = $1 AND project_grant_id = $2
		RETURNING `+delegatedColumns, userID, grantID, pq.Array(keys)))
	if err != nil {
		return UserGrant{}, UserGrant{}, delegatedConstraint(err, "replacing")
	}
	return before, after, nil
}

// RemoveDelegated deletes a user's delegated roles under this grant.
func (s *Store) RemoveDelegated(ctx context.Context, tx *postgres.Tx, grantID, userID string) (UserGrant, error) {
	g, err := scanDelegated(tx.QueryRow(ctx, `
		DELETE FROM user_grants WHERE user_id = $1 AND project_grant_id = $2
		RETURNING `+delegatedColumns, userID, grantID))
	if errors.Is(err, sql.ErrNoRows) {
		return UserGrant{}, ErrNotFound
	}
	if err != nil {
		return UserGrant{}, fmt.Errorf("projectgrant: removing a delegated grant: %w", err)
	}
	return g, nil
}

// ListDelegated returns the delegated grants under this grant, oldest first.
func (s *Store) ListDelegated(
	ctx context.Context, tx *postgres.Tx, grantID string, after management.Cursor, size int,
) ([]UserGrant, error) {
	var afterTime, afterID any
	if after.ID != "" {
		afterTime, afterID = after.After, after.ID
	}
	rows, err := tx.Query(ctx, `
		SELECT `+delegatedColumns+`
		  FROM user_grants
		 WHERE project_grant_id = $1
		   AND ($2::timestamptz IS NULL OR (created_at, id) > ($2, $3::uuid))
		 ORDER BY created_at, id
		 LIMIT $4`,
		grantID, afterTime, afterID, size+1)
	if err != nil {
		return nil, fmt.Errorf("projectgrant: listing delegated grants: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []UserGrant{}
	for rows.Next() {
		g, err := scanDelegated(rows)
		if err != nil {
			return nil, fmt.Errorf("projectgrant: listing delegated grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// delegatedConstraint maps the database's refusals. The handler has already
// checked each rule under a lock, so reaching one of these means a rule the
// handler and the trigger disagree on — answered as the trigger says.
func delegatedConstraint(err error, doing string) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "user_grants_user_project_key"):
		return management.Fault{
			Class:   management.Conflict,
			Message: "This user already holds roles in that project.",
			Reason:  "one grant per user per project",
		}
	case strings.Contains(text, "is revoked"):
		return ErrRevoked
	case strings.Contains(text, "is not delegated by project grant"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "A requested role is not delegated by this Project Grant.",
			Details: []api.ErrorDetail{{Field: "role_keys", Issue: "not delegated by this Project Grant"}},
			Reason:  "delegation trigger refused a key the handler accepted",
		}
	case strings.Contains(text, "is not a member of organization"),
		strings.Contains(text, "violates foreign key constraint"):
		return ErrNotFound
	}
	return fmt.Errorf("projectgrant: %s a delegated grant: %w", doing, err)
}

// --- handlers ---------------------------------------------------------------

func (h *Handler) ListDelegatedUserGrants(
	ctx context.Context, request api.ListDelegatedUserGrantsRequestObject,
) (api.ListDelegatedUserGrantsResponseObject, error) {
	size := management.PageSize(intParam(request.Params.PageSize))
	cursor, err := management.DecodeCursor(stringParam(request.Params.PageToken))
	if err != nil {
		return nil, err
	}

	var rows []UserGrant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		g, err := h.Grants.Received(ctx, tx, orgID, request.GrantId.String())
		if err != nil {
			return err
		}
		if err := requireStanding(ctx, g); err != nil {
			return err
		}
		rows, err = h.Grants.ListDelegated(ctx, tx, g.ID, cursor, size)
		return err
	}); err != nil {
		return nil, delegatedFault(err)
	}

	page, err := management.Paginate(rows, size, func(g UserGrant) management.Cursor {
		return management.Cursor{After: g.CreatedAt.Time, ID: g.ID}
	})
	if err != nil {
		return nil, err
	}
	out := api.DelegatedGrantList{Grants: make([]api.Grant, 0, len(page.Items))}
	for _, g := range page.Items {
		rendered, err := renderUserGrant(g)
		if err != nil {
			return nil, err
		}
		out.Grants = append(out.Grants, rendered)
	}
	if page.NextPageToken != "" {
		out.PageInfo = &api.PageInfo{NextPageToken: &page.NextPageToken}
	}
	return api.ListDelegatedUserGrants200JSONResponse(out), nil
}

func (h *Handler) AssignDelegatedRoles(
	ctx context.Context, request api.AssignDelegatedRolesRequestObject,
) (api.AssignDelegatedRolesResponseObject, error) {
	if request.Body == nil {
		return nil, management.Fault{Class: management.Invalid, Message: "A request body is required.", Reason: "empty body"}
	}
	userID := request.Body.UserId.String()
	keys, err := delegatedKeys(request.Body.RoleKeys)
	if err != nil {
		return nil, err
	}
	if err := refuseSelf(ctx, userID); err != nil {
		return nil, err
	}

	var created UserGrant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		g, err := h.Grants.Received(ctx, tx, orgID, request.GrantId.String())
		if err != nil {
			return err
		}
		if err := requireStanding(ctx, g); err != nil {
			return err
		}
		if err := requireAssignable(g, keys); err != nil {
			return err
		}
		if err := requireMember(ctx, tx, userID); err != nil {
			return err
		}
		var superseded string
		created, superseded, err = h.Grants.AssignDelegated(ctx, tx, g, userID, keys)
		if err != nil {
			return err
		}
		payload := delegatedPayload(g, userID)
		payload["roles_added"] = created.RoleKeys
		if superseded != "" {
			payload["superseded_grant_id"] = superseded
		}
		return h.audit(ctx, tx, audit.EventDelegatedRoleAssigned, payload)
	}); err != nil {
		return nil, delegatedFault(err)
	}

	rendered, err := renderUserGrant(created)
	if err != nil {
		return nil, err
	}
	return api.AssignDelegatedRoles201JSONResponse(rendered), nil
}

func (h *Handler) ReplaceDelegatedRoles(
	ctx context.Context, request api.ReplaceDelegatedRolesRequestObject,
) (api.ReplaceDelegatedRolesResponseObject, error) {
	if request.Body == nil {
		return nil, management.Fault{Class: management.Invalid, Message: "A request body is required.", Reason: "empty body"}
	}
	userID := request.UserId.String()
	keys, err := delegatedKeys(request.Body.RoleKeys)
	if err != nil {
		return nil, err
	}
	if err := refuseSelf(ctx, userID); err != nil {
		return nil, err
	}

	var updated UserGrant
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		g, err := h.Grants.Received(ctx, tx, orgID, request.GrantId.String())
		if err != nil {
			return err
		}
		if err := requireStanding(ctx, g); err != nil {
			return err
		}
		if err := requireAssignable(g, keys); err != nil {
			return err
		}
		if err := requireMember(ctx, tx, userID); err != nil {
			return err
		}
		var before UserGrant
		before, updated, err = h.Grants.ReplaceDelegated(ctx, tx, g.ID, userID, keys)
		if err != nil {
			return err
		}
		added, removed := difference(before.RoleKeys, updated.RoleKeys)
		if len(added) == 0 && len(removed) == 0 {
			management.Unchanged(ctx)
			return nil
		}
		payload := delegatedPayload(g, userID)
		payload["roles_added"] = added
		payload["roles_removed"] = removed
		return h.audit(ctx, tx, audit.EventDelegatedRoleReplaced, payload)
	}); err != nil {
		return nil, delegatedFault(err)
	}

	rendered, err := renderUserGrant(updated)
	if err != nil {
		return nil, err
	}
	return api.ReplaceDelegatedRoles200JSONResponse(rendered), nil
}

func (h *Handler) RemoveDelegatedRoles(
	ctx context.Context, request api.RemoveDelegatedRolesRequestObject,
) (api.RemoveDelegatedRolesResponseObject, error) {
	userID := request.UserId.String()

	// Allowed under a revoked grant (spec F-10), and for the caller's own
	// roles: removal can only reduce what somebody holds.
	if err := h.inScope(ctx, func(tx *postgres.Tx, orgID string) error {
		g, err := h.Grants.Received(ctx, tx, orgID, request.GrantId.String())
		if err != nil {
			return err
		}
		if err := requireStanding(ctx, g); err != nil {
			return err
		}
		removed, err := h.Grants.RemoveDelegated(ctx, tx, g.ID, userID)
		if err != nil {
			return err
		}
		payload := delegatedPayload(g, userID)
		payload["roles_removed"] = removed.RoleKeys
		return h.audit(ctx, tx, audit.EventDelegatedRoleRemoved, payload)
	}); err != nil {
		return nil, delegatedFault(err)
	}
	return api.RemoveDelegatedRoles204Response{}, nil
}

// --- helpers ----------------------------------------------------------------

func delegatedPayload(g Grant, userID string) map[string]any {
	return map[string]any{
		"grant_id":        g.ID,
		"project_id":      g.ProjectID,
		"granting_org_id": g.GrantingOrgID,
		"granted_org_id":  g.GrantedOrgID,
		"subject_user_id": userID,
	}
}

// delegatedKeys checks the shape of the requested set. Whether each key is
// delegated is asked of the grant, under its lock.
func delegatedKeys(in []string) ([]string, error) {
	switch {
	case len(in) == 0:
		return nil, invalidKeys("At least one role is required. To remove a user's delegated roles, delete them.")
	case len(in) > MaxDelegatedRoleKeys:
		return nil, invalidKeys(fmt.Sprintf("At most %d roles may be assigned.", MaxDelegatedRoleKeys))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, k := range in {
		if seen[k] {
			return nil, invalidKeys(fmt.Sprintf("The role %q is listed twice.", k))
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// refuseSelf stops a receiving administrator assigning delegated roles to
// themselves — the same vertical escalation P2-03 refuses for direct grants.
func refuseSelf(ctx context.Context, subjectUserID string) error {
	caller, ok := management.CallerFrom(ctx)
	if !ok {
		return management.Fault{
			Class: management.Internal, Message: "An unexpected error occurred.",
			Reason: "a delegated grant operation ran with no caller",
		}
	}
	if caller.UserID == subjectUserID {
		return management.Fault{
			Class:   management.Forbidden,
			Message: "You cannot change your own roles. Ask another administrator.",
			Reason:  "self-service delegated grant refused",
		}
	}
	return nil
}

func difference(before, after []string) (added, removed []string) {
	had := map[string]bool{}
	for _, k := range before {
		had[k] = true
	}
	has := map[string]bool{}
	for _, k := range after {
		has[k] = true
	}
	added, removed = []string{}, []string{}
	for _, k := range after {
		if !had[k] {
			added = append(added, k)
		}
	}
	for _, k := range before {
		if !has[k] {
			removed = append(removed, k)
		}
	}
	return added, removed
}

func delegatedFault(err error) error {
	var notDelegated NotDelegated
	switch {
	case errors.As(err, &notDelegated):
		issue := "Not delegated by this Project Grant: " + strings.Join(notDelegated.Keys, ", ")
		message := issue + "."
		if len(notDelegated.Delegated) > 0 {
			message += " It delegates: " + strings.Join(notDelegated.Delegated, ", ") + "."
		}
		return management.Fault{
			Class: management.Invalid, Message: message,
			Details: []api.ErrorDetail{{Field: "role_keys", Issue: issue}},
			Reason:  "delegated assignment outside granted_role_keys",
		}
	case errors.Is(err, ErrRevoked):
		return management.Fault{
			Class:   management.Conflict,
			Message: "This Project Grant has been revoked. No roles can be assigned through it; existing assignments can be removed.",
			Reason:  "write through a revoked project grant",
		}
	}
	return faultFrom(err)
}

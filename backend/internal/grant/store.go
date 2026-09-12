// Package grant implements user grants — the row that actually gives somebody
// access to something (P2-03).
//
// `P2-01` defined what a role is. This is what hands one out, and it is the
// first thing in the system that grants access to anything at all.
//
// # Least privilege, as a property of the schema
//
// `docs/PLAN/08` § Least Privilege: a new user has no access until explicitly
// granted, with no implicit or default role. That is not enforced by a check
// anywhere — it is true because **nothing writes a grant except this package**,
// and a user with no row has no roles. The test that pins it asserts the
// absence rather than any code path, because the absence is the control.
//
// # The Phase 4 slot
//
// `user_grants.project_grant_id` exists and is refused. `docs/PLAN/08` Part C
// requires a delegated grant's role keys to be a subset of the delegation's
// `granted_role_keys`, revalidated on every request, and that check belongs to
// `P4-01`. The refusal lives in a trigger whose body Phase 4 replaces, so the
// call site and the tests are already in place rather than retrofitted around
// live data.
package grant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Grant is one user's roles in one project.
type Grant struct {
	ID        string
	UserID    string
	ProjectID string
	RoleKeys  []string

	// ProjectGrantID is always empty in this phase. Kept on the type rather
	// than omitted so Phase 4 adds behaviour, not a field.
	ProjectGrantID string

	CreatedAt, UpdatedAt sql.NullTime
}

// MaxRoleKeys bounds one grant.
//
// A user holding hundreds of roles in one project is a configuration mistake
// rather than a requirement, and every one of them lands in a token (`P2-04`).
const MaxRoleKeys = 64

type Store struct{}

func NewStore() *Store { return &Store{} }

// ErrNotFound covers "no such grant" and "in another tenant" alike.
var ErrNotFound = errors.New("grant: not found")

// ErrUnknownRole names a role key that does not exist in the project.
//
// It carries the key, because "one of your role keys is invalid" sends an
// administrator to guess which one — and a grant is usually written by a
// script that sent several.
type ErrUnknownRole struct{ Key string }

func (e ErrUnknownRole) Error() string {
	return fmt.Sprintf("grant: no role %q in this project", e.Key)
}

// ErrDelegationNotImplemented is the closed Phase 4 slot.
var ErrDelegationNotImplemented = errors.New("grant: delegated grants arrive in P4-01")

const columns = `id, user_id, project_id, coalesce(project_grant_id::text, ''), role_keys, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Grant, error) {
	var g Grant
	err := row.Scan(&g.ID, &g.UserID, &g.ProjectID, &g.ProjectGrantID,
		pq.Array(&g.RoleKeys), &g.CreatedAt, &g.UpdatedAt)
	if g.RoleKeys == nil {
		g.RoleKeys = []string{}
	}
	return g, err
}

// --- reading ---------------------------------------------------------------

// ForUser returns every grant a user holds, ordered by project.
//
// One query. The obvious alternative — a query per project the organization
// has — is an N+1 that gets slower as the organization grows, and a user's
// grant list is read on every screen that shows their access.
func (s *Store) ForUser(ctx context.Context, tx *postgres.Tx, userID string) ([]Grant, error) {
	rows, err := tx.Query(ctx,
		`SELECT `+columns+` FROM user_grants WHERE user_id = $1 ORDER BY project_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("grant: listing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Grant{}
	for rows.Next() {
		g, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("grant: listing: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Get reads one user's grant in one project.
func (s *Store) Get(ctx context.Context, tx *postgres.Tx, userID, projectID string) (Grant, error) {
	g, err := scan(tx.QueryRow(ctx,
		`SELECT `+columns+` FROM user_grants WHERE user_id = $1 AND project_id = $2`,
		userID, projectID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Grant{}, ErrNotFound
	case err != nil:
		return Grant{}, fmt.Errorf("grant: reading: %w", err)
	}
	return g, nil
}

// RoleKeysFor returns the roles a user holds in a project, or nothing.
//
// The read `P2-04` and `P2-06` will both use. It returns an empty slice for a
// user with no grant rather than an error: having no access is an ordinary
// state, and making it an error would push every caller into treating the
// normal case as exceptional.
func (s *Store) RoleKeysFor(ctx context.Context, tx *postgres.Tx, userID, projectID string) ([]string, error) {
	g, err := s.Get(ctx, tx, userID, projectID)
	switch {
	case errors.Is(err, ErrNotFound):
		return []string{}, nil
	case err != nil:
		return nil, err
	}
	return g.RoleKeys, nil
}

// --- writing ---------------------------------------------------------------

// Create grants roles to a user in a project.
func (s *Store) Create(
	ctx context.Context, tx *postgres.Tx, userID, projectID string, roleKeys []string,
) (Grant, error) {
	if err := validateKeys(roleKeys); err != nil {
		return Grant{}, err
	}

	orgID := tx.OrgID()
	if orgID == "" {
		return Grant{}, fmt.Errorf("grant: creating without a tenant scope")
	}

	g, err := scan(tx.QueryRow(ctx, `
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)
		RETURNING `+columns,
		userID, projectID, orgID, pq.Array(roleKeys)))
	if err != nil {
		return Grant{}, wrapConstraint(err, "creating")
	}
	return g, nil
}

// Replace sets a user's roles in a project to exactly this set.
//
// Replace rather than merge, for the same reason `P2-02`'s role update
// replaces its permission keys: a partial update of an array is ambiguous, and
// the audit event records what was added and removed, which needs a complete
// before and after.
func (s *Store) Replace(
	ctx context.Context, tx *postgres.Tx, userID, projectID string, roleKeys []string,
) (Grant, error) {
	if err := validateKeys(roleKeys); err != nil {
		return Grant{}, err
	}

	g, err := scan(tx.QueryRow(ctx, `
		UPDATE user_grants SET role_keys = $3
		 WHERE user_id = $1 AND project_id = $2
		RETURNING `+columns,
		userID, projectID, pq.Array(roleKeys)))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Grant{}, ErrNotFound
	case err != nil:
		return Grant{}, wrapConstraint(err, "updating")
	}
	return g, nil
}

// Revoke removes a user's access to a project entirely.
//
// The row is DELETED, not flagged. `docs/PLAN/08` wants revocation to take
// effect immediately rather than when a token expires, and a row that still
// exists is a row some future read path can still find — including one written
// after this, by somebody who did not know a flag existed.
func (s *Store) Revoke(ctx context.Context, tx *postgres.Tx, userID, projectID string) (Grant, error) {
	g, err := scan(tx.QueryRow(ctx, `
		DELETE FROM user_grants WHERE user_id = $1 AND project_id = $2
		RETURNING `+columns,
		userID, projectID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Grant{}, ErrNotFound
	case err != nil:
		return Grant{}, fmt.Errorf("grant: revoking: %w", err)
	}
	return g, nil
}

// --- validation ------------------------------------------------------------

// validateKeys checks the shape of a role key set. Whether each key EXISTS is
// the database's question, asked by a trigger — one place that knows what
// roles there are beats two places that disagree about it.
func validateKeys(keys []string) error {
	switch {
	case len(keys) == 0:
		// docs/PLAN/08 § Least Privilege: a grant with no roles grants nothing
		// and should not exist. The operation the caller wants is a delete.
		return management.Fault{
			Class:   management.Invalid,
			Message: "A grant must carry at least one role. To remove access, delete the grant.",
			Details: []api.ErrorDetail{{Field: "role_keys", Issue: "must not be empty"}},
			Reason:  "empty role_keys",
		}
	case len(keys) > MaxRoleKeys:
		return management.Fault{
			Class:   management.Invalid,
			Message: fmt.Sprintf("A grant may carry at most %d roles.", MaxRoleKeys),
			Details: []api.ErrorDetail{{Field: "role_keys", Issue: "too many"}},
			Reason:  "role_keys over the bound",
		}
	}

	seen := make(map[string]int, len(keys))
	for i, k := range keys {
		if first, duplicate := seen[k]; duplicate {
			return management.Fault{
				Class:   management.Invalid,
				Message: fmt.Sprintf("The role %q is listed twice.", k),
				Details: []api.ErrorDetail{{
					Field: fmt.Sprintf("role_keys[%d]", i),
					Issue: fmt.Sprintf("already listed at index %d", first),
				}},
				Reason: "duplicate role key",
			}
		}
		seen[k] = i
	}
	return nil
}

// wrapConstraint turns a database refusal into something the API can answer.
func wrapConstraint(err error, doing string) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "does not exist in project"):
		// The trigger names the key; pull it back out so the API can too.
		return ErrUnknownRole{Key: keyFrom(text)}
	case strings.Contains(text, "delegated grants are not implemented"):
		return ErrDelegationNotImplemented
	case strings.Contains(text, "user_grants_user_project_key"):
		return management.Fault{
			Class:   management.Conflict,
			Message: "This user already has a grant in that project. Update it instead.",
			Reason:  "one grant per user per project",
		}
	case strings.Contains(text, "user_grants_role_keys_not_empty"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "A grant must carry at least one role.",
			Reason:  "empty role_keys reached the database",
		}
	case strings.Contains(text, "does not match project"),
		strings.Contains(text, "not visible in this tenant"):
		return ErrNotFound
	case strings.Contains(text, "violates foreign key constraint"):
		// A user or project that does not exist. Not found rather than a
		// constraint name, which would leak the schema.
		return ErrNotFound
	}
	return fmt.Errorf("grant: %s: %w", doing, err)
}

// keyFrom pulls the role key out of the trigger's message.
//
// Parsing an error string is unpleasant and is the lesser evil: the
// alternative is a second round trip that asks which key was missing, which
// can disagree with the write that just failed.
func keyFrom(text string) string {
	const marker = "role key "
	i := strings.Index(text, marker)
	if i < 0 {
		return ""
	}
	rest := text[i+len(marker):]
	if j := strings.Index(rest, " does not exist"); j >= 0 {
		return rest[:j]
	}
	return ""
}

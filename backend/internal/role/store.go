package role

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

// Store reads and writes roles inside whatever tenant the transaction is
// scoped to.
//
// **There is no org_id parameter in this file**, the same shape `P1-17`'s
// project store uses: the tenant is supplied by `WithTenant`, so there is
// nothing for a caller to supply wrongly. The one place an organization is
// named is `Create`, which reads it back off the transaction rather than
// accepting it.
type Store struct{}

func NewStore() *Store { return &Store{} }

// ErrNotFound covers "no such role" and "a role in another tenant" alike, so
// the API answers 404 for both. Distinguishing them would answer "is this id
// real?" for anyone willing to send one request per guess (`docs/SECURITY/02`
// §12).
var ErrNotFound = errors.New("role: not found")

// ErrInUse is returned when a delete is refused because grants reference the
// role. It carries the count, which is what an operator needs; it does not
// carry who holds them, which from Phase 4 is another tenant's data.
type ErrInUse struct{ Grants int }

func (e ErrInUse) Error() string {
	return fmt.Sprintf("role: still referenced by %d grant(s)", e.Grants)
}

// ErrBuiltin is returned when a built-in role is re-keyed or deleted.
var ErrBuiltin = errors.New("role: built-in roles cannot be re-keyed or deleted")

const columns = `id, project_id, key, display_name, permission_keys, is_builtin, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Role, error) {
	var r Role
	// pq.Array so a NULL or empty text[] arrives as an empty slice rather than
	// a scan error — `permission_keys` defaults to '{}' and a role with no
	// permissions is a legitimate label.
	err := row.Scan(&r.ID, &r.ProjectID, &r.Key, &r.DisplayName, pq.Array(&r.PermissionKeys),
		&r.IsBuiltin, &r.CreatedAt, &r.UpdatedAt)
	if r.PermissionKeys == nil {
		r.PermissionKeys = []string{}
	}
	return r, err
}

// --- reading ---------------------------------------------------------------

// Get reads one role by id.
func (s *Store) Get(ctx context.Context, tx *postgres.Tx, id string) (Role, error) {
	r, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM roles WHERE id = $1`, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Role{}, ErrNotFound
	case err != nil:
		return Role{}, fmt.Errorf("role: reading: %w", err)
	}
	return r, nil
}

// GetByKey reads one role by its project-scoped key, which is how a grant
// refers to it (`user_grants.role_keys` holds keys, not ids).
func (s *Store) GetByKey(ctx context.Context, tx *postgres.Tx, projectID, key string) (Role, error) {
	r, err := scan(tx.QueryRow(ctx,
		`SELECT `+columns+` FROM roles WHERE project_id = $1 AND key = $2`, projectID, key))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Role{}, ErrNotFound
	case err != nil:
		return Role{}, fmt.Errorf("role: reading by key: %w", err)
	}
	return r, nil
}

// List returns a page of the project's roles, ordered by key.
//
// Ordered by `key` rather than by creation time, unlike every other list in
// this API: a role list is read as a reference table, and a reader looking for
// `billing-admin` should not have to know when somebody created it. The cursor
// is still (sort column, id), so the pagination contract is unchanged.
func (s *Store) List(
	ctx context.Context, tx *postgres.Tx, projectID string, afterKey string, size int,
) ([]Role, error) {
	rows, err := tx.Query(ctx, `
		SELECT `+columns+`
		  FROM roles
		 WHERE project_id = $1
		   AND ($2::text IS NULL OR key > $2)
		 ORDER BY key
		 LIMIT $3`,
		projectID, nullString(afterKey), size+1)
	if err != nil {
		return nil, fmt.Errorf("role: listing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Role
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("role: listing: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GrantCounts returns, for each role key in the project, how many user grants
// reference it.
//
// One query for the whole page. `P2-02` returns these so the console can warn
// before a delete, and the obvious implementation — a count per row — is an
// N+1 against a table that grows with every user (`NFR-3`).
func (s *Store) GrantCounts(ctx context.Context, tx *postgres.Tx, projectID string) (map[string]int, error) {
	rows, err := tx.Query(ctx, `
		SELECT k, count(*)
		  FROM user_grants, unnest(role_keys) AS k
		 WHERE project_id = $1
		 GROUP BY k`, projectID)
	if err != nil {
		return nil, fmt.Errorf("role: counting grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int{}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("role: counting grants: %w", err)
		}
		counts[key] = n
	}
	return counts, rows.Err()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// --- writing ---------------------------------------------------------------

// Create inserts a role into the given project.
//
// `org_id` is read off the transaction, never accepted from the caller. The
// database checks the same thing from the other direction: a trigger refuses a
// role whose org_id disagrees with its project's, because a mismatch would
// file the role under a tenant that row-level security then hides it from.
func (s *Store) Create(
	ctx context.Context, tx *postgres.Tx, projectID, key, displayName string, permissionKeys []string,
) (Role, error) {
	if err := Validate(key, displayName, permissionKeys); err != nil {
		return Role{}, asFault(err)
	}

	orgID := tx.OrgID()
	if orgID == "" {
		return Role{}, fmt.Errorf("role: creating without a tenant scope")
	}
	if permissionKeys == nil {
		permissionKeys = []string{}
	}

	r, err := scan(tx.QueryRow(ctx, `
		INSERT INTO roles (org_id, project_id, key, display_name, permission_keys)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+columns,
		orgID, projectID, key, strings.TrimSpace(displayName), pq.Array(permissionKeys)))
	if err != nil {
		return Role{}, wrapConstraint(err, "creating")
	}
	return r, nil
}

// Update changes a role's display name and permission keys.
//
// The key is not updatable, for anybody — not only for built-in roles. Grants
// reference roles by key (`user_grants.role_keys`), with no foreign key to
// cascade, so re-keying a role would silently orphan every grant that holds
// the old one. That is a rename that removes access, which is exactly the
// failure `P2-01` step 5 is about. A caller who wants a different key creates
// a different role.
func (s *Store) Update(
	ctx context.Context, tx *postgres.Tx, id, displayName string, permissionKeys []string,
) (Role, error) {
	if err := ValidateDisplayName(displayName); err != nil {
		return Role{}, asFault(err)
	}
	if err := ValidatePermissionKeys(permissionKeys); err != nil {
		return Role{}, asFault(err)
	}
	if permissionKeys == nil {
		permissionKeys = []string{}
	}

	r, err := scan(tx.QueryRow(ctx, `
		UPDATE roles SET display_name = $2, permission_keys = $3
		 WHERE id = $1
		RETURNING `+columns,
		id, strings.TrimSpace(displayName), pq.Array(permissionKeys)))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Role{}, ErrNotFound
	case err != nil:
		return Role{}, wrapConstraint(err, "updating")
	}
	return r, nil
}

// Delete removes a role, refusing while any grant still references it.
//
// Refused rather than cascaded. A cascade here removes access from every user
// holding the role, across every application reading it, in response to a
// request that looks like tidying up — and nothing afterwards says why those
// people lost access. `P2-01` step 5 names the silent cascade as the failure
// to avoid, and `P1-17` made the same choice for projects.
func (s *Store) Delete(ctx context.Context, tx *postgres.Tx, id string) error {
	existing, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if existing.IsBuiltin {
		return ErrBuiltin
	}

	var referencing int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM user_grants
		 WHERE project_id = $1 AND $2 = ANY(role_keys)`,
		existing.ProjectID, existing.Key,
	).Scan(&referencing); err != nil {
		return fmt.Errorf("role: counting references: %w", err)
	}
	if referencing > 0 {
		return ErrInUse{Grants: referencing}
	}

	result, err := tx.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return wrapConstraint(err, "deleting")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- errors ----------------------------------------------------------------

// asFault turns a validation error into the API's fault shape, keeping the
// field name so the response can point at it.
func asFault(err error) error {
	var fe FieldError
	if errors.As(err, &fe) {
		return management.Fault{
			Class:   management.Invalid,
			Message: fe.Detail,
			Details: []api.ErrorDetail{{Field: fe.Field, Issue: fe.Detail}},
			Reason:  "role validation",
		}
	}
	return err
}

// wrapConstraint turns a database constraint into something the API can answer
// with. Every branch here is a rule that also exists above in Go: this is the
// second line, for writers that did not come through it and for races between
// a check and a write.
func wrapConstraint(err error, doing string) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "roles_project_key_key"):
		return management.Fault{
			Class:   management.Conflict,
			Message: "A role with that key already exists in this project.",
			Details: []api.ErrorDetail{{Field: "key", Issue: "A role with that key already exists in this project."}},
			Reason:  "role key uniqueness violation",
		}
	case strings.Contains(text, "roles_permission_keys_well_formed"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "Every permission key must be resource:action — for example user:read.",
			Details: []api.ErrorDetail{{Field: "permission_keys", Issue: "Every permission key must be resource:action — for example user:read."}},
			Reason:  "malformed permission key reached the database",
		}
	case strings.Contains(text, "roles_permission_keys_bounded"):
		return management.Fault{
			Class:   management.Invalid,
			Message: fmt.Sprintf("A role may carry at most %d permission keys.", MaxPermissionKeys),
			Details: []api.ErrorDetail{{Field: "permission_keys", Issue: fmt.Sprintf("A role may carry at most %d permission keys.", MaxPermissionKeys)}},
			Reason:  "permission key count reached the database bound",
		}
	case strings.Contains(text, "roles_permission_keys_distinct"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "The permission keys must not repeat.",
			Details: []api.ErrorDetail{{Field: "permission_keys", Issue: "The permission keys must not repeat."}},
			Reason:  "duplicate permission key reached the database",
		}
	case strings.Contains(text, "roles_key_shape"), strings.Contains(text, "roles_key_not_blank"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "A role key may contain only lower-case letters, digits, underscores and hyphens.",
			Details: []api.ErrorDetail{{Field: "key", Issue: "A role key may contain only lower-case letters, digits, underscores and hyphens."}},
			Reason:  "malformed role key reached the database",
		}
	case strings.Contains(text, "does not match project"),
		strings.Contains(text, "not visible in this tenant"):
		// The trigger. A project in another organization, or none at all —
		// answered as not found, never as forbidden, so the response cannot be
		// used to discover which project ids exist.
		return ErrNotFound
	case strings.Contains(text, "cannot be deleted"),
		strings.Contains(text, "cannot be re-keyed"),
		strings.Contains(text, "cannot stop being built-in"):
		return ErrBuiltin
	case strings.Contains(text, "row-level security"):
		// Not reachable through this package, which reads the tenant off the
		// transaction. If it happens, something else is wrong and it must not
		// read to the caller as a validation problem.
		return fmt.Errorf("role: %s: refused by row-level security: %w", doing, err)
	}
	return fmt.Errorf("role: %s: %w", doing, err)
}

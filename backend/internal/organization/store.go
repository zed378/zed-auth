package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Reading and writing organizations.
//
// **Which transaction each operation runs in is the interesting part**, and it
// is not uniform, because `organizations` is the one table whose RLS policy is
// keyed on `id` rather than `org_id` — an organization row IS the tenant.
//
//	GET/PATCH/DELETE one   WithTenant(target)   — RLS confines it to that one row
//	LIST                   a SECURITY DEFINER function
//	CREATE                 a SECURITY DEFINER function
//
// The last two cannot run under either scope. Tenant scope is wrong by
// definition: a list spans organizations, and a create happens before the
// organization exists. Instance scope sets current_org_id() to NULL, under
// which `id = current_org_id()` is false for every row — so a list would return
// nothing and a create would fail its WITH CHECK.
//
// The answer is the one P0-12, P1-11 and P1-15 already reached for the same
// obstacle: a SECURITY DEFINER function, pinned search_path, granted only to
// auth_app, narrow to exactly its question. Note what these two CANNOT do —
// neither takes an organization id, so neither is a way to reach a specific
// tenant's row by guessing.

// Organization is a tenant, as the API represents it.
//
// `instance_id` is deliberately absent. It is an internal grouping with one row
// in Phase 1, and a field a client can see is a field a client will send back.
type Organization struct {
	ID        string
	Name      string
	Domain    *string
	Status    string
	Settings  json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Status values (docs/PLAN/04 § organizations).
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// MaxNameLength bounds a display name.
//
// Not unique: two customers may legitimately be called the same thing, and a
// uniqueness constraint on a display name is a support-ticket generator.
const MaxNameLength = 200

type Store struct{}

func NewStore() *Store { return &Store{} }

// ErrNotFound means no live organization with that id is visible here.
//
// Deliberately one error for "does not exist", "belongs to another tenant" and
// "was deleted". The caller turns all three into a 404, which is the point:
// distinguishing them would answer "is this id real?" for anybody willing to
// send one request per guess (abuse case A-3).
var ErrNotFound = errors.New("organization: not found")

// --- reading one --------------------------------------------------------------------

// Get reads the organization the transaction is scoped to.
//
// It takes no id, and that is the safety property rather than an omission: the
// row RLS lets this transaction see IS the target, so there is no id parameter
// for a handler to pass wrongly.
func (s *Store) Get(ctx context.Context, tx *postgres.Tx) (Organization, error) {
	const query = `
		SELECT id, name, domain, status, settings, created_at, updated_at
		  FROM organizations
		 WHERE deleted_at IS NULL`

	var o Organization
	err := tx.QueryRow(ctx, query).Scan(
		&o.ID, &o.Name, &o.Domain, &o.Status, &o.Settings, &o.CreatedAt, &o.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Organization{}, ErrNotFound
	case err != nil:
		return Organization{}, fmt.Errorf("organization: reading: %w", err)
	}
	return o, nil
}

// --- listing ------------------------------------------------------------------------

// List returns a page of live organizations, ordered by (created_at, id).
//
// `size+1` rows so the caller can tell whether there is another page without a
// second query — P1-15's Paginate expects the extra row and drops it.
func (s *Store) List(
	ctx context.Context, db *postgres.DB, after management.Cursor, size int,
) ([]Organization, error) {
	var out []Organization

	err := db.WithInstanceScope(ctx,
		"listing organizations, which spans tenants by definition",
		func(tx *postgres.Tx) error {
			rows, err := tx.Query(ctx,
				`SELECT id, name, domain, status, settings, created_at, updated_at
				   FROM organizations_page($1, $2, $3)`,
				nullTime(after), nullID(after), size+1)
			if err != nil {
				return fmt.Errorf("organization: listing: %w", err)
			}
			defer func() { _ = rows.Close() }()

			for rows.Next() {
				var o Organization
				if err := rows.Scan(&o.ID, &o.Name, &o.Domain, &o.Status,
					&o.Settings, &o.CreatedAt, &o.UpdatedAt); err != nil {
					return fmt.Errorf("organization: listing: %w", err)
				}
				out = append(out, o)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Position is the sort position of a row, for P1-15's cursor.
func Position(o Organization) management.Cursor {
	return management.Cursor{After: o.CreatedAt, ID: o.ID}
}

func nullTime(c management.Cursor) any {
	if c.ID == "" {
		return nil
	}
	return c.After
}

func nullID(c management.Cursor) any {
	if c.ID == "" {
		return nil
	}
	return c.ID
}

// --- creating -----------------------------------------------------------------------

// NewOrganization is what a caller may set at creation.
//
// Note what is not here: id, instance_id, status, created_at. Every one of
// those is server-set, and a struct with no field for them is why a mass-
// assignment attempt has nowhere to land (abuse case A-4).
type NewOrganization struct {
	Name     string
	Domain   *string
	Settings json.RawMessage
}

// Create inserts an organization and returns it.
func (s *Store) Create(ctx context.Context, db *postgres.DB, in NewOrganization) (Organization, error) {
	var o Organization

	err := db.WithInstanceScope(ctx,
		"creating an organization, which happens before its tenant exists",
		func(tx *postgres.Tx) error {
			settings := in.Settings
			if len(settings) == 0 {
				// NULL, so the column DEFAULT applies. Passing '{}' instead
				// would create a tenant with no policy at all rather than with
				// the instance defaults — an organization whose password rules
				// are silently absent.
				settings = nil
			}

			err := tx.QueryRow(ctx,
				`SELECT id, name, domain, status, settings, created_at, updated_at
				   FROM organization_create($1, $2, $3)`,
				strings.TrimSpace(in.Name), normalizeDomain(in.Domain), nullableJSON(settings),
			).Scan(&o.ID, &o.Name, &o.Domain, &o.Status, &o.Settings, &o.CreatedAt, &o.UpdatedAt)
			if err != nil {
				return wrapConstraint(err, "creating")
			}
			return nil
		})
	if err != nil {
		return Organization{}, err
	}
	return o, nil
}

// --- updating -----------------------------------------------------------------------

// Changes is a partial update. A nil field is one the request did not mention.
type Changes struct {
	Name     *string
	Domain   **string // a pointer to a pointer: nil means absent, *nil means "clear it"
	Status   *string
	Settings json.RawMessage
}

// Any reports whether anything was actually asked for.
func (c Changes) Any() bool {
	return c.Name != nil || c.Domain != nil || c.Status != nil || len(c.Settings) > 0
}

// Update applies a partial change to the organization the transaction is
// scoped to, and returns the result.
//
// The settings merge is done in SQL with `||`, so an update naming one key
// leaves the rest alone. Read-modify-write in Go would lose a concurrent change
// to a different key, which for a policy document means a control somebody
// turned on quietly turning itself off again.
func (s *Store) Update(ctx context.Context, tx *postgres.Tx, c Changes) (Organization, error) {
	if !c.Any() {
		return s.Get(ctx, tx)
	}

	var (
		domain    any
		setDomain bool
	)
	if c.Domain != nil {
		setDomain = true
		domain = normalizeDomain(*c.Domain)
	}

	var o Organization
	err := tx.QueryRow(ctx, `
		UPDATE organizations
		   SET name     = COALESCE($1, name),
		       domain   = CASE WHEN $2 THEN $3::text ELSE domain END,
		       status   = COALESCE($4, status),
		       -- A DEEP merge (P2-14). The jsonb concatenation operator is
		       -- shallow, so an update naming one password rule replaced the
		       -- whole password_policy object
		       -- and discarded the other two — silently, and specifically for
		       -- the settings whose absence weakens a policy rather than
		       -- breaking it. The contract has always promised key-by-key.
		       settings = CASE WHEN $5::jsonb IS NULL THEN settings
		                       ELSE jsonb_deep_merge(settings, $5::jsonb) END
		 WHERE deleted_at IS NULL
		RETURNING id, name, domain, status, settings, created_at, updated_at`,
		trimmed(c.Name), setDomain, domain, c.Status, nullableJSON(c.Settings),
	).Scan(&o.ID, &o.Name, &o.Domain, &o.Status, &o.Settings, &o.CreatedAt, &o.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Organization{}, ErrNotFound
	case err != nil:
		return Organization{}, wrapConstraint(err, "updating")
	}
	return o, nil
}

// --- deleting -----------------------------------------------------------------------

// SoftDelete marks the organization deleted and releases its domain.
//
// The domain is cleared because it routes login traffic to a tenant. Left
// claimed by a deleted organization it could never be reused — and reusing it
// is the ordinary case: a customer leaves and comes back, or a domain changes
// hands. The schema's organizations_deleted_has_no_domain constraint is what
// makes that a rule rather than a habit.
func (s *Store) SoftDelete(ctx context.Context, tx *postgres.Tx, now time.Time) error {
	result, err := tx.Exec(ctx, `
		UPDATE organizations
		   SET deleted_at = $1, domain = NULL
		 WHERE deleted_at IS NULL`, now)
	if err != nil {
		return fmt.Errorf("organization: deleting: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- helpers -------------------------------------------------------------------------

// normalizeDomain lowercases and trims, or returns NULL.
//
// Lowercased because the unique index is on lower(domain) and a caller who
// types "Acme.Example" must collide with "acme.example" — otherwise two tenants
// hold the same domain in different cases and tenant resolution is ambiguous,
// which is a cross-tenant access bug waiting to happen (P2-09).
func normalizeDomain(d *string) any {
	if d == nil {
		return nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(*d))
	if trimmed == "" {
		// An empty string is a request to clear it, not a domain of "".
		return nil
	}
	return trimmed
}

func trimmed(s *string) any {
	if s == nil {
		return nil
	}
	return strings.TrimSpace(*s)
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

// wrapConstraint turns a database constraint into an error the API can answer.
//
// The domain conflict says the domain is taken and **does not say by whom**:
// which organization holds a domain is not something a caller who does not
// administer it should learn from a 409.
func wrapConstraint(err error, doing string) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "organizations_domain_key"):
		return management.Fault{
			Class:   management.Conflict,
			Message: "That domain is already in use.",
			Reason:  "domain uniqueness violation",
		}
	case strings.Contains(text, "organizations_name_not_blank"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "The organization name must not be blank.",
			Reason:  "blank name reached the database",
		}
	case strings.Contains(text, "organizations_status_valid"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "That status is not valid.",
			Reason:  "invalid status reached the database",
		}
	}
	return fmt.Errorf("organization: %s: %w", doing, err)
}

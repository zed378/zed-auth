// Package projectgrant is the delegation contract of docs/PLAN/08 Part C: an
// owning organization lends one project to another organization with a subset
// of the project's roles (P4-01).
//
// Specification: MEMORY/specs/P4-01-project-grants.md.
//
// This package creates, reads and revokes the contract, and nothing else. A
// grant confers no access until P4-02 lets the receiving organization assign
// the delegated roles and P4-04 puts them in tokens and checks — which is what
// makes this safe to ship before either.
package projectgrant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// MaxRoleKeys bounds a grant's role subset. Matches the OpenAPI schema.
const MaxRoleKeys = 50

// Status values, as the table's CHECK constraint names them.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

var (
	// ErrNotFound is a grant that is not visible under this project and the
	// organization that owns it — including one the caller's organization can
	// SEE only because it is the receiving side.
	ErrNotFound = errors.New("projectgrant: not found")

	// ErrUnknownRoles lists requested keys that are not roles of this project.
	ErrUnknownRoles = errors.New("projectgrant: unknown role keys")
)

// UnknownRoles carries the keys that were refused, for the error detail.
type UnknownRoles struct{ Keys []string }

func (e UnknownRoles) Error() string {
	return "projectgrant: unknown role keys: " + strings.Join(e.Keys, ", ")
}
func (e UnknownRoles) Unwrap() error { return ErrUnknownRoles }

// Grant is one delegation.
type Grant struct {
	ID              string
	ProjectID       string
	GrantingOrgID   string
	GrantedOrgID    string
	GrantedOrgName  string
	GrantedRoleKeys []string
	Status          string
	CreatedAt       time.Time
	RevokedAt       *time.Time
}

// Store reads and writes project_grants inside a tenant transaction.
type Store struct{}

func NewStore() *Store { return &Store{} }

const columns = `id, project_id, granting_org_id, granted_org_id, granted_role_keys, status, created_at, revoked_at`

func scan(row interface{ Scan(...any) error }, g *Grant) error {
	var revoked sql.NullTime
	if err := row.Scan(&g.ID, &g.ProjectID, &g.GrantingOrgID, &g.GrantedOrgID,
		pq.Array(&g.GrantedRoleKeys), &g.Status, &g.CreatedAt, &revoked); err != nil {
		return err
	}
	if revoked.Valid {
		t := revoked.Time
		g.RevokedAt = &t
	}
	return nil
}

// Create validates and inserts a grant from the organization the transaction is
// scoped to.
//
// The receiving organization and the role keys are checked HERE, in the same
// transaction as the insert, so a role deleted between the check and the write
// cannot leave a grant naming a key that no longer exists — role deletion reads
// the active grants in its own transaction and refuses (role.Store.Delete).
func (s *Store) Create(
	ctx context.Context, tx *postgres.Tx, grantingOrgID, projectID, grantedOrgID string, roleKeys []string,
) (Grant, error) {
	if grantedOrgID == grantingOrgID {
		return Grant{}, invalidOrganization()
	}

	var accepts bool
	if err := tx.QueryRow(ctx, `SELECT organization_accepts_grants($1)`, grantedOrgID).Scan(&accepts); err != nil {
		return Grant{}, fmt.Errorf("projectgrant: checking the receiving organization: %w", err)
	}
	if !accepts {
		return Grant{}, invalidOrganization()
	}

	var unknown []string
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(array_agg(k ORDER BY k), '{}')
		  FROM unnest($2::text[]) AS k
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE project_id = $1 AND key = k)`,
		projectID, pq.Array(roleKeys)).Scan(pq.Array(&unknown)); err != nil {
		return Grant{}, fmt.Errorf("projectgrant: checking the role keys: %w", err)
	}
	if len(unknown) > 0 {
		return Grant{}, UnknownRoles{Keys: unknown}
	}

	var g Grant
	err := scan(tx.QueryRow(ctx, `
		INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
		VALUES ($1, $2, $3, $4)
		RETURNING `+columns,
		projectID, grantingOrgID, grantedOrgID, pq.Array(roleKeys)), &g)
	if err != nil {
		if strings.Contains(err.Error(), "project_grants_project_granted_org_key") {
			return Grant{}, management.Fault{
				Class:   management.Conflict,
				Message: "This project already has an active grant to that organization. Revoke it first to delegate a different set of roles.",
				Reason:  "second active grant to the same organization",
			}
		}
		return Grant{}, fmt.Errorf("projectgrant: creating: %w", err)
	}
	return g, s.names(ctx, tx, []*Grant{&g})
}

// List returns the project's grants from the owning organization, newest first.
//
// Filtered on granting_org_id as well as the project, although RLS already
// bounds the rows: RLS lets a RECEIVING organization see a grant too, and a
// query that relied on visibility alone would answer the granting project's
// route with rows the path's organization did not create.
func (s *Store) List(
	ctx context.Context, tx *postgres.Tx, grantingOrgID, projectID string, after management.Cursor, size int,
) ([]Grant, error) {
	var afterTime, afterID any
	if after.ID != "" {
		afterTime, afterID = after.After, after.ID
	}
	rows, err := tx.Query(ctx, `
		SELECT `+columns+`
		  FROM project_grants
		 WHERE granting_org_id = $1 AND project_id = $2
		   AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4::uuid))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $5`,
		grantingOrgID, projectID, afterTime, afterID, size+1)
	if err != nil {
		return nil, fmt.Errorf("projectgrant: listing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Grant
	for rows.Next() {
		var g Grant
		if err := scan(rows, &g); err != nil {
			return nil, fmt.Errorf("projectgrant: listing: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projectgrant: listing: %w", err)
	}

	pointers := make([]*Grant, len(out))
	for i := range out {
		pointers[i] = &out[i]
	}
	return out, s.names(ctx, tx, pointers)
}

// Get reads one grant of this project from the owning organization.
func (s *Store) Get(ctx context.Context, tx *postgres.Tx, grantingOrgID, projectID, id string) (Grant, error) {
	var g Grant
	err := scan(tx.QueryRow(ctx, `
		SELECT `+columns+`
		  FROM project_grants
		 WHERE id = $1 AND granting_org_id = $2 AND project_id = $3`,
		id, grantingOrgID, projectID), &g)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, fmt.Errorf("projectgrant: reading: %w", err)
	}
	return g, s.names(ctx, tx, []*Grant{&g})
}

// Revoke moves an active grant to revoked. It reports whether anything
// changed: revoking a grant that is already revoked succeeds and changes
// nothing, so the caller audits only a real transition.
//
// FOR UPDATE, so two concurrent revocations cannot both report a transition
// and write two audit events for one revocation.
func (s *Store) Revoke(
	ctx context.Context, tx *postgres.Tx, grantingOrgID, projectID, id string, now time.Time,
) (Grant, bool, error) {
	var g Grant
	err := scan(tx.QueryRow(ctx, `
		SELECT `+columns+`
		  FROM project_grants
		 WHERE id = $1 AND granting_org_id = $2 AND project_id = $3
		 FOR UPDATE`,
		id, grantingOrgID, projectID), &g)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, false, ErrNotFound
	}
	if err != nil {
		return Grant{}, false, fmt.Errorf("projectgrant: reading for revocation: %w", err)
	}
	if g.Status == StatusRevoked {
		return g, false, s.names(ctx, tx, []*Grant{&g})
	}

	if _, err := tx.Exec(ctx, `
		UPDATE project_grants SET status = $1, revoked_at = $2 WHERE id = $3`,
		StatusRevoked, now, id); err != nil {
		return Grant{}, false, fmt.Errorf("projectgrant: revoking: %w", err)
	}
	g.Status = StatusRevoked
	revokedAt := now
	g.RevokedAt = &revokedAt
	return g, true, s.names(ctx, tx, []*Grant{&g})
}

// names fills in each receiving organization's name, through the one function
// that may read another organization's row — and only for organizations that
// hold a grant from this one.
func (s *Store) names(ctx context.Context, tx *postgres.Tx, grants []*Grant) error {
	if len(grants) == 0 {
		return nil
	}
	ids := make([]string, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.GrantedOrgID)
	}
	rows, err := tx.Query(ctx, `SELECT id, name FROM granted_organization_names($1::uuid[])`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("projectgrant: reading organization names: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return fmt.Errorf("projectgrant: reading organization names: %w", err)
		}
		byID[id] = name
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("projectgrant: reading organization names: %w", err)
	}
	for _, g := range grants {
		g.GrantedOrgName = byID[g.GrantedOrgID]
	}
	return nil
}

func invalidOrganization() error {
	// One answer for "that is you", "no such organization" and "a deleted or
	// suspended one", so the endpoint cannot be used to learn which ids are
	// live organizations beyond what already holding a random UUID implies.
	return management.Fault{
		Class:   management.Invalid,
		Message: "That organization cannot receive a grant of this project.",
		Details: []api.ErrorDetail{{Field: "granted_org_id", Issue: "That organization cannot receive a grant of this project."}},
		Reason:  "granted_org_id is this organization, unknown, deleted or suspended",
	}
}

// Package project implements the Management API's project endpoints (P1-17).
//
// A project is the container an application, a role and — from Phase 4 — a
// project grant all hang from. It carries no configuration of its own, which is
// why this package is short: settings that could live here would be settings
// with two homes, since an application already has its own and an
// organization's policy already applies to everything inside it.
package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Every operation here is tenant-scoped, and that is the whole story.
//
// Unlike `organizations` — whose RLS policy is keyed on `id`, so listing and
// creating need SECURITY DEFINER functions — `projects` is an ordinary
// org-scoped table. `org_id = current_org_id()` does the work, the transaction
// is scoped to the organization in the PATH, and no query here carries an
// org_id predicate.
//
// **There is no org_id parameter anywhere in this file.** That is the P1-17
// card's step 2 ("never trust an org_id supplied in the body") implemented as a
// shape rather than as a check: there is nothing to trust, because there is
// nowhere to put it.

// Project is a container, as the API represents it.
type Project struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MaxNameLength bounds a display name.
const MaxNameLength = 200

type Store struct{}

func NewStore() *Store { return &Store{} }

// ErrNotFound means no project with that id is visible in this tenant.
//
// One error for "does not exist" and "belongs to another organization", so the
// caller answers 404 for both — distinguishing them would answer "is this id
// real?" for anybody willing to send one request per guess.
var ErrNotFound = errors.New("project: not found")

// Dependents is what stops a project being deleted.
type Dependents struct {
	Applications int
	Roles        int
}

func (d Dependents) Any() bool { return d.Applications > 0 || d.Roles > 0 }

// ErrHasDependents is returned by Delete when something still hangs off the
// project. It carries the counts so the refusal can say what blocks it.
type ErrHasDependents struct{ Dependents }

func (e ErrHasDependents) Error() string {
	return fmt.Sprintf("project: %d application(s) and %d role(s) still attached",
		e.Applications, e.Roles)
}

// --- reading ---------------------------------------------------------------------------

func (s *Store) Get(ctx context.Context, tx *postgres.Tx, id string) (Project, error) {
	var p Project
	err := tx.QueryRow(ctx,
		`SELECT id, name, created_at, updated_at FROM projects WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Includes another organization's project: RLS makes it invisible, and
		// invisible arrives here as no rows.
		return Project{}, ErrNotFound
	case err != nil:
		return Project{}, fmt.Errorf("project: reading: %w", err)
	}
	return p, nil
}

// List returns a page of projects in the scoped organization.
//
// size+1 rows so Paginate can tell whether there is another page.
func (s *Store) List(
	ctx context.Context, tx *postgres.Tx, after management.Cursor, size int,
) ([]Project, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, created_at, updated_at
		  FROM projects
		 WHERE ($1::timestamptz IS NULL) OR ((created_at, id) > ($1, $2::uuid))
		 ORDER BY created_at, id
		 LIMIT $3`,
		nullTime(after), nullID(after), size+1)
	if err != nil {
		return nil, fmt.Errorf("project: listing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("project: listing: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Position is a project's sort position, for P1-15's cursor.
func Position(p Project) management.Cursor {
	return management.Cursor{After: p.CreatedAt, ID: p.ID}
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

// --- writing ---------------------------------------------------------------------------

// Create inserts a project into the scoped organization.
//
// `org_id` comes from the transaction's own scope, read back from the Tx rather
// than passed in. RLS would refuse a row for any other tenant anyway — the
// WITH CHECK clause sees to that — so this is belt and braces, and the braces
// are the ones that report a clear error instead of a policy violation.
func (s *Store) Create(ctx context.Context, tx *postgres.Tx, name string) (Project, error) {
	orgID := tx.OrgID()
	if orgID == "" {
		return Project{}, fmt.Errorf("project: creating without a tenant scope")
	}

	var p Project
	err := tx.QueryRow(ctx, `
		INSERT INTO projects (org_id, name) VALUES ($1, $2)
		RETURNING id, name, created_at, updated_at`,
		orgID, strings.TrimSpace(name),
	).Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Project{}, wrapConstraint(err, "creating")
	}
	return p, nil
}

// Rename changes a project's name. It is the only thing a project has.
func (s *Store) Rename(ctx context.Context, tx *postgres.Tx, id, name string) (Project, error) {
	var p Project
	err := tx.QueryRow(ctx, `
		UPDATE projects SET name = $2 WHERE id = $1
		RETURNING id, name, created_at, updated_at`,
		id, strings.TrimSpace(name),
	).Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Project{}, ErrNotFound
	case err != nil:
		return Project{}, wrapConstraint(err, "renaming")
	}
	return p, nil
}

// DependentsOf counts what would block a delete.
func (s *Store) DependentsOf(ctx context.Context, tx *postgres.Tx, id string) (Dependents, error) {
	var d Dependents
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM applications WHERE project_id = $1),
		       (SELECT count(*) FROM roles        WHERE project_id = $1)`, id,
	).Scan(&d.Applications, &d.Roles)
	if err != nil {
		return Dependents{}, fmt.Errorf("project: counting dependents: %w", err)
	}
	return d, nil
}

// Delete removes a project, or reports what is still attached.
//
// **The count happens first and the refusal is explicit**, rather than letting
// the foreign key raise a constraint violation. The database would stop the
// delete either way — every reference is ON DELETE RESTRICT — but "violates
// foreign key constraint applications_project_id_fkey" is not something to put
// in front of an administrator, and it cannot say that there are four
// applications and two roles.
//
// Not cascaded, and that is the important half. Deleting a project would take
// every OIDC client in it, and every consumer application configured against
// those client ids would stop authenticating — a far larger consequence than
// the request appears to ask for.
func (s *Store) Delete(ctx context.Context, tx *postgres.Tx, id string) error {
	if _, err := s.Get(ctx, tx, id); err != nil {
		return err
	}

	dependents, err := s.DependentsOf(ctx, tx, id)
	if err != nil {
		return err
	}
	if dependents.Any() {
		return ErrHasDependents{dependents}
	}

	result, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		// Reachable if something was attached between the count and the delete.
		// The transaction makes that unlikely and not impossible, and the
		// honest answer is the same refusal rather than a 500.
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			return ErrHasDependents{Dependents{}}
		}
		return fmt.Errorf("project: deleting: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// wrapConstraint turns a database constraint into an error the API can answer.
func wrapConstraint(err error, doing string) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "projects_org_name_key"):
		// Named without saying which project holds it — although here the
		// caller can already list every project in the organization, so this is
		// consistency rather than concealment.
		return management.Fault{
			Class:   management.Conflict,
			Message: "A project with that name already exists in this organization.",
			Reason:  "project name uniqueness violation",
		}
	case strings.Contains(text, "projects_name_not_blank"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "The project name must not be blank.",
			Reason:  "blank name reached the database",
		}
	case strings.Contains(text, "row-level security"):
		// A write aimed at another tenant. Not reachable through this package
		// — Create reads the scope from the transaction — so if it happens,
		// something else is wrong and it must not read as a validation error.
		return fmt.Errorf("project: %s: refused by row-level security: %w", doing, err)
	}
	return fmt.Errorf("project: %s: %w", doing, err)
}

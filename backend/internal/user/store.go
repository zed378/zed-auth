// Package user implements the Management API's user endpoints and the two
// unauthenticated flows that let a person take possession of an account
// (P1-19).
//
// The rule that shapes the whole package: **no password crosses this API in
// either direction.** There is no password field on create, none on update,
// none in any response, and no method here that accepts a hash. A password is
// set only by the person the account belongs to, through a single-use link
// sent to their own address — which is also the only thing that proves the
// address is theirs.
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Every query here is tenant-scoped by the transaction, exactly as P1-17
// established: RLS supplies `org_id = current_org_id()` and no method takes an
// org_id, so there is no second way to name a tenant and therefore no second
// thing to check.

// Status values, matching the users_status_valid CHECK from P0-07.
const (
	StatusInvited     = "invited"
	StatusActive      = "active"
	StatusLocked      = "locked"
	StatusDeactivated = "deactivated"
)

// Bounds, matching the contract.
const (
	MaxEmailLength       = 320
	MaxUsernameLength    = 100
	MaxDisplayNameLength = 200
	MaxSearchLength      = 200
)

// ErrNotFound means no such user is visible in this tenant.
var ErrNotFound = errors.New("user: not found")

// ErrEmailTaken means the address is already used in this organization.
//
// A conflict here is NOT hidden, unlike the not-found answers elsewhere in this
// service. The caller is an authenticated administrator who can already list
// every user in the organization, so refusing to say "that address is taken"
// would withhold nothing and would leave them guessing why the create failed.
// Enumeration is a concern where the caller is anonymous, and here they are not.
var ErrEmailTaken = errors.New("user: email already in use in this organization")

// ErrUsernameTaken means the username is already used in this organization.
var ErrUsernameTaken = errors.New("user: username already in use in this organization")

// User is an account as the API represents it.
type User struct {
	ID          string
	Email       string
	Username    string
	DisplayName string
	Status      string
	MFAEnabled  bool

	// EmailVerifiedAt is nil until an invitation is accepted through a link
	// sent to that address (PG-18). A timestamp rather than a boolean because
	// "when" answers questions "whether" cannot.
	EmailVerifiedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Verified reports whether the address has been proven reachable.
func (u User) Verified() bool { return u.EmailVerifiedAt != nil }

type Store struct{}

func NewStore() *Store { return &Store{} }

const columns = `id, email, coalesce(username, ''), coalesce(display_name, ''),
                 status, mfa_enabled, email_verified_at, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (User, error) {
	var (
		u        User
		verified sql.NullTime
	)
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName,
		&u.Status, &u.MFAEnabled, &verified, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return User{}, err
	}
	if verified.Valid {
		at := verified.Time
		u.EmailVerifiedAt = &at
	}
	return u, nil
}

// --- reading ---------------------------------------------------------------------------

func (s *Store) Get(ctx context.Context, tx *postgres.Tx, id string) (User, error) {
	u, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM users WHERE id = $1`, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, ErrNotFound
	case err != nil:
		return User{}, fmt.Errorf("user: reading: %w", err)
	}
	return u, nil
}

// ByEmail finds a user by address within the scoped tenant.
//
// Used by the self-service reset flow, which knows its organization because it
// is reached from inside a pending authorization request — the same way P1-12's
// login handler knows it. No cross-tenant lookup, and therefore no
// SECURITY DEFINER function that could answer "does this address exist
// anywhere".
func (s *Store) ByEmail(ctx context.Context, tx *postgres.Tx, email string) (User, error) {
	normalised := authn.NormaliseEmail(email)
	if normalised == "" || len(normalised) > MaxEmailLength {
		return User{}, ErrNotFound
	}

	u, err := scan(tx.QueryRow(ctx,
		`SELECT `+columns+` FROM users WHERE lower(email) = $1`, normalised))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, ErrNotFound
	case err != nil:
		return User{}, fmt.Errorf("user: reading by email: %w", err)
	}
	return u, nil
}

// List returns a page of users, optionally filtered.
//
// The search term is a bound parameter wrapped in wildcards by the database,
// never interpolated into the statement — `%` and `_` inside a caller's term
// are then matched literally by ESCAPE, so somebody searching for "a_b" does
// not get every three-character name.
func (s *Store) List(
	ctx context.Context, tx *postgres.Tx, search string, after management.Cursor, size int,
) ([]User, error) {
	term := strings.TrimSpace(search)
	if len(term) > MaxSearchLength {
		term = term[:MaxSearchLength]
	}

	rows, err := tx.Query(ctx, `
		SELECT `+columns+`
		  FROM users
		 WHERE (($1::text = '') OR (
		         email ILIKE '%' || replace(replace($1, '%', '\%'), '_', '\_') || '%' ESCAPE '\'
		      OR coalesce(username, '') ILIKE '%' || replace(replace($1, '%', '\%'), '_', '\_') || '%' ESCAPE '\'
		      OR coalesce(display_name, '') ILIKE '%' || replace(replace($1, '%', '\%'), '_', '\_') || '%' ESCAPE '\'))
		   AND (($2::timestamptz IS NULL) OR ((created_at, id) > ($2, $3::uuid)))
		 ORDER BY created_at, id
		 LIMIT $4`,
		term, nullTime(after), nullID(after), size+1)
	if err != nil {
		return nil, fmt.Errorf("user: listing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []User
	for rows.Next() {
		u, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("user: listing: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Position is a user's sort position, for P1-15's cursor.
func Position(u User) management.Cursor {
	return management.Cursor{After: u.CreatedAt, ID: u.ID}
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

// Create inserts an invited user.
//
// **No password and no hash parameter.** The row is written with
// `password_hash` NULL and `status` invited, which is a user who cannot sign
// in until they set one themselves. P0-07 made `password_hash` nullable for
// exactly this state.
func (s *Store) Create(
	ctx context.Context, tx *postgres.Tx, email, username, displayName string,
) (User, error) {
	orgID := tx.OrgID()
	if orgID == "" {
		return User{}, fmt.Errorf("user: creating without a tenant scope")
	}

	u, err := scan(tx.QueryRow(ctx, `
		INSERT INTO users (org_id, email, username, display_name, status)
		VALUES ($1, $2, nullif($3, ''), nullif($4, ''), 'invited')
		RETURNING `+columns,
		orgID, authn.NormaliseEmail(email), strings.TrimSpace(username),
		strings.TrimSpace(displayName)))
	if err != nil {
		return User{}, wrapConstraint(err, "creating")
	}
	return u, nil
}

// Profile is the mutable part of a user. Every field is a pointer, so "not
// mentioned" and "set to empty" are different requests — clearing a display
// name is a real thing to want, and a non-pointer struct cannot express it.
type Profile struct {
	Email       *string
	Username    *string
	DisplayName *string
}

// UpdateProfile changes profile fields and nothing else.
//
// Status is not here, and neither is email_verified_at except as a
// consequence: changing the address clears the verification, because a
// verification is a statement about one address and not about the user.
func (s *Store) UpdateProfile(
	ctx context.Context, tx *postgres.Tx, id string, p Profile,
) (User, error) {
	before, err := s.Get(ctx, tx, id)
	if err != nil {
		return User{}, err
	}

	email := before.Email
	clearVerification := false
	if p.Email != nil {
		normalised := authn.NormaliseEmail(*p.Email)
		if normalised != authn.NormaliseEmail(before.Email) {
			email = normalised
			clearVerification = true
		}
	}

	username := before.Username
	if p.Username != nil {
		username = strings.TrimSpace(*p.Username)
	}
	displayName := before.DisplayName
	if p.DisplayName != nil {
		displayName = strings.TrimSpace(*p.DisplayName)
	}

	u, err := scan(tx.QueryRow(ctx, `
		UPDATE users
		   SET email = $2,
		       username = nullif($3, ''),
		       display_name = nullif($4, ''),
		       email_verified_at = CASE WHEN $5 THEN NULL ELSE email_verified_at END
		 WHERE id = $1
		RETURNING `+columns,
		id, email, username, displayName, clearVerification))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, ErrNotFound
	case err != nil:
		return User{}, wrapConstraint(err, "updating")
	}
	return u, nil
}

// SetStatus moves a user between statuses.
//
// Returns the previous status so the caller can tell a real change from a
// repeat — a deactivation of an already-deactivated user should not write a
// second audit event saying it happened again.
func (s *Store) SetStatus(
	ctx context.Context, tx *postgres.Tx, id, status string,
) (User, string, error) {
	before, err := s.Get(ctx, tx, id)
	if err != nil {
		return User{}, "", err
	}

	u, err := scan(tx.QueryRow(ctx,
		`UPDATE users SET status = $2 WHERE id = $1 RETURNING `+columns, id, status))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, "", ErrNotFound
	case err != nil:
		return User{}, "", wrapConstraint(err, "changing status")
	}
	return u, before.Status, nil
}

// SetPassword stores a new password hash and marks when it changed.
//
// Takes the PLAINTEXT and hashes it here, so no caller can pass a hash of its
// own choosing — an API that accepts a hash is an API where a stolen hash is a
// credential.
//
// `verifyEmail` is set only by the invitation flow: accepting an invitation
// proves the address was reachable, which is what verification means (PG-18).
// A password reset proves it too, but P1-19.5 is the flow the backlog names
// and widening it silently would be a security claim made by accident.
func (s *Store) SetPassword(
	ctx context.Context, tx *postgres.Tx, id, plaintext string, verifyEmail bool, now time.Time,
) error {
	hash, err := authn.Hash(plaintext)
	if err != nil {
		return fmt.Errorf("user: hashing the new password: %w", err)
	}

	result, err := tx.Exec(ctx, `
		UPDATE users
		   SET password_hash = $2,
		       password_changed_at = $3,
		       status = CASE WHEN status = 'invited' THEN 'active' ELSE status END,
		       email_verified_at = CASE
		           WHEN $4 AND email_verified_at IS NULL THEN $3
		           ELSE email_verified_at END
		 WHERE id = $1`, id, hash, now, verifyEmail)
	if err != nil {
		return fmt.Errorf("user: storing the new password: %w", err)
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
	case strings.Contains(text, "users_org_email_key"):
		return ErrEmailTaken
	case strings.Contains(text, "users_org_username_key"):
		return ErrUsernameTaken
	case strings.Contains(text, "users_email_shape"), strings.Contains(text, "users_email_not_blank"):
		return management.Fault{
			Class:   management.Invalid,
			Message: "That email address is not valid.",
			Reason:  "an unusable address reached the database",
		}
	case strings.Contains(text, "row-level security"):
		return fmt.Errorf("user: %s: refused by row-level security: %w", doing, err)
	}
	return fmt.Errorf("user: %s: %w", doing, err)
}

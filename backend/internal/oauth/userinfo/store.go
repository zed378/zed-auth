package userinfo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// ErrNoSubject means there is nobody to describe: the user is gone, no longer
// active, or their session is no longer live.
//
// One error for all three, because the caller answers all three identically.
// Separating them here would only invite a caller to separate them on the
// wire, which is the disclosure §12 of the spec is about.
var ErrNoSubject = errors.New("userinfo: no live subject for this token")

// Store reads the subject behind a token.
type Store struct{}

func NewStore() *Store { return &Store{} }

// Subject reads the user's claims and confirms the session is still live, in
// one query.
//
// The liveness predicate is the interesting part, and it is a deliberate
// choice rather than an oversight in either direction.
//
// PLAN/04 § What Is Deliberately Not Stored Here says access tokens are not
// stored because "storing them would add a lookup to the hottest path in the
// system for no security gain". That reasoning is about the token endpoint and
// about storing tokens; neither changes here. Nothing is stored, and this is
// not the hottest path.
//
// What would change without the check is real: a user clicks "log out", their
// session is revoked, and this endpoint keeps describing them for up to ten
// minutes — the access token's remaining life. For a call an SPA makes on
// every page load, that is the difference between logging out and appearing
// to.
//
// It costs nothing, because the user's row has to be read anyway. The liveness
// test is a predicate in the same statement, in the same tenant-scoped
// transaction, in one round trip. If it needed a second round trip the trade
// would be worth arguing about.
//
// The transaction is scoped to the organization named in the token, so RLS
// confines the read without this file containing an org_id predicate anybody
// could forget — and the organization came from a token this service signed,
// not from anything the caller chose.
func (s *Store) Subject(
	ctx context.Context, db *postgres.DB, token AccessToken, now time.Time,
) (Subject, string, error) {
	var (
		subject   Subject
		email     string
		name      sql.NullString
		username  sql.NullString
		updatedAt sql.NullTime
	)

	err := db.WithTenant(ctx, token.OrgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT u.id, u.email, u.display_name, u.username, u.updated_at
			  FROM users u
			 WHERE u.id = $1
			   AND u.status = 'active'
			   AND EXISTS (
			         SELECT 1 FROM sessions s
			          WHERE s.id = $2
			            AND s.user_id = u.id
			            AND s.revoked_at IS NULL
			            AND s.expires_at > $3
			       )`,
			token.Subject, token.SessionID, now,
		).Scan(&subject.UserID, &email, &name, &username, &updatedAt)
	})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Subject{}, "", ErrNoSubject
	case err != nil:
		return Subject{}, "", fmt.Errorf("userinfo: reading the subject: %w", err)
	}

	subject.Name = name.String
	subject.PreferredUsername = username.String
	if updatedAt.Valid {
		subject.UpdatedAt = updatedAt.Time
	}

	return subject, email, nil
}

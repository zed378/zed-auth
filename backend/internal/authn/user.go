package authn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Verifying a password against a stored user.
//
// The half of P1-01 that had no caller until P1-12. Everything here exists to
// make one property hold: an address with no account must be indistinguishable
// from an address with a wrong password — in what comes back, and in how long
// it takes.
//
// The stored hash never leaves this file. Authenticate takes the password and
// returns a decision, rather than returning a hash for a caller to compare,
// because a hash on an exported struct is a hash that eventually gets logged,
// serialised into an error, or included in a debug dump.

// maxEmailLength bounds the address before it reaches a query.
//
// An address is attacker-supplied on an unauthenticated endpoint. RFC 5321
// caps a path at 254 octets; 320 is generous against that and stops the login
// form being a way to push bytes into a LIKE-free but still indexed lookup.
const maxEmailLength = 320

// Status values from docs/PLAN/04 § users.
//
// Only StatusActive may sign in. The other three are enumerated rather than
// left as a string comparison at the call site, because "not active" is the
// security-relevant condition and it should be one named thing.
const (
	StatusActive      = "active"
	StatusLocked      = "locked"
	StatusInvited     = "invited"
	StatusDeactivated = "deactivated"
)

// User is a person as the login path needs them.
//
// No password hash, by construction rather than by discipline.
type User struct {
	ID          string
	OrgID       string
	Email       string
	DisplayName string
	Status      string
	MFAEnabled  bool

	// PasswordChangedAt is nil when it was never recorded. PG-13's column is
	// nullable with no backfill, and NULL means NOT expired — see Expired.
	PasswordChangedAt *time.Time

	// NeedsRehash is set only when the password verified and the stored hash
	// used weaker parameters than Current. Rehashing needs the plaintext, so
	// login is the only moment it can happen.
	NeedsRehash bool
}

// CanSignIn reports whether this account may authenticate at all.
//
// Locked, invited and deactivated are all false, and the caller must give the
// same answer for all three as it gives for a wrong password. "This account is
// locked" confirms the account exists, which is the enumeration disclosure the
// uniform message exists to prevent (docs/SECURITY/02 §12).
func (u User) CanSignIn() bool { return u.Status == StatusActive }

// UserStore reads users for authentication.
type UserStore struct{}

func NewUserStore() *UserStore { return &UserStore{} }

// Authenticate verifies a password against the user with this email.
//
// The three outcomes are (User{}, false) for no such user, (user, false) for a
// wrong password or an account with no password at all, and (user, true) for a
// verified one. An error means the lookup itself failed — never that the
// password was wrong.
//
// Every path performs one Argon2 computation of the current cost. That is the
// enumeration defence and it is why this is one method: a caller that looked a
// user up and then decided whether to verify would be a caller that returns
// early on the not-found path, and the timing difference is the disclosure.
//
// The transaction is tenant-scoped, so RLS confines the lookup to one
// organization without an org_id predicate anybody could forget — and the
// organization comes from the client the authorization request named, not from
// anything the person typing chose.
func (s *UserStore) Authenticate(
	ctx context.Context, tx *postgres.Tx, email, password string,
) (User, bool, error) {
	user, hash, err := s.byEmail(ctx, tx, email)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// The equal-cost path. NOT a bare return: this is the whole reason
		// VerifyDummy exists (P1-01), and returning early here would make the
		// response time a registered-address oracle.
		VerifyDummy(password)
		return User{}, false, nil
	case err != nil:
		return User{}, false, fmt.Errorf("authn: looking up user: %w", err)
	}

	if hash == "" {
		// A federated or not-yet-invited user with no password. Indistinguishable
		// from a wrong password, at the same cost, for the same reason.
		VerifyDummy(password)
		return user, false, nil
	}

	result, err := Verify(hash, password)
	if err != nil {
		// An unusable stored hash: corrupt, or written by something that did
		// not use this encoder. Not a wrong password, and worth surfacing —
		// but the caller must still answer the browser identically.
		return user, false, fmt.Errorf("authn: verifying password for user %s: %w", user.ID, err)
	}
	if !result.Match {
		return user, false, nil
	}

	user.NeedsRehash = result.NeedsRehash
	return user, true, nil
}

// byEmail reads one user and their stored hash.
//
// Unexported and returning the hash as a bare local, so the only thing that
// can reach it is Authenticate, three lines below.
func (s *UserStore) byEmail(
	ctx context.Context, tx *postgres.Tx, email string,
) (User, string, error) {
	normalised := NormaliseEmail(email)
	if normalised == "" || len(normalised) > maxEmailLength {
		// Not a query. A blank or absurd address cannot match a row, and
		// sql.ErrNoRows is exactly what the caller's not-found path expects —
		// including its Argon2 computation, so this shortcut does not become a
		// timing difference of its own.
		return User{}, "", sql.ErrNoRows
	}

	var (
		user      User
		hash      sql.NullString
		display   sql.NullString
		changedAt sql.NullTime
	)

	err := tx.QueryRow(ctx, `
		SELECT id, org_id, email, display_name, status, mfa_enabled,
		       password_hash, password_changed_at
		  FROM users
		 WHERE lower(email) = $1`, normalised,
	).Scan(&user.ID, &user.OrgID, &user.Email, &display, &user.Status, &user.MFAEnabled,
		&hash, &changedAt)
	if err != nil {
		return User{}, "", err
	}

	user.DisplayName = display.String
	if changedAt.Valid {
		at := changedAt.Time
		user.PasswordChangedAt = &at
	}

	return user, hash.String, nil
}

// RecordRehash stores an upgraded password hash.
//
// Called only after a successful verification with NeedsRehash set. It does
// NOT touch password_changed_at: the password did not change, only the cost of
// storing it did, and moving that column would silently reset every user's
// expiry clock on the day the parameters were raised.
func (s *UserStore) RecordRehash(ctx context.Context, tx *postgres.Tx, userID, password string) error {
	hash, err := Hash(password)
	if err != nil {
		return fmt.Errorf("authn: rehashing password: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, hash)
	if err != nil {
		return fmt.Errorf("authn: storing rehashed password: %w", err)
	}
	return nil
}

// NormaliseEmail is how an address is compared.
//
// Lowercased and trimmed, matching the `users_email_unique` index on
// lower(email) from P0-07's migration. Nothing cleverer: stripping dots or
// plus-addressing is provider-specific behaviour, and a service that decides
// two different addresses are the same person has decided something it was
// never told.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

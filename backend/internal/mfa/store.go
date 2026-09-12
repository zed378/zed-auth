package mfa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// `user_mfa_factors`, read and written (P3-02).
//
// Every method takes the caller's transaction, so a factor and its audit event
// commit together (ADR-012) — an enrolment recorded without an audit entry, or
// an audit entry for an enrolment that rolled back, are both worse than either
// failing.
//
// No method returns a decrypted secret. `Sealed` returns the ciphertext and the
// only thing that opens it is the verifier that owns the key, which keeps the
// number of places a plaintext factor secret exists to one.

// Store is the Postgres implementation of FactorStore.
type Store struct{}

func NewStore() *Store { return &Store{} }

// Insert creates a PENDING factor.
//
// Pending is not a default somebody could forget to set: the column's own
// default is `pending`, so a row inserted by any path that does not name the
// status is inert rather than live.
func (s *Store) Insert(
	ctx context.Context, tx *postgres.Tx, userID, orgID string, t Type, label string, sealed []byte,
) (string, error) {
	if !t.Valid() {
		// Refused before the database sees it, so the error names the type
		// rather than quoting a CHECK constraint at whoever is reading the log.
		return "", fmt.Errorf("%w: %q", ErrUnsupported, t)
	}
	if len(sealed) == 0 {
		// An empty secret would satisfy the NOT NULL and verify nothing: the
		// factor would read as enrolled and refuse every code.
		return "", fmt.Errorf("mfa: refusing to store a factor with no secret")
	}

	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO user_mfa_factors (user_id, org_id, type, label, secret_encrypted)
		 VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		 RETURNING id`,
		userID, orgID, string(t), label, sealed).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("mfa: inserting a factor: %w", err)
	}
	return id, nil
}

// Sealed reads one factor's ciphertext and status.
func (s *Store) Sealed(
	ctx context.Context, tx *postgres.Tx, factorID string,
) ([]byte, Status, error) {
	var sealed []byte
	var status string

	err := tx.QueryRow(ctx,
		`SELECT secret_encrypted, status FROM user_mfa_factors WHERE id = $1`,
		factorID).Scan(&sealed, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrFactorNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("mfa: reading a factor: %w", err)
	}
	if len(sealed) == 0 {
		// A TOTP factor with no secret cannot verify anything. Reported as a
		// fault rather than as a wrong code, because it is one.
		return nil, Status(status), fmt.Errorf("mfa: factor %s has no stored secret", factorID)
	}
	return sealed, Status(status), nil
}

// Activate marks a pending factor active.
func (s *Store) Activate(ctx context.Context, tx *postgres.Tx, factorID string) error {
	// `WHERE status = 'pending'` rather than an unconditional update: a second
	// confirmation of an already-active factor must not look like a successful
	// enrolment, and making the database refuse it means the check cannot be
	// skipped by a future caller.
	result, err := tx.Exec(ctx,
		`UPDATE user_mfa_factors
		    SET status = 'active', updated_at = now()
		  WHERE id = $1 AND status = 'pending'`, factorID)
	if err != nil {
		return fmt.Errorf("mfa: activating a factor: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("mfa: factor %s was not pending", factorID)
	}
	return nil
}

// RecordUse stores the counter a verification consumed.
//
// Returns `replayed` when the step has already been used — which is the whole
// replay bound (`P3-02` step 6), and it is enforced by the WHERE clause rather
// than by a read-then-write. Two requests presenting the same code at the same
// moment would both pass a read-then-write; only one can win this.
//
// Strictly greater, not merely different: a code from a step already passed is
// refused too, so an attacker who captured a code cannot use it once the clock
// has moved on and then moved back within the skew window.
func (s *Store) RecordUse(
	ctx context.Context, tx *postgres.Tx, factorID string, counter uint64, at time.Time,
) (bool, error) {
	result, err := tx.Exec(ctx,
		`UPDATE user_mfa_factors
		    SET last_used_counter = $2, last_used_at = $3, updated_at = now()
		  WHERE id = $1
		    AND (last_used_counter IS NULL OR last_used_counter < $2)`,
		// #nosec G115 -- the counter is `unix_seconds/30`, so it passes int64's
		// range around the year 292-billion. Postgres has no unsigned integer
		// type, so int64 is the column's type and the conversion is the
		// boundary, not a narrowing.
		factorID, int64(counter), at)
	if err != nil {
		return false, fmt.Errorf("mfa: recording a factor use: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mfa: recording a factor use: %w", err)
	}
	// Nothing updated means the counter was not greater — the code belongs to
	// a step already spent.
	return affected == 0, nil
}

// Delete removes a factor.
func (s *Store) Delete(ctx context.Context, tx *postgres.Tx, factorID string) error {
	result, err := tx.Exec(ctx, `DELETE FROM user_mfa_factors WHERE id = $1`, factorID)
	if err != nil {
		return fmt.Errorf("mfa: deleting a factor: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrFactorNotFound
	}
	return nil
}

// ForUser lists a user's factors.
//
// **Without their secrets.** The column is not in the SELECT at all, so no
// caller of this can hold one even by mistake — the same shape `session.Session`
// uses to keep a token hash out of the type system (`PG-14`).
func (s *Store) ForUser(ctx context.Context, tx *postgres.Tx, userID string) ([]Factor, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, user_id, org_id, type, coalesce(label, ''), status, last_used_at, created_at
		   FROM user_mfa_factors
		  WHERE user_id = $1
		  ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("mfa: listing a user's factors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Factor
	for rows.Next() {
		var f Factor
		var kind, status string
		var lastUsed sql.NullTime

		if err := rows.Scan(&f.ID, &f.UserID, &f.OrgID, &kind, &f.Label,
			&status, &lastUsed, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("mfa: listing a user's factors: %w", err)
		}
		f.Type = Type(kind)
		f.Status = Status(status)
		if lastUsed.Valid {
			when := lastUsed.Time
			f.LastUsedAt = &when
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

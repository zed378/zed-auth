package mfa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The recovery-code store (P3-04).
//
// Four operations, and the interesting property is which of them are one
// statement: consuming a code and replacing a batch both have to be, because
// both have a window in which a user could hold two working sets of codes or
// none.

// ErrNoRecoveryCode is a code that matches nothing unspent for this user.
//
// **One error for wrong, used, and belonging-to-somebody-else.** The
// distinction between "that code is wrong" and "you already used that one" is a
// fact about the user's own history, and the caller who would learn it is an
// attacker who has already proven a password — which is exactly who this
// control exists for.
var ErrNoRecoveryCode = errors.New("mfa: no unused recovery code matches")

// RecoveryStore reads and writes recovery codes.
type RecoveryStore struct{}

func NewRecoveryStore() *RecoveryStore { return &RecoveryStore{} }

// Issue replaces a user's codes with a fresh batch.
//
// **Delete and insert in the caller's transaction**, so there is no moment when
// both batches work and none when neither does. Regeneration is F-5 and it is
// the whole reason `batch_id` exists: invalidating the previous set is one
// statement rather than a list of ids assembled by the application.
//
// Returns the plaintext codes. They exist in this return value and in the
// response it becomes, and nowhere else — not in a column, not in an audit
// payload, not in a log line.
func (s *RecoveryStore) Issue(
	ctx context.Context, tx *postgres.Tx, userID, orgID string, count int, now time.Time,
) ([]string, string, error) {
	codes, err := NewRecoveryCodes(count)
	if err != nil {
		return nil, "", err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, "", fmt.Errorf("mfa: clearing previous recovery codes: %w", err)
	}

	batchID := uuid.NewString()

	for _, code := range codes {
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_recovery_codes (user_id, org_id, code_hash, batch_id, created_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			userID, orgID, HashRecoveryCode(code), batchID, now); err != nil {
			return nil, "", fmt.Errorf("mfa: storing a recovery code: %w", err)
		}
	}

	return codes, batchID, nil
}

// Consume spends one code and reports how many remain.
//
// Single-use is enforced by `used_at IS NULL` in the UPDATE, not by a
// read-then-write. Two browsers presenting the same code at the same moment
// would both pass a read-then-write and both be let in; only one can win this.
//
// The lookup is by hash AND user, so a code cannot be spent against an account
// it was not issued for even if an attacker somehow held one — abuse case A-3,
// closed by the query rather than by a check somebody has to remember.
func (s *RecoveryStore) Consume(
	ctx context.Context, tx *postgres.Tx, userID, submitted string, now time.Time,
) (remaining int, err error) {
	normalised := NormaliseRecoveryCode(submitted)
	if !ValidRecoveryCodeShape(normalised) {
		// Refused before the query. A malformed value could not have been
		// generated, so asking the database about it is work done to reach an
		// answer already available.
		return 0, ErrNoRecoveryCode
	}

	result, err := tx.Exec(ctx,
		`UPDATE user_recovery_codes
		    SET used_at = $3
		  WHERE user_id = $1
		    AND code_hash = $2
		    AND used_at IS NULL`,
		userID, HashRecoveryCode(normalised), now)
	if err != nil {
		return 0, fmt.Errorf("mfa: consuming a recovery code: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mfa: consuming a recovery code: %w", err)
	}
	if affected == 0 {
		// Wrong, already used, or another user's. All the same answer.
		return 0, ErrNoRecoveryCode
	}

	return s.Remaining(ctx, tx, userID)
}

// Remaining counts a user's unspent codes (F-4).
func (s *RecoveryStore) Remaining(ctx context.Context, tx *postgres.Tx, userID string) (int, error) {
	var count int
	err := tx.QueryRow(ctx,
		`SELECT count(*) FROM user_recovery_codes
		  WHERE user_id = $1 AND used_at IS NULL`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("mfa: counting recovery codes: %w", err)
	}
	return count, nil
}

// HasUnused reports whether a recovery code can be offered at all.
//
// Read when a challenge is raised, so the page does not offer a way in that the
// user has no way to take — a dead option on the challenge page is somebody
// who cannot sign in reading that they can.
func (s *RecoveryStore) HasUnused(ctx context.Context, tx *postgres.Tx, userID string) (bool, error) {
	count, err := s.Remaining(ctx, tx, userID)
	return count > 0, err
}

// Clear destroys a user's codes, used and unused alike.
//
// For the administrator-assisted reset (F-6). Used codes go too: they are spent
// credentials of an account that is being handed back, and keeping the row
// would leave a hash of a code somebody once wrote down.
func (s *RecoveryStore) Clear(ctx context.Context, tx *postgres.Tx, userID string) (int, error) {
	result, err := tx.Exec(ctx,
		`DELETE FROM user_recovery_codes WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("mfa: clearing recovery codes: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mfa: clearing recovery codes: %w", err)
	}
	return int(affected), nil
}

// --- the framework's seam ------------------------------------------------------

// PostgresRecovery is the RecoveryCodes interface over the store.
//
// Named for what the framework asks rather than for the table, on the same
// pattern as PostgresFactors — and taking the organization rather than
// resolving it, for the reason P3-03 changed that seam: every caller already
// holds the org, so resolving it would need a privilege none of them needs.
type PostgresRecovery struct {
	Store *RecoveryStore
	DB    Tenant

	// Now is overridable for tests.
	Now func() time.Time
}

func (p *PostgresRecovery) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *PostgresRecovery) Unspent(ctx context.Context, orgID, userID string) (bool, error) {
	var has bool
	err := p.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		var err error
		has, err = p.Store.HasUnused(ctx, tx, userID)
		return err
	})
	return has, err
}

func (p *PostgresRecovery) Spend(ctx context.Context, orgID, userID, code string) (int, error) {
	var remaining int
	err := p.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		var err error
		remaining, err = p.Store.Consume(ctx, tx, userID, code, p.now())
		return err
	})
	return remaining, err
}

// --- the administrator-assisted reset -------------------------------------------

// AdminReset is the `user` package's MfaFactors, implemented here.
//
// It lives in this package rather than in `user` so that the SQL touching
// factor tables stays with the factors. The `user` package holds an interface
// and never learns what a factor is — which is what keeps "clear this user's
// second factor" from becoming a second place that knows how factors are
// stored.
type AdminReset struct {
	Factors  *Store
	Recovery *RecoveryStore
}

// ClearFactors removes every enrolled factor, pending and active alike.
//
// Pending ones too: a half-finished enrolment is a secret the user cannot use
// and cannot see, and leaving it behind would let a reset be followed by a
// confirmation of something enrolled before the account was lost.
func (a *AdminReset) ClearFactors(ctx context.Context, tx *postgres.Tx, userID string) (int, error) {
	result, err := tx.Exec(ctx, `DELETE FROM user_mfa_factors WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("mfa: clearing a user's factors: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mfa: clearing a user's factors: %w", err)
	}
	return int(affected), nil
}

// ClearRecoveryCodes removes every recovery code.
func (a *AdminReset) ClearRecoveryCodes(ctx context.Context, tx *postgres.Tx, userID string) (int, error) {
	return a.Recovery.Clear(ctx, tx, userID)
}

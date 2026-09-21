package saml

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Replay protection (P4-07 A-4).
//
// A SAML assertion is a bearer credential for the length of its validity
// window. Anyone who obtains a copy — from a proxy log, a browser history, a
// misconfigured service provider that echoes it — can present it again, and a
// consumer that only checks the signature and the window accepts it, because
// both are still perfectly valid. Nothing about the second presentation differs
// from the first except that it already happened.
//
// So the assertion ID is recorded, and a second presentation of the same ID is
// refused.

// ErrReplayed is an assertion that has been seen before.
var ErrReplayed = errors.New("saml: the assertion has already been used")

// Replay records assertion IDs and refuses a repeat.
type Replay struct{}

// NewReplay returns a Replay.
func NewReplay() *Replay { return &Replay{} }

// Record writes an assertion ID, or reports that it was already there.
//
// The uniqueness of the primary key IS the check, and that is a deliberate
// choice over the obvious shape:
//
//	SELECT ... ; if not found { INSERT ... }
//
// which has a window between the two statements. Two copies of the same
// assertion arriving together both find nothing, both insert, and both are
// accepted — the race is small and an attacker replaying a captured assertion
// controls the timing exactly. The insert is atomic; the conflict is the answer.
//
// It runs inside the caller's tenant transaction, so the row is written under
// the same organization the assertion belongs to and row-level security applies
// without this package restating it.
func (r *Replay) Record(
	ctx context.Context, tx *postgres.Tx, orgID, assertionID string, issuedAt, expiresAt time.Time,
) error {
	if strings.TrimSpace(assertionID) == "" {
		// An assertion with no ID cannot be recorded, and therefore cannot be
		// protected from replay. Refusing is the only safe reading: accepting
		// it would create a credential that can be used an unlimited number of
		// times, which is the opposite of what this function is for.
		return fmt.Errorf("%w: the assertion has no ID", ErrReplayed)
	}
	if !expiresAt.After(issuedAt) {
		return fmt.Errorf("saml: the assertion expires before it was issued")
	}

	// issued_at is written explicitly rather than left to a column default.
	// The CHECK compares it against expires_at, and a comparison between two
	// clocks asserts something about clock skew rather than about the row —
	// this repository has already spent three days on exactly that.
	result, err := tx.Exec(ctx, `
		INSERT INTO saml_assertion_ids (assertion_id, org_id, issued_at, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (assertion_id) DO NOTHING`,
		assertionID, orgID, issuedAt, expiresAt)
	if err != nil {
		return fmt.Errorf("saml: recording the assertion id: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("saml: recording the assertion id: %w", err)
	}
	if affected == 0 {
		// The ID was already present. ON CONFLICT DO NOTHING rather than a
		// raised error, so this is an ordinary answer rather than a database
		// error the caller has to parse a driver message to recognise.
		return ErrReplayed
	}
	return nil
}

// Prune removes assertion IDs that can no longer be replayed.
//
// An expired assertion is refused by CheckConditions regardless, so keeping its
// ID protects nothing and the table would otherwise grow without bound. The
// bound is the point: an unbounded table on the login path is a slow outage
// scheduled for whenever it stops fitting in memory.
//
// Deliberately NOT tied to the assertion's own lifetime plus a margin. The
// window is already enforced; anything beyond it is storage spent on a
// credential that two independent checks would refuse.
func (r *Replay) Prune(ctx context.Context, tx *postgres.Tx, now time.Time) (int64, error) {
	result, err := tx.Exec(ctx, `DELETE FROM saml_assertion_ids WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("saml: pruning assertion ids: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("saml: pruning assertion ids: %w", err)
	}
	return affected, nil
}

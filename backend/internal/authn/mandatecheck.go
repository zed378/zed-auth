package authn

import (
	"context"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// MandateCheck answers the one question the paths that do NOT go through the
// login page must ask: does this organization's MFA mandate now require this
// user to enrol before anything else is issued? (P3-14, closing P3-07's A-2.)
//
// P3-07 enforced the mandate at the password step, and its specification's
// abuse case A-2 said "the refresh grant re-reads the policy". Nothing did.
// A user with no factor who held a refresh token from before the deadline went
// on refreshing for the family's 90 days, and one with a live browser session
// went on receiving codes silently for the session's lifetime. The mandate
// applied only to people who happened to type a password.
//
// The rule is `RequireMFA`'s, unchanged, so the login page and these paths
// cannot disagree about who is past the grace.
type MandateCheck struct {
	DB       *postgres.DB
	Policies *PolicyStore

	// Types are the factor types this build can challenge, as stored in
	// `user_mfa_factors.type`. A factor of any other type is treated as none,
	// for the reason the login path does: a user holding only a factor that
	// cannot be served would otherwise pass a mandate they cannot satisfy.
	//
	// Empty means this deployment cannot enrol anybody. The mandate is then
	// never reported unmet — the same non-lockout answer the login path gives,
	// where it is logged at ERROR.
	Types []string
}

// Unmet reports whether the user must be sent to sign in (and so into
// enrolment) instead of being issued anything.
//
// An error is returned rather than a guess. The callers refuse on error: a
// settings read that fails must not silently disable the policy.
func (m *MandateCheck) Unmet(ctx context.Context, orgID, userID string, now time.Time) (bool, error) {
	if m == nil || len(m.Types) == 0 {
		return false, nil
	}

	var unmet bool
	err := m.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		policy, err := m.Policies.LoginPolicy(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if !policy.MFARequired {
			return nil
		}

		var has bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM user_mfa_factors
				 WHERE user_id = $1 AND status = 'active' AND type = ANY($2)
			)`, userID, pq.Array(m.Types)).Scan(&has); err != nil {
			return fmt.Errorf("authn: checking for an active factor: %w", err)
		}

		unmet = RequireMFA(policy, has, now) == MFAEnrolmentRequired
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("authn: reading the MFA mandate: %w", err)
	}
	return unmet, nil
}

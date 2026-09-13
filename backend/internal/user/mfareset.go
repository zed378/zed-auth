package user

import (
	"context"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The administrator-assisted MFA reset (P3-04 step 4).
//
// Specification: MEMORY/specs/P3-04-recovery-codes.md § 7.
// Process: deploy/RUNBOOK-mfa-recovery.md.
//
// This is the weakest link in the whole multi-factor design, and saying so
// plainly is more useful than pretending otherwise. No control in this file
// prevents an administrator from being talked into resetting the wrong person's
// factors; the out-of-band identity verification is procedural and lives in the
// runbook.
//
// What the code CAN do, and does, is make the path as narrow as it can be:
//
//   - It destroys credentials and mints none. There is no response carrying
//     codes, no session, and no way for an administrator to end up holding a
//     working credential for somebody else's account. An endpoint that returned
//     fresh recovery codes would let anyone with ORG_ADMIN take over any account
//     in the organization — a far greater power than the one being granted, and
//     an invisible one, because the takeover would look like a normal login.
//   - It is permissioned, at the same level as every other destructive action on
//     a user, and enforced server-side against the manager-role tables rather
//     than a claim in a token.
//   - It is loud. `user.mfa.reset_by_admin` names the actor and the target, and
//     is one of the three events `docs/SECURITY/04` has an incident reviewer
//     search for first.

// MfaFactors is what the reset destroys, as this handler needs it.
//
// An interface rather than the concrete stores, so the `user` package does not
// take a dependency on `mfa` to delete two sets of rows — and so the handler's
// behaviour is answerable without a factor implementation present.
type MfaFactors interface {
	// ClearFactors removes every enrolled factor and reports how many went.
	ClearFactors(ctx context.Context, tx *postgres.Tx, userID string) (int, error)

	// ClearRecoveryCodes removes every recovery code, spent ones included, and
	// reports how many went.
	ClearRecoveryCodes(ctx context.Context, tx *postgres.Tx, userID string) (int, error)
}

// ResetUserMfa clears a user's second factor.
func (h *Handler) ResetUserMfa(
	ctx context.Context, request api.ResetUserMfaRequestObject,
) (api.ResetUserMfaResponseObject, error) {
	id := request.UserId.String()

	var (
		factors int
		codes   int
	)

	if err := h.inScope(ctx, func(tx *postgres.Tx) error {
		// The target must exist and must be in this organization. `Get` is
		// tenant-scoped, so a user id from elsewhere is a 404 rather than a
		// reset applied across a tenant boundary.
		target, err := h.Store.Get(ctx, tx, id)
		if err != nil {
			return err
		}

		if h.MFA == nil {
			// No factor implementation in this build. Refused rather than
			// answered with zeroes: reporting "nothing to remove" to an
			// administrator who is on the phone to a locked-out user would
			// send them away believing the account was reset.
			return management.Fault{
				Class:   management.Conflict,
				Message: "Two-step verification is not available on this deployment.",
				Reason:  "an MFA reset was requested on a build with no factor support",
			}
		}

		if factors, err = h.MFA.ClearFactors(ctx, tx, target.ID); err != nil {
			return err
		}
		if codes, err = h.MFA.ClearRecoveryCodes(ctx, tx, target.ID); err != nil {
			return err
		}

		// In the same transaction as the deletions, so an account cannot be
		// reset without the row that says who did it (ADR-012's property,
		// applied to the event that most needs it).
		return h.write(ctx, tx, audit.EventMFAResetByAdmin, map[string]any{
			"user_id":                target.ID,
			"factors_removed":        factors,
			"recovery_codes_removed": codes,
			// Named so an incident reviewer reading this row alone knows the
			// reset came through the documented path rather than from the
			// user's own account screen.
			"initiator": "administrator",
		})
	}); err != nil {
		return nil, faultFrom(err)
	}

	return api.ResetUserMfa200JSONResponse(api.MfaReset{
		FactorsRemoved:       factors,
		RecoveryCodesRemoved: codes,
	}), nil
}

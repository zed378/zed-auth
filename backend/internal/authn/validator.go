package authn

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// PasswordValidator is every check a chosen password must pass, in one place
// (P1-02, wired in P3-12).
//
// **Found in P3-12: the breach check guarded nothing.** P1-02 built the corpus
// client and ADR-015 decided how it fails — open, with an audit event, a counter
// and an alert on every skip — and `cmd/authservice` built the client at start-up
// and never handed it to anything. The set-password page validated against the
// organization's policy only, so a password from a public breach list was
// accepted, no skip was ever audited, and the alert rules watched a counter
// nothing incremented. The start-up log still said no password-set path existed.
//
// One validator, used by every path that sets a password, so the next path
// cannot pick the policy half and forget the corpus half.
type PasswordValidator struct {
	Policies *PolicyStore

	// Breaches is the corpus. Nil means the check is switched off, which is
	// counted as "disabled" rather than silently passing (ADR-015).
	Breaches BreachChecker

	// Observer counts refusals by rule and corpus outcomes. Optional.
	Observer PasswordObserver

	// Audit records a skipped check. Optional in the type; the service always
	// sets it, because an unaudited skip is the failure ADR-015 exists to make
	// visible.
	Audit PasswordAuditor

	Log *slog.Logger
}

// PasswordObserver counts what the validator decided.
//
// Both halves were defined as metrics in P1-02 and neither was ever incremented
// — `auth_password_policy_rejections_total` and
// `auth_password_breach_checks_total` read zero forever, and an alert on a
// metric that cannot move is not an alert.
type PasswordObserver interface {
	PolicyRejection(rule string)
	BreachCheck(outcome string)
}

// PasswordAuditor writes an audit event inside the caller's transaction.
type PasswordAuditor interface {
	Write(ctx context.Context, tx *postgres.Tx, e audit.Event) error
}

// PasswordRejected is a password refused, with every rule it failed.
type PasswordRejected struct {
	Violations []Violation
}

func (e PasswordRejected) Error() string {
	reasons := make([]string, 0, len(e.Violations))
	for _, v := range e.Violations {
		reasons = append(reasons, v.Message)
	}
	return strings.Join(reasons, " ")
}

// Validate checks a candidate password for a user in an organization.
//
// Returns PasswordRejected for a password that fails a rule — every rule it
// fails, the policy's and the corpus's together, so somebody fixing it learns
// everything at once — and a plain error only for a failure to decide.
//
// The corpus is consulted only when the policy passes. A password that is too
// short is refused regardless, and sending its hash prefix to a third party
// would be a network round trip spent learning nothing that changes the answer.
func (v *PasswordValidator) Validate(ctx context.Context, tx *postgres.Tx, orgID, userID, password string) error {
	policy, err := v.Policies.Policy(ctx, tx, orgID)
	if err != nil {
		return fmt.Errorf("authn: reading the password policy: %w", err)
	}

	if violations := Evaluate(password, policy); len(violations) > 0 {
		v.countRejections(violations)
		return PasswordRejected{Violations: violations}
	}

	violations, outcome, err := CheckBreach(ctx, v.Breaches, password)
	if v.Observer != nil {
		v.Observer.BreachCheck(string(outcome))
	}

	switch outcome {
	case OutcomeBreached:
		v.countRejections(violations)
		return PasswordRejected{Violations: violations}
	case OutcomeSkipped:
		// Accepted, and recorded (ADR-015). The error is the corpus's, logged
		// for the operator; the event is what an incident review finds.
		if v.Log != nil && err != nil {
			v.Log.Warn("the breached-password corpus could not be reached; the password was accepted unchecked",
				"error", err.Error())
		}
		if v.Audit != nil {
			if auditErr := v.Audit.Write(ctx, tx, audit.Event{
				OrgID:       orgID,
				ActorUserID: userID,
				Type:        audit.EventPasswordBreachCheckSkipped,
				Payload:     map[string]any{"user_id": userID},
			}); auditErr != nil {
				return fmt.Errorf("authn: auditing a skipped breach check: %w", auditErr)
			}
		}
	}
	return nil
}

func (v *PasswordValidator) countRejections(violations []Violation) {
	if v.Observer == nil {
		return
	}
	for _, violation := range violations {
		v.Observer.PolicyRejection(violation.Rule)
	}
}

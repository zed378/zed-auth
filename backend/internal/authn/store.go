package authn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Reading a password policy out of an organization row.
//
// This is the half of P1-02 that makes the rest worth having. An evaluator
// with correct rules and a compiled-in configuration is a system where P2-14's
// policy editor cannot be built without rewriting every call site — so the
// values come from the database from the first day enforcement exists, even
// though the only writer today is P0-07's column default.

// ErrOrganizationNotFound means no organization with that id is visible in the
// current tenant scope.
//
// "Visible" is doing real work: the query runs inside a tenant-scoped
// transaction, so RLS (P0-08) makes another tenant's organization not-found
// rather than forbidden. The distinction is deliberate — a "forbidden" would
// confirm the row exists.
var ErrOrganizationNotFound = errors.New("authn: organization not found")

// PolicyStore reads password policy from organizations.settings.
type PolicyStore struct {
	log *slog.Logger
}

// NewPolicyStore returns a store that logs policy corrections through log.
func NewPolicyStore(log *slog.Logger) *PolicyStore {
	return &PolicyStore{log: log}
}

// Policy reads the policy for one organization inside an existing transaction.
//
// Takes a *postgres.Tx rather than opening its own, so the policy read joins
// the transaction of the operation it governs. Reading policy in a separate
// transaction would create a window in which the policy that was checked and
// the policy in force are different rows.
//
// Never returns a policy error: a settings document that cannot be read yields
// the secure default with the substitutions reported. The only error is the
// row being absent, which is a caller bug or a deleted tenant rather than a
// configuration problem.
func (s *PolicyStore) Policy(ctx context.Context, tx *postgres.Tx, orgID string) (Policy, error) {
	var settings []byte

	err := tx.QueryRow(ctx,
		`SELECT settings FROM organizations WHERE id = $1`, orgID,
	).Scan(&settings)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return DefaultPolicy, fmt.Errorf("%w: %s", ErrOrganizationNotFound, orgID)
	case err != nil:
		return DefaultPolicy, fmt.Errorf("authn: reading password policy: %w", err)
	}

	policy, adjustments := ParsePolicy(settings)
	s.warn(orgID, adjustments)
	return policy, nil
}

// warn reports every value this code had to correct.
//
// At WARN and per adjustment, because a clamped policy is a policy an
// administrator believes is in force and is not. Silently applying the floor
// would be the more comfortable behaviour and would leave the difference
// between the configured and the enforced policy discoverable only by
// experiment.
func (s *PolicyStore) warn(orgID string, adjustments []Adjustment) {
	if s.log == nil {
		return
	}
	for _, a := range adjustments {
		s.log.Warn("password policy value was corrected",
			"org_id", orgID,
			"field", a.Field,
			"configured", a.Configured,
			"applied", a.Applied,
			"reason", a.Reason)
	}
}

//go:build integration

package authn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The password validator against the organization's real settings and the real
// audit table (P3-12).

type stubCorpus struct {
	breached bool
	err      error
	asked    int
}

func (c *stubCorpus) Breached(context.Context, string) (bool, error) {
	c.asked++
	return c.breached, c.err
}

type countedOutcomes struct {
	rules    []string
	outcomes []string
}

func (c *countedOutcomes) PolicyRejection(rule string) { c.rules = append(c.rules, rule) }
func (c *countedOutcomes) BreachCheck(outcome string)  { c.outcomes = append(c.outcomes, outcome) }

func (f userFixture) validate(t *testing.T, v *PasswordValidator, password string) error {
	t.Helper()
	var result error
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		result = v.Validate(context.Background(), tx, f.orgID, f.userID, password)
		var rejected PasswordRejected
		if result != nil && !errors.As(result, &rejected) {
			return result
		}
		return nil
	}); err != nil {
		t.Fatalf("Validate failed to decide: %v", err)
	}
	return result
}

func validator(f userFixture, c *stubCorpus, observed *countedOutcomes) *PasswordValidator {
	v := &PasswordValidator{
		Policies: NewPolicyStore(slog.New(slog.NewTextHandler(io.Discard, nil))),
		Observer: observed,
		Audit:    audit.NewWriter(f.db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil),
	}
	if c != nil {
		v.Breaches = c
	}
	return v
}

const strongPassword = "Correct Horse Battery Staple 42"

func TestAPolicyViolationIsRefusedWithoutAskingTheCorpus(t *testing.T) {
	f := setupUsers(t)
	c := &stubCorpus{}
	observed := &countedOutcomes{}

	err := f.validate(t, validator(f, c, observed), "short")
	var rejected PasswordRejected
	if !errors.As(err, &rejected) || len(rejected.Violations) == 0 {
		t.Fatalf("a short password: err = %v, want PasswordRejected", err)
	}
	if c.asked != 0 {
		t.Error("the corpus was asked about a password the policy already refused")
	}
	if len(observed.rules) == 0 {
		t.Error("a policy refusal was not counted")
	}
}

// The gap this closes: a breached password used to be accepted everywhere.
func TestABreachedPasswordIsRefused(t *testing.T) {
	f := setupUsers(t)
	observed := &countedOutcomes{}

	err := f.validate(t, validator(f, &stubCorpus{breached: true}, observed), strongPassword)
	var rejected PasswordRejected
	if !errors.As(err, &rejected) || len(rejected.Violations) != 1 || rejected.Violations[0].Rule != RuleBreached {
		t.Fatalf("a breached password: err = %v, want a breached violation", err)
	}
	if len(observed.outcomes) != 1 || observed.outcomes[0] != string(OutcomeBreached) {
		t.Errorf("outcomes counted = %v, want [breached]", observed.outcomes)
	}
}

// ADR-015: a corpus outage fails open — and is audited and counted every time.
func TestACorpusOutageAcceptsButRecordsTheSkip(t *testing.T) {
	f := setupUsers(t)
	observed := &countedOutcomes{}

	if err := f.validate(t, validator(f, &stubCorpus{err: errors.New("timeout")}, observed), strongPassword); err != nil {
		t.Fatalf("a password during a corpus outage was refused: %v", err)
	}
	if len(observed.outcomes) != 1 || observed.outcomes[0] != string(OutcomeSkipped) {
		t.Errorf("outcomes counted = %v, want [skipped]", observed.outcomes)
	}

	var skipped int
	f.factory.QueryRow(&skipped, `SELECT count(*) FROM events WHERE event_type = $1 AND actor_user_id = $2`,
		string(audit.EventPasswordBreachCheckSkipped), f.userID)
	if skipped != 1 {
		t.Errorf("found %d skip events, want 1 — an unaudited skip is what ADR-015 forbids", skipped)
	}
}

func TestADisabledCorpusIsCountedAsDisabled(t *testing.T) {
	f := setupUsers(t)
	observed := &countedOutcomes{}

	if err := f.validate(t, validator(f, nil, observed), strongPassword); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(observed.outcomes) != 1 || observed.outcomes[0] != string(OutcomeDisabled) {
		t.Errorf("outcomes counted = %v, want [disabled]", observed.outcomes)
	}
}

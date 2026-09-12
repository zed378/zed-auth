//go:build integration

// TOTP enrolment end to end (P3-02).
//
// The whole path a user walks: a secret is generated and shown once, the
// factor is pending and inert, a code proves it, and only then is it a factor.
// Then the properties that make it one — a code cannot be replayed, a pending
// factor cannot answer a challenge, and the secret never comes back.
package mfa

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// totpFixture wires a real TOTP verifier against the fixture's database.
func (f *factorFixture) totp(t *testing.T, now func() time.Time) *TOTP {
	t.Helper()

	return &TOTP{
		Store:  f.store,
		DB:     f.db,
		Sealer: f.sealer,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Issuer: "Zed Auth",
		// Every factor and every user in this fixture belongs to one
		// organization, so the resolvers are constants. In the service they
		// are queries; what is being tested here is the verifier.
		OrgOf: func(context.Context, string) (string, error) { return f.orgID, nil },
		Now:   now,
	}
}

// The full enrolment.
func TestATOTPEnrolmentBecomesAFactorOnlyAfterItIsProven(t *testing.T) {
	f := factorSetup(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	totp := f.totp(t, func() time.Time { return at })

	enrolment, err := totp.Begin(context.Background(), f.userID, f.orgID, "phone")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if enrolment.Secret == "" {
		t.Fatal("enrolment returned no secret, so nothing could be enrolled")
	}
	if enrolment.Challenge["provisioning_uri"] == "" {
		t.Error("enrolment returned no provisioning URI")
	}

	// Pending: it answers nothing yet.
	if err := totp.Verify(context.Background(), enrolment.FactorID, "000000"); !errors.Is(err, ErrNoSuchFactor) {
		t.Errorf("a PENDING factor answered a challenge attempt with %v — a half-finished "+
			"enrolment must not be a way in", err)
	}

	// A wrong code does not activate it.
	if err := totp.Confirm(context.Background(), enrolment.FactorID, "000000"); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("confirming with a wrong code gave %v, want ErrWrongCode", err)
	}

	var status Status
	_ = f.within(t, func(tx *postgres.Tx) error {
		_, got, err := f.store.Sealed(context.Background(), tx, enrolment.FactorID)
		status = got
		return err
	})
	if status != StatusPending {
		t.Fatalf("a failed confirmation left the factor %q", status)
	}

	// The right code does.
	secret, err := DecodeTOTPSecret(enrolment.Secret)
	if err != nil {
		t.Fatalf("decoding the enrolment secret: %v", err)
	}
	code := TOTPCode(secret, TOTPCounter(at))

	if err := totp.Confirm(context.Background(), enrolment.FactorID, code); err != nil {
		t.Fatalf("confirming with the correct code: %v", err)
	}

	_ = f.within(t, func(tx *postgres.Tx) error {
		_, got, err := f.store.Sealed(context.Background(), tx, enrolment.FactorID)
		status = got
		return err
	})
	if status != StatusActive {
		t.Errorf("a proven factor is %q, want %q", status, StatusActive)
	}
}

// **A code cannot be replayed inside its own window** (`P3-02` DoD).
//
// The clock does not move between the two attempts, so the code is still
// valid — which is exactly the case the bound exists for.
func TestAUsedCodeCannotBeReplayedWithinItsWindow(t *testing.T) {
	f := factorSetup(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	totp := f.totp(t, func() time.Time { return at })

	enrolment, _ := totp.Begin(context.Background(), f.userID, f.orgID, "phone")
	secret, _ := DecodeTOTPSecret(enrolment.Secret)

	// Confirming consumes the step.
	code := TOTPCode(secret, TOTPCounter(at))
	if err := totp.Confirm(context.Background(), enrolment.FactorID, code); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// The same code, still inside its window, presented again.
	if err := totp.Verify(context.Background(), enrolment.FactorID, code); !errors.Is(err, ErrWrongCode) {
		t.Errorf("a code was accepted twice inside its window (%v) — a shoulder-surfed "+
			"code is a login for the rest of its 30 seconds", err)
	}
}

// The next step's code works, or the factor would authenticate exactly once.
func TestTheNextStepsCodeIsAccepted(t *testing.T) {
	f := factorSetup(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	clock := at
	totp := f.totp(t, func() time.Time { return clock })

	enrolment, _ := totp.Begin(context.Background(), f.userID, f.orgID, "phone")
	secret, _ := DecodeTOTPSecret(enrolment.Secret)

	if err := totp.Confirm(context.Background(), enrolment.FactorID, TOTPCode(secret, TOTPCounter(at))); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Two steps on, which is outside the skew window of the consumed step.
	clock = at.Add(2 * TOTPPeriod)
	next := TOTPCode(secret, TOTPCounter(clock))

	if err := totp.Verify(context.Background(), enrolment.FactorID, next); err != nil {
		t.Errorf("the next step's code was refused (%v), so the factor authenticates once", err)
	}
}

// The secret is never returned after enrolment.
//
// `P3-02` step 7 and its DoD. The one place it crosses is `Begin`; after that
// it is readable only as ciphertext, and nothing exposes a way to open it.
func TestTheSecretIsNeverReturnedAfterEnrolment(t *testing.T) {
	f := factorSetup(t)
	totp := f.totp(t, time.Now)

	enrolment, err := totp.Begin(context.Background(), f.userID, f.orgID, "phone")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	var factors []Factor
	_ = f.within(t, func(tx *postgres.Tx) error {
		got, err := f.store.ForUser(context.Background(), tx, f.userID)
		factors = got
		return err
	})
	if len(factors) != 1 {
		t.Fatalf("the listing has %d factors", len(factors))
	}

	// The Factor type has no field for a secret at all, which is the point —
	// this asserts the shape rather than a value, because a shape cannot be
	// forgotten. What is checked here is that nothing else leaked it.
	if factors[0].Label == enrolment.Secret {
		t.Error("the secret came back as the label")
	}

	// And the provisioning URI, which contains the secret, is not stored.
	var data string
	f.factory.QueryRow(&data, `SELECT data::text FROM user_mfa_factors WHERE id = $1`, enrolment.FactorID)
	if strings_Contains([]byte(data), []byte(enrolment.Secret)) {
		t.Error("the secret was stored in the factor's data column")
	}
}

// Without an encryption key, nothing is enrolled.
//
// A deployment that cannot encrypt must not silently start holding second
// factors in the clear — and the refusal lands on the operator at enrolment
// rather than on a user at login.
func TestWithoutAKeyNoFactorIsEnrolled(t *testing.T) {
	f := factorSetup(t)
	totp := f.totp(t, time.Now)
	totp.Sealer = nil

	if _, err := totp.Begin(context.Background(), f.userID, f.orgID, "phone"); !errors.Is(err, ErrNoSealKey) {
		t.Errorf("enrolling without a key gave %v, want ErrNoSealKey", err)
	}

	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM user_mfa_factors`)
	if count != 0 {
		t.Errorf("%d factor(s) were stored despite no encryption key", count)
	}
}

// A factor sealed under one key does not verify under another.
//
// Reported as a failure to decide rather than as a wrong code, so the caller
// refuses instead of telling somebody their authenticator app is broken.
func TestAKeyChangeIsAnOperatorProblemNotAWrongCode(t *testing.T) {
	f := factorSetup(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	totp := f.totp(t, func() time.Time { return at })

	enrolment, _ := totp.Begin(context.Background(), f.userID, f.orgID, "phone")
	secret, _ := DecodeTOTPSecret(enrolment.Secret)
	code := TOTPCode(secret, TOTPCounter(at))

	// The key changes underneath.
	other, err := NewSealer([]byte("a-completely-different-key-here-now"))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	totp.Sealer = other

	err = totp.Confirm(context.Background(), enrolment.FactorID, code)
	if err == nil {
		t.Fatal("a factor confirmed under a key that cannot open its secret")
	}
	if errors.Is(err, ErrWrongCode) {
		t.Error("a key mismatch was reported as a wrong code, which sends the user to " +
			"their recovery codes for an operator's problem")
	}
	if !errors.Is(err, ErrSealed) {
		t.Errorf("a key mismatch gave %v, want ErrSealed", err)
	}
}

// The framework sees active factors and not pending ones.
func TestTheFrameworkOnlySeesActiveFactors(t *testing.T) {
	f := factorSetup(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	totp := f.totp(t, func() time.Time { return at })

	enrolment, _ := totp.Begin(context.Background(), f.userID, f.orgID, "phone")

	factors := &PostgresFactors{
		Store: f.store,
		DB:    f.db,
		OrgOf: func(context.Context, string) (string, error) { return f.orgID, nil },
	}

	pending, err := factors.Confirmed(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("Confirmed: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("a pending enrolment is visible to the challenge: %+v", pending)
	}

	secret, _ := DecodeTOTPSecret(enrolment.Secret)
	if err := totp.Confirm(context.Background(), enrolment.FactorID, TOTPCode(secret, TOTPCounter(at))); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	active, err := factors.Confirmed(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("Confirmed: %v", err)
	}
	if len(active) != 1 || active[0].ID != enrolment.FactorID {
		t.Errorf("the challenge sees %+v after confirmation", active)
	}
}

// Removing a factor removes it.
func TestAFactorCanBeRemoved(t *testing.T) {
	f := factorSetup(t)
	totp := f.totp(t, time.Now)

	enrolment, _ := totp.Begin(context.Background(), f.userID, f.orgID, "phone")

	if err := totp.Remove(context.Background(), enrolment.FactorID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	var count int
	f.factory.QueryRow(&count, `SELECT count(*) FROM user_mfa_factors WHERE id = $1`, enrolment.FactorID)
	if count != 0 {
		t.Error("the factor is still there after removal")
	}
}

// The whole framework, with a real factor behind it.
//
// The first time `P3-01`'s `Required` and `Answer` run against something that
// actually verifies — until now the only implementation was a fake.
func TestTheFrameworkChallengesWithARealFactor(t *testing.T) {
	f := factorSetup(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	totp := f.totp(t, func() time.Time { return at })

	enrolment, _ := totp.Begin(context.Background(), f.userID, f.orgID, "phone")
	secret, _ := DecodeTOTPSecret(enrolment.Secret)
	if err := totp.Confirm(context.Background(), enrolment.FactorID, TOTPCode(secret, TOTPCounter(at))); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	framework := &Framework{
		Registry: NewRegistry(totp),
		Store: &PostgresFactors{
			Store: f.store,
			DB:    f.db,
			OrgOf: func(context.Context, string) (string, error) { return f.orgID, nil },
		},
		Challenges: newMemoryChallenges(),
		Now:        func() time.Time { return at },
	}

	decision, err := framework.Required(context.Background(), f.userID, f.orgID, "pending-1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if !decision.Challenge {
		t.Fatal("a user with an active TOTP factor was not challenged")
	}
	if len(decision.Offered) != 1 || decision.Offered[0] != TypeTOTP {
		t.Errorf("the challenge offers %v", decision.Offered)
	}

	// A wrong code is wrong.
	if _, err := framework.Answer(context.Background(), decision.Handle, enrolment.FactorID, "000000"); !errors.Is(err, ErrWrongCode) {
		t.Errorf("a wrong code gave %v", err)
	}

	// The next step's code completes it — the current step was consumed by the
	// confirmation.
	later := at.Add(2 * TOTPPeriod)
	totp.Now = func() time.Time { return later }

	used, err := framework.Answer(context.Background(), decision.Handle, enrolment.FactorID,
		TOTPCode(secret, TOTPCounter(later)))
	if err != nil {
		t.Fatalf("answering with a correct code: %v", err)
	}
	if len(used) != 1 || used[0] != TypeTOTP {
		t.Errorf("the completed challenge reports %v as used", used)
	}

	// And `amr` says what happened.
	amr := AuthMethods(true, used...)
	for _, want := range []string{"pwd", "otp", "mfa"} {
		found := false
		for _, got := range amr {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("amr is %v, missing %q", amr, want)
		}
	}
}

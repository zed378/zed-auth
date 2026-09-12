package mfa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// TOTP as a factor type (P3-02).
//
// An implementation of `Verifier`, which is the whole point of `P3-01`: the
// framework does not know this is TOTP, and `P3-05`'s WebAuthn will not be a
// second system beside it.

// FactorStore reads and writes `user_mfa_factors`.
//
// An interface here rather than a concrete type so the verifier can be tested
// without Postgres, and — more usefully — so the seam where a secret crosses
// is one named place.
type FactorStore interface {
	// Insert creates a PENDING factor and returns its id.
	Insert(ctx context.Context, tx *postgres.Tx, userID, orgID string, t Type, label string, sealed []byte) (string, error)

	// Sealed reads one factor's encrypted secret, with its status.
	Sealed(ctx context.Context, tx *postgres.Tx, factorID string) (sealed []byte, status Status, err error)

	// Activate marks a pending factor active. A no-op on one already active.
	Activate(ctx context.Context, tx *postgres.Tx, factorID string) error

	// RecordUse stores the counter a verification consumed, and reports
	// whether it was already spent — which is how a replay is refused.
	RecordUse(ctx context.Context, tx *postgres.Tx, factorID string, counter uint64, at time.Time) (replayed bool, err error)

	// Delete removes a factor.
	Delete(ctx context.Context, tx *postgres.Tx, factorID string) error

	// ForUser lists a user's factors.
	ForUser(ctx context.Context, tx *postgres.Tx, userID string) ([]Factor, error)
}

// Tenant is the transaction seam, matching the rest of the service.
type Tenant interface {
	WithTenant(ctx context.Context, orgID string, fn func(tx *postgres.Tx) error) error
}

// TOTP implements Verifier for authenticator apps.
type TOTP struct {
	Store  FactorStore
	DB     Tenant
	Sealer *Sealer
	Log    *slog.Logger

	// Issuer is what appears in the authenticator app's entry.
	Issuer string

	// OrgOf resolves a factor's tenant, because every write goes through
	// `WithTenant` and a factor id alone does not name one.
	OrgOf func(ctx context.Context, factorID string) (orgID string, err error)

	Now func() time.Time
}

func (t *TOTP) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Type identifies this verifier to the registry.
func (t *TOTP) Type() Type { return TypeTOTP }

// Begin creates a pending factor and returns what the user needs to enrol.
//
// **The secret crosses to the user exactly once, here.** After this call it is
// readable only as ciphertext, and no read path returns it — the same
// discipline `P1-18` applies to a client secret, for the same reason: a value
// that can be fetched again is one an attacker can fetch.
//
// The factor is PENDING. Nothing about the account's security has changed yet,
// and it will not until `Confirm` proves the user can actually use it — `P3-02`
// step 3, and the reason is that enrolling without proof is how users lock
// themselves out.
func (t *TOTP) Begin(ctx context.Context, userID, orgID, label string) (Enrolment, error) {
	if !t.Sealer.Configured() {
		// Refused rather than stored in plaintext. A deployment that cannot
		// encrypt must not silently start holding second factors in the clear.
		//
		// `Seal` refuses too, so removing this changes no behaviour — the
		// mutation run confirmed it. It stays because failing HERE means no
		// secret is generated at all, and a secret generated and discarded is
		// a secret that briefly existed in a process's memory for no reason.
		return Enrolment{}, ErrNoSealKey
	}

	secret, err := NewTOTPSecret()
	if err != nil {
		return Enrolment{}, err
	}

	sealed, err := t.Sealer.Seal(secret)
	if err != nil {
		return Enrolment{}, err
	}

	var factorID string
	err = t.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		id, err := t.Store.Insert(ctx, tx, userID, orgID, TypeTOTP, label, sealed)
		if err != nil {
			return err
		}
		factorID = id
		return nil
	})
	if err != nil {
		return Enrolment{}, fmt.Errorf("mfa: starting a TOTP enrolment: %w", err)
	}

	return Enrolment{
		FactorID: factorID,
		// The one time it leaves the service.
		Secret: EncodeTOTPSecret(secret),
		Challenge: map[string]any{
			"provisioning_uri": TOTPProvisioningURI(t.Issuer, userID, secret),
			"digits":           TOTPDigits,
			"period":           int(TOTPPeriod.Seconds()),
			"algorithm":        "SHA1",
		},
	}, nil
}

// Confirm activates a pending factor by checking the user can use it.
func (t *TOTP) Confirm(ctx context.Context, factorID, code string) error {
	return t.check(ctx, factorID, code, true)
}

// Verify checks a code against an active factor during a challenge.
func (t *TOTP) Verify(ctx context.Context, factorID, code string) error {
	return t.check(ctx, factorID, code, false)
}

// check is the shared path. `activating` is the one difference between
// confirming an enrolment and answering a challenge, and keeping it one
// function means the replay bound and the skew tolerance cannot differ between
// the two — which they would, eventually, if they were two functions.
func (t *TOTP) check(ctx context.Context, factorID, code string, activating bool) error {
	if !t.Sealer.Configured() {
		return ErrNoSealKey
	}

	orgID, err := t.OrgOf(ctx, factorID)
	if err != nil {
		return fmt.Errorf("mfa: resolving a factor's organization: %w", err)
	}

	now := t.now()

	return t.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		sealed, status, err := t.Store.Sealed(ctx, tx, factorID)
		if err != nil {
			return err
		}

		switch {
		case activating && status != StatusPending:
			// Confirming something already active is not a second enrolment.
			return fmt.Errorf("mfa: this factor is already active")
		case !activating && status != StatusActive:
			// A pending factor must never answer a challenge. It has not been
			// proven, and treating it as answerable would let a half-finished
			// enrolment become a way in.
			return ErrNoSuchFactor
		}

		secret, err := t.Sealer.Open(sealed)
		if err != nil {
			// A key that cannot open a stored secret is an operator problem,
			// not a user one. Reported as a failure to decide rather than as a
			// wrong code, so the caller refuses rather than telling somebody
			// their authenticator app is broken.
			return err
		}

		counter, ok := VerifyTOTP(secret, code, now)
		if !ok {
			return ErrWrongCode
		}

		// **Replay** (`P3-02` step 6). A code is valid for its whole 30-second
		// step, so without this it is accepted as many times as it is
		// presented — which turns a shoulder-surfed code into a login.
		//
		// The COUNTER is recorded, never the code: the code is a live
		// credential for the rest of its window and storing it would be
		// storing a credential to prevent the reuse of a credential.
		replayed, err := t.Store.RecordUse(ctx, tx, factorID, counter, now)
		if err != nil {
			return err
		}
		if replayed {
			// Refused as a wrong code, and that is deliberate: the two are
			// indistinguishable to the person presenting them, and saying "you
			// already used that one" tells an attacker their stolen code was
			// real.
			return ErrWrongCode
		}

		if activating {
			return t.Store.Activate(ctx, tx, factorID)
		}
		return nil
	})
}

// Remove deletes a factor.
//
// Whether removal is ALLOWED is the framework's question — the last active
// factor in an organization that requires MFA is not removable, which `P3-07`
// enforces. This performs it.
func (t *TOTP) Remove(ctx context.Context, factorID string) error {
	orgID, err := t.OrgOf(ctx, factorID)
	if err != nil {
		return fmt.Errorf("mfa: resolving a factor's organization: %w", err)
	}

	return t.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return t.Store.Delete(ctx, tx, factorID)
	})
}

// PostgresFactors implements EnrolledFactors for a Postgres deployment.
//
// Named for what the framework asks — the factors a user may be challenged
// with — rather than for the table. Only ACTIVE factors, so a pending
// enrolment is invisible here as well as inside the verifier.
type PostgresFactors struct {
	Store FactorStore
	DB    Tenant
}

// Confirmed reads a user's answerable factors within a tenant.
//
// The organization is given rather than resolved (P3-03). It used to be looked
// up from the user id, which would have needed a SECURITY DEFINER function
// answering "which organization is this user in" for any id — a privilege the
// callers never needed, because every one of them already holds the org.
func (p *PostgresFactors) Confirmed(ctx context.Context, orgID, userID string) ([]Factor, error) {
	var out []Factor
	err := p.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		factors, err := p.Store.ForUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		for _, factor := range factors {
			if factor.Active() {
				out = append(out, factor)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ErrFactorNotFound is a factor id nothing holds.
var ErrFactorNotFound = errors.New("mfa: factor not found")

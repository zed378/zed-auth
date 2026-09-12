//go:build integration

// `user_mfa_factors` against real Postgres (P3-02).
//
// Four properties here exist only in the database and cannot be tested against
// a fake: the replay bound is a WHERE clause rather than a read-then-write, the
// `mfa_enabled` flag is maintained by a trigger, a factor cannot be filed under
// a tenant that does not own its user, and one user gets one TOTP secret.
package mfa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

type factorFixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	store   *Store
	sealer  *Sealer

	orgID  string
	userID string
}

func factorSetup(t *testing.T) *factorFixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID)

	sealer, err := NewSealer([]byte("an-integration-test-key-long-enough"))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	return &factorFixture{
		db:      openAppPool(t, stack),
		factory: factory,
		store:   NewStore(),
		sealer:  sealer,
		orgID:   orgID,
		userID:  userID,
	}
}

// within runs fn inside the fixture's tenant scope.
func (f *factorFixture) within(t *testing.T, fn func(tx *postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), f.orgID, fn)
}

func (f *factorFixture) enrol(t *testing.T) (factorID string, secret []byte) {
	t.Helper()

	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret: %v", err)
	}
	sealed, err := f.sealer.Seal(secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	err = f.within(t, func(tx *postgres.Tx) error {
		id, err := f.store.Insert(context.Background(), tx, f.userID, f.orgID, TypeTOTP, "phone", sealed)
		factorID = id
		return err
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return factorID, secret
}

// A new enrolment is pending, and pending is not a factor.
func TestANewEnrolmentIsPending(t *testing.T) {
	f := factorSetup(t)
	factorID, _ := f.enrol(t)

	var status Status
	if err := f.within(t, func(tx *postgres.Tx) error {
		_, got, err := f.store.Sealed(context.Background(), tx, factorID)
		status = got
		return err
	}); err != nil {
		t.Fatalf("Sealed: %v", err)
	}

	if status != StatusPending {
		t.Errorf("a new enrolment has status %q, want %q", status, StatusPending)
	}
}

// The secret is stored encrypted, and the column does not contain it.
//
// Read as raw bytes rather than through the store, because the claim is about
// what somebody with SQL access sees — a stolen backup, a replica, a `pg_dump`
// in a ticket.
func TestTheStoredSecretIsNotThePlaintext(t *testing.T) {
	f := factorSetup(t)
	factorID, secret := f.enrol(t)

	var stored []byte
	f.factory.QueryRow(&stored,
		`SELECT secret_encrypted FROM user_mfa_factors WHERE id = $1`, factorID)

	if len(stored) == 0 {
		t.Fatal("nothing was stored")
	}
	if string(stored) == string(secret) {
		t.Fatal("the column holds the plaintext secret")
	}
	if strings_Contains(stored, secret) {
		t.Error("the plaintext appears inside the stored value")
	}

	// And the encoded form a user would have typed is not in there either.
	if strings_Contains(stored, []byte(EncodeTOTPSecret(secret))) {
		t.Error("the base32 form of the secret appears inside the stored value")
	}
}

// Activation moves pending to active, once.
func TestActivationHappensOnce(t *testing.T) {
	f := factorSetup(t)
	factorID, _ := f.enrol(t)

	if err := f.within(t, func(tx *postgres.Tx) error {
		return f.store.Activate(context.Background(), tx, factorID)
	}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// A second activation is refused, so confirming an already-active factor
	// cannot look like a successful enrolment.
	err := f.within(t, func(tx *postgres.Tx) error {
		return f.store.Activate(context.Background(), tx, factorID)
	})
	if err == nil {
		t.Error("a factor was activated twice")
	}
}

// **The replay bound** (`P3-02` step 6, and its DoD).
//
// A code is valid for its whole 30-second step, so without this it is accepted
// as many times as it is presented.
func TestACounterCannotBeSpentTwice(t *testing.T) {
	f := factorSetup(t)
	factorID, _ := f.enrol(t)
	now := time.Now()

	var first, second bool
	if err := f.within(t, func(tx *postgres.Tx) error {
		replayed, err := f.store.RecordUse(context.Background(), tx, factorID, 1000, now)
		first = replayed
		return err
	}); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	if first {
		t.Fatal("the first use of a counter was reported as a replay")
	}

	if err := f.within(t, func(tx *postgres.Tx) error {
		replayed, err := f.store.RecordUse(context.Background(), tx, factorID, 1000, now)
		second = replayed
		return err
	}); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	if !second {
		t.Error("the same counter was accepted twice — a shoulder-surfed code is a login " +
			"for the rest of its window")
	}
}

// An OLD counter is refused too, not only the exact one just used.
//
// The bound is strictly-greater rather than not-equal, so a code captured
// earlier cannot be used once the clock has moved on — which the skew window
// would otherwise still accept.
func TestAnEarlierCounterIsAlsoRefused(t *testing.T) {
	f := factorSetup(t)
	factorID, _ := f.enrol(t)
	now := time.Now()

	if err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.RecordUse(context.Background(), tx, factorID, 1000, now)
		return err
	}); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}

	var replayed bool
	if err := f.within(t, func(tx *postgres.Tx) error {
		got, err := f.store.RecordUse(context.Background(), tx, factorID, 999, now)
		replayed = got
		return err
	}); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	if !replayed {
		t.Error("a counter from an earlier step was accepted after a later one")
	}
}

// A later counter is accepted, or the factor would work exactly once.
func TestALaterCounterIsAccepted(t *testing.T) {
	f := factorSetup(t)
	factorID, _ := f.enrol(t)
	now := time.Now()

	_ = f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.RecordUse(context.Background(), tx, factorID, 1000, now)
		return err
	})

	var replayed bool
	if err := f.within(t, func(tx *postgres.Tx) error {
		got, err := f.store.RecordUse(context.Background(), tx, factorID, 1001, now)
		replayed = got
		return err
	}); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}
	if replayed {
		t.Error("the next step's counter was refused, so the factor works exactly once")
	}
}

// `users.mfa_enabled` follows the factors, maintained by the database.
//
// `docs/PLAN/04` calls it "a fast denormalized flag for the login path" and the
// factor table "the source of truth". A flag that can disagree with its source
// is worse than no flag: the login path takes a fast decision from a value
// nobody maintains, and the disagreement is invisible until somebody with MFA
// enrolled is let in without it.
func TestTheDenormalizedFlagFollowsTheFactors(t *testing.T) {
	f := factorSetup(t)

	flag := func() bool {
		var enabled bool
		f.factory.QueryRow(&enabled, `SELECT mfa_enabled FROM users WHERE id = $1`, f.userID)
		return enabled
	}

	if flag() {
		t.Fatal("a user with no factors has mfa_enabled set")
	}

	factorID, _ := f.enrol(t)
	if flag() {
		t.Error("a PENDING enrolment set mfa_enabled — a half-finished enrolment is not " +
			"multi-factor authentication")
	}

	if err := f.within(t, func(tx *postgres.Tx) error {
		return f.store.Activate(context.Background(), tx, factorID)
	}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !flag() {
		t.Error("an active factor did not set mfa_enabled")
	}

	if err := f.within(t, func(tx *postgres.Tx) error {
		return f.store.Delete(context.Background(), tx, factorID)
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if flag() {
		t.Error("removing the last factor left mfa_enabled set — the login path would " +
			"take a fast decision from a value that is now false")
	}
}

// One TOTP secret per user.
//
// Not a limit on factors in general — a user may hold several passkeys, one per
// device. A second TOTP is not a second device: it is an older enrolment nobody
// removed, which is a live credential the user has forgotten about.
func TestOneUserGetsOneTOTPSecret(t *testing.T) {
	f := factorSetup(t)
	f.enrol(t)

	sealed, _ := f.sealer.Seal([]byte("a second secret"))
	err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Insert(context.Background(), tx, f.userID, f.orgID, TypeTOTP, "another", sealed)
		return err
	})
	if err == nil {
		t.Error("a user was given a second TOTP secret")
	}
}

// A factor cannot be filed under a tenant that does not own its user.
//
// The hole `P2-08` found in `applications`, closed here before anything can
// write it: a row under the wrong organization would then be invisible, under
// RLS, to the one that owns the user.
func TestAFactorCannotBeFiledUnderTheWrongTenant(t *testing.T) {
	f := factorSetup(t)
	other := f.factory.Organization(f.factory.Instance())

	sealed, _ := f.sealer.Seal([]byte("a secret"))

	err := f.db.WithTenant(context.Background(), other, func(tx *postgres.Tx) error {
		_, err := f.store.Insert(context.Background(), tx, f.userID, other, TypeTOTP, "elsewhere", sealed)
		return err
	})
	if err == nil {
		t.Error("a factor was filed under an organization that does not own its user")
	}
}

// A factor is invisible from another tenant.
func TestAFactorIsInvisibleAcrossTenants(t *testing.T) {
	f := factorSetup(t)
	f.enrol(t)

	other := f.factory.Organization(f.factory.Instance())

	var seen int
	err := f.db.WithTenant(context.Background(), other, func(tx *postgres.Tx) error {
		// No org_id predicate, deliberately: `docs/PLAN/08` Part B requires
		// isolation to hold "even when the application layer forgets to
		// filter", and a query that filtered correctly would pass with RLS
		// disabled.
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM user_mfa_factors`).Scan(&seen)
	})
	if err != nil {
		t.Fatalf("counting from the other tenant: %v", err)
	}
	if seen != 0 {
		t.Errorf("another organization can see %d factor(s)", seen)
	}
}

// The list does not carry secrets.
//
// The column is not in the SELECT at all, so no caller can hold one by mistake.
func TestListingFactorsReturnsNoSecret(t *testing.T) {
	f := factorSetup(t)
	factorID, _ := f.enrol(t)

	var factors []Factor
	if err := f.within(t, func(tx *postgres.Tx) error {
		got, err := f.store.ForUser(context.Background(), tx, f.userID)
		factors = got
		return err
	}); err != nil {
		t.Fatalf("ForUser: %v", err)
	}

	if len(factors) != 1 || factors[0].ID != factorID {
		t.Fatalf("the listing is %+v", factors)
	}
	if factors[0].Label != "phone" {
		t.Errorf("the label came back as %q", factors[0].Label)
	}
	if factors[0].Type != TypeTOTP {
		t.Errorf("the type came back as %q", factors[0].Type)
	}
}

// A factor id nothing holds is not found, rather than silently succeeding.
func TestAnUnknownFactorIsNotFound(t *testing.T) {
	f := factorSetup(t)

	err := f.within(t, func(tx *postgres.Tx) error {
		_, _, err := f.store.Sealed(context.Background(), tx, "00000000-0000-4000-8000-000000000000")
		return err
	})
	if !errors.Is(err, ErrFactorNotFound) {
		t.Errorf("an unknown factor gave %v, want ErrFactorNotFound", err)
	}

	err = f.within(t, func(tx *postgres.Tx) error {
		return f.store.Delete(context.Background(), tx, "00000000-0000-4000-8000-000000000000")
	})
	if !errors.Is(err, ErrFactorNotFound) {
		t.Errorf("deleting an unknown factor gave %v, want ErrFactorNotFound", err)
	}
}

// An unimplemented factor type is refused before the database sees it.
func TestAnUnimplementedTypeIsRefusedByTheStore(t *testing.T) {
	f := factorSetup(t)
	sealed, _ := f.sealer.Seal([]byte("a secret"))

	err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Insert(context.Background(), tx, f.userID, f.orgID, Type("sms"), "", sealed)
		return err
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("an unimplemented type gave %v, want ErrUnsupported", err)
	}
}

// A factor with no secret is refused.
func TestAFactorWithNoSecretIsRefused(t *testing.T) {
	f := factorSetup(t)

	err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Insert(context.Background(), tx, f.userID, f.orgID, TypeTOTP, "", nil)
		return err
	})
	if err == nil {
		t.Error("a factor with no secret was stored — it would read as enrolled and " +
			"refuse every code")
	}
}

// strings_Contains reports whether needle appears inside haystack.
//
// Spelled out rather than imported, because `bytes.Contains` on a ciphertext is
// the kind of line somebody later "simplifies" into a string comparison that
// only catches exact equality.
func strings_Contains(haystack, needle []byte) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

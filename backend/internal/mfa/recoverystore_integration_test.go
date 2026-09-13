//go:build integration

// `user_recovery_codes` against real Postgres (P3-04).
//
// Three of the guarantees here exist ONLY in the database and cannot be shown
// against a fake: single-use is a `WHERE used_at IS NULL` rather than a
// read-then-write, regeneration replaces a batch atomically, and RLS confines a
// credential to the tenant that owns it.
package mfa

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

type recoveryFixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	store   *RecoveryStore

	orgID  string
	userID string
}

func recoverySetup(t *testing.T) *recoveryFixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	orgID := factory.Organization(factory.Instance())
	userID := factory.User(orgID)

	return &recoveryFixture{
		db:      openAppPool(t, stack),
		factory: factory,
		store:   NewRecoveryStore(),
		orgID:   orgID,
		userID:  userID,
	}
}

func (f *recoveryFixture) within(t *testing.T, fn func(tx *postgres.Tx) error) error {
	t.Helper()
	return f.db.WithTenant(context.Background(), f.orgID, fn)
}

// issueCodes puts a batch on the fixture's user and returns the plaintexts.
func (f *recoveryFixture) issueCodes(t *testing.T) []string {
	t.Helper()

	var codes []string
	err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		codes, _, err = f.store.Issue(
			context.Background(), tx, f.userID, f.orgID, RecoveryCodeCount, testNow())
		return err
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return codes
}

func testNow() time.Time { return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC) }

// --- single use ------------------------------------------------------------------

func TestARecoveryCodeWorksExactlyOnce(t *testing.T) {
	f := recoverySetup(t)
	codes := f.issueCodes(t)

	var remaining int
	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		remaining, err = f.store.Consume(context.Background(), tx, f.userID, codes[0], testNow())
		return err
	}); err != nil {
		t.Fatalf("the first use was refused: %v", err)
	}
	if remaining != RecoveryCodeCount-1 {
		t.Errorf("remaining = %d, want %d", remaining, RecoveryCodeCount-1)
	}

	// The same code again.
	err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Consume(context.Background(), tx, f.userID, codes[0], testNow())
		return err
	})
	if !errors.Is(err, ErrNoRecoveryCode) {
		t.Errorf("a spent code was accepted again: %v", err)
	}

	// And the others still work — spending one must not spend the batch.
	if err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Consume(context.Background(), tx, f.userID, codes[1], testNow())
		return err
	}); err != nil {
		t.Errorf("a sibling code stopped working: %v", err)
	}
}

// Two requests presenting the same code at the same moment: exactly one wins.
//
// This is the property a read-then-write cannot have, and the reason the
// consume is a conditional UPDATE. Without it a shoulder-surfed code raced
// against its owner would let both in.
func TestConcurrentUseOfOneCodeAdmitsExactlyOne(t *testing.T) {
	f := recoverySetup(t)
	codes := f.issueCodes(t)

	const racers = 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
	)

	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
				_, err := f.store.Consume(context.Background(), tx, f.userID, codes[0], testNow())
				return err
			})
			if err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("%d of %d concurrent uses of one code succeeded, want exactly 1", succeeded, racers)
	}
}

// --- regeneration -------------------------------------------------------------------

func TestRegeneratingInvalidatesEveryPreviousCode(t *testing.T) {
	f := recoverySetup(t)
	first := f.issueCodes(t)

	second := f.issueCodes(t)

	// Every code from the first batch is dead.
	for i, code := range first {
		err := f.within(t, func(tx *postgres.Tx) error {
			_, err := f.store.Consume(context.Background(), tx, f.userID, code, testNow())
			return err
		})
		if !errors.Is(err, ErrNoRecoveryCode) {
			t.Fatalf("code %d from the previous batch still works: %v", i, err)
		}
	}

	// And the new batch is whole — regeneration did not leave a half-replaced
	// set where some codes work and some do not.
	var remaining int
	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		remaining, err = f.store.Remaining(context.Background(), tx, f.userID)
		return err
	}); err != nil {
		t.Fatalf("Remaining: %v", err)
	}
	if remaining != RecoveryCodeCount {
		t.Errorf("remaining = %d after regeneration, want %d", remaining, RecoveryCodeCount)
	}

	if err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Consume(context.Background(), tx, f.userID, second[0], testNow())
		return err
	}); err != nil {
		t.Errorf("a code from the new batch does not work: %v", err)
	}
}

// --- isolation --------------------------------------------------------------------------

// A code is confined to the tenant that owns it, and to the user it was issued
// for. Abuse case A-3.
func TestARecoveryCodeCannotBeSpentAgainstAnotherUser(t *testing.T) {
	f := recoverySetup(t)
	codes := f.issueCodes(t)

	// A second user in the SAME organization, so RLS is not what is being
	// tested here — the user scoping in the query is.
	sibling := f.factory.User(f.orgID)

	err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Consume(context.Background(), tx, sibling, codes[0], testNow())
		return err
	})
	if !errors.Is(err, ErrNoRecoveryCode) {
		t.Errorf("one user's code was spent against another: %v", err)
	}

	// The positive control: the code still works for its owner, so the test
	// above proves scoping rather than a code that was already broken.
	if err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Consume(context.Background(), tx, f.userID, codes[0], testNow())
		return err
	}); err != nil {
		t.Errorf("the code does not work for its owner either: %v", err)
	}
}

// RLS confines the table. A code is invisible from another tenant.
func TestRecoveryCodesAreConfinedByRowLevelSecurity(t *testing.T) {
	f := recoverySetup(t)
	f.issueCodes(t)

	other := f.factory.Organization(f.factory.Instance())

	var seen int
	err := f.db.WithTenant(context.Background(), other, func(tx *postgres.Tx) error {
		var err error
		seen, err = f.store.Remaining(context.Background(), tx, f.userID)
		return err
	})
	if err != nil {
		t.Fatalf("counting from another tenant: %v", err)
	}
	if seen != 0 {
		t.Errorf("%d codes are visible from another organization; RLS did not confine them", seen)
	}

	// Positive control: they ARE visible from their own.
	var own int
	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		own, err = f.store.Remaining(context.Background(), tx, f.userID)
		return err
	}); err != nil {
		t.Fatalf("Remaining: %v", err)
	}
	if own != RecoveryCodeCount {
		t.Errorf("own = %d, want %d — the isolation test above proves nothing if this is zero",
			own, RecoveryCodeCount)
	}
}

// The trigger refuses a code filed under a tenant that does not own its user.
func TestACodeCannotBeFiledUnderTheWrongTenant(t *testing.T) {
	f := recoverySetup(t)
	other := f.factory.Organization(f.factory.Instance())

	err := f.db.WithTenant(context.Background(), other, func(tx *postgres.Tx) error {
		_, _, err := f.store.Issue(
			context.Background(), tx, f.userID, other, RecoveryCodeCount, testNow())
		return err
	})
	if err == nil {
		t.Error("codes were filed under an organization that does not own the user")
	}
}

// --- what the plaintext never touches --------------------------------------------------

// The stored row contains neither the code nor anything it can be recovered
// from. The whole argument for SHA-256 over Argon2 (PG-39) rests on the input's
// entropy, not on the column being unreadable — so what matters is that the
// PLAINTEXT is absent.
func TestTheStoredRowHoldsNoPlaintext(t *testing.T) {
	f := recoverySetup(t)
	codes := f.issueCodes(t)

	plain := NormaliseRecoveryCode(codes[0])

	var stored []byte
	if err := f.within(t, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT code_hash FROM user_recovery_codes WHERE user_id = $1 LIMIT 1`,
			f.userID).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading a stored code: %v", err)
	}

	if string(stored) == plain || string(stored) == codes[0] {
		t.Fatal("the code is stored in the clear")
	}
	if len(stored) != 32 {
		t.Errorf("the stored value is %d bytes, want a 32-byte SHA-256", len(stored))
	}
}

// --- counting and clearing ---------------------------------------------------------------

func TestRemainingTracksWhatHasBeenSpent(t *testing.T) {
	f := recoverySetup(t)
	codes := f.issueCodes(t)

	for spent := 1; spent <= 3; spent++ {
		if err := f.within(t, func(tx *postgres.Tx) error {
			_, err := f.store.Consume(context.Background(), tx, f.userID, codes[spent-1], testNow())
			return err
		}); err != nil {
			t.Fatalf("spending code %d: %v", spent, err)
		}

		var remaining int
		if err := f.within(t, func(tx *postgres.Tx) error {
			var err error
			remaining, err = f.store.Remaining(context.Background(), tx, f.userID)
			return err
		}); err != nil {
			t.Fatalf("Remaining: %v", err)
		}
		if want := RecoveryCodeCount - spent; remaining != want {
			t.Errorf("after %d spent, remaining = %d, want %d", spent, remaining, want)
		}
	}
}

// Clear destroys spent codes too — a used code is a spent credential of an
// account being handed back, and its row would keep a hash of something
// somebody once wrote down.
func TestClearRemovesSpentCodesAsWellAsLiveOnes(t *testing.T) {
	f := recoverySetup(t)
	codes := f.issueCodes(t)

	if err := f.within(t, func(tx *postgres.Tx) error {
		_, err := f.store.Consume(context.Background(), tx, f.userID, codes[0], testNow())
		return err
	}); err != nil {
		t.Fatalf("spending one: %v", err)
	}

	var removed int
	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		removed, err = f.store.Clear(context.Background(), tx, f.userID)
		return err
	}); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if removed != RecoveryCodeCount {
		t.Errorf("Clear removed %d rows, want %d — the spent one was left behind",
			removed, RecoveryCodeCount)
	}

	var left int
	if err := f.within(t, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, f.userID).Scan(&left)
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if left != 0 {
		t.Errorf("%d rows survived Clear", left)
	}
}

// HasUnused is what the challenge reads to decide whether to offer the option.
func TestHasUnusedFollowsTheRemainingCodes(t *testing.T) {
	f := recoverySetup(t)

	var has bool
	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		has, err = f.store.HasUnused(context.Background(), tx, f.userID)
		return err
	}); err != nil {
		t.Fatalf("HasUnused: %v", err)
	}
	if has {
		t.Error("a user with no codes is reported as having some")
	}

	codes := f.issueCodes(t)

	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		has, err = f.store.HasUnused(context.Background(), tx, f.userID)
		return err
	}); err != nil {
		t.Fatalf("HasUnused: %v", err)
	}
	if !has {
		t.Error("a user holding ten codes is reported as having none")
	}

	// Spend them all.
	for _, code := range codes {
		if err := f.within(t, func(tx *postgres.Tx) error {
			_, err := f.store.Consume(context.Background(), tx, f.userID, code, testNow())
			return err
		}); err != nil {
			t.Fatalf("spending: %v", err)
		}
	}

	if err := f.within(t, func(tx *postgres.Tx) error {
		var err error
		has, err = f.store.HasUnused(context.Background(), tx, f.userID)
		return err
	}); err != nil {
		t.Fatalf("HasUnused: %v", err)
	}
	if has {
		t.Error("a user who has spent every code is still offered the option")
	}
}

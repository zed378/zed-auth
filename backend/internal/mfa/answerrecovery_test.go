package mfa

import (
	"context"
	"errors"
	"testing"
	"time"
)

// `AnswerRecovery` (P3-04).
//
// It is a sibling of AnswerType rather than a factor type, and what these
// assert is that being a sibling did not mean losing any of the controls
// AnswerType has: the challenge decides who, the pending request must match,
// the attempt bound is charged, and a spent challenge stays spent.

// fakeRecovery is a recovery store in a map.
type fakeRecovery struct {
	// codes are the unspent codes, keyed by user.
	codes map[string][]string

	// err is returned instead of a verdict, for the outage case.
	err error

	// seenOrg and seenUser record the scope Spend was called with, so a test
	// can assert the framework passed the challenge's own identity rather than
	// anything else. An argument-order slip here is invisible to the compiler,
	// because both are strings.
	seenOrg  string
	seenUser string
	spends   int
}

func newFakeRecovery(user string, codes ...string) *fakeRecovery {
	return &fakeRecovery{codes: map[string][]string{user: codes}}
}

func (f *fakeRecovery) Unspent(_ context.Context, orgID, userID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.seenOrg, f.seenUser = orgID, userID
	return len(f.codes[userID]) > 0, nil
}

func (f *fakeRecovery) Spend(_ context.Context, orgID, userID, code string) (int, error) {
	f.spends++
	f.seenOrg, f.seenUser = orgID, userID
	if f.err != nil {
		return 0, f.err
	}

	held := f.codes[userID]
	for i, candidate := range held {
		if candidate == code {
			f.codes[userID] = append(append([]string{}, held[:i]...), held[i+1:]...)
			return len(f.codes[userID]), nil
		}
	}
	return 0, ErrNoRecoveryCode
}

// recoveryFramework builds one with a recovery store attached.
func recoveryFramework(t *testing.T, codes ...string) (*Framework, *memoryChallenges, *fakeRecovery) {
	t.Helper()

	challenges := newMemoryChallenges()
	store := newFakeRecovery("u1", codes...)

	return &Framework{
		Registry:   NewRegistry(&fakeVerifier{kind: TypeTOTP, correct: "123456"}),
		Store:      &memoryStore{factors: []Factor{confirmedTOTP("f1")}},
		Challenges: challenges,
		Recovery:   store,
	}, challenges, store
}

// --- the happy path ---------------------------------------------------------------

func TestARecoveryCodeCompletesAChallenge(t *testing.T) {
	f, store, recovery := recoveryFramework(t, "CODEONE", "CODETWO")
	handle := issued(t, store, "pending-1", "f1")

	out, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "CODEONE")
	if err != nil {
		t.Fatalf("AnswerRecovery: %v", err)
	}

	if !out.Complete {
		t.Fatal("a correct recovery code did not complete the challenge")
	}
	if !out.Recovery {
		t.Error("the outcome does not say a recovery code was used; the audit event and the amr claim both depend on it")
	}
	if out.Remaining != 1 {
		t.Errorf("Remaining = %d, want 1", out.Remaining)
	}
	if out.UserID != "u1" || out.OrgID != "o1" {
		t.Errorf("the outcome carries %+v, not the challenge's identity", out)
	}

	// The scope came from the challenge, in the right order. This is the slip
	// the compiler cannot see: both arguments are strings, so a transposed
	// call would hand a user id to WithTenant and silently find nothing.
	if recovery.seenOrg != "o1" || recovery.seenUser != "u1" {
		t.Errorf("Spend was scoped to org=%q user=%q, want org=o1 user=u1",
			recovery.seenOrg, recovery.seenUser)
	}
}

// The challenge is consumed, so a code cannot complete two logins.
func TestACompletedRecoveryChallengeCannotBeReused(t *testing.T) {
	f, store, _ := recoveryFramework(t, "CODEONE", "CODETWO")
	handle := issued(t, store, "pending-1", "f1")

	if _, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "CODEONE"); err != nil {
		t.Fatalf("AnswerRecovery: %v", err)
	}

	_, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "CODETWO")
	if !errors.Is(err, ErrNoChallenge) {
		t.Errorf("a completed challenge answered again gave %v, want ErrNoChallenge", err)
	}
}

// --- the bindings AnswerType has, which this must not have lost -------------------

// A challenge completes the request it was issued for, and no other.
func TestARecoveryAnswerDoesNotCompleteADifferentRequest(t *testing.T) {
	f, store, recovery := recoveryFramework(t, "CODEONE")
	handle := issued(t, store, "pending-1", "f1")

	out, err := f.AnswerRecovery(context.Background(), handle, "pending-2", "CODEONE")

	if !errors.Is(err, ErrNoChallenge) {
		t.Errorf("AnswerRecovery gave %v, want ErrNoChallenge", err)
	}
	if out.Complete {
		t.Fatal("a challenge issued for one request completed another")
	}
	// And the code was NOT spent. A correct code refused after the fact would
	// still be gone, so the user would have lost it to a request they never
	// made.
	if recovery.spends != 0 {
		t.Errorf("the code was spent %d time(s) for a mismatched request", recovery.spends)
	}
}

// The attempt bound is charged, and it is the SAME counter TOTP guesses use.
//
// Two separate allowances would not be two bounds — it would be one bound twice
// as large, reached by choosing which form to guess in.
func TestRecoveryGuessesShareTheFactorAttemptBound(t *testing.T) {
	f, store, _ := recoveryFramework(t, "CODEONE")
	bound := &countingAttempts{allow: true}
	f.Attempts = bound
	handle := issued(t, store, "pending-1", "f1")

	if _, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "WRONG"); !errors.Is(err, ErrNoRecoveryCode) {
		t.Fatalf("AnswerRecovery gave %v, want ErrNoRecoveryCode", err)
	}

	if bound.checked == 0 {
		t.Error("the attempt bound was not consulted before a recovery guess")
	}
	if bound.failed == 0 {
		t.Error("a wrong recovery code did not count against the attempt bound")
	}
}

// An exhausted bound refuses before the store is touched.
func TestAnExhaustedBoundRefusesARecoveryGuessWithoutAskingTheStore(t *testing.T) {
	f, store, recovery := recoveryFramework(t, "CODEONE")
	f.Attempts = &countingAttempts{allow: false}
	handle := issued(t, store, "pending-1", "f1")

	_, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "CODEONE")

	if !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("AnswerRecovery gave %v, want ErrTooManyAttempts", err)
	}
	if recovery.spends != 0 {
		t.Error("an exhausted user still reached the recovery store; a correct code would have been spent")
	}
}

// A spent challenge is refused and consumed.
func TestASpentChallengeRefusesARecoveryCode(t *testing.T) {
	challenges := newMemoryChallenges()
	f := &Framework{
		Registry:   NewRegistry(&fakeVerifier{kind: TypeTOTP, correct: "123456"}),
		Store:      &memoryStore{factors: []Factor{confirmedTOTP("f1")}},
		Challenges: challenges,
		Recovery:   newFakeRecovery("u1", "CODEONE"),
	}

	handle, err := challenges.Put(context.Background(), Challenge{
		UserID: "u1", OrgID: "o1", PendingID: "pending-1",
		FactorIDs: []string{"f1"}, Attempts: MaxAttempts,
	}, ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "CODEONE"); !errors.Is(err, ErrChallengeSpent) {
		t.Errorf("AnswerRecovery gave %v, want ErrChallengeSpent", err)
	}
}

// Wrong codes eventually spend the challenge, exactly as wrong TOTP codes do.
func TestWrongRecoveryCodesSpendTheChallenge(t *testing.T) {
	f, store, _ := recoveryFramework(t, "CODEONE")
	handle := issued(t, store, "pending-1", "f1")

	var last error
	for i := 0; i < MaxAttempts; i++ {
		_, last = f.AnswerRecovery(context.Background(), handle, "pending-1", "WRONG")
	}

	if !errors.Is(last, ErrChallengeSpent) {
		t.Errorf("after %d wrong recovery codes the challenge gave %v, want ErrChallengeSpent",
			MaxAttempts, last)
	}
}

// --- what the offer says -------------------------------------------------------------

// A challenge raised for a user holding codes offers the recovery option.
func TestAChallengeOffersRecoveryWhenCodesExist(t *testing.T) {
	f, _, _ := recoveryFramework(t, "CODEONE")

	decision, err := f.Required(context.Background(), "u1", "o1", "pending-1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if !decision.Challenge {
		t.Fatal("no challenge was raised")
	}
	if !decision.Recovery {
		t.Error("the challenge does not offer recovery to a user holding codes")
	}
}

// And does not, when there are none — a dead option is a user who cannot sign
// in reading that they can.
func TestAChallengeDoesNotOfferRecoveryWithoutCodes(t *testing.T) {
	f, _, _ := recoveryFramework(t)

	decision, err := f.Required(context.Background(), "u1", "o1", "pending-1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if decision.Recovery {
		t.Error("recovery is offered to a user with no codes")
	}
}

// A recovery store that cannot answer does NOT fail the login.
//
// The challenge is already stored and the factor still works, so the only
// consequence of answering "no" is one option not offered. Refusing instead
// would turn an outage in the recovery table into an outage in sign-in for
// everybody who has a factor.
func TestARecoveryStoreOutageDoesNotBlockTheChallenge(t *testing.T) {
	f, _, recovery := recoveryFramework(t, "CODEONE")
	recovery.err = errors.New("the recovery table is unreachable")

	decision, err := f.Required(context.Background(), "u1", "o1", "pending-1")
	if err != nil {
		t.Fatalf("a recovery-store outage failed the whole challenge: %v", err)
	}
	if !decision.Challenge {
		t.Error("no challenge was raised, so a user with a working factor cannot sign in")
	}
	if decision.Recovery {
		t.Error("recovery was offered despite the store being unreachable")
	}
}

// A build with no recovery store refuses recovery codes rather than panicking.
func TestABuildWithoutRecoveryRefusesRecoveryCodes(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}},
		[]Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	if _, err := f.AnswerRecovery(context.Background(), handle, "pending-1", "CODEONE"); !errors.Is(err, ErrNoRecoveryCode) {
		t.Errorf("AnswerRecovery on a build with no recovery store gave %v, want ErrNoRecoveryCode", err)
	}
}

// --- the amr claim -----------------------------------------------------------------------

// A recovery login claims `mfa` and NOT `otp`.
//
// `otp` would be a lie — a consumer refusing a payment unless `amr` contains it
// is asking whether a one-time-password DEVICE was used, and the user no longer
// has one. `mfa` is true: a password plus a held credential is two factors.
func TestARecoveryLoginClaimsMultiFactorButNotOTP(t *testing.T) {
	methods := AuthMethodsWithRecovery(true, true)

	var hasMFA, hasOTP bool
	for _, m := range methods {
		switch m {
		case MethodMultiFactor:
			hasMFA = true
		case TypeTOTP.AMR():
			hasOTP = true
		}
	}

	if !hasMFA {
		t.Errorf("amr = %v, missing %q — a recovery login would be indistinguishable from a password-only one",
			methods, MethodMultiFactor)
	}
	if hasOTP {
		t.Errorf("amr = %v claims %q, but no authenticator device was presented",
			methods, TypeTOTP.AMR())
	}
	if len(methods) != 2 {
		t.Errorf("amr = %v, want exactly [mfa pwd]", methods)
	}
}

// A normal factor login is unchanged by the new function.
func TestAFactorLoginsAmrIsUnchanged(t *testing.T) {
	withRecovery := AuthMethodsWithRecovery(true, false, TypeTOTP)
	plain := AuthMethods(true, TypeTOTP)

	if len(withRecovery) != len(plain) {
		t.Fatalf("amr = %v with the recovery-aware builder, %v without", withRecovery, plain)
	}
	for i := range plain {
		if withRecovery[i] != plain[i] {
			t.Errorf("amr = %v, want %v", withRecovery, plain)
		}
	}
}

// --- a bound that counts ------------------------------------------------------------------

type countingAttempts struct {
	allow   bool
	checked int
	failed  int
}

func (c *countingAttempts) Allowed(context.Context, string, time.Time) (bool, error) {
	c.checked++
	return c.allow, nil
}

func (c *countingAttempts) Fail(context.Context, string, time.Time) (bool, error) {
	c.failed++
	return c.allow, nil
}

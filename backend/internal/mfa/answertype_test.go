package mfa

import (
	"context"
	"errors"
	"testing"
	"time"
)

// `AnswerType`, `Peek` and `idOfType` in their own package (P3-03).
//
// They were added for the login flow and were tested only through it, which
// left them at 0% here — and that is not only a number. The framework's
// contract is what `P3-05` and `P3-12` will build against, and a contract whose
// only test lives in one consumer is a contract that changes shape the moment a
// second consumer appears.
//
// The binding these assert is the one the end-to-end run caught after the unit
// tests had all passed: a challenge completes the authorization request it was
// ISSUED for and no other.

// issued puts a challenge in the store and returns its handle.
func issued(t *testing.T, store *memoryChallenges, pendingID string, factorIDs ...string) string {
	t.Helper()

	handle, err := store.Put(context.Background(), Challenge{
		UserID:    "u1",
		OrgID:     "o1",
		PendingID: pendingID,
		FactorIDs: factorIDs,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}, ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return handle
}

// --- the binding ----------------------------------------------------------------

// A challenge completes the request it was issued for.
func TestAnAnswerCompletesTheRequestTheChallengeWasIssuedFor(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}}, []Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	out, err := f.AnswerType(context.Background(), handle, "pending-1", TypeTOTP, "123456")
	if err != nil {
		t.Fatalf("AnswerType: %v", err)
	}
	if !out.Complete {
		t.Fatal("a correct code did not complete the challenge")
	}
	if out.UserID != "u1" || out.OrgID != "o1" || out.PendingID != "pending-1" {
		t.Errorf("the outcome carries %+v, not the challenge's own identity", out)
	}
	if len(out.Methods) != 1 || out.Methods[0] != TypeTOTP {
		t.Errorf("Methods = %v, want [totp]", out.Methods)
	}
}

// And it completes NO OTHER request, even with a correct code.
//
// This is the bug `P3-03`'s first end-to-end run found: the handler took the
// pending id from the form and the challenge from a cookie and never asked
// whether they were the same login. A second factor legitimately proven would
// then have been attached to an authorization somebody else started.
func TestAChallengeDoesNotCompleteADifferentRequest(t *testing.T) {
	verifier := &fakeVerifier{kind: TypeTOTP, correct: "123456"}
	f, store := framework(t, []Verifier{verifier}, []Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	out, err := f.AnswerType(context.Background(), handle, "pending-2", TypeTOTP, "123456")

	if !errors.Is(err, ErrNoChallenge) {
		t.Errorf("AnswerType gave %v, want ErrNoChallenge — for THIS login there is none", err)
	}
	if out.Complete {
		t.Fatal("a challenge issued for one request completed another")
	}
	// Nothing about the other login is disclosed, not even that it exists.
	if out.UserID != "" || out.OrgID != "" {
		t.Errorf("the refusal disclosed %+v about a login this caller does not own", out)
	}
}

// The check runs BEFORE the verifier. A correct answer refused after the fact
// would still have recorded its counter, so the user's next real code would be
// the one after a step they never used.
func TestAMismatchedRequestDoesNotSpendTheCode(t *testing.T) {
	verifier := &countingFakeVerifier{correct: "123456"}
	f, store := framework(t, []Verifier{verifier}, []Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	_, _ = f.AnswerType(context.Background(), handle, "pending-2", TypeTOTP, "123456")

	if verifier.verifies != 0 {
		t.Errorf("the verifier ran %d time(s) for a mismatched request; the code was spent",
			verifier.verifies)
	}
}

// --- what a wrong answer yields ---------------------------------------------------

// A wrong code leaves the challenge live and says what may still be answered,
// so the page can be re-rendered without the caller holding state of its own.
func TestAWrongCodeReturnsWhatMayStillBeAnswered(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}}, []Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	out, err := f.AnswerType(context.Background(), handle, "pending-1", TypeTOTP, "000000")

	if !errors.Is(err, ErrWrongCode) {
		t.Fatalf("AnswerType gave %v, want ErrWrongCode", err)
	}
	if len(out.Offered) != 1 || out.Offered[0] != TypeTOTP {
		t.Errorf("Offered = %v, want [totp] so the page can re-render", out.Offered)
	}
	// The identity is carried even on the failure, so the caller can audit the
	// attempt against the account it was aimed at without a second lookup.
	if out.UserID != "u1" || out.OrgID != "o1" {
		t.Errorf("a failed answer carries %+v; an audit row could not name the account", out)
	}
}

// A factor TYPE this challenge has no factor for answers as ErrNoSuchFactor —
// the same answer a guessed id gets, because the difference is a fact about
// what this user has enrolled.
func TestAnUnenrolledTypeIsRefusedLikeAnUnknownFactor(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}}, []Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	_, err := f.AnswerType(context.Background(), handle, "pending-1", TypeWebAuthn, "123456")

	if !errors.Is(err, ErrNoSuchFactor) {
		t.Errorf("AnswerType gave %v, want ErrNoSuchFactor", err)
	}
}

// A factor the challenge did not name cannot be answered with, even when the
// user genuinely holds it. The challenge's id list is the authority.
func TestAFactorTheChallengeDidNotNameCannotAnswerIt(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}},
		[]Factor{confirmedTOTP("f1"), confirmedTOTP("f2")})

	// The challenge names f2 only; the user also holds f1.
	handle := issued(t, store, "pending-1", "f2")

	out, err := f.AnswerType(context.Background(), handle, "pending-1", TypeTOTP, "123456")
	if err != nil {
		t.Fatalf("AnswerType: %v", err)
	}
	if !out.Complete {
		t.Fatal("the factor the challenge DID name could not answer it")
	}

	// And a challenge naming nothing cannot be answered at all.
	empty := issued(t, store, "pending-3")
	if _, err := f.AnswerType(context.Background(), empty, "pending-3", TypeTOTP, "123456"); !errors.Is(err, ErrNoSuchFactor) {
		t.Errorf("a challenge naming no factor gave %v, want ErrNoSuchFactor", err)
	}
}

// --- Peek --------------------------------------------------------------------------

// Peek reports what may be answered and consumes nothing, so a refresh does
// not cost the user an attempt.
func TestPeekConsumesNothing(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}}, []Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	for i := 0; i < 3; i++ {
		offered, err := f.Peek(context.Background(), handle)
		if err != nil {
			t.Fatalf("Peek %d: %v", i, err)
		}
		if len(offered) != 1 || offered[0] != TypeTOTP {
			t.Fatalf("Peek %d offered %v", i, offered)
		}
	}

	// Still answerable after three peeks.
	if out, err := f.AnswerType(context.Background(), handle, "pending-1", TypeTOTP, "123456"); err != nil || !out.Complete {
		t.Errorf("peeking spent the challenge: %v", err)
	}
}

// It reads the factors FRESH, so one removed mid-challenge stops being offered
// at once rather than being shown until the challenge expires.
func TestPeekStopsOfferingAFactorThatHasGone(t *testing.T) {
	challenges := newMemoryChallenges()
	store := &growingStore{factors: []Factor{confirmedTOTP("f1")}}
	f := &Framework{
		Registry:   NewRegistry(&fakeVerifier{kind: TypeTOTP, correct: "123456"}),
		Store:      store,
		Challenges: challenges,
	}
	handle := issued(t, challenges, "pending-1", "f1")

	if offered, _ := f.Peek(context.Background(), handle); len(offered) != 1 {
		t.Fatalf("the factor was not offered to begin with: %v", offered)
	}

	store.factors = nil

	offered, err := f.Peek(context.Background(), handle)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(offered) != 0 {
		t.Errorf("a removed factor is still offered: %v", offered)
	}
}

// A spent challenge offers nothing and says so, rather than rendering a form
// whose every answer is refused.
func TestPeekRefusesASpentChallenge(t *testing.T) {
	challenges := newMemoryChallenges()
	f := &Framework{
		Registry:   NewRegistry(&fakeVerifier{kind: TypeTOTP, correct: "123456"}),
		Store:      &memoryStore{factors: []Factor{confirmedTOTP("f1")}},
		Challenges: challenges,
	}

	handle, err := challenges.Put(context.Background(), Challenge{
		UserID: "u1", OrgID: "o1", PendingID: "pending-1",
		FactorIDs: []string{"f1"}, Attempts: MaxAttempts,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}, ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := f.Peek(context.Background(), handle); !errors.Is(err, ErrChallengeSpent) {
		t.Errorf("Peek gave %v, want ErrChallengeSpent", err)
	}
}

// A handle naming nothing is refused by both entry points, identically.
func TestAnUnknownHandleIsRefusedByBoth(t *testing.T) {
	f, _ := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}}, []Factor{confirmedTOTP("f1")})

	if _, err := f.Peek(context.Background(), "nothing"); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("Peek gave %v, want ErrNoChallenge", err)
	}
	if _, err := f.AnswerType(context.Background(), "nothing", "pending-1", TypeTOTP, "123456"); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("AnswerType gave %v, want ErrNoChallenge", err)
	}
}

// --- a verifier that counts -------------------------------------------------------

type countingFakeVerifier struct {
	correct  string
	verifies int
}

func (c *countingFakeVerifier) Type() Type { return TypeTOTP }
func (c *countingFakeVerifier) Begin(context.Context, string, string, string) (Enrolment, error) {
	return Enrolment{}, nil
}
func (c *countingFakeVerifier) Confirm(context.Context, string, string) error { return nil }
func (c *countingFakeVerifier) Verify(_ context.Context, _, code string) error {
	c.verifies++
	if code != c.correct {
		return ErrWrongCode
	}
	return nil
}
func (c *countingFakeVerifier) Remove(context.Context, string) error { return nil }

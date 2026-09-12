package mfa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Deciding whether to challenge, and completing one (P3-01 steps 3–5).
//
// The two questions this answers are different and are asked at different
// moments:
//
//	Required   — before a session exists. "Has this login finished?"
//	StepUp     — with a live session. "Is this session strong enough for what
//	             is being asked now?"
//
// Both read what was actually used. Neither reads what is enrolled.

// EnrolledFactors is the framework's view of what a user may be challenged
// with. Named for the question rather than for the table, because the answer
// is "active factors of an implemented type" rather than "rows".
type EnrolledFactors interface {
	// Confirmed returns the factors a user may be challenged with: enrolled,
	// confirmed, and of a type this build implements.
	//
	// The ORGANIZATION is a parameter, not something the implementation looks
	// up. Every caller in this package already holds it — `Required` is given
	// it, and every other path reads it from the challenge — so passing it
	// removes the need for a cross-tenant user lookup, which would mean a
	// SECURITY DEFINER function answering "which organization is this user in"
	// for any user id. A privilege that is never needed is the cheapest kind
	// to not have.
	Confirmed(ctx context.Context, orgID, userID string) ([]Factor, error)
}

// Framework decides and completes challenges.
type Framework struct {
	Registry   *Registry
	Store      EnrolledFactors
	Challenges ChallengeStore
	Log        *slog.Logger

	// Attempts bounds a user's failed guesses across challenges (P3-03).
	//
	// Nil means unbounded, which is what a unit test about something else
	// wants and what no deployment may have — `TestTheLoginFlowBoundsFactorGuesses`
	// in architecture_test.go is what makes the wiring set it, because a
	// comment here would not.
	Attempts AttemptBound

	Now func() time.Time
}

func (f *Framework) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Framework) log() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.Default()
}

// Decision is what should happen after a password is proven.
type Decision struct {
	// Challenge is true when the login must not complete yet.
	Challenge bool

	// Handle is the opaque state for that challenge. Empty when Challenge is
	// false.
	Handle string

	// Offered are the factor types the user may answer with — what they
	// actually have, so a user who lost one device is not shown a dead end.
	Offered []Type
}

// Required decides whether a login must present a factor before it completes.
//
// **Returns no challenge when the user has no confirmed factor**, and that is
// not a gap: whether an organization may *demand* one is `P3-07`'s question,
// enforced against `mfa_required` with a transition designed for the users who
// have not enrolled yet. This function answers only "can they".
//
// A store failure fails CLOSED for the challenge — no factor is treated as
// verified — but does not invent a challenge the user cannot answer. It
// returns the error, and the caller refuses the login. Letting a login through
// because the factor store was unreachable is the one mistake here that cannot
// be walked back.
func (f *Framework) Required(ctx context.Context, userID, orgID, pendingID string) (Decision, error) {
	if f.Registry.Empty() {
		// No factor type is implemented in this build, so there is nothing to
		// challenge with. Not an optimisation: a challenge offering nothing
		// is a dead end, and it would be reachable on every deployment until
		// `P3-02` lands.
		return Decision{}, nil
	}

	factors, err := f.Store.Confirmed(ctx, orgID, userID)
	if err != nil {
		return Decision{}, fmt.Errorf("mfa: reading enrolled factors: %w", err)
	}

	answerable := make([]Factor, 0, len(factors))
	offered := make([]Type, 0, len(factors))
	seen := map[Type]bool{}

	for _, factor := range factors {
		if !factor.Active() {
			// Defensive: the store's query should filter these. A half-finished
			// enrolment must never become answerable, and the cheapest place
			// to be sure is here.
			continue
		}
		if _, err := f.Registry.For(factor.Type); err != nil {
			// Enrolled under a type this build no longer implements — a
			// rollback, or a factor removed from the registry. It cannot be
			// challenged with, so it is not offered. Logged, because a user
			// silently losing a factor is worth noticing.
			f.log().Warn("an enrolled factor has no verifier in this build",
				"factor_type", string(factor.Type), "factor_id", factor.ID)
			continue
		}
		answerable = append(answerable, factor)
		if !seen[factor.Type] {
			seen[factor.Type] = true
			offered = append(offered, factor.Type)
		}
	}

	if len(answerable) == 0 {
		return Decision{}, nil
	}

	ids := make([]string, 0, len(answerable))
	for _, factor := range answerable {
		ids = append(ids, factor.ID)
	}

	handle, err := NewHandle()
	if err != nil {
		return Decision{}, err
	}

	stored, err := f.Challenges.Put(ctx, Challenge{
		UserID:    userID,
		OrgID:     orgID,
		PendingID: pendingID,
		FactorIDs: ids,
		Methods:   nil,
		CreatedAt: f.now(),
	}, ChallengeTTL)
	if err != nil {
		return Decision{}, fmt.Errorf("mfa: storing a challenge: %w", err)
	}
	if stored != "" {
		handle = stored
	}

	return Decision{Challenge: true, Handle: handle, Offered: offered}, nil
}

// Answer verifies one attempt against a challenge.
//
// Returns the factor types proven so far when the challenge is complete, and
// ErrWrongCode when the attempt was simply wrong.
func (f *Framework) Answer(ctx context.Context, handle, factorID, code string) ([]Type, error) {
	challenge, err := f.Challenges.Get(ctx, handle)
	if err != nil {
		return nil, err
	}

	if challenge.Spent() {
		// Consumed rather than left to expire: a spent challenge that stayed
		// readable would let an attacker keep probing whether it existed.
		_ = f.Challenges.Delete(ctx, handle)
		return nil, ErrChallengeSpent
	}

	// The factor must be one THIS challenge named. Without it, a caller could
	// answer with any factor id they could guess — including another user's —
	// and the verification would be against a credential the challenge was
	// never about. Abuse case A-3.
	if !contains(challenge.FactorIDs, factorID) {
		return nil, ErrNoSuchFactor
	}

	// The per-user bound, BEFORE the verifier runs (P3-03 step 2).
	//
	// Before, for the reason `P1-13` checks the login limiter before Argon2: a
	// refused attempt must not cost the verification work, or the bound becomes
	// a way to ask for that work rather than a way to stop asking.
	//
	// It lives here rather than in the handler so that no caller can answer a
	// challenge without it — including `P3-12`'s enrolment confirmation and
	// whatever `P3-05` adds. A control the callers have to remember is a
	// control one of them will not.
	if f.Attempts != nil {
		allowed, err := f.Attempts.Allowed(ctx, challenge.UserID, f.now())
		if err != nil {
			// Fails CLOSED. See RedisAttempts: the challenge store is the same
			// Redis, so this cannot refuse a login that would otherwise have
			// worked — it can only stop an outage from removing the bound.
			return nil, fmt.Errorf("mfa: reading the attempt bound: %w", err)
		}
		if !allowed {
			return nil, ErrTooManyAttempts
		}
	}

	factor, err := f.factorType(ctx, challenge.OrgID, challenge.UserID, factorID)
	if err != nil {
		return nil, err
	}

	verifier, err := f.Registry.For(factor)
	if err != nil {
		return nil, err
	}

	switch err := verifier.Verify(ctx, factorID, code); {
	case err == nil:
		// Complete. Consumed, so it cannot be replayed.
		if delErr := f.Challenges.Delete(ctx, handle); delErr != nil {
			f.log().Warn("deleting a completed challenge failed", "error", delErr.Error())
		}
		return append(challenge.Methods, factor), nil

	case errors.Is(err, ErrWrongCode):
		// Counted against the user before the challenge, so that abandoning a
		// challenge and starting another does not shed the count. That is the
		// whole difference between this bound and `MaxAttempts`.
		if f.Attempts != nil {
			if _, failErr := f.Attempts.Fail(ctx, challenge.UserID, f.now()); failErr != nil {
				// Recorded as a warning and the attempt still refused. The
				// alternative — returning the error — would convert a counter
				// failure into a 500 on a guess that was wrong anyway, which
				// tells the caller their code was wrong by a different route.
				f.log().Warn("counting a failed factor attempt failed", "error", failErr.Error())
			}
		}
		challenge.Attempts++
		// Replace rather than re-put: the TTL must not be extended by a wrong
		// answer, or an attacker refreshes the clock by using the thing it
		// bounds.
		if repErr := f.Challenges.Replace(ctx, handle, challenge); repErr != nil {
			f.log().Warn("recording a failed attempt failed", "error", repErr.Error())
		}
		if challenge.Spent() {
			_ = f.Challenges.Delete(ctx, handle)
			return nil, ErrChallengeSpent
		}
		return nil, ErrWrongCode

	default:
		// A failure to decide. NOT a wrong code, and not a pass: a factor
		// store that is unreachable has verified nothing.
		return nil, fmt.Errorf("mfa: verifying a factor: %w", err)
	}
}

// --- answering, as the login flow needs it ----------------------------------

// Outcome is what one answered attempt yields.
//
// It carries the identity fields so the caller never has to read them from
// anything the browser holds. That is the same property `Challenge` has and
// the reason it has it: a login completing on the strength of a user id the
// client supplied would be a login the client chose.
type Outcome struct {
	// Complete is true when the challenge is satisfied and a session may be
	// created.
	Complete bool

	// UserID, OrgID and PendingID are set whenever the challenge was READ —
	// including for a wrong code, so the failure can be audited against the
	// account it was aimed at without a second lookup.
	//
	// None of them reaches the browser on any path. They exist so that every
	// decision downstream is taken against server-side state rather than
	// against something a form could name.
	UserID    string
	OrgID     string
	PendingID string

	// Methods are the factor types proven, for the session's `auth_methods`.
	Methods []Type

	// Offered are the types the user may still answer with — set when the
	// attempt was wrong and the challenge is still live, so the page can be
	// re-rendered without the caller holding state of its own.
	Offered []Type
}

// AnswerType verifies an attempt against the challenge's factor OF ONE TYPE.
//
// The login form names a factor TYPE, never a factor id, and this is why: an
// id in a form is an id a caller can edit, and abuse case A-2 is somebody
// answering with an id belonging to another user. `Answer` refuses that by
// checking membership, which is a control that has to be right; naming a type
// instead means the request cannot express the attack in the first place.
//
// A user holds at most one TOTP factor (`ErrAlreadyEnrolled`), so a type
// resolves to one factor. When `P3-05` allows several passkeys, the first of
// that type the challenge named is used and the verifier decides — which is
// how WebAuthn works anyway, since the authenticator picks the credential.
//
// `pendingID` is the authorization request the caller believes it is
// finishing, and it is checked against the one the challenge was ISSUED for
// before anything is verified. Without it a challenge handle completes
// whichever request the form names — so somebody who obtained a handle could
// attach a legitimate second factor to an authorization they started
// themselves, which is the whole of `Challenge.PendingID`'s purpose and was
// the bug the end-to-end test found.
//
// Checked BEFORE the verifier, so a mismatched request does not spend the
// user's code: a correct answer refused after the fact would still have
// recorded its counter, and the user's next real code would be the one after
// a step they never used.
func (f *Framework) AnswerType(
	ctx context.Context, handle, pendingID string, t Type, code string,
) (Outcome, error) {
	challenge, err := f.Challenges.Get(ctx, handle)
	if err != nil {
		return Outcome{}, err
	}

	if challenge.PendingID != pendingID {
		// ErrNoChallenge, not a distinct error: for THIS login there is no
		// challenge, and saying anything more precise would confirm that the
		// handle names a real one somewhere else.
		return Outcome{}, ErrNoChallenge
	}

	// Identity from the challenge, carried on every return below — including
	// the failures, so a caller can audit against the right account without
	// asking a second question whose answer it would then have to trust.
	who := Outcome{
		UserID:    challenge.UserID,
		OrgID:     challenge.OrgID,
		PendingID: challenge.PendingID,
	}

	factorID, err := f.idOfType(ctx, challenge, t)
	if err != nil {
		return who, err
	}

	methods, err := f.Answer(ctx, handle, factorID, code)
	if err != nil {
		if errors.Is(err, ErrWrongCode) {
			// Still live. Re-read what may be offered rather than trusting the
			// copy taken before the attempt — a factor removed in between must
			// not be offered again.
			offered, offerErr := f.Peek(ctx, handle)
			if offerErr != nil {
				return who, err
			}
			who.Offered = offered
			return who, err
		}
		return who, err
	}

	who.Complete = true
	who.Methods = methods
	return who, nil
}

// Peek reports what a live challenge may be answered with, consuming nothing.
//
// For re-rendering the page — a refresh, or a wrong code. It reads the factors
// fresh rather than returning what the challenge recorded, so a factor removed
// mid-challenge stops being offered at once. The challenge's id list is still
// the authority on what may be ANSWERED; this only governs what is shown.
func (f *Framework) Peek(ctx context.Context, handle string) ([]Type, error) {
	challenge, err := f.Challenges.Get(ctx, handle)
	if err != nil {
		return nil, err
	}
	if challenge.Spent() {
		return nil, ErrChallengeSpent
	}

	factors, err := f.Store.Confirmed(ctx, challenge.OrgID, challenge.UserID)
	if err != nil {
		return nil, fmt.Errorf("mfa: reading enrolled factors: %w", err)
	}

	offered := make([]Type, 0, len(factors))
	seen := map[Type]bool{}
	for _, factor := range factors {
		if !factor.Active() || !contains(challenge.FactorIDs, factor.ID) {
			continue
		}
		if _, err := f.Registry.For(factor.Type); err != nil {
			continue
		}
		if !seen[factor.Type] {
			seen[factor.Type] = true
			offered = append(offered, factor.Type)
		}
	}
	return offered, nil
}

// idOfType resolves a type to one of the factors THIS challenge named.
func (f *Framework) idOfType(ctx context.Context, challenge Challenge, t Type) (string, error) {
	factors, err := f.Store.Confirmed(ctx, challenge.OrgID, challenge.UserID)
	if err != nil {
		return "", fmt.Errorf("mfa: reading enrolled factors: %w", err)
	}
	for _, factor := range factors {
		if factor.Type == t && factor.Active() && contains(challenge.FactorIDs, factor.ID) {
			return factor.ID, nil
		}
	}
	// A type the challenge has no factor for. Refused as ErrNoSuchFactor, the
	// same answer a guessed id gets, because the difference is a fact about
	// what this user has enrolled.
	return "", ErrNoSuchFactor
}

// factorType finds the type of one of a user's factors.
func (f *Framework) factorType(ctx context.Context, orgID, userID, factorID string) (Type, error) {
	factors, err := f.Store.Confirmed(ctx, orgID, userID)
	if err != nil {
		return "", fmt.Errorf("mfa: reading enrolled factors: %w", err)
	}
	for _, factor := range factors {
		if factor.ID == factorID {
			return factor.Type, nil
		}
	}
	// The challenge named it and the store does not have it: removed between
	// the challenge being issued and answered. Refused, because a factor that
	// no longer exists must not authenticate anybody.
	return "", ErrNoSuchFactor
}

// --- step-up ----------------------------------------------------------------

// StepUp reports whether a live session already satisfies a requirement.
//
// `docs/PLAN/08` § Least Privilege recommends step-up for sensitive
// administrative actions, and OIDC already has the vocabulary: `prompt=login`
// forces a fresh authentication, `acr_values` says what kind.
//
// The rule is a SUBSET test against what the session recorded, and it is
// deliberately not clever: a session satisfies a requirement when every method
// the requirement asks for is one the session actually used. A requirement for
// `hwk` is not satisfied by `otp`, which is abuse case A-4 — downgrade — and is
// the reason `Type.AMR` distinguishes them in the first place.
func StepUp(sessionMethods []string, required []string) (satisfied bool) {
	if len(required) == 0 {
		return true
	}

	have := make(map[string]bool, len(sessionMethods))
	for _, method := range sessionMethods {
		have[method] = true
	}
	for _, want := range required {
		if !have[want] {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

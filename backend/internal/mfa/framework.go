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
	Confirmed(ctx context.Context, userID string) ([]Factor, error)
}

// Framework decides and completes challenges.
type Framework struct {
	Registry   *Registry
	Store      EnrolledFactors
	Challenges ChallengeStore
	Log        *slog.Logger

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

	factors, err := f.Store.Confirmed(ctx, userID)
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

	factor, err := f.factorType(ctx, challenge.UserID, factorID)
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

// factorType finds the type of one of a user's factors.
func (f *Framework) factorType(ctx context.Context, userID, factorID string) (Type, error) {
	factors, err := f.Store.Confirmed(ctx, userID)
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

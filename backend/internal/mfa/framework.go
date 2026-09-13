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

// RecoveryCodes is the framework's view of a user's recovery codes.
//
// An interface for the same reason EnrolledFactors is one: the framework
// decides, the store reads. It is named for the question rather than the table.
type RecoveryCodes interface {
	// Unspent reports whether this user holds a code they could still use.
	Unspent(ctx context.Context, orgID, userID string) (bool, error)

	// Spend consumes one code and reports how many remain. It returns
	// ErrNoRecoveryCode for wrong, used, and another user's alike.
	Spend(ctx context.Context, orgID, userID, code string) (remaining int, err error)
}

// WebAuthnCeremony is the framework's view of a passkey ceremony (P3-05).
//
// Two methods, and the shape is forced by what WebAuthn is: a challenge the
// server issues and an assertion signed over it. That does not fit `Verifier`,
// whose `Verify(ctx, factorID, code)` has no way to reach the challenge this
// login issued — so WebAuthn plugs in here as well as there, rather than being
// bent into a shape that would have had to carry the challenge in the "code".
type WebAuthnCeremony interface {
	// Options builds a challenge for a user's registered credentials. It
	// returns nil options when the user holds none, so a caller can tell "no
	// passkey" from "a passkey and something went wrong".
	Options(ctx context.Context, orgID, userID string) (options, session []byte, err error)

	// Verify checks an assertion against the session that issued it and
	// reports which factor answered.
	Verify(ctx context.Context, orgID, userID string, session, assertion []byte) (factorID string, err error)
}

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

	// WebAuthn performs the passkey ceremony (P3-05). Nil means this build
	// offers no passkey, which is what every deployment before P3-05 was.
	WebAuthn WebAuthnCeremony

	// Recovery reads whether a user has unspent recovery codes, and spends
	// them (P3-04). Nil means recovery codes are not available in this build,
	// which is what every deployment before P3-04 was.
	Recovery RecoveryCodes

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

	// WebAuthnOptions is what the page hands `navigator.credentials.get`, when
	// the user has a passkey and this build can serve it (P3-05). Empty
	// otherwise, and the page renders no passkey form.
	WebAuthnOptions []byte

	// Recovery is true when the user holds at least one unspent recovery code
	// (P3-04).
	//
	// Read when the challenge is RAISED, not when the page is rendered, so the
	// page never offers a way in the user has no way to take. A dead option on
	// this page is somebody who cannot sign in reading that they can.
	Recovery bool
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
// answerable reports whether this build can challenge with a factor type.
//
// **A passkey is answered by the ceremony, not the registry.** The registry
// holds code verifiers — a code is typed and checked — and a WebAuthn assertion
// is a signed document with its own verifier, wired as Framework.WebAuthn. This
// used to ask only the registry, which in the running service never held a
// WebAuthn entry, so every passkey was skipped: a user whose only factor was a
// passkey signed in with a password alone (found in P3-10).
func (f *Framework) answerable(t Type) bool {
	if t == TypeWebAuthn {
		return f.WebAuthn != nil
	}
	_, err := f.Registry.For(t)
	return err == nil
}

func (f *Framework) Required(ctx context.Context, userID, orgID, pendingID string) (Decision, error) {
	if f.Registry.Empty() && f.WebAuthn == nil {
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
		if !f.answerable(factor.Type) {
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

	// The passkey ceremony, if this user has one and this build can serve it
	// (P3-05).
	//
	// Issued HERE rather than when the page renders, so the options and the
	// session that checks them are minted together and stored together. A
	// ceremony begun at render time would let a refresh mint fresh challenges
	// indefinitely.
	var options, ceremony []byte
	if f.WebAuthn != nil && seen[TypeWebAuthn] {
		options, ceremony, err = f.WebAuthn.Options(ctx, orgID, userID)
		if err != nil {
			// Refused, not degraded. Unlike the recovery-code lookup, which
			// answers "no" on failure because the user's factor still works,
			// this IS the user's factor — silently falling back to offering
			// nothing would lock out somebody whose only credential is a
			// passkey.
			return Decision{}, fmt.Errorf("mfa: beginning a passkey ceremony: %w", err)
		}
	}

	stored, err := f.Challenges.Put(ctx, Challenge{
		UserID:          userID,
		OrgID:           orgID,
		PendingID:       pendingID,
		FactorIDs:       ids,
		Methods:         nil,
		WebAuthnOptions: options,
		WebAuthnSession: ceremony,
		CreatedAt:       f.now(),
	}, ChallengeTTL)
	if err != nil {
		return Decision{}, fmt.Errorf("mfa: storing a challenge: %w", err)
	}
	if stored != "" {
		handle = stored
	}

	// Whether a recovery code could answer this challenge (P3-04).
	//
	// A store failure here does NOT fail the login: the challenge is already
	// stored and the user's factor still works, so the only consequence of
	// answering "no" is that a way in the user might not need is not offered.
	// Refusing the login instead would turn an outage in the recovery table
	// into an outage in sign-in for everybody who has a factor.
	recovery := false
	if f.Recovery != nil {
		available, err := f.Recovery.Unspent(ctx, orgID, userID)
		if err != nil {
			f.log().Warn("reading whether recovery codes are available failed",
				"error", err.Error())
		}
		recovery = available
	}

	return Decision{
		Challenge:       true,
		Handle:          handle,
		Offered:         offered,
		Recovery:        recovery,
		WebAuthnOptions: options,
	}, nil
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

	// WebAuthnOptions are re-offered alongside Offered when an attempt was
	// wrong and the challenge is still live, so the page re-renders with the
	// same ceremony rather than a new one.
	WebAuthnOptions []byte

	// RecoveryAvailable is whether a recovery code could still answer — set
	// alongside Offered when an attempt was wrong and the challenge is live, so
	// the page re-renders with the same options it had.
	RecoveryAvailable bool

	// Recovery is true when the challenge was completed with a recovery code
	// rather than a factor (P3-04).
	//
	// The caller needs it for two things that must not be guessed at: the audit
	// event is a different one, and the session's `amr` must not claim a
	// possession factor that was never presented.
	Recovery bool

	// Remaining is how many recovery codes are left, set when Recovery is true.
	// F-4's low-water warning is built on it.
	Remaining int
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
			offer, offerErr := f.Peek(ctx, handle)
			if offerErr != nil {
				return who, err
			}
			who.Offered = offer.Types
			who.RecoveryAvailable = offer.Recovery
			return who, err
		}
		return who, err
	}

	who.Complete = true
	who.Methods = methods
	return who, nil
}

// AnswerRecovery completes a challenge with a recovery code (P3-04).
//
// A sibling of AnswerType rather than a factor type of its own. A recovery code
// is not something the user HAS in the sense `user_mfa_factors` means — there is
// no device, nothing to enrol, and no `amr` value in RFC 8176 that names one —
// so modelling it as a factor would have put a row in that table which no
// verifier could serve and no `Type.AMR()` could name.
//
// What it shares with AnswerType is everything that makes either safe: the
// challenge is the authority on who, the pending request must match, and the
// per-user attempt bound is charged before any lookup.
func (f *Framework) AnswerRecovery(
	ctx context.Context, handle, pendingID, code string,
) (Outcome, error) {
	if f.Recovery == nil {
		// No recovery store in this build. Refused as a wrong code rather than
		// as an error: the caller offered the option because Decision said so,
		// and a build where those disagree is a wiring bug, not a user's
		// problem to read about.
		return Outcome{}, ErrNoRecoveryCode
	}

	challenge, err := f.Challenges.Get(ctx, handle)
	if err != nil {
		return Outcome{}, err
	}

	// The same binding AnswerType enforces, and for the same reason: a
	// challenge answered in one login must not complete a different one.
	if challenge.PendingID != pendingID {
		return Outcome{}, ErrNoChallenge
	}

	who := Outcome{
		UserID:    challenge.UserID,
		OrgID:     challenge.OrgID,
		PendingID: challenge.PendingID,
	}

	if challenge.Spent() {
		_ = f.Challenges.Delete(ctx, handle)
		return who, ErrChallengeSpent
	}

	// The per-user bound, before any store read (P3-03 step 2, card step 5).
	//
	// The SAME counter TOTP guesses are charged against, deliberately. Two
	// separate allowances would mean an attacker gets ten guesses at the code
	// and ten more at the recovery codes — which is not two bounds, it is one
	// bound twice as large, reached by choosing which form to guess in.
	if f.Attempts != nil {
		allowed, err := f.Attempts.Allowed(ctx, challenge.UserID, f.now())
		if err != nil {
			return who, fmt.Errorf("mfa: reading the attempt bound: %w", err)
		}
		if !allowed {
			return who, ErrTooManyAttempts
		}
	}

	remaining, err := f.Recovery.Spend(ctx, challenge.OrgID, challenge.UserID, code)
	switch {
	case err == nil:
		// Consumed, so the challenge cannot be answered twice.
		if delErr := f.Challenges.Delete(ctx, handle); delErr != nil {
			f.log().Warn("deleting a completed challenge failed", "error", delErr.Error())
		}
		who.Complete = true
		who.Recovery = true
		who.Remaining = remaining
		return who, nil

	case errors.Is(err, ErrNoRecoveryCode):
		if f.Attempts != nil {
			if _, failErr := f.Attempts.Fail(ctx, challenge.UserID, f.now()); failErr != nil {
				f.log().Warn("counting a failed recovery attempt failed", "error", failErr.Error())
			}
		}

		challenge.Attempts++
		if repErr := f.Challenges.Replace(ctx, handle, challenge); repErr != nil {
			f.log().Warn("recording a failed attempt failed", "error", repErr.Error())
		}
		if challenge.Spent() {
			_ = f.Challenges.Delete(ctx, handle)
			return who, ErrChallengeSpent
		}

		offer, offerErr := f.Peek(ctx, handle)
		if offerErr == nil {
			who.Offered = offer.Types
			who.RecoveryAvailable = offer.Recovery
		}
		return who, ErrNoRecoveryCode

	default:
		// A failure to decide. Not a wrong code, and not a pass.
		return who, fmt.Errorf("mfa: spending a recovery code: %w", err)
	}
}

// Offer is everything a live challenge may be answered with.
//
// One value rather than two calls, because the page renders both together and a
// second round trip to the same challenge could see a different answer — a
// factor removed between the two reads would produce a page offering nothing
// while claiming a challenge is live.
type Offer struct {
	// Types are the factor kinds this build can challenge with and this user
	// holds.
	Types []Type

	// WebAuthnOptions are the ones this challenge already issued, so a
	// re-render hands the browser the same ceremony rather than a new one.
	WebAuthnOptions []byte

	// Recovery is whether an unspent recovery code could answer instead
	// (P3-04).
	Recovery bool
}

// Empty reports whether there is no way at all to answer.
//
// The caller turns this into the "start again" page rather than a form whose
// every answer is refused — a dead end that says so beats one that does not.
func (o Offer) Empty() bool { return len(o.Types) == 0 && !o.Recovery }

// Passkey reports whether a passkey form should render.
//
// Both halves matter: a user may hold a WebAuthn factor while this build has no
// ceremony to serve it — after a rollback, say — and a form with no options is
// one whose button does nothing.
func (o Offer) Passkey() bool {
	if len(o.WebAuthnOptions) == 0 {
		return false
	}
	for _, t := range o.Types {
		if t == TypeWebAuthn {
			return true
		}
	}
	return false
}

// AnswerWebAuthn completes a challenge with a signed assertion (P3-05).
//
// The third sibling of AnswerType and AnswerRecovery, and it shares every
// control that makes those safe: the challenge decides who, the pending request
// must match, the per-user bound is charged before any verification work, and a
// spent challenge stays spent.
//
// What it adds is the reason WebAuthn exists — the assertion is checked against
// **the session this login issued**, which names the origin and the challenge
// the authenticator had to sign over. A relayed assertion from a lookalike page
// carries that page's origin and fails here.
func (f *Framework) AnswerWebAuthn(
	ctx context.Context, handle, pendingID string, assertion []byte,
) (Outcome, error) {
	if f.WebAuthn == nil {
		return Outcome{}, ErrUnsupported
	}

	challenge, err := f.Challenges.Get(ctx, handle)
	if err != nil {
		return Outcome{}, err
	}
	if challenge.PendingID != pendingID {
		return Outcome{}, ErrNoChallenge
	}

	who := Outcome{
		UserID:    challenge.UserID,
		OrgID:     challenge.OrgID,
		PendingID: challenge.PendingID,
	}

	if challenge.Spent() {
		_ = f.Challenges.Delete(ctx, handle)
		return who, ErrChallengeSpent
	}

	if len(challenge.WebAuthnSession) == 0 {
		// No ceremony was issued for this login, so there is nothing an
		// assertion could be checked against. Refused rather than verified
		// against a session built here: a check whose expected value comes from
		// the same request as the answer is not a check.
		return who, ErrNoSuchFactor
	}

	if f.Attempts != nil {
		allowed, err := f.Attempts.Allowed(ctx, challenge.UserID, f.now())
		if err != nil {
			return who, fmt.Errorf("mfa: reading the attempt bound: %w", err)
		}
		if !allowed {
			return who, ErrTooManyAttempts
		}
	}

	factorID, err := f.WebAuthn.Verify(
		ctx, challenge.OrgID, challenge.UserID, challenge.WebAuthnSession, assertion)

	switch {
	case err == nil:
		if delErr := f.Challenges.Delete(ctx, handle); delErr != nil {
			f.log().Warn("deleting a completed challenge failed", "error", delErr.Error())
		}
		_ = factorID
		who.Complete = true
		who.Methods = append(challenge.Methods, TypeWebAuthn)
		return who, nil

	case errors.Is(err, ErrWrongCode), errors.Is(err, ErrOriginMismatch),
		errors.Is(err, ErrClonedAuthenticator):
		// All three are refusals and all three count against the bound. They
		// are distinguishable to an OPERATOR — an origin mismatch is a phishing
		// attempt or a misconfiguration, a counter regression is a possible
		// clone — and identical to the browser, because telling a caller which
		// of their attacks was detected helps only them.
		if f.Attempts != nil {
			if _, failErr := f.Attempts.Fail(ctx, challenge.UserID, f.now()); failErr != nil {
				f.log().Warn("counting a failed passkey attempt failed", "error", failErr.Error())
			}
		}

		challenge.Attempts++
		if repErr := f.Challenges.Replace(ctx, handle, challenge); repErr != nil {
			f.log().Warn("recording a failed attempt failed", "error", repErr.Error())
		}
		if challenge.Spent() {
			_ = f.Challenges.Delete(ctx, handle)
			return who, ErrChallengeSpent
		}

		offer, offerErr := f.Peek(ctx, handle)
		if offerErr == nil {
			who.Offered = offer.Types
			who.RecoveryAvailable = offer.Recovery
			who.WebAuthnOptions = offer.WebAuthnOptions
		}
		return who, err

	default:
		// A failure to decide. Not a wrong answer, and not a pass.
		return who, fmt.Errorf("mfa: verifying a passkey: %w", err)
	}
}

// Peek reports what a live challenge may be answered with, consuming nothing.
//
// For re-rendering the page — a refresh, or a wrong code. It reads the factors
// fresh rather than returning what the challenge recorded, so a factor removed
// mid-challenge stops being offered at once. The challenge's id list is still
// the authority on what may be ANSWERED; this only governs what is shown.
func (f *Framework) Peek(ctx context.Context, handle string) (Offer, error) {
	challenge, err := f.Challenges.Get(ctx, handle)
	if err != nil {
		return Offer{}, err
	}
	if challenge.Spent() {
		return Offer{}, ErrChallengeSpent
	}

	factors, err := f.Store.Confirmed(ctx, challenge.OrgID, challenge.UserID)
	if err != nil {
		return Offer{}, fmt.Errorf("mfa: reading enrolled factors: %w", err)
	}

	offered := make([]Type, 0, len(factors))
	seen := map[Type]bool{}
	for _, factor := range factors {
		if !factor.Active() || !contains(challenge.FactorIDs, factor.ID) {
			continue
		}
		if !f.answerable(factor.Type) {
			continue
		}
		if !seen[factor.Type] {
			seen[factor.Type] = true
			offered = append(offered, factor.Type)
		}
	}

	// Recovery availability, read fresh for the same reason the factors are: a
	// batch regenerated or cleared mid-challenge must stop being offered at
	// once. A store failure answers "no" rather than failing the render — the
	// user's factor still works, and the cost of being wrong here is one option
	// not shown.
	recovery := false
	if f.Recovery != nil {
		available, err := f.Recovery.Unspent(ctx, challenge.OrgID, challenge.UserID)
		if err != nil {
			f.log().Warn("reading whether recovery codes are available failed",
				"error", err.Error())
		}
		recovery = available
	}

	return Offer{Types: offered, Recovery: recovery, WebAuthnOptions: challenge.WebAuthnOptions}, nil
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

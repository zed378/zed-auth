// Package mfa is the frame both factor types plug into (P3-01).
//
// It contains no factor. TOTP arrives in `P3-02` and WebAuthn in `P3-05`, and
// both are implementations of the interface below rather than parallel
// systems — because two systems would mean two challenge steps, two rate
// limits, two audit shapes, and two chances to get the partially-authenticated
// state wrong.
//
// Specification: MEMORY/specs/P3-01-mfa-framework.md.
package mfa

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Status is an enrolment's state. The values are the ones
// `user_mfa_factors.status` allows.
type Status string

const (
	// StatusPending is enrolled and not yet proven. It is not a factor.
	StatusPending Status = "pending"

	// StatusActive is proven and answerable.
	StatusActive Status = "active"
)

// Active reports whether this factor may be challenged with.
func (f Factor) Active() bool { return f.Status == StatusActive }

// Type names a factor kind. The values are the ones
// `user_mfa_factors.type` allows.
type Type string

const (
	// TypeTOTP is an authenticator app's shared secret (P3-02).
	TypeTOTP Type = "totp"

	// TypeWebAuthn is a passkey or security key (P3-05).
	TypeWebAuthn Type = "webauthn"
)

// AMR is the RFC 8176 authentication-method value a factor contributes.
//
// Not the same as Type, and the distinction is the whole point of `amr`. A
// consumer that refuses a payment unless `amr` contains `hwk` is asking for a
// hardware-backed credential, and must not be satisfied by an authenticator
// app — so the mapping is stated here once rather than left to each factor.
func (t Type) AMR() string {
	switch t {
	case TypeTOTP:
		return "otp"
	case TypeWebAuthn:
		return "hwk"
	default:
		return ""
	}
}

// Valid reports whether this is a factor type the service implements.
//
// An unimplemented type is refused rather than stored, for the reason
// `allowed_login_methods` refuses `passkey` today: a user who enrolled one
// would hold a factor that can never be challenged, and an organization that
// required it would have locked everybody out with a value nothing reads.
func (t Type) Valid() bool { return t.AMR() != "" }

// Factor is one enrolled credential, as the framework sees it.
//
// Deliberately without the secret. Nothing outside the factor implementation
// that owns a secret ever holds one, so no read path can return it by
// accident — the same separation `session.Session` uses to keep a token hash
// out of the type system (`PG-14`).
type Factor struct {
	ID     string
	UserID string
	OrgID  string
	Type   Type
	Label  string

	// Status is `pending` until the user has proven they can use the factor,
	// then `active` (`docs/PLAN/04` § user_mfa_factors).
	//
	// A pending factor is invisible to the challenge, does not satisfy
	// `mfa_required`, and does not count as the last factor for the purposes
	// of refusing a removal. Enrolment happens when the user proves the
	// factor works, not when a secret is generated.
	Status Status

	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// Errors the framework distinguishes.
var (
	// ErrNoSuchFactor is a factor id that does not belong to this user, or
	// does not exist. One error for both, because telling them apart tells a
	// caller whose factor id they guessed.
	ErrNoSuchFactor = errors.New("mfa: no such factor")

	// ErrWrongCode is a failed verification. It is NOT uniform with the login
	// page's refusal, deliberately — see Verifier.
	ErrWrongCode = errors.New("mfa: the code is not correct")

	// ErrUnsupported is a factor type this build does not implement.
	ErrUnsupported = errors.New("mfa: unsupported factor type")

	// ErrAlreadyEnrolled is a second TOTP secret for one user. A second one is
	// not a second device — it is an older enrolment nobody removed, which is
	// a live credential the user has forgotten about.
	ErrAlreadyEnrolled = errors.New("mfa: this factor type is already enrolled")
)

// Enrolment is what a factor hands back when enrolment begins.
//
// `Secret` is the one-time display: it exists in this struct, crosses to the
// user once, and is never readable again — the same discipline as a client
// secret (`P1-18`). For a factor type that has no user-visible secret it is
// empty and `Challenge` carries whatever the browser needs instead.
type Enrolment struct {
	FactorID string

	// Secret is shown to the user exactly once. Never logged, never audited,
	// never returned by a read path.
	Secret string

	// Challenge is per-type material the client needs to complete enrolment —
	// a WebAuthn creation options blob, for instance.
	Challenge map[string]any
}

// Verifier is what a factor type implements.
//
// Four operations, and the reason there are exactly four: enrolment begins,
// enrolment is confirmed by proving the factor works, a live factor is verified
// during a challenge, and a factor is removed. Anything a factor type needs
// beyond these belongs inside its own implementation, not in this interface —
// an interface that grows a method per factor is two systems wearing one name.
type Verifier interface {
	// Type is the kind this verifier handles. The registry keys on it.
	Type() Type

	// Begin starts an enrolment and returns what the user needs to complete
	// it. It creates an UNCONFIRMED factor: nothing about the account's
	// security has changed yet.
	Begin(ctx context.Context, userID, orgID, label string) (Enrolment, error)

	// Confirm completes an enrolment by checking the user can actually use
	// the factor. Only after this does the factor count for anything.
	Confirm(ctx context.Context, factorID, code string) error

	// Verify checks a code against a confirmed factor during a challenge.
	//
	// It returns ErrWrongCode for a wrong answer and a wrapped error for a
	// failure to decide. The caller must treat the second as a refusal — a
	// factor store that is unreachable has not verified anything, and
	// treating "could not check" as "checked and it was fine" is the one
	// mistake here that is not recoverable.
	Verify(ctx context.Context, factorID, code string) error

	// Remove deletes a factor. The framework decides whether removal is
	// allowed; this performs it.
	Remove(ctx context.Context, factorID string) error
}

// Registry is the set of factor types this build implements.
//
// Empty until `P3-02` registers TOTP, and that is the deployment state this
// task ships in: the framework is inert, the challenge step never triggers, and
// nothing about an existing login changes.
type Registry struct {
	verifiers map[Type]Verifier
}

func NewRegistry(verifiers ...Verifier) *Registry {
	r := &Registry{verifiers: make(map[Type]Verifier, len(verifiers))}
	for _, v := range verifiers {
		r.verifiers[v.Type()] = v
	}
	return r
}

// For returns the verifier for a type, or ErrUnsupported.
func (r *Registry) For(t Type) (Verifier, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, t)
	}
	v, ok := r.verifiers[t]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, t)
	}
	return v, nil
}

// Implemented lists the factor types this build can actually challenge with.
//
// Sorted, so a caller rendering it does not get a different order per request
// — map iteration order is random in Go and a list that reshuffles on every
// page load looks broken.
func (r *Registry) Implemented() []Type {
	if r == nil {
		return nil
	}
	out := make([]Type, 0, len(r.verifiers))
	for t := range r.verifiers {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Empty reports whether this build implements no factors at all.
//
// The login flow reads it to skip the challenge step entirely. Not an
// optimisation: a challenge offering nothing is a dead end a user cannot get
// out of, and it would be reachable on every deployment until `P3-02` lands.
func (r *Registry) Empty() bool { return r == nil || len(r.verifiers) == 0 }

// --- what the session records -----------------------------------------------

// MethodPassword is the `amr` value for a password. RFC 8176's `pwd`.
const MethodPassword = "pwd"

// MethodMultiFactor is RFC 8176's `mfa`.
//
// **Emitted only when two distinct factor CATEGORIES were used**, never as a
// synonym for "a second factor existed". A consumer reading `mfa` is asking
// whether this authentication used more than one kind of thing, and answering
// yes because the user happens to have a factor enrolled is the failure this
// whole task exists to prevent.
const MethodMultiFactor = "mfa"

// AuthMethods builds the `amr` list for an authentication.
//
// From what was USED, never from what is enrolled. The distinction is the
// task's goal sentence: a consumer refusing a payment unless `amr` contains
// `otp` is trusting this service to have actually challenged.
//
// Sorted and deduplicated, so the claim is stable across requests — a consumer
// comparing two tokens' `amr` should not see a difference that is only
// ordering.
func AuthMethods(password bool, used ...Type) []string {
	seen := map[string]bool{}
	var out []string

	add := func(value string) {
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}

	if password {
		add(MethodPassword)
	}
	for _, t := range used {
		add(t.AMR())
	}

	// `mfa` when more than one category was used. Counting CATEGORIES rather
	// than factors: two passkeys are two credentials of one kind, and a
	// consumer asking for `mfa` is asking whether something other than one
	// kind of secret was involved.
	if len(out) > 1 {
		add(MethodMultiFactor)
	}

	sort.Strings(out)
	return out
}

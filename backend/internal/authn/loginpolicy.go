package authn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// LoginPolicy is the part of an organization's settings that governs signing
// in, as opposed to the part that governs passwords (P2-10).
//
// `docs/PLAN/17`'s Phase 2 criterion is precise about the distinction that
// matters: settings must be **enforced at login time, not merely stored**.
// `password_policy` already was (`P1-02`). These two were not — every
// organization got the handler's single session lifetime whatever its settings
// said, and `allowed_login_methods` was validated on write and read by nothing.
type LoginPolicy struct {
	// SessionLifetimeHours is how long a session may live.
	SessionLifetimeHours int

	// MFARequired is the organization's mandate (P3-07).
	//
	// `P2-10` stored this and deliberately did not enforce it, because MFA did
	// not exist. It does now, so this is read at the password step and decides
	// whether a user without a factor is routed into enrolment.
	MFARequired bool

	// MFARequiredSince is when the mandate was turned on, and it is what the
	// grace period is measured from.
	//
	// **From activation, not from the user's last login.** Measuring per user
	// would let somebody who never signs in sit outside the policy forever,
	// and would make the deadline unanswerable for an administrator asking
	// "when does this take effect".
	//
	// Zero when the mandate is off, or when it predates this field — an
	// organization that had it on before `P3-07` shipped gets the grace
	// measured from its first login after the upgrade rather than a deadline
	// already in the past.
	MFARequiredSince time.Time

	// AllowedMethods are the ways a user may authenticate.
	//
	// Only `password` exists today. The list is enforced anyway, because the
	// point of the setting is that a method not on it is refused **even if it
	// is implemented** — and the moment Phase 3 adds passkeys, an organization
	// that excluded them must not acquire them by upgrade.
	AllowedMethods []string
}

// MethodPassword is the only method this phase implements.
const MethodPassword = "password"

// DefaultLoginPolicy matches P0-07's column defaults.
//
// Twelve hours, and password-only. The default is what an organization gets
// when its settings say nothing, so it is deliberately the same as the schema's
// own default rather than something this code invents — two defaults that drift
// is a policy nobody can predict.
var DefaultLoginPolicy = LoginPolicy{
	SessionLifetimeHours: 12,
	AllowedMethods:       []string{MethodPassword},
}

// Allows reports whether a method may be used.
//
// An EMPTY list allows nothing, which is the safe reading and the one that
// matches what an administrator who cleared the field meant. An ABSENT list is
// a different thing and never reaches here: `ParseLoginPolicy` substitutes the
// default for it, because "the setting is not configured" and "the setting is
// configured to nothing" are different intentions.
func (p LoginPolicy) Allows(method string) bool {
	return slices.Contains(p.AllowedMethods, method)
}

// ParseLoginPolicy reads the login half of a settings document.
//
// Never fails. A settings document that cannot be read yields the secure
// default with every substitution reported, for the same reason `ParsePolicy`
// does: a login that refuses to happen because a JSON field is malformed is an
// outage caused by a typo in a form.
func ParseLoginPolicy(settings []byte) (LoginPolicy, []Adjustment) {
	policy := DefaultLoginPolicy
	var adjustments []Adjustment

	if len(settings) == 0 {
		return policy, nil
	}

	var doc struct {
		SessionLifetimeHours *int       `json:"session_lifetime_hours"`
		AllowedLoginMethods  *[]string  `json:"allowed_login_methods"`
		MFARequired          *bool      `json:"mfa_required"`
		MFARequiredSince     *time.Time `json:"mfa_required_since"`
	}
	if err := json.Unmarshal(settings, &doc); err != nil {
		return policy, []Adjustment{{
			Field: "settings", Configured: "unreadable", Applied: "defaults",
			Reason: "the settings document could not be parsed",
		}}
	}

	if doc.MFARequired != nil {
		policy.MFARequired = *doc.MFARequired
	}
	if doc.MFARequiredSince != nil {
		policy.MFARequiredSince = *doc.MFARequiredSince
	}

	if doc.SessionLifetimeHours != nil {
		hours := *doc.SessionLifetimeHours
		switch {
		case hours < MinSessionLifetimeHours:
			adjustments = append(adjustments, Adjustment{
				Field: "session_lifetime_hours", Configured: strconv.Itoa(hours),
				Applied: strconv.Itoa(MinSessionLifetimeHours),
				Reason:  "below the floor",
			})
			policy.SessionLifetimeHours = MinSessionLifetimeHours
		case hours > MaxSessionLifetimeHours:
			adjustments = append(adjustments, Adjustment{
				Field: "session_lifetime_hours", Configured: strconv.Itoa(hours),
				Applied: strconv.Itoa(MaxSessionLifetimeHours),
				Reason:  "above the ceiling",
			})
			policy.SessionLifetimeHours = MaxSessionLifetimeHours
		default:
			policy.SessionLifetimeHours = hours
		}
	}

	if doc.AllowedLoginMethods != nil {
		methods := *doc.AllowedLoginMethods
		known := make([]string, 0, len(methods))
		for _, m := range methods {
			// A method this service does not implement is dropped rather than
			// kept. Keeping it would make the policy look like it permits
			// something, and the first person to notice would be whoever tried
			// to use it.
			if m == MethodPassword {
				known = append(known, m)
				continue
			}
			adjustments = append(adjustments, Adjustment{
				Field: "allowed_login_methods", Configured: m, Applied: "ignored",
				Reason: "this service does not implement that method yet",
			})
		}
		policy.AllowedMethods = known
	}

	return policy, adjustments
}

// MFAGracePeriod is how long a user has to enrol after the mandate is enabled.
//
// **Fourteen days**, and the number is a judgement rather than a fact, so here
// is the judgement: two working weeks means somebody on a one-week holiday
// comes back to a warning rather than a lockout, and it is short enough that an
// administrator who enables it sees it take effect inside a sprint.
//
// The grace exists because the alternative is a hard cutover, which locks out
// everybody without a factor the moment the switch flips. That produces a
// support queue rather than security — and the fastest way out of a support
// queue is to turn the setting off again, which leaves the organization less
// safe than before anybody tried.
//
// It is a real window in which the policy is not enforced. That is the cost,
// and it is bounded and visible rather than indefinite.
const MFAGracePeriod = 14 * 24 * time.Hour

// MFAOutcome is what the mandate says about one login.
type MFAOutcome int

const (
	// MFANotRequired: no mandate, or the user already holds a factor.
	MFANotRequired MFAOutcome = iota

	// MFAInGrace: the mandate applies, the user has no factor, and the
	// deadline has not passed. They are signed in AND told.
	MFAInGrace

	// MFAEnrolmentRequired: the mandate applies, the user has no factor, and
	// the grace has run out. No session until they enrol.
	MFAEnrolmentRequired
)

// RequireMFA decides what the mandate means for one login.
//
// Pure, so the truth table can be tested without a database, a clock or a
// request — the same split `Evaluate` makes for the password policy. Three
// inputs and three outcomes, and every combination is enumerated in the tests
// rather than described here.
//
// `since` being zero with the mandate ON is the upgrade case: an organization
// that had `mfa_required` set before this field existed. Their grace starts
// now rather than having expired in the past, because a deadline nobody could
// have known about is not a deadline.
func RequireMFA(policy LoginPolicy, hasFactor bool, now time.Time) MFAOutcome {
	if !policy.MFARequired || hasFactor {
		return MFANotRequired
	}
	if policy.MFARequiredSince.IsZero() {
		return MFAInGrace
	}
	if now.Before(policy.MFARequiredSince.Add(MFAGracePeriod)) {
		return MFAInGrace
	}
	return MFAEnrolmentRequired
}

// MFADeadline is when the grace runs out, for a page that must show it.
//
// Zero when there is no deadline to show.
func MFADeadline(policy LoginPolicy) time.Time {
	if !policy.MFARequired || policy.MFARequiredSince.IsZero() {
		return time.Time{}
	}
	return policy.MFARequiredSince.Add(MFAGracePeriod)
}

// Bounds on a session lifetime, matching internal/session's own.
//
// Duplicated as hour counts rather than imported, because importing
// `internal/session` here would be a cycle: sessions already depend on this
// package's password policy. The two are pinned together by a test.
const (
	MinSessionLifetimeHours = 1
	MaxSessionLifetimeHours = 720
)

// LoginPolicy reads the login policy for one organization, inside an existing
// transaction — the same contract as Policy, and for the same reason: reading
// it separately would create a window where the policy checked and the policy
// in force are different rows.
func (s *PolicyStore) LoginPolicy(ctx context.Context, tx *postgres.Tx, orgID string) (LoginPolicy, error) {
	var settings []byte

	err := tx.QueryRow(ctx,
		`SELECT settings FROM organizations WHERE id = $1`, orgID,
	).Scan(&settings)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return DefaultLoginPolicy, fmt.Errorf("%w: %s", ErrOrganizationNotFound, orgID)
	case err != nil:
		return DefaultLoginPolicy, fmt.Errorf("authn: reading login policy: %w", err)
	}

	policy, adjustments := ParseLoginPolicy(settings)
	s.warn(orgID, adjustments)
	return policy, nil
}

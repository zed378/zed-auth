package authn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

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
		SessionLifetimeHours *int      `json:"session_lifetime_hours"`
		AllowedLoginMethods  *[]string `json:"allowed_login_methods"`
	}
	if err := json.Unmarshal(settings, &doc); err != nil {
		return policy, []Adjustment{{
			Field: "settings", Configured: "unreadable", Applied: "defaults",
			Reason: "the settings document could not be parsed",
		}}
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

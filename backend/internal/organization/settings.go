// Package organization implements the Management API's organization endpoints
// (P1-16).
//
// An organization is the tenant boundary (docs/PLAN/04), so this is the first
// place P1-15's machinery — permissions, scoping, idempotency, quotas, audit —
// is exercised against a real resource rather than a test handler.
//
// Specification: MEMORY/specs/P1-16-organizations.md.
package organization

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/management"
)

// Validating `settings` (docs/PLAN/08 Part B § Policies per Organization).
//
// The rule that matters is not any individual range: it is that **an unknown
// key is refused rather than stored**. Storing one silently is how a typo —
// `mfa_requried` — becomes a policy that is not in force and looks like it is.
// Nobody notices until the audit that was supposed to find it does not.

// Settings is an organization's policy.
//
// Every field is a pointer so that a PATCH can distinguish "not mentioned"
// from "set to the zero value". `mfa_required: false` and an absent
// `mfa_required` are different intentions, and a plain bool cannot tell them
// apart — which is the classic way a partial update silently turns a control
// off.
type Settings struct {
	PasswordPolicy       *PasswordPolicy `json:"password_policy,omitempty"`
	MFARequired          *bool           `json:"mfa_required,omitempty"`
	SessionLifetimeHours *int            `json:"session_lifetime_hours,omitempty"`
	AllowedLoginMethods  *[]string       `json:"allowed_login_methods,omitempty"`
}

type PasswordPolicy struct {
	MinLength        *int  `json:"min_length,omitempty"`
	RequireUppercase *bool `json:"require_uppercase,omitempty"`
	MaxAgeDays       *int  `json:"max_age_days,omitempty"`
}

// The bounds.
const (
	// MinPasswordLengthFloor is the platform's own minimum, which a
	// per-organization policy may raise and never lower.
	//
	// A tenant policy able to go below the instance floor is a per-tenant way
	// to disable a platform control, which is not a setting — it is a hole
	// with a form field in front of it. docs/PLAN/09 § Passwords sets 12.
	MinPasswordLengthFloor = 12
	MaxPasswordLength      = 128

	// MaxPasswordAgeDays bounds rotation. Zero means "never expires", which is
	// a real choice and increasingly the recommended one (NIST SP 800-63B
	// argues forced rotation makes passwords worse), so it is representable
	// rather than approximated by a very large number.
	MaxPasswordAgeDays = 3650

	MinSessionLifetimeHours = 1
	MaxSessionLifetimeHours = 720 // thirty days
)

// LoginMethods are the values docs/PLAN/08 Part B names.
//
// Only `password` is implemented in Phase 1, and only `password` is accepted.
// An API that takes "passkey" is describing a capability that does not exist,
// which is exactly what docs/UI-UX/21's governance rule forbids — and a tenant
// that configured it would have silently disabled every login method that
// works.
var (
	implementedLoginMethods = []string{"password"}
	plannedLoginMethods     = []string{"passkey", "social"}
)

// ValidateSettings checks a settings document and returns it normalised.
//
// `raw` is the JSON exactly as it arrived, because the unknown-key check needs
// the keys and a decoded struct has already thrown them away.
func ValidateSettings(raw json.RawMessage) (Settings, error) {
	var details []api.ErrorDetail

	if len(raw) == 0 {
		return Settings{}, nil
	}

	// Unknown keys first, at both levels. Reported before the value checks, so
	// a caller who misspelled a key is told that rather than being told the
	// key they DID spell correctly is fine.
	details = append(details, unknownKeys(raw)...)

	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, management.Fault{
			Class:   management.Invalid,
			Message: "The settings document could not be read.",
			Details: []api.ErrorDetail{{Field: "settings", Issue: "must be a JSON object"}},
			Reason:  "settings did not decode: " + err.Error(),
		}
	}

	details = append(details, validateValues(s)...)

	if len(details) > 0 {
		return Settings{}, management.Fault{
			Class:   management.Invalid,
			Message: "One or more settings are not valid.",
			Details: details,
			Reason:  fmt.Sprintf("%d invalid settings", len(details)),
		}
	}
	return s, nil
}

// unknownKeys reports every key that is not part of the schema.
//
// Walked explicitly rather than with DisallowUnknownFields, because that stops
// at the FIRST unknown key and returns an error whose text names it in prose.
// A caller fixing three typos should learn about three, and each should arrive
// as a field the console can highlight (docs/UI-UX/15).
func unknownKeys(raw json.RawMessage) []api.ErrorDetail {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		// Not an object. The decode below reports it; nothing to add here.
		return nil
	}

	known := map[string]bool{
		"password_policy": true, "mfa_required": true,
		"session_lifetime_hours": true, "allowed_login_methods": true,
	}
	knownPolicy := map[string]bool{
		"min_length": true, "require_uppercase": true, "max_age_days": true,
	}

	var details []api.ErrorDetail
	for _, key := range sortedKeys(top) {
		if !known[key] {
			details = append(details, detail("settings."+key, "is not a known setting"))
		}
	}

	if policy, ok := top["password_policy"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(policy, &nested); err == nil {
			for _, key := range sortedKeys(nested) {
				if !knownPolicy[key] {
					details = append(details,
						detail("settings.password_policy."+key, "is not a known setting"))
				}
			}
		}
	}
	return details
}

func validateValues(s Settings) []api.ErrorDetail {
	var details []api.ErrorDetail

	if p := s.PasswordPolicy; p != nil {
		if p.MinLength != nil {
			switch {
			case *p.MinLength < MinPasswordLengthFloor:
				// Named against the floor rather than a generic range, because
				// "must be at least 12" tells an administrator the platform has
				// a minimum, which is the fact they need.
				details = append(details, detail("settings.password_policy.min_length",
					fmt.Sprintf("must be at least %d, the platform minimum", MinPasswordLengthFloor)))
			case *p.MinLength > MaxPasswordLength:
				details = append(details, detail("settings.password_policy.min_length",
					fmt.Sprintf("must be at most %d", MaxPasswordLength)))
			}
		}
		if p.MaxAgeDays != nil && (*p.MaxAgeDays < 0 || *p.MaxAgeDays > MaxPasswordAgeDays) {
			details = append(details, detail("settings.password_policy.max_age_days",
				fmt.Sprintf("must be 0 (never expires) or between 1 and %d", MaxPasswordAgeDays)))
		}
	}

	if h := s.SessionLifetimeHours; h != nil && (*h < MinSessionLifetimeHours || *h > MaxSessionLifetimeHours) {
		details = append(details, detail("settings.session_lifetime_hours",
			fmt.Sprintf("must be between %d and %d", MinSessionLifetimeHours, MaxSessionLifetimeHours)))
	}

	if m := s.AllowedLoginMethods; m != nil {
		switch {
		case len(*m) == 0:
			// An empty list is not "no restriction" — it is a tenant nobody can
			// log in to, applied by a caller who almost certainly meant the
			// opposite.
			details = append(details, detail("settings.allowed_login_methods",
				"must name at least one login method"))
		default:
			for _, method := range *m {
				switch {
				case slices.Contains(implementedLoginMethods, method):
				case slices.Contains(plannedLoginMethods, method):
					details = append(details, detail("settings.allowed_login_methods",
						fmt.Sprintf("%q is planned but not yet available", method)))
				default:
					details = append(details, detail("settings.allowed_login_methods",
						fmt.Sprintf("%q is not a login method", method)))
				}
			}
		}
	}

	return details
}

func detail(field, issue string) api.ErrorDetail {
	return api.ErrorDetail{Field: field, Issue: issue}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

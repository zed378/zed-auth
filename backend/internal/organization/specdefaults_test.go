package organization

import (
	"slices"
	"strconv"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/authn"
)

// The contract's published defaults are the ones the service applies (P2-14).
//
// There are three copies of these numbers and there is no getting to one:
//
//   - `organizations.settings`'s column DEFAULT, which protects a row created
//     without settings;
//   - `authn.DefaultPolicy` / `authn.DefaultLoginPolicy`, which protect a row
//     whose settings were edited into an unusable state;
//   - `openapi/openapi.yaml`, which is what the console and every consumer
//     read to say what happens when a setting is not configured.
//
// The first two already knew about each other — `authn/policy.go` says so in a
// comment. The third did not exist until `P2-14` needed it for the Policies
// screen, and a published default that quietly stops matching the enforced one
// is a lie told to every consumer at once rather than a bug in any of them.
//
// `SpecDefaults` is generated from the contract by
// `console/scripts/gen-patterns.mjs`, so this compares the contract to the
// service rather than a hand-copied number to another hand-copied number.
func TestSpecDefaultsMatchTheService(t *testing.T) {
	if SpecDefaults.MinLength != authn.DefaultPolicy.MinLength {
		t.Errorf("the contract publishes min_length %d; the service applies %d",
			SpecDefaults.MinLength, authn.DefaultPolicy.MinLength)
	}
	if SpecDefaults.RequireUppercase != authn.DefaultPolicy.RequireUppercase {
		t.Errorf("the contract publishes require_uppercase %v; the service applies %v",
			SpecDefaults.RequireUppercase, authn.DefaultPolicy.RequireUppercase)
	}
	if SpecDefaults.MaxAgeDays != authn.DefaultPolicy.MaxAgeDays {
		t.Errorf("the contract publishes max_age_days %d; the service applies %d",
			SpecDefaults.MaxAgeDays, authn.DefaultPolicy.MaxAgeDays)
	}
	if SpecDefaults.SessionLifetimeHours != authn.DefaultLoginPolicy.SessionLifetimeHours {
		t.Errorf("the contract publishes session_lifetime_hours %d; the service applies %d",
			SpecDefaults.SessionLifetimeHours, authn.DefaultLoginPolicy.SessionLifetimeHours)
	}
	if !slices.Equal(SpecDefaults.AllowedLoginMethods, authn.DefaultLoginPolicy.AllowedMethods) {
		t.Errorf("the contract publishes allowed_login_methods %v; the service applies %v",
			SpecDefaults.AllowedLoginMethods, authn.DefaultLoginPolicy.AllowedMethods)
	}

	// `mfa_required` has no enforcement to compare against, by design — Phase 3
	// brings it. The contract must publish `false`, because publishing `true`
	// would describe a control that does not exist (`docs/UI-UX/21`).
	if SpecDefaults.MFARequired {
		t.Error("the contract publishes mfa_required: true, which claims an enforcement that does not exist until Phase 3")
	}
}

// The contract's bounds are the ones `ValidateSettings` enforces.
//
// The console validates against the published bounds, so a form that accepts a
// value the server refuses is a confusing rejection, and a form that refuses a
// value the server accepts is a capability nobody can reach.
func TestSpecBoundsMatchTheValidator(t *testing.T) {
	// Below the floor, at it, and above the ceiling — through the real
	// validator, so this measures behaviour rather than a constant.
	tooShort := MinPasswordLengthFloor - 1
	if _, err := ValidateSettings(settingsWithMinLength(tooShort)); err == nil {
		t.Errorf("min_length %d was accepted, below the floor the contract publishes", tooShort)
	}
	if _, err := ValidateSettings(settingsWithMinLength(MinPasswordLengthFloor)); err != nil {
		t.Errorf("min_length %d was refused, though it is the floor the contract publishes: %v",
			MinPasswordLengthFloor, err)
	}
	if _, err := ValidateSettings(settingsWithMinLength(MaxPasswordLength + 1)); err == nil {
		t.Errorf("min_length %d was accepted, above the ceiling the contract publishes",
			MaxPasswordLength+1)
	}
}

func settingsWithMinLength(n int) []byte {
	return []byte(`{"password_policy": {"min_length": ` + strconv.Itoa(n) + `}}`)
}

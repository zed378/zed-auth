package organization

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/management"
)

func validate(t *testing.T, doc string) error {
	t.Helper()
	_, err := ValidateSettings(json.RawMessage(doc))
	return err
}

// fields returns the field paths a refusal names.
func fields(t *testing.T, err error) []string {
	t.Helper()

	var fault management.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("err is %T, not a Fault the middleware can render", err)
	}
	if fault.Class != management.Invalid {
		t.Fatalf("class = %v, want Invalid", fault.Class)
	}

	out := make([]string, 0, len(fault.Details))
	for _, d := range fault.Details {
		if d.Issue == "" {
			t.Errorf("detail for %q has no issue", d.Field)
		}
		out = append(out, d.Field)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// --- unknown keys -----------------------------------------------------------------------

// **The rule this file exists for.** A typo becomes a policy that is not in
// force and looks like it is, and nobody notices until the audit that was
// supposed to find it does not.
func TestAnUnknownSettingIsRefused(t *testing.T) {
	err := validate(t, `{"mfa_requried": true}`)
	if err == nil {
		t.Fatal("a misspelled setting was accepted")
	}
	if got := fields(t, err); !contains(got, "settings.mfa_requried") {
		t.Errorf("the refusal names %v, not the offending key", got)
	}
}

// Nested keys too. `password_policy` is where the interesting typos live,
// because it is the only nested object.
func TestAnUnknownPasswordPolicyKeyIsRefused(t *testing.T) {
	err := validate(t, `{"password_policy": {"min_lenght": 16}}`)
	if err == nil {
		t.Fatal("a misspelled password policy key was accepted")
	}
	if got := fields(t, err); !contains(got, "settings.password_policy.min_lenght") {
		t.Errorf("the refusal names %v", got)
	}
}

// Every unknown key is reported, not just the first. A caller fixing three
// typos should learn about three — which is why this walks the document rather
// than using DisallowUnknownFields, and this test is what stops somebody
// simplifying it back.
func TestEveryUnknownKeyIsReported(t *testing.T) {
	err := validate(t, `{
		"mfa_requried": true,
		"session_lifetime_hrs": 12,
		"password_policy": {"min_lenght": 16, "requires_uppercase": true}
	}`)
	if err == nil {
		t.Fatal("accepted")
	}

	got := fields(t, err)
	for _, want := range []string{
		"settings.mfa_requried",
		"settings.session_lifetime_hrs",
		"settings.password_policy.min_lenght",
		"settings.password_policy.requires_uppercase",
	} {
		if !contains(got, want) {
			t.Errorf("%q was not reported; got %v", want, got)
		}
	}
}

// The control: the real keys are accepted, so "unknown keys are refused" is not
// "everything is refused".
func TestTheDocumentedSettingsAreAccepted(t *testing.T) {
	err := validate(t, `{
		"password_policy": {"min_length": 16, "require_uppercase": true, "max_age_days": 90},
		"mfa_required": true,
		"session_lifetime_hours": 8,
		"allowed_login_methods": ["password"]
	}`)
	if err != nil {
		t.Fatalf("a valid settings document was refused: %v", err)
	}
}

// An empty document is valid: a PATCH that changes nothing else should not have
// to restate the whole policy.
func TestAnEmptyDocumentIsValid(t *testing.T) {
	if err := validate(t, ``); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := validate(t, `{}`); err != nil {
		t.Errorf("{}: %v", err)
	}
}

// --- the password floor ------------------------------------------------------------------

// **A per-organization policy may raise the platform minimum and never lower
// it.** A tenant setting that can go below the instance floor is a per-tenant
// way to disable a platform control.
func TestAPolicyCannotGoBelowThePlatformFloor(t *testing.T) {
	for _, length := range []int{0, 1, 8, MinPasswordLengthFloor - 1} {
		doc := `{"password_policy": {"min_length": ` + itoa(length) + `}}`
		err := validate(t, doc)
		if err == nil {
			t.Errorf("min_length %d was accepted, below the floor of %d", length, MinPasswordLengthFloor)
			continue
		}
		if got := fields(t, err); !contains(got, "settings.password_policy.min_length") {
			t.Errorf("min_length %d was refused, but the error names %v", length, got)
		}
	}

	// The control: at and above the floor is fine, or the test above would
	// pass against a validator that refuses everything.
	for _, length := range []int{MinPasswordLengthFloor, 20, MaxPasswordLength} {
		if err := validate(t, `{"password_policy": {"min_length": `+itoa(length)+`}}`); err != nil {
			t.Errorf("min_length %d was refused: %v", length, err)
		}
	}
}

// The refusal says what the minimum IS. "Invalid value" leaves an administrator
// guessing; naming the floor tells them the platform has one.
func TestTheFloorRefusalNamesTheMinimum(t *testing.T) {
	err := validate(t, `{"password_policy": {"min_length": 4}}`)

	var fault management.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("err is %T", err)
	}
	found := false
	for _, d := range fault.Details {
		if strings.Contains(d.Issue, itoa(MinPasswordLengthFloor)) {
			found = true
		}
	}
	if !found {
		t.Errorf("no detail names the floor: %+v", fault.Details)
	}
}

func TestAnExcessiveMinLengthIsRefused(t *testing.T) {
	if err := validate(t, `{"password_policy": {"min_length": 1000}}`); err == nil {
		t.Error("a min_length nobody could satisfy was accepted")
	}
}

// --- max age ------------------------------------------------------------------------------

// Zero means "never expires" and is a real choice — NIST SP 800-63B argues
// forced rotation makes passwords worse — so it is representable rather than
// approximated by a very large number.
func TestZeroMaxAgeMeansNeverExpires(t *testing.T) {
	if err := validate(t, `{"password_policy": {"max_age_days": 0}}`); err != nil {
		t.Errorf("max_age_days 0 was refused: %v", err)
	}
}

func TestAnOutOfRangeMaxAgeIsRefused(t *testing.T) {
	for _, days := range []int{-1, MaxPasswordAgeDays + 1, 100000} {
		if err := validate(t, `{"password_policy": {"max_age_days": `+itoa(days)+`}}`); err == nil {
			t.Errorf("max_age_days %d was accepted", days)
		}
	}
}

// --- session lifetime ------------------------------------------------------------------------

func TestSessionLifetimeIsBounded(t *testing.T) {
	for _, hours := range []int{-1, 0, MaxSessionLifetimeHours + 1} {
		if err := validate(t, `{"session_lifetime_hours": `+itoa(hours)+`}`); err == nil {
			t.Errorf("session_lifetime_hours %d was accepted", hours)
		}
	}
	for _, hours := range []int{MinSessionLifetimeHours, 12, MaxSessionLifetimeHours} {
		if err := validate(t, `{"session_lifetime_hours": `+itoa(hours)+`}`); err != nil {
			t.Errorf("session_lifetime_hours %d was refused: %v", hours, err)
		}
	}
}

// --- login methods ----------------------------------------------------------------------------

// An API that accepts "passkey" is describing a capability that does not exist
// (docs/UI-UX/21's governance rule) — and a tenant that configured it would have
// silently disabled every login method that works.
func TestAPlannedLoginMethodIsRefusedAndSaysSo(t *testing.T) {
	for _, method := range []string{"passkey", "social"} {
		err := validate(t, `{"allowed_login_methods": ["`+method+`"]}`)
		if err == nil {
			t.Fatalf("%q was accepted, and nothing implements it", method)
		}

		var fault management.Fault
		_ = errors.As(err, &fault)
		joined := ""
		for _, d := range fault.Details {
			joined += d.Issue
		}
		// The distinction matters to an administrator planning a rollout:
		// "not yet available" and "not a login method" are different facts.
		if !strings.Contains(joined, "planned") {
			t.Errorf("%q was refused as though it were a typo: %q", method, joined)
		}
	}
}

func TestAnUnknownLoginMethodIsRefused(t *testing.T) {
	err := validate(t, `{"allowed_login_methods": ["telepathy"]}`)
	if err == nil {
		t.Fatal("accepted")
	}
	if got := fields(t, err); !contains(got, "settings.allowed_login_methods") {
		t.Errorf("the refusal names %v", got)
	}
}

// An empty list is not "no restriction" — it is a tenant nobody can log in to,
// applied by a caller who almost certainly meant the opposite.
func TestAnEmptyLoginMethodListIsRefused(t *testing.T) {
	if err := validate(t, `{"allowed_login_methods": []}`); err == nil {
		t.Error("an empty allowed_login_methods was accepted, which locks everybody out")
	}
}

// --- shapes ----------------------------------------------------------------------------------

// A document that is not an object, or whose fields are the wrong type, is
// refused rather than partially applied.
func TestAMalformedDocumentIsRefused(t *testing.T) {
	for name, doc := range map[string]string{
		"a list":                `[1,2,3]`,
		"a string":              `"settings"`,
		"a number":              `42`,
		"mfa_required a string": `{"mfa_required": "yes"}`,
		"lifetime a string":     `{"session_lifetime_hours": "twelve"}`,
		"policy a list":         `{"password_policy": [1,2]}`,
		"methods a string":      `{"allowed_login_methods": "password"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(t, doc); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// --- absent versus false -------------------------------------------------------------------

// **`mfa_required: false` and an absent `mfa_required` are different
// intentions.** A plain bool cannot tell them apart, which is the classic way a
// partial update silently turns a control off.
func TestAnAbsentSettingIsDistinguishableFromFalse(t *testing.T) {
	absent, err := ValidateSettings(json.RawMessage(`{"session_lifetime_hours": 8}`))
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if absent.MFARequired != nil {
		t.Error("an absent mfa_required decoded as a value")
	}

	explicit, err := ValidateSettings(json.RawMessage(`{"mfa_required": false}`))
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}
	if explicit.MFARequired == nil {
		t.Fatal("an explicit mfa_required: false decoded as absent — a PATCH could never turn it off")
	}
	if *explicit.MFARequired {
		t.Error("mfa_required: false decoded as true")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

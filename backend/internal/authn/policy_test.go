package authn

import (
	"strings"
	"testing"
	"time"
)

// rules returns just the rule keys, which is what most assertions care about —
// the message is human text and asserting on it makes the tests brittle
// against wording changes that carry no meaning.
func rules(violations []Violation) []string {
	out := make([]string, 0, len(violations))
	for _, v := range violations {
		out = append(out, v.Rule)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- the pure evaluator ----------------------------------------------------

// P1-02 DoD item 1: table-driven, including boundary lengths.
//
// Both sides of every boundary, not just the failing side. A test that only
// checks that 11 characters is rejected passes equally against an
// implementation that rejects everything.
func TestEvaluate(t *testing.T) {
	strict := Policy{MinLength: 12, RequireUppercase: true}
	lengthOnly := Policy{MinLength: 12}

	cases := []struct {
		name     string
		password string
		policy   Policy
		want     []string
	}{
		{"exactly at the minimum", "Abcdefghijkl", strict, nil},
		{"one over the minimum", "Abcdefghijklm", strict, nil},
		{"one under the minimum", "Abcdefghijk", strict, []string{RuleMinLength}},
		{"far under", "A", strict, []string{RuleMinLength}},
		{"empty", "", strict, []string{RuleMinLength, RuleRequireUppercase}},

		{"no uppercase", "abcdefghijkl", strict, []string{RuleRequireUppercase}},
		{"uppercase not required", "abcdefghijkl", lengthOnly, nil},
		{"uppercase at the end", "abcdefghijkL", strict, nil},
		{"uppercase mid-string", "abcdefgHijkl", strict, nil},

		// Every violation at once, in a stable order. A form that reveals one
		// problem per submit is a form the user fights.
		{"both rules fail", "abc", strict, []string{RuleMinLength, RuleRequireUppercase}},

		// A digit is not an uppercase letter, and neither is a symbol. Worth
		// pinning because unicode.IsUpper is easy to reach for something
		// looser.
		{"digits do not satisfy uppercase", "abcdefghij12", strict, []string{RuleRequireUppercase}},
		{"symbols do not satisfy uppercase", "abcdefghij!@", strict, []string{RuleRequireUppercase}},

		// Non-Latin uppercase counts. The rule is about case, not about ASCII.
		{"greek capital satisfies uppercase", "abcdefghijkΔ", strict, nil},
		{"cyrillic capital satisfies uppercase", "abcdefghijkЖ", strict, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(Evaluate(tc.password, tc.policy))
			if !equal(got, tc.want) {
				t.Errorf("Evaluate(%q) = %v, want %v", tc.password, got, tc.want)
			}
		})
	}
}

// Length is characters, not bytes.
//
// Bytes would make "at least twelve characters" mean twelve in English and
// four in Japanese — a different policy per language, enforced by an
// implementation detail nobody chose.
func TestEvaluateCountsRunesNotBytes(t *testing.T) {
	policy := Policy{MinLength: 12}

	// Twelve characters, thirty-six bytes.
	japanese := "パスワードは十二文字です"
	if runes := len([]rune(japanese)); runes != 12 {
		t.Fatalf("test fixture is wrong: %d runes", runes)
	}
	if bytes := len(japanese); bytes <= 12 {
		t.Fatalf("test fixture is wrong: %d bytes, expected many more", bytes)
	}

	if got := Evaluate(japanese, policy); len(got) != 0 {
		t.Errorf("a twelve-character password was rejected: %v", rules(got))
	}

	// And eleven of them is still short, so the rule is being applied rather
	// than skipped for non-ASCII.
	short := string([]rune(japanese)[:11])
	if got := rules(Evaluate(short, policy)); !equal(got, []string{RuleMinLength}) {
		t.Errorf("Evaluate(11 characters) = %v, want a length violation", got)
	}
}

// NFC normalization can only make the count stricter, never laxer.
//
// A decomposed "é" is two runes on the wire and one character to the user.
// Counting the raw form would let a password of accented characters pass a
// length rule it does not actually meet.
func TestEvaluateNormalizesBeforeCounting(t *testing.T) {
	policy := Policy{MinLength: 12}

	// Twelve "é" written decomposed: 24 runes raw, 12 after NFC.
	decomposed := strings.Repeat("é", 12)
	if len([]rune(decomposed)) != 24 {
		t.Fatalf("test fixture is wrong: %d runes", len([]rune(decomposed)))
	}
	if got := Evaluate(decomposed, policy); len(got) != 0 {
		t.Errorf("twelve composed characters were rejected: %v", rules(got))
	}

	// Eleven of them is eleven characters, and must be rejected — this is the
	// half that fails if normalization is dropped.
	short := strings.Repeat("é", 11)
	if got := rules(Evaluate(short, policy)); !equal(got, []string{RuleMinLength}) {
		t.Errorf("Evaluate(11 decomposed characters) = %v, want a length violation; "+
			"without normalization this counts as 22 and wrongly passes", got)
	}
}

// The byte bound is checked before anything else, so a huge body is rejected
// without a normalization pass over it.
func TestEvaluateBoundsTheInput(t *testing.T) {
	policy := Policy{MinLength: 12, RequireUppercase: true}

	atLimit := strings.Repeat("A", maxPasswordBytes)
	if got := Evaluate(atLimit, policy); len(got) != 0 {
		t.Errorf("a password at the byte limit was rejected: %v", rules(got))
	}

	overLimit := strings.Repeat("A", maxPasswordBytes+1)
	got := rules(Evaluate(overLimit, policy))
	if !equal(got, []string{RuleTooLong}) {
		t.Errorf("Evaluate(oversized) = %v, want only %q", got, RuleTooLong)
	}
}

// The floor holds even when a Policy is built by hand rather than parsed.
//
// Evaluate sanitizes its own input for exactly this reason: a future caller
// that constructs a Policy from somewhere other than ParsePolicy must not be
// able to route around the floor.
func TestEvaluateAppliesTheFloorToAHandBuiltPolicy(t *testing.T) {
	got := rules(Evaluate("Abc", Policy{MinLength: 1}))
	if !equal(got, []string{RuleMinLength}) {
		t.Errorf("Evaluate with min_length 1 = %v; the %d-character floor should still apply",
			got, MinLengthFloor)
	}
}

// --- expiry ----------------------------------------------------------------

func TestExpired(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	at := func(days int) *time.Time {
		t := now.AddDate(0, 0, -days)
		return &t
	}
	ninety := Policy{MinLength: 12, MaxAgeDays: 90}

	cases := []struct {
		name      string
		changedAt *time.Time
		policy    Policy
		want      bool
	}{
		// PG-13: the column is nullable with no backfill, so nil means "never
		// recorded". Treating it as infinitely old would make deploying the
		// migration a mass lockout.
		{"never recorded", nil, ninety, false},

		{"changed today", at(0), ninety, false},
		{"one day before the limit", at(89), ninety, false},
		{"exactly at the limit", at(90), ninety, false},
		{"one day past the limit", at(91), ninety, true},
		{"long past the limit", at(400), ninety, true},

		{"no expiry configured", at(400), Policy{MinLength: 12, MaxAgeDays: 0}, false},
		{"negative max age is no expiry", at(400), Policy{MinLength: 12, MaxAgeDays: -1}, false},

		// Clock skew between the database and the service is normal. No
		// arithmetic here may turn it into a lockout.
		{"future-dated", at(-5), ninety, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Expired(tc.changedAt, tc.policy, now); got != tc.want {
				t.Errorf("Expired() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- parsing ---------------------------------------------------------------

func TestParsePolicy(t *testing.T) {
	cases := []struct {
		name            string
		settings        string
		want            Policy
		wantAdjustments int
	}{
		{
			// The exact default P0-07's migration writes.
			name:     "the migration default",
			settings: `{"password_policy":{"min_length":12,"require_uppercase":true,"max_age_days":90},"mfa_required":false}`,
			want:     Policy{MinLength: 12, RequireUppercase: true, MaxAgeDays: 90},
		},
		{
			name:     "a stricter policy is honoured",
			settings: `{"password_policy":{"min_length":20,"require_uppercase":true,"max_age_days":30}}`,
			want:     Policy{MinLength: 20, RequireUppercase: true, MaxAgeDays: 30},
		},
		{
			// An explicit false must survive. This is what the pointer fields
			// in settingsShape exist for: with a plain bool, "absent" and
			// "false" are the same value and the default silently wins.
			name:     "explicit false is not treated as absent",
			settings: `{"password_policy":{"require_uppercase":false}}`,
			want:     Policy{MinLength: 12, RequireUppercase: false, MaxAgeDays: 90},
		},
		{
			name:     "a partial policy takes defaults for the rest",
			settings: `{"password_policy":{"min_length":16}}`,
			want:     Policy{MinLength: 16, RequireUppercase: true, MaxAgeDays: 90},
		},
		{
			name:     "no password_policy key",
			settings: `{"mfa_required":true}`,
			want:     DefaultPolicy,
		},
		{
			name:     "empty settings",
			settings: ``,
			want:     DefaultPolicy,
		},
		{
			name:     "empty object",
			settings: `{}`,
			want:     DefaultPolicy,
		},
		{
			// An administrator-editable column with a typo in it cannot mean
			// that passwords stop being checked.
			name:            "malformed JSON falls back to defaults",
			settings:        `{"password_policy":{`,
			want:            DefaultPolicy,
			wantAdjustments: 1,
		},
		{
			// The mechanism built to give administrators control must not be
			// the mechanism by which one of them switches it off.
			name:            "min_length below the floor is clamped",
			settings:        `{"password_policy":{"min_length":1}}`,
			want:            Policy{MinLength: MinLengthFloor, RequireUppercase: true, MaxAgeDays: 90},
			wantAdjustments: 1,
		},
		{
			name:            "min_length of zero is clamped",
			settings:        `{"password_policy":{"min_length":0}}`,
			want:            Policy{MinLength: MinLengthFloor, RequireUppercase: true, MaxAgeDays: 90},
			wantAdjustments: 1,
		},
		{
			name:            "a min_length no password could meet is clamped",
			settings:        `{"password_policy":{"min_length":999999}}`,
			want:            Policy{MinLength: MinLengthCeiling, RequireUppercase: true, MaxAgeDays: 90},
			wantAdjustments: 1,
		},
		{
			name:            "an absurd max_age is clamped",
			settings:        `{"password_policy":{"max_age_days":99999}}`,
			want:            Policy{MinLength: 12, RequireUppercase: true, MaxAgeDays: MaxAgeDaysCeiling},
			wantAdjustments: 1,
		},
		{
			name:            "a negative max_age reads as no expiry",
			settings:        `{"password_policy":{"max_age_days":-30}}`,
			want:            Policy{MinLength: 12, RequireUppercase: true, MaxAgeDays: 0},
			wantAdjustments: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, adjustments := ParsePolicy([]byte(tc.settings))
			if got != tc.want {
				t.Errorf("ParsePolicy(%s) = %+v, want %+v", tc.settings, got, tc.want)
			}
			if len(adjustments) != tc.wantAdjustments {
				t.Errorf("got %d adjustments, want %d: %+v",
					len(adjustments), tc.wantAdjustments, adjustments)
			}
		})
	}
}

// A correction that is not reported is indistinguishable from a policy that
// was never applied — the shape of vacuous verification this project keeps
// finding. The Adjustment has to carry enough to act on.
func TestParsePolicyReportsWhatItChanged(t *testing.T) {
	_, adjustments := ParsePolicy([]byte(`{"password_policy":{"min_length":3}}`))

	if len(adjustments) != 1 {
		t.Fatalf("got %d adjustments, want 1", len(adjustments))
	}

	a := adjustments[0]
	if a.Field != "min_length" {
		t.Errorf("Field = %q, want %q", a.Field, "min_length")
	}
	if a.Configured != "3" {
		t.Errorf("Configured = %q, want %q — an operator has to be able to find the bad value", a.Configured, "3")
	}
	if a.Applied != "8" {
		t.Errorf("Applied = %q, want %q", a.Applied, "8")
	}
	if a.Reason == "" {
		t.Error("Reason is empty; a warning that does not say why is a warning nobody acts on")
	}
}

// P1-02 DoD item 5, at the level this package can assert it: enforcement
// follows the settings document, with nothing about the values compiled in.
// The integration half — the same thing through a real organizations row — is
// in the storage package.
func TestPolicyChangeChangesEnforcementWithNoCodeChange(t *testing.T) {
	password := "abcdefghijklmnop" // 16 characters, no uppercase

	lenient, _ := ParsePolicy([]byte(`{"password_policy":{"min_length":12,"require_uppercase":false}}`))
	if got := Evaluate(password, lenient); len(got) != 0 {
		t.Fatalf("under the lenient policy the password was rejected: %v", rules(got))
	}

	stricter, _ := ParsePolicy([]byte(`{"password_policy":{"min_length":20,"require_uppercase":true}}`))
	got := rules(Evaluate(password, stricter))
	if !equal(got, []string{RuleMinLength, RuleRequireUppercase}) {
		t.Errorf("under the stricter policy = %v, want both rules to fire", got)
	}
}

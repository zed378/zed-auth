package authn

import (
	"testing"
	"time"
)

// The mandate's truth table (P3-07).
//
// Three inputs — the mandate, whether the user holds a factor, and where the
// clock is relative to the grace — and every combination is here rather than
// described. `docs/PLAN/17`'s Phase 2 criterion rests on this function, and it
// is the kind of decision that is easy to get subtly wrong in a way no
// end-to-end test would isolate.

func TestTheMandateDecidesEveryCombination(t *testing.T) {
	enabled := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	inGrace := enabled.Add(MFAGracePeriod - time.Hour)
	past := enabled.Add(MFAGracePeriod + time.Hour)

	for _, tc := range []struct {
		name      string
		policy    LoginPolicy
		hasFactor bool
		now       time.Time
		want      MFAOutcome
		why       string
	}{
		{
			name:   "no mandate, no factor",
			policy: LoginPolicy{},
			now:    past,
			want:   MFANotRequired,
			why:    "an organization that has not asked for MFA does not get it imposed",
		},
		{
			name:      "no mandate, with a factor",
			policy:    LoginPolicy{},
			hasFactor: true,
			now:       past,
			want:      MFANotRequired,
			why:       "the user's own factor is challenged by P3-03; the mandate has no opinion",
		},
		{
			name:      "mandate, user already has a factor",
			policy:    LoginPolicy{MFARequired: true, MFARequiredSince: enabled},
			hasFactor: true,
			now:       past,
			want:      MFANotRequired,
			why:       "they satisfy it; there is nothing to force",
		},
		{
			name:   "mandate, no factor, inside the grace",
			policy: LoginPolicy{MFARequired: true, MFARequiredSince: enabled},
			now:    inGrace,
			want:   MFAInGrace,
			why:    "signed in and warned — the grace is what stops a hard cutover",
		},
		{
			name:   "mandate, no factor, grace expired",
			policy: LoginPolicy{MFARequired: true, MFARequiredSince: enabled},
			now:    past,
			want:   MFAEnrolmentRequired,
			why:    "the deadline is the point at which the policy becomes real",
		},
		{
			name:   "mandate, no factor, exactly at the deadline",
			policy: LoginPolicy{MFARequired: true, MFARequiredSince: enabled},
			now:    enabled.Add(MFAGracePeriod),
			want:   MFAEnrolmentRequired,
			why:    "the boundary is exclusive: at the deadline the grace has run out",
		},
		{
			name:   "mandate on before the field existed",
			policy: LoginPolicy{MFARequired: true},
			now:    past,
			want:   MFAInGrace,
			why: "an organization upgrading with the flag already set gets a grace " +
				"starting now — a deadline nobody could have known about is not a deadline",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RequireMFA(tc.policy, tc.hasFactor, tc.now)
			if got != tc.want {
				t.Errorf("RequireMFA = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// The deadline is shown only when there is one.
func TestTheDeadlineIsTheActivationPlusTheGrace(t *testing.T) {
	enabled := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	if got := MFADeadline(LoginPolicy{MFARequired: true, MFARequiredSince: enabled}); !got.Equal(enabled.Add(MFAGracePeriod)) {
		t.Errorf("MFADeadline = %s, want %s", got, enabled.Add(MFAGracePeriod))
	}
	if got := MFADeadline(LoginPolicy{}); !got.IsZero() {
		t.Errorf("MFADeadline = %s with no mandate, want zero", got)
	}
	if got := MFADeadline(LoginPolicy{MFARequired: true}); !got.IsZero() {
		t.Errorf("MFADeadline = %s with no activation time, want zero — "+
			"showing a made-up deadline would be worse than showing none", got)
	}
}

// The grace is bounded and neither trivial nor indefinite.
//
// A number nobody can state is a number nobody maintains, so widening it is a
// visible decision rather than a quiet one.
func TestTheGracePeriodIsBounded(t *testing.T) {
	if MFAGracePeriod < 7*24*time.Hour {
		t.Errorf("MFAGracePeriod is %s — shorter than a week means somebody on holiday "+
			"comes back locked out, which is the hard cutover this exists to avoid", MFAGracePeriod)
	}
	if MFAGracePeriod > 30*24*time.Hour {
		t.Errorf("MFAGracePeriod is %s — the policy is decorative for that long", MFAGracePeriod)
	}
}

// The mandate is parsed out of a settings document.
func TestTheMandateIsReadFromSettings(t *testing.T) {
	enabled := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	policy, adjustments := ParseLoginPolicy([]byte(
		`{"mfa_required":true,"mfa_required_since":"2026-09-01T12:00:00Z"}`))

	if !policy.MFARequired {
		t.Error("mfa_required was not read")
	}
	if !policy.MFARequiredSince.Equal(enabled) {
		t.Errorf("MFARequiredSince = %s, want %s", policy.MFARequiredSince, enabled)
	}
	if len(adjustments) != 0 {
		t.Errorf("a valid document produced adjustments: %+v", adjustments)
	}
}

// Absent means off, and a malformed document means the secure default rather
// than a refusal — the same choice the rest of this parser makes.
func TestAnAbsentMandateIsOff(t *testing.T) {
	policy, _ := ParseLoginPolicy([]byte(`{"session_lifetime_hours":8}`))
	if policy.MFARequired {
		t.Error("a settings document that says nothing about MFA turned it on")
	}

	policy, adjustments := ParseLoginPolicy([]byte(`not json`))
	if policy.MFARequired {
		t.Error("an unreadable settings document turned the mandate on")
	}
	if len(adjustments) == 0 {
		t.Error("an unreadable document reported no adjustment, so nobody would know")
	}
}

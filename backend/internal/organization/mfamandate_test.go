package organization

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/management"
)

// When the mandate's activation is stamped, and when it must not be (P3-07).
//
// The stamp decides everybody's grace deadline, so the rules about when it
// moves are the security-relevant part: a stamp that restarted on every
// settings edit would make the policy permanently ineffective, and one a
// caller could supply would let an administrator lock out an organization
// instantly.

func mandateNow() time.Time { return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC) }

func stampedTime(t *testing.T, settings json.RawMessage) (time.Time, bool) {
	t.Helper()

	var fields struct {
		Since *time.Time `json:"mfa_required_since"`
	}
	if err := json.Unmarshal(settings, &fields); err != nil {
		t.Fatalf("reading the stamped settings: %v\n%s", err, settings)
	}
	if fields.Since == nil {
		return time.Time{}, false
	}
	return *fields.Since, true
}

// Turning the mandate on stamps the moment it happened.
func TestEnablingTheMandateStampsIt(t *testing.T) {
	got, err := StampMandate(
		json.RawMessage(`{"session_lifetime_hours":8}`),
		json.RawMessage(`{"mfa_required":true}`),
		mandateNow())
	if err != nil {
		t.Fatalf("StampMandate: %v", err)
	}

	at, present := stampedTime(t, got)
	if !present {
		t.Fatalf("enabling the mandate stamped nothing, so there is no deadline to measure:\n%s", got)
	}
	if !at.Equal(mandateNow()) {
		t.Errorf("stamped %s, want %s", at, mandateNow())
	}

	// The caller's own field survives.
	var fields struct {
		MFARequired *bool `json:"mfa_required"`
	}
	if err := json.Unmarshal(got, &fields); err != nil || fields.MFARequired == nil || !*fields.MFARequired {
		t.Errorf("the patch lost mfa_required:\n%s", got)
	}
}

// Re-sending `mfa_required: true` does NOT restart the grace.
//
// Without this, a routine settings edit would hand everybody a fresh fourteen
// days and the policy would never take effect.
func TestRestatingTheMandateDoesNotRestartTheGrace(t *testing.T) {
	current := json.RawMessage(`{"mfa_required":true,"mfa_required_since":"2026-09-01T00:00:00Z"}`)

	got, err := StampMandate(current, json.RawMessage(`{"mfa_required":true}`), mandateNow())
	if err != nil {
		t.Fatalf("StampMandate: %v", err)
	}

	if _, present := stampedTime(t, got); present {
		t.Errorf("an already-enabled mandate was stamped again, restarting everybody's grace:\n%s", got)
	}
}

// A settings edit that does not mention the mandate leaves it alone.
func TestAnUnrelatedEditDoesNotStamp(t *testing.T) {
	current := json.RawMessage(`{"mfa_required":true,"mfa_required_since":"2026-09-01T00:00:00Z"}`)

	got, err := StampMandate(current, json.RawMessage(`{"session_lifetime_hours":8}`), mandateNow())
	if err != nil {
		t.Fatalf("StampMandate: %v", err)
	}
	if _, present := stampedTime(t, got); present {
		t.Errorf("an unrelated settings edit stamped the mandate:\n%s", got)
	}
}

// Turning it OFF does not stamp, and deliberately leaves the old timestamp in
// place — so toggling it off and on again within the window does not buy a
// fresh grace period.
func TestDisablingDoesNotStamp(t *testing.T) {
	current := json.RawMessage(`{"mfa_required":true,"mfa_required_since":"2026-09-01T00:00:00Z"}`)

	got, err := StampMandate(current, json.RawMessage(`{"mfa_required":false}`), mandateNow())
	if err != nil {
		t.Fatalf("StampMandate: %v", err)
	}
	if _, present := stampedTime(t, got); present {
		t.Errorf("disabling the mandate stamped an activation time:\n%s", got)
	}
}

// An empty patch is returned untouched rather than turned into an object.
func TestAnEmptyPatchIsUnchanged(t *testing.T) {
	got, err := StampMandate(json.RawMessage(`{}`), nil, mandateNow())
	if err != nil {
		t.Fatalf("StampMandate: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("an empty patch became %s", got)
	}
}

// --- the transition, for the audit event ---------------------------------------------

func TestTheTransitionIsReported(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after string
		want          MandateTransition
		why           string
	}{
		{
			name: "off to on", before: `{}`, after: `{"mfa_required":true}`,
			want: MandateEnabled,
			why:  "somebody imposed a policy on every user in the organization",
		},
		{
			name: "on to off", before: `{"mfa_required":true}`, after: `{"mfa_required":false}`,
			want: MandateDisabled,
			why:  "somebody REMOVED a security control, which is what an incident review looks for",
		},
		{
			name: "on to on", before: `{"mfa_required":true}`, after: `{"mfa_required":true}`,
			want: MandateUnchanged,
			why:  "nothing happened; an event here would be noise in the log",
		},
		{
			name: "off to off", before: `{}`, after: `{"session_lifetime_hours":8}`,
			want: MandateUnchanged,
			why:  "an unrelated settings edit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := MandateChange(json.RawMessage(tc.before), json.RawMessage(tc.after))
			if got != tc.want {
				t.Errorf("MandateChange = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// --- what a caller may not send ----------------------------------------------------------

// `mfa_required_since` is refused as an unknown setting.
//
// This is abuse case A-5 and it is closed by the existing allow-list rather
// than by anything this task added — so the test lives here, next to the
// stamping, because that is where somebody would think to add the key.
func TestACallerCannotSupplyTheActivationTime(t *testing.T) {
	_, err := ValidateSettings(json.RawMessage(
		`{"mfa_required":true,"mfa_required_since":"2020-01-01T00:00:00Z"}`))

	if err == nil {
		t.Fatal("a caller supplied the activation time; the grace could be backdated to zero")
	}

	// The field name is in the fault's DETAILS, not in its message — the
	// message is deliberately a summary, and `docs/UI-UX/15` maps the details
	// back to a form field. Asserting on Error() checked the wrong surface.
	var fault management.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("the refusal is not a Fault, so no field could be named: %T", err)
	}

	named := false
	for _, d := range fault.Details {
		if strings.Contains(d.Field, MFARequiredSinceKey) {
			named = true
		}
	}
	if !named {
		t.Errorf("the refusal does not name %q, so a caller could not tell what was wrong: %+v",
			MFARequiredSinceKey, fault.Details)
	}
}

// And the mandate itself IS accepted, so the test above is about the timestamp
// rather than about settings validation refusing everything.
func TestTheMandateItselfIsAccepted(t *testing.T) {
	if _, err := ValidateSettings(json.RawMessage(`{"mfa_required":true}`)); err != nil {
		t.Errorf("a valid mandate was refused: %v", err)
	}
}

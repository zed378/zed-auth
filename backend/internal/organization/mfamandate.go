package organization

import (
	"encoding/json"
	"fmt"
	"time"
)

// Stamping when the MFA mandate was turned on (P3-07).
//
// The grace period is measured from activation, so activation has to be
// recorded somewhere. Three properties decide where and how:
//
//   - **Not from the audit log**, though it could be derived there. An audit
//     log is an append-only record of what happened; reading policy out of it
//     would make a retention change silently move the enforcement deadline.
//     Policy belongs in the policy.
//   - **Not from the caller.** `unknownKeys` refuses `mfa_required_since` as an
//     unknown setting, which is what closes abuse case A-5: an administrator
//     who could supply it could backdate the grace to zero and lock out every
//     colleague immediately. It must stay out of that allow-list.
//   - **Stamped only on the transition**, so re-sending `mfa_required: true`
//     in an unrelated PATCH does not restart everybody's grace period.

// MFARequiredSinceKey is the settings field, written only by this service.
const MFARequiredSinceKey = "mfa_required_since"

// StampMandate returns the settings patch to apply, with the activation time
// added when this patch is what turns the mandate on.
//
// `current` is the organization's stored settings and `patch` is what the
// caller sent. Returns `patch` unchanged on every path but the transition.
func StampMandate(current, patch json.RawMessage, now time.Time) (json.RawMessage, error) {
	if len(patch) == 0 {
		return patch, nil
	}

	wants, present := mandateIn(patch)
	if !present || !wants {
		// Not mentioned, or being turned OFF. Turning it off deliberately
		// leaves the timestamp behind: it costs nothing, and it means turning
		// the mandate back on within the same grace window does not hand
		// everybody a fresh fourteen days. Somebody toggling the setting to
		// buy time is exactly who this should not accommodate.
		return patch, nil
	}

	if already, _ := mandateIn(current); already {
		// Already on. Re-sending it in an unrelated PATCH must not restart the
		// grace, or a routine settings edit would quietly extend the window
		// during which the policy does nothing.
		return patch, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patch, &fields); err != nil {
		return nil, fmt.Errorf("organization: reading a settings patch: %w", err)
	}

	stamp, err := json.Marshal(now.UTC())
	if err != nil {
		return nil, fmt.Errorf("organization: encoding the mandate timestamp: %w", err)
	}
	fields[MFARequiredSinceKey] = stamp

	stamped, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("organization: writing a settings patch: %w", err)
	}
	return stamped, nil
}

// MandateTransition reports how a settings change moved the mandate, for the
// audit event.
//
// Turning it OFF is reported as its own thing rather than folded into a
// generic update, because somebody removing a security control is what
// `docs/SECURITY/04` has an incident reviewer search for — and "settings
// changed" cannot answer that question.
type MandateTransition int

const (
	MandateUnchanged MandateTransition = iota
	MandateEnabled
	MandateDisabled
)

// MandateChange compares two settings documents.
func MandateChange(before, after json.RawMessage) MandateTransition {
	was, _ := mandateIn(before)
	is, _ := mandateIn(after)

	switch {
	case !was && is:
		return MandateEnabled
	case was && !is:
		return MandateDisabled
	default:
		return MandateUnchanged
	}
}

// mandateIn reads `mfa_required` out of a settings document.
//
// Reports whether the key was present as well as its value, so "absent" and
// "explicitly false" stay distinguishable — the distinction `Settings` uses
// pointers to preserve, and the classic way a partial update turns a control
// off.
func mandateIn(settings json.RawMessage) (value bool, present bool) {
	if len(settings) == 0 {
		return false, false
	}

	var fields struct {
		MFARequired *bool `json:"mfa_required"`
	}
	if err := json.Unmarshal(settings, &fields); err != nil {
		return false, false
	}
	if fields.MFARequired == nil {
		return false, false
	}
	return *fields.MFARequired, true
}

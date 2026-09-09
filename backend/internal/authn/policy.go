package authn

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Password policy, read from organizations.settings rather than compiled in.
//
// PLAN/08 Part B § Policies per Organization specifies the shape; P0-07's
// migration already writes it as the default for every organization. The
// reason it is read rather than constant is P2-14: administrators will edit
// these values, and enforcement that reads a Go constant is enforcement that
// has to be rewritten before that can happen.
//
// Specification: MEMORY/specs/P1-02-password-policy.md.

// Policy is one organization's password rules.
type Policy struct {
	// MinLength is counted in runes, not bytes. See Evaluate.
	MinLength int

	// RequireUppercase demands at least one uppercase letter.
	//
	// Meaningless in a script that has no case — a Japanese passphrase cannot
	// satisfy it at any length. That is a consequence of PLAN/08's rule rather
	// than of this code, and the available remedy is that the rule is per
	// organization: a tenant whose users write in a caseless script can turn
	// it off. Recorded in the spec § 13 so it is a known trade rather than a
	// surprise.
	RequireUppercase bool

	// MaxAgeDays is how long a password stays valid. Zero means no expiry.
	//
	// Enforced at login (P1-11/P1-12) against users.password_changed_at, not
	// here — a candidate password has no age. Expired is the predicate.
	MaxAgeDays int
}

// DefaultPolicy is what an organization gets when its settings say nothing.
//
// The same values P0-07's migration writes as the column default, duplicated
// deliberately: the migration protects rows created through it, and this
// protects a row whose settings were edited into an unusable state. Neither is
// redundant, because they cover different failures.
var DefaultPolicy = Policy{
	MinLength:        12,
	RequireUppercase: true,
	MaxAgeDays:       90,
}

// The bounds a configured policy is clamped into.
//
// MinLengthFloor is a policy on policies, and it is the important one. Without
// it, `"min_length": 1` is a valid configuration and the mechanism built to
// give administrators control becomes the mechanism by which one of them
// switches the control off — most likely by accident, and invisibly, since a
// weak policy produces no error anywhere.
const (
	MinLengthFloor = 8

	// Matching maxPasswordBytes, so a policy cannot demand a password the
	// hasher will refuse. A configuration whose only effect is to reject every
	// password is worse than one that is merely wrong.
	MinLengthCeiling = maxPasswordBytes

	// Ten years. Not a security bound — a sanity bound, so a mistyped value
	// is caught rather than silently meaning "never".
	MaxAgeDaysCeiling = 3650
)

// Rule keys. Stable machine identifiers, safe to branch on and to use as a
// metric label. The human text lives in Violation.Message.
const (
	RuleMinLength        = "min_length"
	RuleRequireUppercase = "require_uppercase"
	RuleTooLong          = "too_long"
	RuleBreached         = "breached"
)

// Violation is one unmet rule.
//
// Structured rather than a formatted string, because the caller decides how
// much of it reaches the response. On an authenticated change form the user
// needs every detail; on the unauthenticated login path none of it may be
// disclosed, since the composition rules for a tenant narrow a credential
// stuffing search for free (spec § 12, P1-02 step 6).
type Violation struct {
	Rule    string
	Message string
}

// Evaluate reports every way the password fails the policy.
//
// Pure: no clock, no I/O, no database. That is what makes exhaustive
// table-driven testing meaningful, and it is why the breach check — which is
// none of those things — is composed on top rather than passed in here.
//
// Every violation, not the first. A form that reveals one problem per submit
// is a form the user fights, and the round trips are ours to pay for.
//
// Length is counted in runes over the NFC-normalized password. Bytes would
// make "twelve characters" mean twelve in English and four in Japanese, which
// is a different policy per language enforced by an implementation detail.
// NFC rather than NFKC: compatibility folding is built for search and
// identifier matching, and it rewrites characters in ways that are surprising
// in a length rule. Where NFC changes the count at all it lowers it — a
// decomposed accent counts once instead of twice — so the normalization can
// only make this stricter, never laxer.
//
// The password itself is never normalized on the way to the hasher. What is
// stored is what the user typed (P1-01), and this function only counts.
func Evaluate(password string, p Policy) []Violation {
	p = p.sanitized()

	var violations []Violation

	// Before any rule, and before any rule's cost. An unbounded input from a
	// request an attacker controls is the same concern P1-01 bounds at the
	// hasher; bounding it here too means a 10MB body is rejected without a
	// normalization pass over it.
	if len(password) > maxPasswordBytes {
		return []Violation{{
			Rule:    RuleTooLong,
			Message: fmt.Sprintf("must be at most %d bytes", maxPasswordBytes),
		}}
	}

	normalized := norm.NFC.String(password)

	if length := len([]rune(normalized)); length < p.MinLength {
		violations = append(violations, Violation{
			Rule:    RuleMinLength,
			Message: fmt.Sprintf("must be at least %d characters", p.MinLength),
		})
	}

	if p.RequireUppercase && !hasUpper(normalized) {
		violations = append(violations, Violation{
			Rule:    RuleRequireUppercase,
			Message: "must contain an uppercase letter",
		})
	}

	return violations
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// Expired reports whether a password set at changedAt has aged out.
//
// Pure, and separate from Evaluate because it answers a question about a
// stored password rather than a candidate one. P1-11 calls it at login; there
// is nothing to call it on at the moment a password is chosen.
//
// A nil changedAt is NOT expired. The column is nullable with no backfill
// (PG-13), so nil means "we never recorded it", and treating an unknown as
// infinitely old would make deploying that migration a mass lockout — an
// outage wearing a security control's clothes.
//
// A future-dated changedAt is not expired either. Clock skew between the
// database and the service is normal, and no arithmetic here should be able to
// produce a negative age that wraps into a lockout.
func Expired(changedAt *time.Time, p Policy, now time.Time) bool {
	p = p.sanitized()

	if p.MaxAgeDays <= 0 || changedAt == nil {
		return false
	}
	if changedAt.After(now) {
		return false
	}

	return now.Sub(*changedAt) > time.Duration(p.MaxAgeDays)*24*time.Hour
}

// Adjustment records a policy value this code had to correct.
//
// Returned rather than only logged, so the caller can surface it and a test
// can assert on it. A silent correction is indistinguishable from a policy
// that was never applied.
type Adjustment struct {
	Field      string
	Configured string
	Applied    string
	Reason     string
}

// settingsShape mirrors the JSON in PLAN/08 Part B.
//
// Pointers throughout, because "absent" and "set to the zero value" are
// different: a missing require_uppercase must fall back to the default, while
// an explicit `false` must be honoured. A plain bool cannot tell them apart,
// and the failure is silent in the insecure direction.
type settingsShape struct {
	PasswordPolicy *struct {
		MinLength        *int  `json:"min_length"`
		RequireUppercase *bool `json:"require_uppercase"`
		MaxAgeDays       *int  `json:"max_age_days"`
	} `json:"password_policy"`
}

// ParsePolicy reads a policy out of an organization's settings JSON.
//
// Never fails. A malformed, partial or absent policy yields DefaultPolicy for
// the fields it could not supply, and every substitution is reported. An
// unparseable policy must not become no policy: the settings column is
// administrator-editable, and the outcome of a typo there cannot be that
// passwords stop being checked.
func ParsePolicy(settings []byte) (Policy, []Adjustment) {
	policy := DefaultPolicy
	var adjustments []Adjustment

	if len(settings) == 0 {
		return policy, nil
	}

	var parsed settingsShape
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return policy, []Adjustment{{
			Field:   "password_policy",
			Applied: "defaults",
			Reason:  "the settings document is not valid JSON",
		}}
	}
	if parsed.PasswordPolicy == nil {
		return policy, nil
	}

	if v := parsed.PasswordPolicy.MinLength; v != nil {
		policy.MinLength = *v
	}
	if v := parsed.PasswordPolicy.RequireUppercase; v != nil {
		policy.RequireUppercase = *v
	}
	if v := parsed.PasswordPolicy.MaxAgeDays; v != nil {
		policy.MaxAgeDays = *v
	}

	return policy.sanitize(&adjustments), adjustments
}

// sanitized clamps without reporting, for the internal callers that only need
// the values. Evaluate and Expired both call it, so a Policy built by hand in
// a test or by a future caller cannot bypass the floor.
func (p Policy) sanitized() Policy {
	return p.sanitize(nil)
}

// sanitize clamps every field into its documented bounds, appending an
// Adjustment for each correction when adjustments is non-nil.
func (p Policy) sanitize(adjustments *[]Adjustment) Policy {
	record := func(field string, configured, applied int, reason string) {
		if adjustments == nil {
			return
		}
		*adjustments = append(*adjustments, Adjustment{
			Field:      field,
			Configured: fmt.Sprintf("%d", configured),
			Applied:    fmt.Sprintf("%d", applied),
			Reason:     reason,
		})
	}

	if p.MinLength < MinLengthFloor {
		record("min_length", p.MinLength, MinLengthFloor,
			fmt.Sprintf("below the %d-character floor this service enforces regardless of configuration", MinLengthFloor))
		p.MinLength = MinLengthFloor
	}
	if p.MinLength > MinLengthCeiling {
		record("min_length", p.MinLength, MinLengthCeiling,
			fmt.Sprintf("above the %d-byte limit the hasher accepts, so no password could satisfy it", MinLengthCeiling))
		p.MinLength = MinLengthCeiling
	}

	if p.MaxAgeDays < 0 {
		record("max_age_days", p.MaxAgeDays, 0, "negative; read as no expiry")
		p.MaxAgeDays = 0
	}
	if p.MaxAgeDays > MaxAgeDaysCeiling {
		record("max_age_days", p.MaxAgeDays, MaxAgeDaysCeiling,
			fmt.Sprintf("above the %d-day sanity bound", MaxAgeDaysCeiling))
		p.MaxAgeDays = MaxAgeDaysCeiling
	}

	return p
}

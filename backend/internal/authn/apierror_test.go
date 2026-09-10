package authn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/api"
)

// P1-02 DoD item 4: validation errors match docs/PLAN/05's error schema exactly.
//
// Asserted on the serialized JSON rather than on the Go struct, because the
// schema is a statement about the wire format. A struct assertion would pass
// against a type whose json tags were wrong.
func TestValidationErrorMatchesTheSchema(t *testing.T) {
	violations := []Violation{
		{Rule: RuleMinLength, Message: "must be at least 12 characters"},
		{Rule: RuleRequireUppercase, Message: "must contain an uppercase letter"},
	}

	encoded, err := json.Marshal(ValidationError(violations))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details []struct {
				Field string `json:"field"`
				Issue string `json:"issue"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decoding: %v\n%s", err, encoded)
	}

	if got.Error.Code != string(api.VALIDATIONERROR) {
		t.Errorf("code = %q, want %q", got.Error.Code, api.VALIDATIONERROR)
	}
	if got.Error.Message == "" {
		t.Error("message is empty; docs/PLAN/05 requires one")
	}

	// One entry per violation, all against the password field so docs/UI-UX/15 can
	// render them under the input rather than in a banner.
	if len(got.Error.Details) != len(violations) {
		t.Fatalf("details has %d entries, want %d — every violation is reported at once, "+
			"because a form that reveals one problem per submit is a form the user fights",
			len(got.Error.Details), len(violations))
	}
	for i, d := range got.Error.Details {
		if d.Field != PasswordField {
			t.Errorf("details[%d].field = %q, want %q", i, d.Field, PasswordField)
		}
		if d.Issue != violations[i].Message {
			t.Errorf("details[%d].issue = %q, want %q", i, d.Issue, violations[i].Message)
		}
	}
}

// details[] is omitted rather than emitted empty when there is nothing to say.
// The schema says "omitted when there are none", and an empty array is a
// different document.
func TestValidationErrorOmitsEmptyDetails(t *testing.T) {
	encoded, err := json.Marshal(ValidationError(nil))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(encoded), "details") {
		t.Errorf("details is present with no violations: %s", encoded)
	}
}

// P1-02 step 6: the unauthenticated form discloses no policy detail.
//
// Not even the number of rules that failed — an empty details array would
// still carry a count, and the rules are identical for every user in the
// organization, so leaking them once leaks them for everyone.
func TestOpaqueValidationErrorDisclosesNothing(t *testing.T) {
	encoded, err := json.Marshal(OpaqueValidationError())
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	body := string(encoded)

	if strings.Contains(body, "details") {
		t.Errorf("the opaque error carries details: %s", body)
	}

	// Nothing that names a rule or a configured value.
	for _, leak := range []string{
		RuleMinLength, RuleRequireUppercase, RuleBreached,
		"characters", "uppercase", "breach", "12",
	} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
			t.Errorf("the opaque error mentions %q, which describes the policy: %s", leak, body)
		}
	}
}

// The two renderings must actually differ.
//
// A control: if OpaqueValidationError ever came to return the same document as
// ValidationError, every test above would still pass individually while the
// disclosure rule had silently stopped existing.
func TestTheTwoRenderingsAreNotTheSame(t *testing.T) {
	detailed, _ := json.Marshal(ValidationError([]Violation{
		{Rule: RuleBreached, Message: "this password has appeared in a known data breach and cannot be used"},
	}))
	opaque, _ := json.Marshal(OpaqueValidationError())

	if string(detailed) == string(opaque) {
		t.Error("the authenticated and unauthenticated renderings are identical; " +
			"one of them is wrong and the disclosure rule is not being applied")
	}
	if len(detailed) <= len(opaque) {
		t.Errorf("the detailed rendering is not longer than the opaque one:\n  %s\n  %s", detailed, opaque)
	}
}

// The breach rejection names neither the corpus nor how many times the
// password appears there. The user needs to know to choose something else, not
// the size of their mistake — and a count in an error message ends up in logs.
func TestTheBreachMessageCarriesNoCount(t *testing.T) {
	_, _, err := CheckBreach(t.Context(), stubChecker{breached: true}, "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	violations, _, _ := CheckBreach(t.Context(), stubChecker{breached: true}, "password")
	encoded, _ := json.Marshal(ValidationError(violations))
	body := strings.ToLower(string(encoded))

	for _, digit := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		if strings.Contains(body, digit) {
			t.Errorf("the breach message contains a digit, which may be an occurrence count: %s", encoded)
			break
		}
	}
	if strings.Contains(body, "pwned") || strings.Contains(body, "haveibeenpwned") {
		t.Errorf("the message names the corpus service: %s", encoded)
	}
}

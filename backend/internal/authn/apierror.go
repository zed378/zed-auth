package authn

import (
	"github.com/zed378/zed-auth/backend/internal/api"
)

// Turning violations into the wire format, and deciding how much to say.
//
// Two functions rather than one, because the right amount of detail is not a
// property of the violations — it is a property of who is asking. P1-02 step 6
// draws the line: full detail on an authenticated form where the user is
// choosing a password, none at all on an unauthenticated path.
//
// Both exist here so the choice is made by picking a function name, which is
// visible in review, rather than by a boolean argument that is easy to pass
// wrong and impossible to notice.

// PasswordField is the JSON path violations are reported against.
//
// UI-UX/15 § Error Presentation maps details[].field back to a form field, so
// this string is what makes the errors render under the password input rather
// than in a banner at the top of the form.
const PasswordField = "password"

// ValidationError renders violations in PLAN/05's error envelope, with one
// details[] entry per unmet rule.
//
// **For authenticated password-set paths only** — a change form, a reset with
// a valid token, an administrator creating a user. The caller has already
// established who is asking, and that person is choosing a password and needs
// to know precisely why theirs was refused.
//
// Every violation appears, sharing the same field. That is deliberate and
// matches UI-UX/15: a form that reveals one problem per submit is a form the
// user fights, and each round trip is one we pay for.
func ValidationError(violations []Violation) api.Error {
	var e api.Error
	e.Error.Code = api.VALIDATIONERROR
	e.Error.Message = "The password does not meet this organization's policy"

	if len(violations) == 0 {
		return e
	}

	details := make([]api.ErrorDetail, 0, len(violations))
	for _, v := range violations {
		details = append(details, api.ErrorDetail{
			Field: PasswordField,
			Issue: v.Message,
		})
	}
	e.Error.Details = &details

	return e
}

// OpaqueValidationError says a password was refused and nothing else.
//
// For any path where the caller is not authenticated. Policy detail there
// tells an anonymous caller the composition rules for a tenant, which narrows
// a credential-stuffing search space for free (SECURITY/02 §12) — and the
// rules are the same for every user in the organization, so leaking them once
// leaks them for everyone.
//
// No details[] at all, rather than a redacted one: an empty array would still
// carry the count of violated rules, which is itself a signal about the policy.
func OpaqueValidationError() api.Error {
	var e api.Error
	e.Error.Code = api.VALIDATIONERROR
	e.Error.Message = "That password cannot be used"
	return e
}

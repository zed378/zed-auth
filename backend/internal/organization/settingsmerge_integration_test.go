//go:build integration

// Merging `settings` on PATCH (P2-14).
//
// `openapi/openapi.yaml` promises: "`settings` is merged key by key, so an
// update naming one setting leaves the rest as they were."
//
// That promise was false for the one nested object in the document. `jsonb ||
// jsonb` is a SHALLOW merge, so `{"password_policy": {"min_length": 16}}`
// replaced the whole `password_policy` object and discarded
// `require_uppercase` and `max_age_days` — silently, and specifically for the
// two settings whose absence weakens the policy rather than breaking it.
package organization

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/management"
)

// settingsOf reads an organization's stored settings back through the API.
func (e *endpoints) settingsOf(t *testing.T, orgID string) map[string]any {
	t.Helper()

	w := e.call(t, http.MethodGet, "/v1/organizations/"+orgID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("reading the organization answered %d:\n%s", w.Code, w.Body.String())
	}

	var body struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the organization is not JSON: %s", w.Body.String())
	}
	return body.Settings
}

// Updating one field of the password policy leaves the other two alone.
//
// The failure this guards against is not an outage. An administrator raising
// `min_length` from 12 to 16 — an unambiguous tightening — would have silently
// cleared `require_uppercase: false` back to the instance default and dropped
// a deliberate `max_age_days: 0`. Nothing reports it, and the policy in force
// afterwards is one nobody chose.
func TestUpdatingOnePasswordRuleLeavesTheOthers(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	// A deliberate, non-default starting policy: uppercase not required, and
	// passwords that never expire. Both are choices NIST supports and both are
	// the opposite of the instance default.
	first := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings": {"password_policy": {"min_length": 14, "require_uppercase": false, "max_age_days": 0}}}`)
	if first.Code != http.StatusOK {
		t.Fatalf("setting the initial policy answered %d:\n%s", first.Code, first.Body.String())
	}

	// Now tighten exactly one rule.
	second := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings": {"password_policy": {"min_length": 16}}}`)
	if second.Code != http.StatusOK {
		t.Fatalf("raising min_length answered %d:\n%s", second.Code, second.Body.String())
	}

	policy, ok := e.settingsOf(t, e.homeOrg)["password_policy"].(map[string]any)
	if !ok {
		t.Fatalf("password_policy is missing from the stored settings")
	}

	if got := policy["min_length"]; got != float64(16) {
		t.Errorf("min_length is %v, not the 16 that was just set", got)
	}
	if _, present := policy["require_uppercase"]; !present {
		t.Error("require_uppercase was discarded by an update that never mentioned it")
	} else if policy["require_uppercase"] != false {
		t.Errorf("require_uppercase is %v, not the false it was set to", policy["require_uppercase"])
	}
	if _, present := policy["max_age_days"]; !present {
		t.Error("max_age_days was discarded by an update that never mentioned it")
	} else if policy["max_age_days"] != float64(0) {
		t.Errorf("max_age_days is %v, not the 0 it was set to", policy["max_age_days"])
	}
}

// A top-level setting still replaces rather than merges where it is not an object.
//
// The deep merge must not become an array merge. `allowed_login_methods` is a
// SET of what is permitted, so an update naming `["password"]` means exactly
// that — a concatenating merge would make it impossible to ever remove a
// method, which is the one direction that matters for a security control.
func TestReplacingAListReplacesItRatherThanAppending(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings": {"allowed_login_methods": ["password"]}}`)

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings": {"allowed_login_methods": ["password"]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("setting login methods answered %d:\n%s", w.Code, w.Body.String())
	}

	methods, ok := e.settingsOf(t, e.homeOrg)["allowed_login_methods"].([]any)
	if !ok {
		t.Fatalf("allowed_login_methods is missing or not a list")
	}
	if len(methods) != 1 {
		t.Errorf("a list-valued setting was merged rather than replaced: %v", methods)
	}
}

// An update that names nothing under `settings` changes nothing.
func TestAnEmptySettingsPatchChangesNothing(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings": {"session_lifetime_hours": 48}}`)
	before := e.settingsOf(t, e.homeOrg)

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg, `{"settings": {}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("an empty settings patch answered %d:\n%s", w.Code, w.Body.String())
	}

	after := e.settingsOf(t, e.homeOrg)
	if after["session_lifetime_hours"] != before["session_lifetime_hours"] {
		t.Errorf("an empty patch changed session_lifetime_hours from %v to %v",
			before["session_lifetime_hours"], after["session_lifetime_hours"])
	}
	if _, present := after["password_policy"]; !present {
		t.Error("an empty patch discarded password_policy")
	}
}

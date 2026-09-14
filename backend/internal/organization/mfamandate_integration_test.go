//go:build integration

// The MFA mandate through the real endpoint (P3-07).
//
// The unit tests prove the stamping rules and the transition logic in
// isolation. What only this level can show is that they are CONNECTED — and
// that is not a formality here: a review of this task found the enable and
// disable events defined, `MandateChange` tested, and nothing emitting them.
// Every unit test was green and the requirement was unmet.
package organization

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/management"
)

func (e *endpoints) mandateEvents(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	e.factory.QueryRow(&n,
		`SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`, e.homeOrg, eventType)
	return n
}

func (e *endpoints) storedSettings(t *testing.T) json.RawMessage {
	t.Helper()
	var raw string
	e.factory.QueryRow(&raw, `SELECT settings::text FROM organizations WHERE id = $1`, e.homeOrg)
	return json.RawMessage(raw)
}

// --- the events ---------------------------------------------------------------------

// Turning the mandate on writes its own event, findable by type.
func TestEnablingTheMandateIsAuditedByType(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body.String())
	}

	if got := e.mandateEvents(t, "organization.mfa_required.enabled"); got != 1 {
		t.Errorf("found %d enable events, want 1 — an incident reviewer searching by type finds nothing", got)
	}
}

// Turning it OFF writes its own event too — the one that matters most.
func TestDisablingTheMandateIsAuditedByType(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true}}`); w.Code != http.StatusOK {
		t.Fatalf("enabling = %d: %s", w.Code, w.Body.String())
	}

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":false}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("disabling = %d: %s", w.Code, w.Body.String())
	}

	if got := e.mandateEvents(t, "organization.mfa_required.disabled"); got != 1 {
		t.Errorf("found %d disable events, want 1 — somebody removed a security control and it is not findable", got)
	}
}

// Restating the mandate writes no mandate event: nothing happened.
func TestRestatingTheMandateIsNotAudited(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	for i := 0; i < 3; i++ {
		if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
			`{"settings":{"mfa_required":true}}`); w.Code != http.StatusOK {
			t.Fatalf("PATCH %d = %d: %s", i, w.Code, w.Body.String())
		}
	}

	if got := e.mandateEvents(t, "organization.mfa_required.enabled"); got != 1 {
		t.Errorf("three identical PATCHes wrote %d enable events, want 1", got)
	}
}

// --- the stamp, end to end ------------------------------------------------------------

// The activation time is stored, and is the service's clock rather than anything
// the caller sent.
func TestTheActivationTimeIsStampedByTheService(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	before := time.Now().Add(-time.Minute)
	if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true}}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body.String())
	}
	after := time.Now().Add(time.Minute)

	policy, _ := authn.ParseLoginPolicy(e.storedSettings(t))
	if !policy.MFARequired {
		t.Fatal("the mandate was not stored")
	}
	if policy.MFARequiredSince.Before(before) || policy.MFARequiredSince.After(after) {
		t.Errorf("mfa_required_since = %s, want the moment of the PATCH", policy.MFARequiredSince)
	}
}

// A caller cannot backdate the grace. Abuse case A-5, through the real endpoint.
func TestACallerCannotBackdateTheGrace(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true,"mfa_required_since":"2000-01-01T00:00:00Z"}}`)

	if w.Code == http.StatusOK {
		t.Fatal("a caller supplied the activation time; the grace could be backdated to zero")
	}

	// And nothing was stored — the refusal is not a partial success.
	policy, _ := authn.ParseLoginPolicy(e.storedSettings(t))
	if policy.MFARequired {
		t.Error("the mandate was enabled by a request that was refused")
	}
}

// An edit that restates the mandate does not move the deadline.
func TestAnUnrelatedEditDoesNotMoveTheDeadline(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true}}`); w.Code != http.StatusOK {
		t.Fatalf("enabling = %d: %s", w.Code, w.Body.String())
	}
	first, _ := authn.ParseLoginPolicy(e.storedSettings(t))

	if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true,"session_lifetime_hours":8}}`); w.Code != http.StatusOK {
		t.Fatalf("editing = %d: %s", w.Code, w.Body.String())
	}
	second, _ := authn.ParseLoginPolicy(e.storedSettings(t))

	if !second.MFARequiredSince.Equal(first.MFARequiredSince) {
		t.Errorf("the deadline moved from %s to %s on an unrelated edit",
			first.MFARequiredSince, second.MFARequiredSince)
	}
}

// --- the impact -----------------------------------------------------------------------------

// The impact counts members without a factor, and says nothing about who.
func TestTheImpactCountsWithoutNaming(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgAdmin, e.homeOrg)

	// Two more members; one with an active factor.
	enrolled := e.factory.User(e.homeOrg, "enrolled@example.test")
	e.factory.User(e.homeOrg, "bare@example.test")
	e.factory.Exec(`INSERT INTO user_mfa_factors (user_id, org_id, type, status, secret_encrypted)
	                VALUES ($1, $2, 'totp', 'active', '\x01')`, enrolled, e.homeOrg)

	w := e.call(t, http.MethodGet, "/v1/organizations/"+e.homeOrg+"/mfa-impact", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if body["members"] != float64(3) {
		t.Errorf("members = %v, want 3", body["members"])
	}
	if body["without_factor"] != float64(2) {
		t.Errorf("without_factor = %v, want 2", body["without_factor"])
	}

	// Counts, never names. The body must not be a list of the accounts a
	// stolen password would be enough for.
	for _, leak := range []string{"bare@example.test", "enrolled@example.test", e.userID} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("the impact response names %q:\n%s", leak, w.Body.String())
		}
	}
}

// A deactivated member is not counted: a policy about signing in cannot affect
// somebody who cannot sign in.
func TestTheImpactIgnoresDeactivatedMembers(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgAdmin, e.homeOrg)

	gone := e.factory.User(e.homeOrg, "gone@example.test")
	e.factory.Exec(`UPDATE users SET status = 'deactivated' WHERE id = $1`, gone)

	w := e.call(t, http.MethodGet, "/v1/organizations/"+e.homeOrg+"/mfa-impact", "")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	if body["members"] != float64(1) {
		t.Errorf("members = %v, want 1 — a deactivated user inflates the number being decided on", body["members"])
	}
}

// The grace deadline is reported once the mandate is on, and absent before.
func TestTheImpactReportsTheDeadlineOnlyWhenThereIsOne(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.OrgOwner, e.homeOrg)

	w := e.call(t, http.MethodGet, "/v1/organizations/"+e.homeOrg+"/mfa-impact", "")
	var before map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &before)
	if before["grace_ends_at"] != nil {
		t.Errorf("a deadline was reported for a mandate nobody enabled: %v", before["grace_ends_at"])
	}

	if w := e.call(t, http.MethodPatch, "/v1/organizations/"+e.homeOrg,
		`{"settings":{"mfa_required":true}}`); w.Code != http.StatusOK {
		t.Fatalf("enabling = %d: %s", w.Code, w.Body.String())
	}

	w = e.call(t, http.MethodGet, "/v1/organizations/"+e.homeOrg+"/mfa-impact", "")
	var after map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &after)
	if after["grace_ends_at"] == nil {
		t.Error("no deadline was reported for an enabled mandate")
	}
	if after["mfa_required"] != true {
		t.Errorf("mfa_required = %v, want true", after["mfa_required"])
	}
	if after["grace_period_days"] != float64(authn.MFAGracePeriod/(24*time.Hour)) {
		t.Errorf("grace_period_days = %v, want the service's own grace", after["grace_period_days"])
	}
}

// --- created with the mandate already on (P3-13) --------------------------------------

// An organization created with `mfa_required: true` is stamped, so its grace
// ends. Before P3-13 creation skipped the stamp, and a missing stamp reads as
// "inside the grace" for ever: the setting said MFA was required and nobody was
// ever asked for it.
func TestAnOrganizationCreatedWithTheMandateIsStampedAndAudited(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	before := time.Now().Add(-time.Minute)
	w := e.call(t, http.MethodPost, "/v1/organizations",
		`{"name":"Mandated","settings":{"mfa_required":true}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	created := decodeOrg(t, w).Id.String()

	var raw string
	e.factory.QueryRow(&raw, `SELECT settings::text FROM organizations WHERE id = $1`, created)
	policy, _ := authn.ParseLoginPolicy(json.RawMessage(raw))
	if policy.MFARequiredSince.Before(before) {
		t.Fatalf("mfa_required_since = %s — the mandate is on and its grace never ends", policy.MFARequiredSince)
	}
	if got := authn.RequireMFA(policy, false, policy.MFARequiredSince.Add(authn.MFAGracePeriod)); got != authn.MFAEnrolmentRequired {
		t.Errorf("at the deadline RequireMFA = %v, want enrolment required", got)
	}

	var events int
	e.factory.QueryRow(&events, `SELECT count(*) FROM events WHERE org_id = $1 AND event_type = $2`,
		created, "organization.mfa_required.enabled")
	if events != 1 {
		t.Errorf("found %d enable events for the new organization, want 1", events)
	}
}

// Creating without the mandate stamps nothing and audits no enablement.
func TestAnOrganizationCreatedWithoutTheMandateIsNotStamped(t *testing.T) {
	e := setupEndpoints(t)
	e.grant(management.InstanceOwner, e.instance)

	w := e.call(t, http.MethodPost, "/v1/organizations",
		`{"name":"Relaxed","settings":{"session_lifetime_hours":8}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	created := decodeOrg(t, w).Id.String()

	var raw string
	e.factory.QueryRow(&raw, `SELECT settings::text FROM organizations WHERE id = $1`, created)
	if strings.Contains(raw, "mfa_required_since") {
		t.Errorf("settings = %s — a deadline for a mandate nobody enabled", raw)
	}
}

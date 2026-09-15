//go:build integration

// The administrator-assisted MFA reset through the real /v1 chain (P3-14).
//
// P3-04 tested the reset as a function (`mfa.AdminReset`) and its runbook as a
// document. The endpoint itself — who may call it, on whom, and what it leaves
// in the audit log — had never been driven over HTTP. It is the most powerful
// credential operation an organization administrator has: it returns an
// account to "password alone", so the refusals matter more than the success.
package user

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/mfa"
)

// enrolledMember creates a member of orgID holding one active factor and three
// unspent recovery codes.
func (f *fixture) enrolledMember(t *testing.T, orgID, email string) string {
	t.Helper()
	id := f.factory.User(orgID, email)
	f.factory.Exec(`INSERT INTO user_mfa_factors (user_id, org_id, type, status, secret_encrypted)
	                VALUES ($1, $2, 'totp', 'active', '\x01')`, id, orgID)
	var batch string
	f.factory.QueryRow(&batch, `SELECT gen_random_uuid()::text`)
	for i := 0; i < 3; i++ {
		f.factory.Exec(`INSERT INTO user_recovery_codes (user_id, org_id, batch_id, code_hash)
		                VALUES ($1, $2, $3, sha256(gen_random_uuid()::text::bytea))`, id, orgID, batch)
	}
	return id
}

func (f *fixture) credentialsHeld(t *testing.T, userID string) (factors, codes int) {
	t.Helper()
	f.factory.QueryRow(&factors, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1`, userID)
	f.factory.QueryRow(&codes, `SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, userID)
	return factors, codes
}

func (f *fixture) resetPath(orgID, userID string) string {
	return f.users(orgID) + "/" + userID + "/mfa-reset"
}

func (f *fixture) withMfaReset() {
	f.api.MFA = &mfa.AdminReset{Factors: &mfa.Store{}, Recovery: mfa.NewRecoveryStore()}
}

func TestAnAdministratorResetDestroysEveryFactorAndCodeAndIsAudited(t *testing.T) {
	f := setup(t)
	f.withMfaReset()
	f.grant(management.OrgAdmin, f.orgA)
	member := f.enrolledMember(t, f.orgA, "locked-out@example.test")

	w := mustStatus(t, f.call(t, http.MethodPost, f.resetPath(f.orgA, member), ""), http.StatusOK)

	var body struct {
		FactorsRemoved       int `json:"factors_removed"`
		RecoveryCodesRemoved int `json:"recovery_codes_removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body.FactorsRemoved != 1 || body.RecoveryCodesRemoved != 3 {
		t.Errorf("reported %+v, want 1 factor and 3 codes", body)
	}
	if factors, codes := f.credentialsHeld(t, member); factors != 0 || codes != 0 {
		t.Errorf("the member still holds %d factors and %d codes", factors, codes)
	}

	// No credential of any kind in the response: an administrator who could
	// read one out could take the account over.
	var raw map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if len(raw) != 2 {
		t.Errorf("the response carries %d fields, want exactly the two counts: %s", len(raw), w.Body.String())
	}

	// Audited, naming the administrator. The event is what an incident review
	// of a takeover through this path starts from.
	var events int
	f.factory.QueryRow(&events,
		`SELECT count(*) FROM events WHERE event_type = $1 AND org_id = $2 AND actor_user_id = $3
		   AND payload->>'user_id' = $4`,
		string(audit.EventMFAResetByAdmin), f.orgA, f.userID, member)
	if events != 1 {
		t.Errorf("found %d reset events naming the administrator and the member, want 1", events)
	}
}

// Abuse: a caller with no administrative role.
func TestAMemberCannotResetAnotherMembersFactors(t *testing.T) {
	f := setup(t)
	f.withMfaReset()
	victim := f.enrolledMember(t, f.orgA, "victim@example.test")

	w := f.call(t, http.MethodPost, f.resetPath(f.orgA, victim), "")
	if w.Code == http.StatusOK {
		t.Fatal("a caller with no administrative role reset somebody's factors")
	}
	if factors, codes := f.credentialsHeld(t, victim); factors != 1 || codes != 3 {
		t.Errorf("a refused reset changed the victim's credentials: %d factors, %d codes", factors, codes)
	}
}

// Abuse: an administrator of one organization reaching into another, by both
// routes — naming the other organization, and naming the other organization's
// user under their own.
func TestAnAdministratorCannotResetAcrossOrganizations(t *testing.T) {
	f := setup(t)
	f.withMfaReset()
	f.grant(management.OrgAdmin, f.orgA)
	elsewhere := f.enrolledMember(t, f.orgB, "elsewhere@example.test")

	for name, path := range map[string]string{
		"the other organization's route": f.resetPath(f.orgB, elsewhere),
		"their own organization's route": f.resetPath(f.orgA, elsewhere),
	} {
		if w := f.call(t, http.MethodPost, path, ""); w.Code == http.StatusOK {
			t.Errorf("%s: an administrator of another organization reset the member's factors", name)
		}
	}
	if factors, codes := f.credentialsHeld(t, elsewhere); factors != 1 || codes != 3 {
		t.Errorf("a refused cross-organization reset changed credentials: %d factors, %d codes", factors, codes)
	}
	var events int
	f.factory.QueryRow(&events, `SELECT count(*) FROM events WHERE event_type = $1`, string(audit.EventMFAResetByAdmin))
	if events != 0 {
		t.Errorf("a refused reset wrote %d reset events", events)
	}
}

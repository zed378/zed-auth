//go:build integration

package token

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/signing"
)

// A role assigned in Project A appears only in the Project A claim.
//
// `docs/PLAN/17`'s Phase 2 criterion, verbatim, and the reason roles are
// project-scoped at all (`docs/PLAN/08` Part A: "admin in Project A does not
// automatically become admin in Project B").
//
// The assertion that matters is the SECOND one — that the other project's
// claim does not carry the role. A test checking only that the granted role
// appears would pass against a service that puts every role in every token.
func TestARoleGrantedInOneProjectAppearsOnlyInThatProjectsToken(t *testing.T) {
	f := setup(t)

	// A second project and application in the same organization.
	var otherProject string
	f.factory.QueryRow(&otherProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'other') RETURNING id`, f.orgID)

	for _, p := range []string{f.projectID, otherProject} {
		f.factory.Exec(`
			INSERT INTO roles (org_id, project_id, key, display_name)
			VALUES ($1, $2, 'cashier', 'Cashier')`, f.orgID, p)
	}

	// Granted in the FIRST project only.
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		f.userID, f.projectID, f.orgID, pq.Array([]string{"cashier"}))

	claims := f.claimsFromToken(t, f.exchangeCode(t))

	roles, ok := claims[RoleClaimNamespace(f.projectID)].(map[string]any)
	if !ok {
		t.Fatalf("no role claim for the granted project: %#v", claims[RoleClaimNamespace(f.projectID)])
	}
	if _, held := roles["cashier"]; !held {
		t.Errorf("the granted role is missing: %#v", roles)
	}

	// The half that matters: the other project's namespace is not in this
	// token at all, because a token is issued for ONE client and carries only
	// that client's project.
	if _, leaked := claims[RoleClaimNamespace(otherProject)]; leaked {
		t.Error("the token carries a claim for a project it was not issued for")
	}
}

// A user holding the same role key in two projects gets it once, scoped to the
// project whose client asked — not twice, and not merged.
func TestTheSameRoleKeyInTwoProjectsDoesNotLeakAcross(t *testing.T) {
	f := setup(t)

	var otherProject string
	f.factory.QueryRow(&otherProject,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'other') RETURNING id`, f.orgID)

	for _, p := range []string{f.projectID, otherProject} {
		f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name)
			VALUES ($1, $2, 'admin', 'Admin')`, f.orgID, p)
	}

	// `admin` in the OTHER project only.
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		f.userID, otherProject, f.orgID, pq.Array([]string{"admin"}))

	claims := f.claimsFromToken(t, f.exchangeCode(t))
	roles, _ := claims[RoleClaimNamespace(f.projectID)].(map[string]any)

	if _, leaked := roles["admin"]; leaked {
		t.Error("a role held in another project appeared in this project's claim")
	}
	if len(roles) != 0 {
		t.Errorf("the claim carries %v", roles)
	}
}

// Administrative roles come from `manager_roles`, which is NOT tenant-scoped —
// so the read has to work even though everything else in the request is.
func TestManagerRolesReachTheToken(t *testing.T) {
	f := setup(t)

	f.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`,
		f.userID, f.orgID)

	claims := f.claimsFromToken(t, f.exchangeCode(t))

	encoded, err := json.Marshal(claims[ManagerRoleClaim])
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(encoded) != `["ORG_ADMIN"]` {
		t.Errorf("manager_roles claim is %s", encoded)
	}
}

// The claims are a snapshot: a role granted after a session began still
// reaches the next token, because they are read at issuance rather than
// carried forward from the session.
//
// The inverse — a revocation not reaching an already-issued token — is the
// snapshot's cost, and is why `docs/PLAN/08` says to prefer a real-time check
// for sensitive actions. That half is `P2-06`'s to assert.
func TestARoleGrantedAfterLoginReachesTheNextToken(t *testing.T) {
	f := setup(t)

	first := f.claimsFromToken(t, f.exchangeCode(t))
	if roles, _ := first[RoleClaimNamespace(f.projectID)].(map[string]any); len(roles) != 0 {
		t.Fatalf("the user started with roles: %v", roles)
	}

	f.factory.Exec(`INSERT INTO roles (org_id, project_id, key, display_name)
		VALUES ($1, $2, 'late', 'Late')`, f.orgID, f.projectID)
	f.factory.Exec(`
		INSERT INTO user_grants (user_id, project_id, org_id, role_keys)
		VALUES ($1, $2, $3, $4)`,
		f.userID, f.projectID, f.orgID, pq.Array([]string{"late"}))

	second := f.claimsFromToken(t, f.exchangeCode(t))
	roles, _ := second[RoleClaimNamespace(f.projectID)].(map[string]any)
	if _, held := roles["late"]; !held {
		t.Errorf("a role granted after login did not reach the next token: %v", roles)
	}
}

// --- helpers ----------------------------------------------------------------

// exchangeCode runs a real authorization code exchange through the HTTP
// handler and returns the access token, so these assertions are about what a
// consumer actually receives rather than about a struct.
func (f fixture) exchangeCode(t *testing.T) string {
	t.Helper()

	rec := f.exchange(t, f.issueCode(t, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("exchanging the code: %d %s", rec.Code, rec.Body)
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatal("no access token in the response")
	}
	return out.AccessToken
}

// claimsFromToken verifies the token the way a consumer would — through the
// real verifier, against the real key set — and returns its claims.
func (f fixture) claimsFromToken(t *testing.T, raw string) map[string]any {
	t.Helper()

	verifier := signing.NewVerifier(f.keys)
	payload, err := verifier.Verify(raw, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("verifying the token: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decoding the claims: %v", err)
	}
	return claims
}

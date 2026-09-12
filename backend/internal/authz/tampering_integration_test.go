//go:build integration

// Claim tampering (P2-16 step 4).
//
// Every test here presents a token that is **validly signed by this service's
// own key** and whose claims say something the database does not. That is not
// a forgery an outsider can produce — it is what happens if the claim assembly
// is ever wrong, if a token is minted through a path that skips a check, or if
// a key is used to issue something it should not have.
//
// The property being asserted is the one `internal/management/store.go` states
// in its opening comment: **roles are read from the database on every request,
// never from the token.** A test that only ever presents honest tokens cannot
// tell that promise from a coincidence.
package authz

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/signing"
)

// forge mints a properly signed access token whose claims have been edited.
//
// The signature is real, so nothing downstream can refuse it on that basis —
// which is the point. What must refuse it is the authorization layer, by
// looking somewhere other than the token.
func (f *fixture) forge(t *testing.T, userID, orgID string, extra map[string]any) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: orgID, UserID: userID, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	for key, value := range extra {
		claims[key] = value
	}

	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	signed, err := f.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

// A forged manager-role claim buys nothing on the Management API.
//
// `INSTANCE_OWNER` is the most powerful thing this service has. A caller who
// holds no `manager_roles` row at all presents a token asserting it, and every
// administrative route must still refuse — because the middleware reads the
// table rather than the claim.
func TestAForgedManagerRoleClaimIsIgnored(t *testing.T) {
	f := setup(t)

	tampered := f.forge(t, f.subject, f.orgA, map[string]any{
		token.ManagerRoleClaim: []string{"INSTANCE_OWNER"},
	})

	// A route only an INSTANCE_OWNER can reach.
	rec := f.request(t, http.MethodGet, "/v1/organizations", "", tampered)
	if rec.Code == http.StatusOK {
		t.Errorf("a forged INSTANCE_OWNER claim listed every organization:\n%s", rec.Body.String())
	}
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Errorf("expected a refusal, got %d:\n%s", rec.Code, rec.Body.String())
	}

	// And an organization-scoped route in an organization the caller is not in.
	rec = f.request(t, http.MethodGet, "/v1/organizations/"+f.orgB, "", tampered)
	if rec.Code == http.StatusOK {
		t.Errorf("a forged INSTANCE_OWNER claim read another organization:\n%s", rec.Body.String())
	}
	// **The positive control.** Everything above would also pass against a
	// route that is simply broken, or a token nothing accepts at all. Granting
	// the real row and presenting an ORDINARY token must now succeed — which
	// is what makes the refusals above statements about the forgery rather
	// than about the endpoint.
	f.factory.Exec(
		`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'INSTANCE_OWNER', $2)`,
		f.subject, f.orgA)

	honest := f.forge(t, f.subject, f.orgA, nil)
	if rec := f.request(t, http.MethodGet, "/v1/organizations", "", honest); rec.Code != http.StatusOK {
		t.Fatalf("a real INSTANCE_OWNER could not list organizations either (%d) — "+
			"so the refusals above prove nothing about the forged claim:\n%s",
			rec.Code, rec.Body.String())
	}
}

// A forged project role claim does not make an authorization check pass.
//
// `/v1/authz/check` exists precisely because the token's claims are a snapshot.
// A decision that read them would be answering the question the caller asked
// it to answer.
func TestAForgedProjectRoleClaimDoesNotDecideACheck(t *testing.T) {
	f := setup(t)

	// The subject genuinely holds nothing.
	honest := f.check(t, f.subject, "approve", "purchase_request")
	if honest.Allowed {
		t.Fatalf("the subject already holds this permission; the test proves nothing")
	}

	tampered := f.forge(t, f.subject, f.orgA, map[string]any{
		token.RoleClaimNamespace(f.projectA): map[string]any{
			"finance_approver": map[string]any{"org_id": f.orgA},
		},
	})

	rec := f.request(t, http.MethodPost, "/v1/authz/check",
		`{"subject":{"user_id":"`+f.subject+`"},"action":"approve","resource":{"type":"purchase_request"}}`,
		tampered)

	if rec.Code != http.StatusOK {
		t.Fatalf("the check answered %d:\n%s", rec.Code, rec.Body.String())
	}

	var out decision
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	if out.Allowed {
		t.Error("a role claim written into the token decided the check — " +
			"the decision is reading the token rather than the grant table")
	}
}

// A token naming another organization does not act in it.
//
// `org_id` is a claim like any other. The tenant a request acts in comes from
// the client the token was issued to (ADR-023), and a token whose `org_id` was
// edited must not move the caller into a tenant they were never in.
func TestAnEditedOrgClaimDoesNotMoveTheCaller(t *testing.T) {
	f := setup(t)

	// Give the caller a real ORG_ADMIN grant over their OWN organization, so
	// the only thing being tested is whether the claim moves them.
	f.factory.Exec(
		`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, 'ORG_ADMIN', $2)`,
		f.subject, f.orgA)

	// Honest token: they can read their own organization.
	honest := f.forge(t, f.subject, f.orgA, nil)
	if rec := f.request(t, http.MethodGet, "/v1/organizations/"+f.orgA, "", honest); rec.Code != http.StatusOK {
		t.Fatalf("an ORG_ADMIN cannot read their own organization (%d):\n%s", rec.Code, rec.Body.String())
	}

	// Tampered: the same user, claiming to belong to organization B.
	tampered := f.forge(t, f.subject, f.orgB, nil)
	rec := f.request(t, http.MethodGet, "/v1/organizations/"+f.orgB, "", tampered)

	if rec.Code == http.StatusOK {
		t.Errorf("editing org_id moved the caller into another tenant:\n%s", rec.Body.String())
	}
}

// An ID token is not an access token, however well it verifies.
//
// It is signed by the same key, carries the same audience, and is handed to
// the browser — so a consumer that checks only the signature accepts a
// credential issued for a different purpose. `typ` is what separates them.
func TestAnIDTokenIsNotAcceptedAsAnAccessToken(t *testing.T) {
	f := setup(t)

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: f.orgA, UserID: f.subject, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	// The same payload, signed as an ID token — `typ: JWT` rather than
	// `typ: at+jwt`, which is the only thing separating the two.
	asIDToken, err := f.signer.SignWithType(payload, signing.TypeJWT)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	rec := f.request(t, http.MethodGet, "/v1/organizations/"+f.orgA, "", asIDToken)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an ID token was accepted on the Management API (%d):\n%s",
			rec.Code, rec.Body.String())
	}
}

// A token with no subject cannot reach an authenticated route.
//
// The management role read refuses an empty subject explicitly rather than
// running a query that matches nothing — because a query matching nothing
// reads exactly like a caller who holds no roles, which is a caller the
// membership shortcut would then treat as a MEMBER.
func TestATokenWithNoSubjectIsRefused(t *testing.T) {
	f := setup(t)

	tampered := f.forge(t, f.subject, f.orgA, map[string]any{"sub": ""})

	rec := f.request(t, http.MethodGet, "/v1/organizations/"+f.orgA, "", tampered)
	if rec.Code == http.StatusOK {
		t.Errorf("a token with no subject was authorized:\n%s", rec.Body.String())
	}
}

// The refusals above are indistinguishable from one another.
//
// A tampered token that produced a DIFFERENT status or body from an ordinary
// unauthorized one would tell whoever is probing which of their forgeries got
// further — which is a map of the authorization layer, drawn one request at a
// time.
func TestTamperedAndOrdinaryRefusalsLookTheSame(t *testing.T) {
	f := setup(t)

	ordinary := f.request(t, http.MethodGet, "/v1/organizations/"+f.orgB, "", f.token)

	tampered := f.forge(t, f.subject, f.orgA, map[string]any{
		token.ManagerRoleClaim: []string{"INSTANCE_OWNER"},
	})
	forged := f.request(t, http.MethodGet, "/v1/organizations/"+f.orgB, "", tampered)

	if ordinary.Code != forged.Code {
		t.Errorf("an ordinary refusal is %d and a forged-claim refusal is %d — "+
			"the difference says the forgery reached further",
			ordinary.Code, forged.Code)
	}
	if strings.TrimSpace(ordinary.Body.String()) != strings.TrimSpace(forged.Body.String()) {
		t.Errorf("the two refusals differ in body:\n ordinary: %s\n forged:   %s",
			ordinary.Body.String(), forged.Body.String())
	}
}

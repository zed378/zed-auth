package token

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The claim format is a contract consumer applications build against, and
// changing it later breaks every one of them at once. So it is asserted
// against the literal JSON `docs/PLAN/08` Part A prints, rather than against a
// Go structure that happens to marshal into something similar.
func TestTheRoleClaimMatchesThePlansJSONExactly(t *testing.T) {
	const project = "proj_pos"
	const org = "org_acme"

	claims, err := AccessTokenClaims(Subject{
		Issuer: "https://auth.example.test", Audience: "https://auth.example.test",
		ClientID: "client", ProjectID: project, OrgID: org, UserID: "user",
		RoleKeys: []string{"cashier", "manager"},
	}, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("claims: %v", err)
	}

	encoded, err := json.Marshal(claims[RoleClaimNamespace(project)])
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	// Exactly docs/PLAN/08 Part A's example, with its own key order.
	const want = `{"cashier":{"org_id":"org_acme"},"manager":{"org_id":"org_acme"}}`
	if string(encoded) != want {
		t.Errorf("the role claim is\n  %s\nwant\n  %s", encoded, want)
	}

	if key := RoleClaimNamespace(project); key != "urn:authservice:iam:org:project:proj_pos:roles" {
		t.Errorf("the claim key is %q", key)
	}
}

// `org_id` inside each value looks redundant today, because in Phase 2 a role
// can only come from one organizational context. `docs/PLAN/08` Part A says it
// is there so Phase 4's delegation can be told apart — and adding a field to a
// claim consumers already parse is a breaking change for every one of them.
//
// So the test asserts the redundant-looking field specifically, because that is
// the one somebody will be tempted to remove.
func TestEveryRoleValueCarriesItsOrganization(t *testing.T) {
	claims, err := AccessTokenClaims(Subject{
		Issuer: "iss", Audience: "iss", ClientID: "c", ProjectID: "p", OrgID: "the-org",
		UserID: "u", RoleKeys: []string{"a", "b", "c"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}

	roles, ok := claims[RoleClaimNamespace("p")].(map[string]any)
	if !ok {
		t.Fatalf("the role claim is %T", claims[RoleClaimNamespace("p")])
	}
	if len(roles) != 3 {
		t.Fatalf("%d roles", len(roles))
	}
	for key, value := range roles {
		inner, ok := value.(map[string]any)
		if !ok {
			t.Errorf("%s is %T, not an object", key, value)
			continue
		}
		if inner["org_id"] != "the-org" {
			t.Errorf("%s carries org_id %v", key, inner["org_id"])
		}
	}
}

// A user with no roles still gets the claim, empty.
//
// `P1-07` reserved it for exactly this: the token's shape does not change when
// somebody is granted their first role, so a consumer written before roles
// existed keeps working and one that would crash on a missing key never gets
// written.
func TestTheRoleClaimIsPresentEvenWithNoRoles(t *testing.T) {
	claims, err := AccessTokenClaims(Subject{
		Issuer: "iss", Audience: "iss", ClientID: "c", ProjectID: "p", OrgID: "o", UserID: "u",
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}

	value, present := claims[RoleClaimNamespace("p")]
	if !present {
		t.Fatal("the role claim is absent for a user with no roles")
	}
	roles, ok := value.(map[string]any)
	if !ok || len(roles) != 0 {
		t.Errorf("the empty claim is %#v", value)
	}
}

// Administrative roles are flat and project-independent (docs/PLAN/08 Part C),
// and ABSENT when there are none.
//
// An empty array would be a claim asserting "this user is administratively
// nothing" — true, and 30 bytes in every token the service issues, for the
// overwhelming majority of users who are administratively nothing.
func TestManagerRolesAreFlatAndOmittedWhenEmpty(t *testing.T) {
	with, err := AccessTokenClaims(Subject{
		Issuer: "iss", Audience: "iss", ClientID: "c", ProjectID: "p", OrgID: "o", UserID: "u",
		ManagerRoles: []string{"ORG_ADMIN"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	encoded, _ := json.Marshal(with[ManagerRoleClaim])
	if string(encoded) != `["ORG_ADMIN"]` {
		t.Errorf("manager roles encoded as %s", encoded)
	}

	without, err := AccessTokenClaims(Subject{
		Issuer: "iss", Audience: "iss", ClientID: "c", ProjectID: "p", OrgID: "o", UserID: "u",
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if _, present := without[ManagerRoleClaim]; present {
		t.Error("a user with no administrative roles carries an empty manager_roles claim")
	}
}

// ADR-021: the claim is bounded, and truncated rather than dropped.
//
// A token lives in an Authorization header and most proxies cap headers at
// 8 KB, so an unbounded claim eventually stops working — at the consumer, in
// production, for whichever user accumulated the most roles. Truncation keeps
// the token a TRUE SUBSET: a consumer denying access it should have granted is
// recoverable, and one granting access it should not have is not.
func TestTheRoleClaimIsBoundedAndTruncatesRatherThanDrops(t *testing.T) {
	many := make([]string, MaxRoleClaims+10)
	for i := range many {
		many[i] = "role-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}

	claims, err := AccessTokenClaims(Subject{
		Issuer: "iss", Audience: "iss", ClientID: "c", ProjectID: "p", OrgID: "o", UserID: "u",
		RoleKeys: many,
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}

	roles := claims[RoleClaimNamespace("p")].(map[string]any)
	if len(roles) > MaxRoleClaims {
		t.Errorf("the claim carries %d roles, over the %d bound", len(roles), MaxRoleClaims)
	}
	if len(roles) == 0 {
		t.Error("the claim was dropped rather than truncated, which denies everything")
	}
	// Every role present must be one the user actually holds.
	held := map[string]bool{}
	for _, k := range many {
		held[k] = true
	}
	for key := range roles {
		if !held[key] {
			t.Errorf("the claim invented a role: %q", key)
		}
	}
}

// A role key containing JSON metacharacters cannot change the claim's shape —
// the abuse case the card names. It cannot, because the claim is built as a Go
// map and marshalled, never assembled as a string; this asserts that the
// property holds rather than that the code looks like it does.
func TestARoleKeyCannotInjectIntoTheClaim(t *testing.T) {
	nasty := `","admin":{"org_id":"other`

	claims, err := AccessTokenClaims(Subject{
		Issuer: "iss", Audience: "iss", ClientID: "c", ProjectID: "p", OrgID: "o", UserID: "u",
		RoleKeys: []string{nasty},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}

	roles := claims[RoleClaimNamespace("p")].(map[string]any)
	if len(roles) != 1 {
		t.Fatalf("%d roles came out of one", len(roles))
	}
	if _, forged := roles["admin"]; forged {
		t.Fatal("a role key created a second role")
	}

	encoded, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	// The dangerous characters survive as escaped text inside one key.
	if !strings.Contains(string(encoded), `\",\"admin\"`) {
		t.Errorf("the key was not escaped: %s", encoded)
	}
}

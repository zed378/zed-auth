package token

import (
	"testing"
	"time"
)

// The nested org_id names the organization whose project defines the role
// (P4-04). For a delegated grant that is the DELEGATING organization, not the
// one the token belongs to — `docs/PLAN/08` Part A put the field there for
// exactly this case, and a consumer that read the token's own organization
// would treat a partner's role as one of its own.
func TestADelegatedRoleClaimNamesTheDelegatingOrganization(t *testing.T) {
	base := Subject{
		Issuer: "https://auth.example.test", Audience: "https://auth.example.test",
		ClientID:  "11111111-1111-4111-8111-111111111111",
		OrgID:     "22222222-2222-4222-8222-222222222222",
		UserID:    "33333333-3333-4333-8333-333333333333",
		ProjectID: "44444444-4444-4444-8444-444444444444",
		RoleKeys:  []string{"cashier"},
	}
	delegating := "55555555-5555-4555-8555-555555555555"

	direct, err := AccessTokenClaims(base, time.Now())
	if err != nil {
		t.Fatalf("direct: %v", err)
	}
	delegated, err := AccessTokenClaims(func() Subject {
		s := base
		s.RoleOrgID = delegating
		return s
	}(), time.Now())
	if err != nil {
		t.Fatalf("delegated: %v", err)
	}

	orgOf := func(claims map[string]any) string {
		roles, ok := claims[RoleClaimNamespace(base.ProjectID)].(map[string]any)
		if !ok {
			t.Fatalf("no role claim in %v", claims)
		}
		value, ok := roles["cashier"].(map[string]any)
		if !ok {
			t.Fatalf("no cashier role in %v", roles)
		}
		return value["org_id"].(string)
	}

	if got := orgOf(direct); got != base.OrgID {
		t.Errorf("a direct grant claimed org_id %q, want the token's own organization", got)
	}
	if got := orgOf(delegated); got != delegating {
		t.Errorf("a delegated grant claimed org_id %q, want the delegating organization %q", got, delegating)
	}
}

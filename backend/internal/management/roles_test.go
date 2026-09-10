package management

import (
	"testing"
)

const (
	orgA     = "11111111-1111-1111-1111-111111111111"
	orgB     = "22222222-2222-2222-2222-222222222222"
	instance = "99999999-9999-9999-9999-999999999999"
	userID   = "44444444-4444-4444-4444-444444444444"
)

func caller(grants ...Grant) Caller {
	return Caller{UserID: userID, OrgID: orgA, Grants: grants}
}

// The hierarchy, exhaustively. Nine pairs is small enough to write out, and
// writing it out is what catches an inheritance edge nobody meant.
func TestTheRoleHierarchy(t *testing.T) {
	roles := []Role{InstanceOwner, OrgOwner, OrgAdmin}

	want := map[Role]map[Role]bool{
		InstanceOwner: {InstanceOwner: true, OrgOwner: true, OrgAdmin: true},
		OrgOwner:      {InstanceOwner: false, OrgOwner: true, OrgAdmin: true},
		OrgAdmin:      {InstanceOwner: false, OrgOwner: false, OrgAdmin: true},
	}

	for _, held := range roles {
		for _, required := range roles {
			got := held.Satisfies(required)
			if got != want[held][required] {
				t.Errorf("%s.Satisfies(%s) = %v, want %v", held, required, got, want[held][required])
			}
		}
	}
}

// ORG_ADMIN is explicitly NOT an ORG_OWNER, which is the whole point of having
// two: docs/PLAN/08 says an admin has org access "except deleting org/changing
// owner", and those are the endpoints that will require ORG_OWNER.
func TestAnAdminIsNotAnOwner(t *testing.T) {
	if OrgAdmin.Satisfies(OrgOwner) {
		t.Error("an ORG_ADMIN satisfies ORG_OWNER, so deleting an organization " +
			"would be reachable by an administrator who is not its owner")
	}
}

// The project roles exist in the schema and not in this phase. A grant of one
// must not accidentally satisfy anything.
func TestPhaseTwoRolesSatisfyNothingYet(t *testing.T) {
	for _, held := range []Role{ProjectOwner, ProjectGrantOwner} {
		if held.Valid() {
			t.Errorf("%s reports itself implemented", held)
		}
		for _, required := range []Role{InstanceOwner, OrgOwner, OrgAdmin} {
			if held.Satisfies(required) {
				t.Errorf("%s satisfies %s", held, required)
			}
		}
	}
}

// --- Authorize -------------------------------------------------------------------

// The default that matters most: an endpoint that declares nothing is
// UNREACHABLE, not open. The failure mode of a default-open design is one
// endpoint somebody did not annotate, and it is invisible until exploited.
func TestAnEndpointWithNoRequirementIsUnreachable(t *testing.T) {
	everyone := []Caller{
		caller(),
		caller(Grant{Role: OrgAdmin, ScopeID: orgA}),
		caller(Grant{Role: OrgOwner, ScopeID: orgA}),
		caller(Grant{Role: InstanceOwner, ScopeID: instance}),
	}

	for _, c := range everyone {
		if d := Authorize(c, Requirement{}, orgA); d.Allowed {
			t.Fatalf("the zero Requirement admitted a caller with %v", c.Grants)
		}
	}
}

// A scope with no role, and a role with no scope, are both incomplete
// declarations and both refuse.
func TestAnIncompleteRequirementRefuses(t *testing.T) {
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})

	if d := Authorize(c, Requirement{Role: OrgAdmin}, orgA); d.Allowed {
		t.Error("a requirement with no scope was satisfied")
	}
	if d := Authorize(c, Requirement{Scope: ScopeOrganization}, orgA); d.Allowed {
		t.Error("a requirement with no role was satisfied")
	}
}

// The control for the tests above: a complete requirement IS satisfiable, or
// they would pass against an Authorize that refuses everything.
func TestACompleteRequirementIsSatisfiable(t *testing.T) {
	c := caller(Grant{Role: OrgAdmin, ScopeID: orgA})

	d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, orgA)
	if !d.Allowed {
		t.Fatalf("an ORG_ADMIN was refused their own organization: %s", d.Reason)
	}
	if d.InstanceScoped {
		t.Error("an organization-scoped grant was marked instance-scoped")
	}
}

// Abuse case A-3, at the layer that decides it. A role held over organization A
// says nothing about organization B, and checking only the role NAME is how one
// administrator ends up able to administer everybody.
func TestARoleOverOneOrganizationDoesNotReachAnother(t *testing.T) {
	for _, held := range []Role{OrgAdmin, OrgOwner} {
		c := caller(Grant{Role: held, ScopeID: orgA})

		if d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, orgB); d.Allowed {
			t.Errorf("an %s of %s reached %s", held, orgA, orgB)
		}
		// The control: they can still reach their own.
		if d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, orgA); !d.Allowed {
			t.Errorf("an %s was refused their own organization: %s", held, d.Reason)
		}
	}
}

// INSTANCE_OWNER is the one role whose scope is not the organization being
// addressed, and the decision says so — so the caller chooses
// WithInstanceScope, which is named, logged and audited, rather than a missing
// filter.
func TestAnInstanceOwnerReachesEveryOrganizationAndIsMarked(t *testing.T) {
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})

	own := Authorize(c, Requirement{Role: OrgOwner, Scope: ScopeOrganization}, orgA)
	if !own.Allowed {
		t.Fatalf("refused their own organization: %s", own.Reason)
	}
	if own.InstanceScoped {
		t.Error("acting on their OWN organization was marked instance-scoped, which " +
			"would log and audit a cross-tenant access that did not happen")
	}

	other := Authorize(c, Requirement{Role: OrgOwner, Scope: ScopeOrganization}, orgB)
	if !other.Allowed {
		t.Fatalf("an INSTANCE_OWNER was refused another organization: %s", other.Reason)
	}
	if !other.InstanceScoped {
		t.Error("acting on ANOTHER organization was not marked instance-scoped, so " +
			"the access would be neither logged nor audited")
	}
}

// An instance-scoped endpoint is not reachable by an organization role,
// however senior it is within its own organization.
func TestAnInstanceEndpointRefusesOrganizationRoles(t *testing.T) {
	for _, held := range []Role{OrgOwner, OrgAdmin} {
		c := caller(Grant{Role: held, ScopeID: orgA})

		if d := Authorize(c, Requirement{Role: OrgOwner, Scope: ScopeInstance}, ""); d.Allowed {
			t.Errorf("an %s reached an instance-scoped endpoint", held)
		}
	}

	// The control.
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})
	if d := Authorize(c, Requirement{Role: InstanceOwner, Scope: ScopeInstance}, ""); !d.Allowed {
		t.Errorf("an INSTANCE_OWNER was refused an instance endpoint: %s", d.Reason)
	}
}

// A caller with no grants at all is refused everything. Obvious, and worth
// pinning: it is the state of every user in the system today.
func TestACallerWithNoGrantsIsRefusedEverything(t *testing.T) {
	c := caller()

	for _, req := range []Requirement{
		{Role: OrgAdmin, Scope: ScopeOrganization},
		{Role: OrgOwner, Scope: ScopeOrganization},
		{Role: InstanceOwner, Scope: ScopeInstance},
	} {
		if d := Authorize(c, req, orgA); d.Allowed {
			t.Errorf("a caller with no grants satisfied %v", req)
		}
	}
}

// A grant of a role this phase does not implement satisfies nothing, rather
// than being ignored in a way that lets some OTHER grant through by accident.
func TestAPhaseTwoGrantIsInert(t *testing.T) {
	c := caller(Grant{Role: ProjectOwner, ScopeID: orgA})

	if d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, orgA); d.Allowed {
		t.Error("a PROJECT_OWNER grant satisfied an ORG_ADMIN requirement")
	}
}

// An endpoint that requires a role this phase does not implement refuses,
// rather than falling through to some weaker check.
func TestARequirementForAnUnimplementedRoleRefuses(t *testing.T) {
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})

	if d := Authorize(c, Requirement{Role: ProjectOwner, Scope: ScopeOrganization}, orgA); d.Allowed {
		t.Error("a requirement for an unimplemented role was satisfied by an INSTANCE_OWNER")
	}
}

// The reason is for the log and must never be empty on a refusal, or an
// operator debugging a permission problem has nothing to go on.
func TestEveryRefusalCarriesAReason(t *testing.T) {
	refusals := []Decision{
		Authorize(caller(), Requirement{}, orgA),
		Authorize(caller(), Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, orgA),
		Authorize(caller(Grant{Role: OrgAdmin, ScopeID: orgA}), Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, orgB),
		Authorize(caller(Grant{Role: OrgAdmin, ScopeID: orgA}), Requirement{Role: OrgOwner, Scope: ScopeInstance}, ""),
		Authorize(caller(), Requirement{Role: ProjectOwner, Scope: ScopeOrganization}, orgA),
	}

	for i, d := range refusals {
		if d.Allowed {
			t.Fatalf("refusal %d was allowed", i)
		}
		if d.Reason == "" {
			t.Errorf("refusal %d carries no reason", i)
		}
	}
}

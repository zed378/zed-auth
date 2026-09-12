package management

import (
	"testing"
)

const (
	orgA     = "11111111-1111-1111-1111-111111111111"
	orgB     = "22222222-2222-2222-2222-222222222222"
	instance = "99999999-9999-9999-9999-999999999999"
	userID   = "44444444-4444-4444-4444-444444444444"

	// Projects, for the project scope `P2-05` added. Both live in orgA unless
	// a test says otherwise — the interesting cases are two projects in ONE
	// organization, which is where a PROJECT_OWNER's narrowness shows.
	projectA = "33333333-3333-3333-3333-333333333333"
	projectB = "55555555-5555-5555-5555-555555555555"
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

// PROJECT_GRANT_OWNER exists in the schema and nowhere else. It only means
// anything once a `project_grant` exists to delegate through (`P4-01`), and a
// role that satisfies nothing would be a role an endpoint could require and
// nobody could hold.
//
// `P2-05` moved PROJECT_OWNER out of this test and into a real one below. The
// test was guarding a "not yet", and the day the "not yet" ends it has to say
// so rather than be deleted quietly.
func TestTheDelegationRoleSatisfiesNothingYet(t *testing.T) {
	for _, held := range []Role{ProjectGrantOwner} {
		if held.Valid() {
			t.Errorf("%s reports itself implemented", held)
		}
		for _, required := range []Role{InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner} {
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
		if d := Authorize(c, Requirement{}, Target{OrgID: orgA}); d.Allowed {
			t.Fatalf("the zero Requirement admitted a caller with %v", c.Grants)
		}
	}
}

// A scope with no role, and a role with no scope, are both incomplete
// declarations and both refuse.
func TestAnIncompleteRequirementRefuses(t *testing.T) {
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})

	if d := Authorize(c, Requirement{Role: OrgAdmin}, Target{OrgID: orgA}); d.Allowed {
		t.Error("a requirement with no scope was satisfied")
	}
	if d := Authorize(c, Requirement{Scope: ScopeOrganization}, Target{OrgID: orgA}); d.Allowed {
		t.Error("a requirement with no role was satisfied")
	}
}

// The control for the tests above: a complete requirement IS satisfiable, or
// they would pass against an Authorize that refuses everything.
func TestACompleteRequirementIsSatisfiable(t *testing.T) {
	c := caller(Grant{Role: OrgAdmin, ScopeID: orgA})

	d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, Target{OrgID: orgA})
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

		if d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, Target{OrgID: orgB}); d.Allowed {
			t.Errorf("an %s of %s reached %s", held, orgA, orgB)
		}
		// The control: they can still reach their own.
		if d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, Target{OrgID: orgA}); !d.Allowed {
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

	own := Authorize(c, Requirement{Role: OrgOwner, Scope: ScopeOrganization}, Target{OrgID: orgA})
	if !own.Allowed {
		t.Fatalf("refused their own organization: %s", own.Reason)
	}
	if own.InstanceScoped {
		t.Error("acting on their OWN organization was marked instance-scoped, which " +
			"would log and audit a cross-tenant access that did not happen")
	}

	other := Authorize(c, Requirement{Role: OrgOwner, Scope: ScopeOrganization}, Target{OrgID: orgB})
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

		if d := Authorize(c, Requirement{Role: OrgOwner, Scope: ScopeInstance}, Target{OrgID: ""}); d.Allowed {
			t.Errorf("an %s reached an instance-scoped endpoint", held)
		}
	}

	// The control.
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})
	if d := Authorize(c, Requirement{Role: InstanceOwner, Scope: ScopeInstance}, Target{OrgID: ""}); !d.Allowed {
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
		if d := Authorize(c, req, Target{OrgID: orgA}); d.Allowed {
			t.Errorf("a caller with no grants satisfied %v", req)
		}
	}
}

// A grant of a role this phase does not implement satisfies nothing, rather
// than being ignored in a way that lets some OTHER grant through by accident.
func TestAPhaseTwoGrantIsInert(t *testing.T) {
	c := caller(Grant{Role: ProjectOwner, ScopeID: orgA})

	if d := Authorize(c, Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, Target{OrgID: orgA}); d.Allowed {
		t.Error("a PROJECT_OWNER grant satisfied an ORG_ADMIN requirement")
	}
}

// An endpoint that requires a role this service does not grant refuses, rather
// than falling through to some weaker check.
func TestARequirementForAnUnimplementedRoleRefuses(t *testing.T) {
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})

	if d := Authorize(c, Requirement{Role: ProjectGrantOwner, Scope: ScopeProject},
		Target{OrgID: orgA, ProjectID: projectA}); d.Allowed {
		t.Error("a requirement for an unimplemented role was satisfied by an INSTANCE_OWNER")
	}
}

// --- P2-05: the project scope -----------------------------------------------

// A PROJECT_OWNER's scope_id is a PROJECT id. Holding one over project A says
// nothing about project B, and checking only the role name is how a narrow
// grant becomes a wide one.
func TestAProjectOwnerReachesOnlyItsOwnProject(t *testing.T) {
	c := caller(Grant{Role: ProjectOwner, ScopeID: projectA})
	req := Requirement{Role: ProjectOwner, Scope: ScopeProject}

	if d := Authorize(c, req, Target{OrgID: orgA, ProjectID: projectA}); !d.Allowed {
		t.Errorf("a PROJECT_OWNER was refused on their own project: %s", d.Reason)
	}
	if d := Authorize(c, req, Target{OrgID: orgA, ProjectID: projectB}); d.Allowed {
		t.Error("a PROJECT_OWNER on project A reached project B")
	}
}

// Upward inheritance is impossible: a project role grants nothing at the
// organization level, however powerful it is inside its project.
func TestAProjectOwnerIsNotAnOrganizationAdministrator(t *testing.T) {
	c := caller(Grant{Role: ProjectOwner, ScopeID: projectA})

	for _, req := range []Requirement{
		{Role: OrgAdmin, Scope: ScopeOrganization},
		{Role: OrgOwner, Scope: ScopeOrganization},
		{Role: InstanceOwner, Scope: ScopeInstance},
	} {
		if d := Authorize(c, req, Target{OrgID: orgA, ProjectID: projectA}); d.Allowed {
			t.Errorf("a PROJECT_OWNER satisfied %s", req.Role)
		}
	}

	if ProjectOwner.Satisfies(OrgAdmin) || ProjectOwner.Satisfies(OrgOwner) || ProjectOwner.Satisfies(InstanceOwner) {
		t.Error("the table lets a project role inherit upward")
	}
}

// An organization-scoped role covers every project inside it — `PG-32`, and the
// reason `P1-17` and `P1-18` were not wrong to let an ORG_ADMIN administer a
// project.
func TestAnOrgAdminAdministersEveryProjectInItsOwnOrganization(t *testing.T) {
	c := caller(Grant{Role: OrgAdmin, ScopeID: orgA})
	req := Requirement{Role: ProjectOwner, Scope: ScopeProject}

	for _, project := range []string{projectA, projectB} {
		if d := Authorize(c, req, Target{OrgID: orgA, ProjectID: project}); !d.Allowed {
			t.Errorf("an ORG_ADMIN was refused on a project in their own organization: %s", d.Reason)
		}
	}

	// And not one in somebody else's, which is the half that matters.
	if d := Authorize(c, req, Target{OrgID: orgB, ProjectID: projectB}); d.Allowed {
		t.Error("an ORG_ADMIN of organization A administered a project in organization B")
	}
}

// A project-scoped route reached with no project is a routing mistake.
// Treating an empty project as "any project" is how a narrow role becomes a
// wide one, so it is refused.
func TestAProjectScopedRouteWithNoProjectIsRefused(t *testing.T) {
	c := caller(Grant{Role: ProjectOwner, ScopeID: projectA})

	d := Authorize(c, Requirement{Role: ProjectOwner, Scope: ScopeProject}, Target{OrgID: orgA})
	if d.Allowed {
		t.Error("a project-scoped route was satisfied with no project named")
	}
	if d.Reason == "" {
		t.Error("the refusal carries no reason")
	}
}

// An INSTANCE_OWNER holds every right in every organization, including inside
// projects they have no specific grant over.
func TestAnInstanceOwnerReachesEveryProject(t *testing.T) {
	c := caller(Grant{Role: InstanceOwner, ScopeID: instance})

	d := Authorize(c, Requirement{Role: ProjectOwner, Scope: ScopeProject},
		Target{OrgID: orgB, ProjectID: projectB})
	if !d.Allowed {
		t.Errorf("an INSTANCE_OWNER was refused inside a project: %s", d.Reason)
	}
	if !d.InstanceScoped {
		t.Error("acting outside their own organization was not flagged as instance-scoped")
	}
}

// A caller holding nothing over the project OR its organization must not learn
// that either exists — the 404-not-403 rule, extended to the project scope.
func TestAStrangerCannotTellAProjectExists(t *testing.T) {
	// Built by hand rather than through `caller()`, which puts the caller IN
	// orgA — and a member of an organization already knows it exists, so that
	// helper cannot express a stranger to it. Getting this wrong first is what
	// showed the distinction is real.
	stranger := Caller{UserID: userID, OrgID: orgB, Grants: []Grant{{Role: OrgAdmin, ScopeID: orgB}}}

	d := Authorize(stranger, Requirement{Role: ProjectOwner, Scope: ScopeProject},
		Target{OrgID: orgA, ProjectID: projectA})
	if d.Allowed {
		t.Fatal("a stranger was allowed")
	}
	if !d.Invisible {
		t.Error("a stranger to both the organization and the project was told 403, which confirms they exist")
	}

	// But somebody who holds something over the organization is told 403: they
	// already know it exists, and hiding it from them buys nothing.
	member := caller(Grant{Role: OrgAdmin, ScopeID: orgA})
	d = Authorize(member, Requirement{Role: OrgOwner, Scope: ScopeOrganization}, Target{OrgID: orgA})
	if d.Invisible {
		t.Error("an ORG_ADMIN of the organization was told 404 for it")
	}
}

// The whole hierarchy, exhaustively: every (held role, required role) pair,
// against the table docs/PLAN/08 Part C draws. A table-driven test over every
// combination is what the card asks for, and it is the only way to notice that
// one cell changed when somebody edits `satisfies`.
func TestTheHierarchyMatchesThePlan(t *testing.T) {
	// want[held][required]
	want := map[Role]map[Role]bool{
		InstanceOwner: {InstanceOwner: true, OrgOwner: true, OrgAdmin: true, ProjectOwner: true, ProjectGrantOwner: false},
		OrgOwner:      {InstanceOwner: false, OrgOwner: true, OrgAdmin: true, ProjectOwner: true, ProjectGrantOwner: false},
		OrgAdmin:      {InstanceOwner: false, OrgOwner: false, OrgAdmin: true, ProjectOwner: true, ProjectGrantOwner: false},
		ProjectOwner:  {InstanceOwner: false, OrgOwner: false, OrgAdmin: false, ProjectOwner: true, ProjectGrantOwner: false},
		// Reserved: holds nothing, satisfies nothing.
		ProjectGrantOwner: {InstanceOwner: false, OrgOwner: false, OrgAdmin: false, ProjectOwner: false, ProjectGrantOwner: false},
	}

	all := []Role{InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner, ProjectGrantOwner}
	for _, held := range all {
		for _, required := range all {
			got := held.Satisfies(required)
			if got != want[held][required] {
				t.Errorf("%s.Satisfies(%s) = %v, want %v", held, required, got, want[held][required])
			}
		}
	}

	// Downward only, stated as a property rather than as cells: if A satisfies
	// B and they differ, B must not satisfy A.
	for _, a := range all {
		for _, b := range all {
			if a != b && a.Satisfies(b) && b.Satisfies(a) {
				t.Errorf("%s and %s satisfy each other, so inheritance is not a hierarchy", a, b)
			}
		}
	}
}

// The reason is for the log and must never be empty on a refusal, or an
// operator debugging a permission problem has nothing to go on.
func TestEveryRefusalCarriesAReason(t *testing.T) {
	refusals := []Decision{
		Authorize(caller(), Requirement{}, Target{OrgID: orgA}),
		Authorize(caller(), Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, Target{OrgID: orgA}),
		Authorize(caller(Grant{Role: OrgAdmin, ScopeID: orgA}), Requirement{Role: OrgAdmin, Scope: ScopeOrganization}, Target{OrgID: orgB}),
		Authorize(caller(Grant{Role: OrgAdmin, ScopeID: orgA}), Requirement{Role: OrgOwner, Scope: ScopeInstance}, Target{OrgID: ""}),
		Authorize(caller(), Requirement{Role: ProjectOwner, Scope: ScopeOrganization}, Target{OrgID: orgA}),
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

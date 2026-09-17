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

// PROJECT_GRANT_OWNER was reserved until P4-03, and this test guarded the
// "not yet". Inverted rather than deleted: the role is implemented, and it
// reaches no ORGANIZATION or PROJECT requirement — the narrowest role in the
// hierarchy by design.
func TestTheDelegationRoleReachesOnlyItsOwnRequirement(t *testing.T) {
	if !ProjectGrantOwner.Valid() {
		t.Fatal("PROJECT_GRANT_OWNER reports itself unimplemented")
	}
	for _, required := range []Role{InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner} {
		if ProjectGrantOwner.Satisfies(required) {
			t.Errorf("PROJECT_GRANT_OWNER satisfies %s", required)
		}
	}
	// And the granting side's project owner does not satisfy it (T4-4).
	if ProjectOwner.Satisfies(ProjectGrantOwner) {
		t.Error("PROJECT_OWNER satisfies PROJECT_GRANT_OWNER — the granting organization would administer the partner's people")
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
	c := caller(Grant{Role: OrgOwner, ScopeID: orgA})

	if d := Authorize(c, Requirement{Role: "PROJECT_VIEWER", Scope: ScopeProject},
		Target{OrgID: orgA, ProjectID: projectA}); d.Allowed {
		t.Error("a requirement for a role this service does not define was satisfied")
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

// Visibility on a project-scoped route follows GRANTS, not membership.
//
// A PROJECT_OWNER's token belongs to the organization, so "is a member of" is
// true for every project in it — and listing projects requires ORG_ADMIN, so
// that same caller cannot enumerate them. A 403 on one project id would
// therefore answer "does this project exist?" one request at a time.
//
// Found by `P2-02`'s endpoint tests. This unit test exists so the rule is
// pinned where it is implemented as well as where it was noticed.
func TestAProjectIsInvisibleToSomebodyWithNoGrantOverIt(t *testing.T) {
	// A member of orgA who owns project A, asking about project B.
	owner := caller(Grant{Role: ProjectOwner, ScopeID: projectA})

	d := Authorize(owner, Requirement{Role: ProjectOwner, Scope: ScopeProject},
		Target{OrgID: orgA, ProjectID: projectB})
	if d.Allowed {
		t.Fatal("allowed")
	}
	if !d.Invisible {
		t.Error("a project the caller holds nothing over was answered 403, which confirms it exists")
	}

	// An ORG_ADMIN of the same organization gets 403 instead: they can list
	// the projects, so concealing one buys nothing and costs clarity.
	admin := caller(Grant{Role: OrgAdmin, ScopeID: orgA})
	d = Authorize(admin, Requirement{Role: InstanceOwner, Scope: ScopeOrganization},
		Target{OrgID: orgA, ProjectID: projectB})
	if d.Invisible {
		t.Error("an ORG_ADMIN was told 404 for a project in their own organization")
	}
}

// The whole hierarchy, exhaustively: every (held role, required role) pair,
// against the table docs/PLAN/08 Part C draws. A table-driven test over every
// combination is what the card asks for, and it is the only way to notice that
// one cell changed when somebody edits `satisfies`.
func TestTheHierarchyMatchesThePlan(t *testing.T) {
	// want[held][required]
	//
	// Member is in here because `P2-06` added it, and adding a role without
	// adding its row would leave this test passing while covering less than it
	// claims — the exact way an exhaustive test stops being exhaustive.
	want := map[Role]map[Role]bool{
		InstanceOwner: {InstanceOwner: true, OrgOwner: true, OrgAdmin: true, ProjectOwner: true, ProjectGrantOwner: true, Member: true},
		OrgOwner:      {InstanceOwner: false, OrgOwner: true, OrgAdmin: true, ProjectOwner: true, ProjectGrantOwner: true, Member: true},
		OrgAdmin:      {InstanceOwner: false, OrgOwner: false, OrgAdmin: true, ProjectOwner: true, ProjectGrantOwner: true, Member: true},
		// Not PROJECT_GRANT_OWNER: the project owner is the GRANTING side (T4-4).
		ProjectOwner: {InstanceOwner: false, OrgOwner: false, OrgAdmin: false, ProjectOwner: true, ProjectGrantOwner: false, Member: true},
		// P4-03: the narrowest role. Itself, and membership.
		ProjectGrantOwner: {InstanceOwner: false, OrgOwner: false, OrgAdmin: false, ProjectOwner: false, ProjectGrantOwner: true, Member: true},
		// The floor. Being inside the tenant implies nothing else at all.
		Member: {InstanceOwner: false, OrgOwner: false, OrgAdmin: false, ProjectOwner: false, ProjectGrantOwner: false, Member: true},
	}

	all := []Role{InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner, ProjectGrantOwner, Member}
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

// --- P2-06: membership ------------------------------------------------------

// `Member` is satisfied by the caller's own token saying which organization
// they belong to, with no grant anywhere.
//
// It exists because `/v1/authz/check` is called by consumer applications on
// every protected request they serve, and requiring ORG_ADMIN there would mean
// every such service holds an administrative role over the organization.
func TestMembershipNeedsNoGrant(t *testing.T) {
	// No grants at all, and a token belonging to orgA.
	plain := Caller{UserID: userID, OrgID: orgA}

	if d := Authorize(plain, Requirement{Role: Member, Scope: ScopeOrganization},
		Target{OrgID: orgA}); !d.Allowed {
		t.Errorf("a member of the organization was refused: %s", d.Reason)
	}

	// And not somebody else's organization, which is the half that matters.
	if d := Authorize(plain, Requirement{Role: Member, Scope: ScopeOrganization},
		Target{OrgID: orgB}); d.Allowed {
		t.Error("a member of organization A passed a Member check for organization B")
	}
}

// Membership is the floor, not a back door: it satisfies nothing above itself.
func TestMembershipBuysNothingElse(t *testing.T) {
	plain := Caller{UserID: userID, OrgID: orgA}

	for _, req := range []Requirement{
		{Role: OrgAdmin, Scope: ScopeOrganization},
		{Role: OrgOwner, Scope: ScopeOrganization},
		{Role: ProjectOwner, Scope: ScopeProject},
		{Role: InstanceOwner, Scope: ScopeInstance},
	} {
		if d := Authorize(plain, req, Target{OrgID: orgA, ProjectID: projectA}); d.Allowed {
			t.Errorf("a plain member satisfied %s", req.Role)
		}
	}
}

// An administrator of another organization is not a member of this one — their
// grant over orgB says nothing about orgA.
func TestAGrantElsewhereIsNotMembershipHere(t *testing.T) {
	elsewhere := Caller{UserID: userID, OrgID: orgB, Grants: []Grant{{Role: OrgAdmin, ScopeID: orgB}}}

	if d := Authorize(elsewhere, Requirement{Role: Member, Scope: ScopeOrganization},
		Target{OrgID: orgA}); d.Allowed {
		t.Error("an administrator of another organization passed a Member check here")
	}
}

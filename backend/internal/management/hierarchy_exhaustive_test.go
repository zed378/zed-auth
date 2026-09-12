package management

import (
	"fmt"
	"testing"
)

// Every (held role, held scope, required role, required scope, target) the
// service can be asked about (P2-16 step 1).
//
// The named tests beside this one each pin a case somebody thought about.
// This covers the ones nobody did — and, more usefully, it makes the **whole**
// behaviour surveyable: 1,000-odd combinations, one expectation function, and
// a count that changes visibly if a rule moves.
//
// **The expectation is derived from the documented rules, not from
// `satisfies`.** A table that asked the implementation what it does and then
// asserted it does that is a very long way of writing `true`. `expected` below
// is written from `docs/PLAN/08` Part C's prose, in a different shape from the
// code, so a transcription error in either one shows up as a disagreement.

// The dimensions. Deliberately including values that cannot occur together —
// a PROJECT_OWNER whose scope is an organization, an INSTANCE_OWNER scoped to
// a project — because "cannot occur" is an assumption, and an authorization
// function that misbehaves on a row the database should never hold is one bad
// migration away from mattering.
var (
	allRoles  = []Role{InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner, Member}
	allScopes = []Scope{ScopeUnset, ScopeOrganization, ScopeInstance, ScopeProject, ScopeSelf}
)

const (
	homeOrg     = "org-home"
	otherOrg    = "org-other"
	homeProject = "prj-home"
	otherProj   = "prj-other"
	instanceID  = "instance-1"
)

// scopeTargets are the ids a grant's scope_id can point at.
var scopeTargets = []string{homeOrg, otherOrg, homeProject, otherProj, instanceID}

// targets are what a request can address.
var targets = []Target{
	{OrgID: homeOrg},
	{OrgID: otherOrg},
	{OrgID: homeOrg, ProjectID: homeProject},
	{OrgID: homeOrg, ProjectID: otherProj},
	{OrgID: otherOrg, ProjectID: otherProj},
	{}, // no organization addressed at all
}

// expected is the documented rule, restated.
//
// Written from `docs/PLAN/08` Part C in the order the document states them,
// which is deliberately NOT the order `Authorize` evaluates them — two
// orderings that agree on every input is a stronger statement than one.
//
//  1. An endpoint that declares no scope is refused rather than opened.
//  2. A requirement naming a role the service does not grant is refused.
//  3. An INSTANCE_OWNER satisfies everything, over any target.
//  4. A self-scoped endpoint needs authentication and nothing else.
//  5. An instance-scoped endpoint needs INSTANCE_OWNER and nothing else.
//  6. A request addressing no organization cannot be authorized.
//  7. MEMBER is satisfied by belonging to the target organization — no grant.
//  8. Otherwise a grant must name the target: the organization for an
//     organization-scoped route, the project OR its organization for a
//     project-scoped one.
func expected(caller Caller, req Requirement, target Target) bool {
	if req.Scope == ScopeUnset {
		return false // (1)
	}
	if !req.Role.Valid() {
		return false // (2)
	}

	for _, grant := range caller.Grants {
		if grant.Role == InstanceOwner {
			return true // (3) — over any target, whatever its scope_id says
		}
	}

	if req.Scope == ScopeSelf {
		return true // (4)
	}
	if req.Scope == ScopeInstance {
		return false // (5) — the instance-owner case already returned
	}
	if target.OrgID == "" {
		return false // (6)
	}
	if req.Role == Member && caller.OrgID == target.OrgID {
		return true // (7)
	}

	// (8)
	for _, grant := range caller.Grants {
		if !grant.Role.Satisfies(req.Role) {
			continue
		}
		switch req.Scope {
		case ScopeOrganization:
			if grant.ScopeID == target.OrgID {
				return true
			}
		case ScopeProject:
			if target.ProjectID == "" {
				// A project-scoped route naming no project is a routing
				// mistake, and treating it as "any project" is how a narrow
				// role becomes a wide one.
				return false
			}
			if grant.ScopeID == target.ProjectID || grant.ScopeID == target.OrgID {
				return true
			}
		}
	}
	return false
}

func TestTheHierarchyExhaustively(t *testing.T) {
	var checked, allowed int
	var disagreements []string

	for _, heldRole := range append([]Role{""}, allRoles...) {
		for _, heldScope := range scopeTargets {
			for _, reqRole := range append([]Role{"", "SOMETHING_ELSE"}, allRoles...) {
				for _, reqScope := range allScopes {
					for _, target := range targets {
						caller := Caller{UserID: "u1", OrgID: homeOrg}
						if heldRole != "" {
							caller.Grants = []Grant{{Role: heldRole, ScopeID: heldScope}}
						}
						req := Requirement{Role: reqRole, Scope: reqScope}

						got := Authorize(caller, req, target)
						want := expected(caller, req, target)

						checked++
						if got.Allowed {
							allowed++
						}
						if got.Allowed != want {
							disagreements = append(disagreements, fmt.Sprintf(
								"holding %q over %q, asked for %q scope %d, target org=%q project=%q: "+
									"Authorize says %v, the documented rule says %v",
								heldRole, heldScope, reqRole, reqScope,
								target.OrgID, target.ProjectID, got.Allowed, want))
						}
					}
				}
			}
		}
	}

	// A guard against the whole thing passing vacuously. If the loops ever
	// produce nothing — a dimension emptied by a refactor — every assertion
	// above holds trivially and the suite reports success having checked
	// nothing.
	if checked < 1000 {
		t.Fatalf("only %d combinations checked; this test would prove very little", checked)
	}
	// And against the opposite: a change that opens everything would produce
	// no disagreements only if `expected` were broken the same way, but a
	// wildly different allow-rate is worth failing on its own.
	if allowed == 0 || allowed == checked {
		t.Fatalf("%d of %d combinations allowed — an authorization function that "+
			"answers the same thing everywhere is not one", allowed, checked)
	}

	for _, line := range disagreements {
		t.Error(line)
	}
	t.Logf("%d combinations, %d allowed", checked, allowed)
}

// A caller holding two grants gets the union of what each buys, and nothing more.
//
// The single-grant table above cannot see an interaction: a rule that ANDed
// grants, or one that let a project-scoped grant widen an organization-scoped
// requirement, would pass every row of it.
func TestTwoGrantsGiveTheUnionAndNothingMore(t *testing.T) {
	for _, first := range allRoles {
		for _, second := range allRoles {
			for _, scope := range scopeTargets {
				for _, req := range []Requirement{
					{Role: OrgAdmin, Scope: ScopeOrganization},
					{Role: OrgOwner, Scope: ScopeOrganization},
					{Role: ProjectOwner, Scope: ScopeProject},
				} {
					target := Target{OrgID: homeOrg, ProjectID: homeProject}

					both := Caller{UserID: "u1", OrgID: homeOrg, Grants: []Grant{
						{Role: first, ScopeID: scope},
						{Role: second, ScopeID: scope},
					}}
					onlyFirst := Caller{UserID: "u1", OrgID: homeOrg, Grants: []Grant{
						{Role: first, ScopeID: scope},
					}}
					onlySecond := Caller{UserID: "u1", OrgID: homeOrg, Grants: []Grant{
						{Role: second, ScopeID: scope},
					}}

					want := Authorize(onlyFirst, req, target).Allowed ||
						Authorize(onlySecond, req, target).Allowed
					got := Authorize(both, req, target).Allowed

					if got != want {
						t.Errorf("holding %q and %q over %q, asked for %q: got %v, "+
							"but the union of holding each alone is %v",
							first, second, scope, req.Role, got, want)
					}
				}
			}
		}
	}
}

// Every allowed decision carries no reason, and every refusal carries one.
//
// Checked across the whole table rather than on one example: a reason is what
// an operator reads to understand a refusal, and a branch that forgets one is
// invisible until somebody is debugging a 403 at 3am.
func TestEveryOutcomeIsExplicableAcrossTheWholeTable(t *testing.T) {
	var checked int

	for _, heldRole := range append([]Role{""}, allRoles...) {
		for _, reqRole := range allRoles {
			for _, reqScope := range allScopes {
				for _, target := range targets {
					caller := Caller{UserID: "u1", OrgID: homeOrg}
					if heldRole != "" {
						caller.Grants = []Grant{{Role: heldRole, ScopeID: homeOrg}}
					}

					decision := Authorize(caller, Requirement{Role: reqRole, Scope: reqScope}, target)
					checked++

					if !decision.Allowed && decision.Reason == "" {
						t.Errorf("holding %q, asked for %q scope %d, target org=%q: refused with no reason",
							heldRole, reqRole, reqScope, target.OrgID)
					}
					if decision.Allowed && decision.Reason != "" {
						t.Errorf("holding %q, asked for %q scope %d: allowed, but carries a refusal reason %q",
							heldRole, reqRole, reqScope, decision.Reason)
					}
				}
			}
		}
	}

	if checked < 500 {
		t.Fatalf("only %d combinations checked; this proves very little", checked)
	}
}

// Invisibility is decided by scope, never by which role was asked for.
//
// `Invisible` is the difference between a 403 and a 404, and a 403 to somebody
// probing an organization they hold nothing over answers "is this real?" for
// anybody willing to send one request per guess (abuse case A-3). A rule that
// leaked visibility for some required roles and not others would be very hard
// to notice by hand.
func TestInvisibilityDoesNotDependOnTheRequiredRole(t *testing.T) {
	caller := Caller{UserID: "u1", OrgID: homeOrg} // no grants anywhere

	// Per (scope, target), because those are the two things a caller CAN vary
	// from outside: the route they hit and the id in its path. The required
	// role is a property of the route, not of the request, so comparing
	// across scopes would conflate "does the answer leak the required role"
	// with "do two different endpoints behave differently" — and the second is
	// not a leak, it is the point.
	cases := []struct {
		scope  Scope
		target Target
	}{
		{ScopeOrganization, Target{OrgID: otherOrg}},
		{ScopeProject, Target{OrgID: otherOrg, ProjectID: otherProj}},
	}

	// Only targets in an organization the caller does not belong to. Inside
	// their OWN organization a caller is a MEMBER without any grant, so the
	// question does not arise — and a case list that included one would fail
	// on correct behaviour.

	for _, c := range cases {
		var first *bool
		for _, reqRole := range allRoles {
			decision := Authorize(caller, Requirement{Role: reqRole, Scope: c.scope}, c.target)
			if decision.Allowed {
				t.Fatalf("a caller with no grants was allowed %q over %v", reqRole, c.target)
			}
			if first == nil {
				value := decision.Invisible
				first = &value
				continue
			}
			if decision.Invisible != *first {
				t.Errorf("scope %d, target %v: invisibility differs by required role "+
					"(%q gives %v, an earlier role gave %v) — which makes the required "+
					"role guessable from the status code",
					c.scope, c.target, reqRole, decision.Invisible, *first)
			}
		}
	}
}

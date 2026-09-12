// Package management provides the cross-cutting mechanics every /v1 endpoint
// depends on: bearer authentication, permission checks, tenant scoping, the
// error envelope, pagination, idempotency, per-client rate limiting and audit.
//
// Built once, and that is the objective rather than a convenience. docs/PLAN/02
// FR-14 requires every console capability to exist in this API, so there will
// be dozens of endpoints — and a control implemented per endpoint is a control
// that will be missing from one of them.
//
// Specification: MEMORY/specs/P1-15-management-api-foundation.md.
package management

import (
	"fmt"
	"slices"
)

// Role is a manager role from docs/PLAN/08 Part C.
//
// These govern who administers the Auth Service ITSELF, and are a different
// thing from the application roles that govern access within a consumer's
// product. Conflating them is how an administrator of one application ends up
// able to administer the platform.
type Role string

const (
	// InstanceOwner administers every organization.
	InstanceOwner Role = "INSTANCE_OWNER"

	// OrgOwner administers one organization entirely.
	OrgOwner Role = "ORG_OWNER"

	// OrgAdmin administers one organization except deleting it or changing
	// its owner.
	OrgAdmin Role = "ORG_ADMIN"

	// Member is "holds a valid token for this organization", and nothing more.
	//
	// **It is not a `manager_roles` value and never appears in that table.** It
	// is the requirement an endpoint declares when it needs a caller who is
	// authenticated and inside the tenant, without needing them to administer
	// anything.
	//
	// `P2-06` is why it exists. `/v1/authz/check` is called by consumer
	// applications asking about their own users, on every protected request
	// they serve. Requiring ORG_ADMIN there would mean every service that
	// checks a permission holds an administrative role over the organization —
	// the exact inversion of least privilege, and a far larger blast radius
	// than the endpoint needs.
	//
	// Before this existed the policy table could say "an administrator" or
	// nothing at all, because the zero Requirement is unsatisfiable by design.
	// A table with no way to express "authenticated" pushes an endpoint into
	// one of those two, and both are wrong.
	Member Role = "MEMBER"
)

// The project-scoped roles.
//
// `ProjectOwner` became real in `P2-05`. `ProjectGrantOwner` is still reserved:
// it only means anything once a `project_grant` exists to delegate through
// (`P4-01`), and a role that satisfies nothing until then would be a role an
// endpoint could require and nobody could hold.
const (
	ProjectOwner      Role = "PROJECT_OWNER"
	ProjectGrantOwner Role = "PROJECT_GRANT_OWNER"
)

// satisfies maps each role to everything it also counts as.
//
// Written out rather than computed from a hierarchy graph. There are four
// roles that mean anything today and five in the schema; a table anybody can
// read in five seconds is worth more than a traversal that is correct for
// reasons a reader has to reconstruct.
//
// # Why ORG_ADMIN satisfies PROJECT_OWNER
//
// `docs/PLAN/08` Part C draws ORG_ADMIN and PROJECT_OWNER as siblings under
// ORG_OWNER, with "permissions flow downward only". Read strictly, an
// ORG_ADMIN would not satisfy a PROJECT_OWNER requirement — and that reading
// contradicts the diagram's own label for ORG_ADMIN ("org access, except
// deleting org/changing owner"), makes every project and application endpoint
// shipped in `P1-17` and `P1-18` wrong since the day it was written, and
// produces a system where an administrator can create a project but not manage
// the roles inside it.
//
// So an organization-scoped role covers every project in its organization.
// **Downward-only still holds in the direction that matters**: PROJECT_OWNER
// gains nothing organization-wide, and `Authorize` never lets a project-scoped
// grant reach past its own scope_id. Recorded as `PG-32`; the plan needs one
// sentence, and picking it is a plan change rather than this task's to make.
var satisfies = map[Role][]Role{
	InstanceOwner: {InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner, Member},
	OrgOwner:      {OrgOwner, OrgAdmin, ProjectOwner, Member},
	OrgAdmin:      {OrgAdmin, ProjectOwner, Member},
	ProjectOwner:  {ProjectOwner, Member},

	// Member sits under everything: holding any role implies being inside the
	// tenant. It is satisfied by membership rather than by a grant, which is
	// the one case `Authorize` handles before looking at grants at all.
	Member: {Member},
}

// Satisfies reports whether holding `held` meets a requirement for `required`.
func (r Role) Satisfies(required Role) bool {
	return slices.Contains(satisfies[r], required)
}

// Reach is how many roles a role satisfies — its height in the hierarchy.
//
// Exported so a caller can order roles by strength without restating the
// hierarchy. "ORG_ADMIN, ORG_OWNER" is alphabetical and reads as though the
// first is the stronger; ordering by Reach cannot go out of date when
// `satisfies` changes, because it IS `satisfies` (P2-13).
//
// Zero for a role this service does not define, which sorts such a value at
// the bottom rather than the top.
func (r Role) Reach() int {
	return len(satisfies[r])
}

// Valid reports whether a role is one this phase understands.
func (r Role) Valid() bool {
	_, known := satisfies[r]
	return known
}

// Grant is one row of manager_roles: a role, and what it applies to.
type Grant struct {
	Role Role

	// ScopeID is the instance for INSTANCE_OWNER and the organization for the
	// two organization roles. The schema calls it scope_id precisely because
	// what it points at depends on the role — a PROJECT_OWNER on project X is
	// not one on project Y.
	ScopeID string
}

// Caller is who is making a request, and what they may do.
type Caller struct {
	UserID   string
	ClientID string

	// OrgID is the organization the TOKEN belongs to. It is not automatically
	// the organization the request addresses: an INSTANCE_OWNER acts on
	// others, and that difference is the whole reason Authorize takes a target.
	OrgID string

	// Grants are every manager role this caller holds, read from the database
	// on this request. See the package's Authorize.
	Grants []Grant
}

// Requirement is what an endpoint demands.
//
// The zero value demands InstanceOwner over nothing, which no caller can
// satisfy — so an endpoint that forgets to declare a requirement is
// unreachable rather than open. That default is the point: the failure mode of
// a default-open design is one endpoint somebody did not annotate, and it is
// invisible until it is exploited.
type Requirement struct {
	// Role is the minimum. A caller holding something that Satisfies it passes.
	Role Role

	// Scope names which id the role must apply to. See Authorize.
	Scope Scope
}

// Scope says what a Requirement's role must be held over.
type Scope int

const (
	// ScopeUnset is the zero value and is never satisfiable. An endpoint that
	// declares no scope is refused rather than opened.
	ScopeUnset Scope = iota

	// ScopeOrganization requires the role over the organization the request
	// addresses — or InstanceOwner, which spans them all.
	ScopeOrganization

	// ScopeInstance requires InstanceOwner. Listing organizations, and the
	// instance audit log.
	ScopeInstance

	// ScopeProject requires the role over the PROJECT the request addresses,
	// or over the organization that contains it.
	//
	// The second half is what makes an ORG_ADMIN able to administer a project
	// without holding a row per project — see the note on `satisfies`. The
	// first half is what makes a PROJECT_OWNER mean anything at all: their
	// grant's scope_id is a project id, and a role held over project X says
	// nothing whatsoever about project Y.
	ScopeProject

	// ScopeSelf requires nothing beyond a valid token, because the endpoint
	// describes the CALLER rather than any resource (P2-13).
	//
	// `GET /v1/me/organizations` is the only one, and the reason it can be
	// unauthorized is that it grants nothing: it is derived from the caller's
	// own `manager_roles` rows, so it can only ever report what they already
	// hold. There is no organization it could disclose that they do not
	// already administer.
	//
	// It is a named scope rather than an absence, so it still goes through
	// Authorize and still appears in `Policy` — `TestEveryV1RouteHasADeclaredPermission`
	// keeps holding, and "this route needs no permission" stays a decision
	// somebody wrote down rather than an omission nobody noticed. The zero
	// value remains unsatisfiable.
	ScopeSelf
)

// Decision is the outcome of an authorization check.
type Decision struct {
	Allowed bool

	// Reason is for the log, never for the response. "you are an ORG_ADMIN and
	// this needs ORG_OWNER" is useful to an operator and, told to a caller
	// probing an organization they do not administer, confirms it exists.
	Reason string

	// InstanceScoped is true when the caller passed as an INSTANCE_OWNER
	// acting outside their own organization. The caller uses it to choose
	// WithInstanceScope over WithTenant, which is a named, logged, audited
	// path rather than a missing filter.
	InstanceScoped bool

	// Invisible distinguishes "you may not" from "you cannot see this", and it
	// is the difference between a 403 and a 404.
	//
	// A caller who holds nothing over the target organization must not be able
	// to learn that it exists. A 403 there answers "is org 8f3e... real?" for
	// anybody willing to send a request per guess, which is abuse case A-3's
	// disclosure (docs/SECURITY/02 §2, §14).
	//
	// A caller who DOES hold something over the target — an ORG_ADMIN asked
	// for ORG_OWNER — is told 403, because they already know the organization
	// exists and hiding it from them would be a worse experience for no gain.
	Invisible bool
}

// Target is what a request addresses.
//
// Both ids, because a project-scoped endpoint needs the project AND the
// organization that contains it: a PROJECT_OWNER is matched against the
// project, and an ORG_ADMIN against the organization. Passing only one would
// mean either project roles cannot be checked or organization roles cannot
// reach a project, and both were briefly true while this was one string.
type Target struct {
	// OrgID is the organization the REQUEST addresses, which is not
	// necessarily the caller's own: that is what makes an INSTANCE_OWNER
	// useful and what makes forgetting the distinction a cross-tenant hole.
	OrgID string

	// ProjectID is set only for ScopeProject routes. Empty elsewhere, and an
	// empty one on a ScopeProject route is refused rather than treated as a
	// wildcard.
	ProjectID string
}

// Authorize decides whether a caller may act on a target.
//
// This is the single resolution function `P2-05` step 5 asks for. Every /v1
// route reaches it through `Middleware.Require`, and `TestEveryV1RouteHasADeclaredPermission`
// fails on a route that declares no requirement — so a bespoke check in a
// handler would be an addition to this, never a replacement for it.
func Authorize(c Caller, req Requirement, target Target) Decision {
	if req.Scope == ScopeUnset {
		return Decision{
			Reason: "the endpoint declares no permission requirement, which is refused rather than opened",
		}
	}
	if !req.Role.Valid() {
		return Decision{Reason: fmt.Sprintf("the endpoint requires %q, which is not a role this service grants", req.Role)}
	}

	// InstanceOwner first, and over any target. It is the only role whose
	// scope is not the organization being addressed.
	for _, g := range c.Grants {
		if g.Role == InstanceOwner && g.Role.Satisfies(req.Role) {
			return Decision{
				Allowed:        true,
				InstanceScoped: target.OrgID != "" && target.OrgID != c.OrgID,
			}
		}
	}

	if req.Scope == ScopeSelf {
		// After the InstanceOwner branch above, so an instance owner's
		// Decision still carries InstanceScoped and the handler reads outside
		// one tenant. Before every scope check below, because there is no
		// target to check against: the endpoint is about whoever is asking.
		return Decision{Allowed: true}
	}

	if req.Scope == ScopeInstance {
		// Not Invisible: the endpoint is about the instance, which every
		// caller is already on. There is nothing to conceal, and a 404 here
		// would tell an administrator their own service has no such endpoint.
		return Decision{Reason: "this endpoint is instance-scoped and the caller is not an INSTANCE_OWNER"}
	}

	if target.OrgID == "" {
		return Decision{Reason: "the request addresses no organization"}
	}

	// Membership, which needs no grant.
	//
	// Checked here rather than in the grant loops below because it is not a
	// grant: the caller's token says which organization they belong to, and
	// for a Member requirement that is the whole question. A caller acting on
	// a DIFFERENT organization falls through and is refused — an INSTANCE_OWNER
	// has already been allowed above, and nobody else may act outside their
	// own tenant.
	if req.Role == Member && c.OrgID == target.OrgID {
		return Decision{Allowed: true}
	}

	// A grant over the project itself. Checked first because it is the
	// narrower claim: a PROJECT_OWNER on this project passes here and reaches
	// nothing else in the organization.
	if req.Scope == ScopeProject {
		if target.ProjectID == "" {
			// An empty project on a project-scoped route is a routing mistake,
			// and treating it as "any project" is how a narrow role becomes a
			// wide one.
			return Decision{Reason: "this endpoint is project-scoped and the request names no project"}
		}
		for _, g := range c.Grants {
			if g.ScopeID == target.ProjectID && g.Role.Satisfies(req.Role) {
				return Decision{Allowed: true}
			}
		}
	}

	for _, g := range c.Grants {
		// The scope must MATCH the target. A role held over organization A
		// says nothing about organization B, and checking only the role name
		// is how one administrator ends up able to administer everybody.
		//
		// On a ScopeProject route this is the ORG_ADMIN path: an
		// organization-scoped role covers every project inside it (`PG-32`).
		if g.ScopeID == target.OrgID && g.Role.Satisfies(req.Role) {
			return Decision{Allowed: true}
		}
	}

	// Refused. Which refusal depends on whether the caller can see the target
	// at all: see Decision.Invisible.
	return Decision{
		Reason:    fmt.Sprintf("no grant of %s over %s", req.Role, describe(target)),
		Invisible: invisible(c, req, target),
	}
}

// invisible decides between 404 and 403 — whether the caller may learn that
// the thing they were refused exists at all.
//
// The organization rule is `P1-16`'s: a caller who holds nothing over an
// organization is told 404, and one who holds something is told 403, because
// they already know it exists and hiding it buys nothing.
//
// **A project needs a different question**, and getting this wrong is how a
// narrow role becomes an enumeration oracle. A PROJECT_OWNER's token belongs
// to the organization, so "is a member of" is true for every project in it —
// and `GET /projects` requires ORG_ADMIN, so that same caller cannot list
// them. A 403 on one project id would therefore answer "does this project
// exist?" for anybody willing to send a request per guess, which is exactly
// the disclosure `docs/SECURITY/02` §12 is about.
//
// So on a project-scoped route, visibility follows GRANTS rather than
// membership: over the project, or over the organization containing it.
// Found by `P2-02`'s endpoint tests; the pure-function tests in `P2-05` could
// not see it, because the difference only appears once a route has a project.
func invisible(c Caller, req Requirement, target Target) bool {
	if req.Scope == ScopeProject {
		return !holdsGrantOver(c, target.ProjectID) && !holdsGrantOver(c, target.OrgID)
	}
	return !holdsAnythingOver(c, target.OrgID)
}

// holdsGrantOver is holdsAnythingOver without the membership shortcut: an
// actual row in manager_roles, scoped to this id.
func holdsGrantOver(c Caller, id string) bool {
	if id == "" {
		return false
	}
	for _, g := range c.Grants {
		if g.ScopeID == id {
			return true
		}
	}
	return false
}

func describe(t Target) string {
	if t.ProjectID != "" {
		return t.ProjectID + " (in " + t.OrgID + ")"
	}
	return t.OrgID
}

// holdsAnythingOver reports whether the caller has ANY grant over an
// organization — not necessarily a sufficient one.
//
// Deliberately any rather than a matching one: the question is whether the
// caller already knows this organization exists, and holding any role over it
// means they do.
func holdsAnythingOver(c Caller, orgID string) bool {
	if c.OrgID == orgID {
		// Their own organization. They are a member of it; its existence is
		// not news.
		return true
	}
	for _, g := range c.Grants {
		if g.ScopeID == orgID {
			return true
		}
	}
	return false
}

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
)

// Phase2Roles are the project-scoped roles the schema already allows and this
// phase does not implement.
//
// Named rather than omitted, so that a token or a row carrying one is
// recognised as "not yet" rather than silently treated as no role at all —
// and so the day Phase 2 arrives, the compiler has somewhere to put them.
const (
	ProjectOwner      Role = "PROJECT_OWNER"
	ProjectGrantOwner Role = "PROJECT_GRANT_OWNER"
)

// satisfies maps each role to everything it also counts as.
//
// Written out rather than computed from a hierarchy graph. There are three
// roles in Phase 1 and five in the schema; a table anybody can read in five
// seconds is worth more than a traversal that is correct for reasons a reader
// has to reconstruct. When Phase 2 adds the project roles, the table grows and
// stays readable.
var satisfies = map[Role][]Role{
	InstanceOwner: {InstanceOwner, OrgOwner, OrgAdmin},
	OrgOwner:      {OrgOwner, OrgAdmin},
	OrgAdmin:      {OrgAdmin},
}

// Satisfies reports whether holding `held` meets a requirement for `required`.
func (r Role) Satisfies(required Role) bool {
	return slices.Contains(satisfies[r], required)
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
}

// Authorize decides whether a caller may act on a target organization.
//
// `targetOrgID` is the organization the REQUEST addresses, which is not
// necessarily the caller's own: that is what makes an INSTANCE_OWNER useful
// and what makes forgetting the distinction a cross-tenant hole.
func Authorize(c Caller, req Requirement, targetOrgID string) Decision {
	if req.Scope == ScopeUnset {
		return Decision{
			Reason: "the endpoint declares no permission requirement, which is refused rather than opened",
		}
	}
	if !req.Role.Valid() {
		return Decision{Reason: fmt.Sprintf("the endpoint requires %q, which this phase does not implement", req.Role)}
	}

	// InstanceOwner first, and over any target. It is the only role whose
	// scope is not the organization being addressed.
	for _, g := range c.Grants {
		if g.Role == InstanceOwner && g.Role.Satisfies(req.Role) {
			return Decision{
				Allowed:        true,
				InstanceScoped: targetOrgID != "" && targetOrgID != c.OrgID,
			}
		}
	}

	if req.Scope == ScopeInstance {
		return Decision{Reason: "this endpoint is instance-scoped and the caller is not an INSTANCE_OWNER"}
	}

	if targetOrgID == "" {
		return Decision{Reason: "the request addresses no organization"}
	}

	for _, g := range c.Grants {
		// The scope must MATCH the target. A role held over organization A
		// says nothing about organization B, and checking only the role name
		// is how one administrator ends up able to administer everybody.
		if g.ScopeID == targetOrgID && g.Role.Satisfies(req.Role) {
			return Decision{Allowed: true}
		}
	}

	return Decision{Reason: fmt.Sprintf("no grant of %s over %s", req.Role, targetOrgID)}
}

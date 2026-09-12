// Package authz answers "may this user do this?" against live grant data
// (P2-06).
//
// `P2-04` put roles in the access token. A token is a snapshot, and the gap
// between "this token says so" and "this is still true" is where a revoked
// administrator keeps their access until expiry. This is the endpoint that
// closes it, and the one `docs/PLAN/08` tells consumers to prefer for anything
// sensitive.
//
// # The whole of Phase 2's authorization semantics
//
//	permission := resource.type + ":" + action
//	allowed    := the subject holds a role, in the caller's project, whose
//	              permission_keys contain that permission
//
// `resource.id` and `resource.attributes` are accepted and unused. They are
// what Phase 4b's policies will read, and accepting them now means a consumer
// that sends them today does not change its code then.
package authz

import (
	"fmt"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/role"
)

// Request is one question, already parsed and scoped.
//
// There is no organization or project field: both come from the caller's
// token, and this type exists partly so that there is nowhere to put one.
type Request struct {
	SubjectUserID string
	Action        string
	ResourceType  string
}

// Grant is one role the subject holds, with what it carries.
type Grant struct {
	RoleKey        string
	PermissionKeys []string
}

// Decision is the answer, in the documented shape.
type Decision struct {
	Allowed bool

	// MatchedPolicy names what decided the outcome. In RBAC the role IS the
	// policy that matched, so this is a role key; Phase 4b replaces the
	// content with a policy name and leaves the field alone.
	MatchedPolicy string

	// Reasons explains the outcome in the same terms, for support and audit.
	//
	// It is part of the response, so it must not distinguish a subject who
	// does not exist from one who holds no matching role — see Decide.
	Reasons []string
}

// Permission is the key a request asks about.
func (r Request) Permission() string {
	return r.ResourceType + ":" + r.Action
}

// Validate checks that the question is answerable.
//
// A malformed permission is refused rather than denied. A deny would be
// correct — nobody can hold a key that cannot exist — and it would hide a
// consumer's bug behind a plausible answer, which is worse than an error for
// the one person who can fix it.
func (r Request) Validate() error {
	switch {
	case r.SubjectUserID == "":
		return fmt.Errorf("subject.user_id is required")
	case r.Action == "":
		return fmt.Errorf("action is required")
	case r.ResourceType == "":
		return fmt.Errorf("resource.type is required")
	}
	if err := role.ValidatePermissionKey(r.Permission()); err != nil {
		return fmt.Errorf("%q and %q do not form a permission key: %w",
			r.ResourceType, r.Action, err)
	}
	return nil
}

// Decide answers the question from the subject's grants.
//
// Pure, with no I/O, for two reasons. It is the piece Phase 4b extends — a
// policy step runs after this one, in one place, rather than threaded through
// a query — and it is the piece worth testing exhaustively, which a function
// that needs a database is not.
//
// # The answer for an unknown subject
//
// A subject with no grants and a subject who does not exist produce the SAME
// decision, with the same reasons. The caller cannot tell them apart, because
// an endpoint that could is an endpoint that answers "does user X exist in
// this organization?" for anybody holding a valid client token —
// `docs/SECURITY/02` §12's enumeration wearing an authorization question's
// clothes. The distinction is logged server-side, where an operator can see it
// and a caller cannot.
func Decide(req Request, grants []Grant) Decision {
	permission := req.Permission()

	for _, g := range grants {
		for _, held := range g.PermissionKeys {
			if held != permission {
				continue
			}
			return Decision{
				Allowed:       true,
				MatchedPolicy: g.RoleKey,
				Reasons: []string{
					fmt.Sprintf("subject holds role %q", g.RoleKey),
					fmt.Sprintf("role %q carries permission %q", g.RoleKey, permission),
				},
			}
		}
	}

	// One refusal for every way of not being allowed. Deliberately says
	// nothing about whether the subject exists, whether the project has such a
	// permission, or how many roles were considered — each of those is a
	// question somebody could ask a few thousand times.
	return Decision{
		Allowed:       false,
		MatchedPolicy: "",
		Reasons:       []string{fmt.Sprintf("subject holds no role carrying permission %q", permission)},
	}
}

// Summarise renders a decision for a log line.
//
// Never includes resource.id or resource.attributes: `docs/PLAN/13` and
// `CLAUDE.md` both name the attributes explicitly, and the id is a consumer's
// own business identifier — `pr_9931` in their purchase-request table.
func Summarise(req Request, d Decision) string {
	outcome := "deny"
	if d.Allowed {
		outcome = "allow"
	}
	return strings.Join([]string{outcome, req.Permission()}, " ")
}

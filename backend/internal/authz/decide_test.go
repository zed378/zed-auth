package authz

import (
	"reflect"
	"strings"
	"testing"
)

func TestThePermissionIsResourceTypeAndAction(t *testing.T) {
	req := Request{SubjectUserID: "u", Action: "approve", ResourceType: "purchase_request"}
	if got := req.Permission(); got != "purchase_request:approve" {
		t.Errorf("permission is %q", got)
	}
}

func TestAnAllowNamesTheRoleThatGrantedIt(t *testing.T) {
	d := Decide(
		Request{SubjectUserID: "u", Action: "approve", ResourceType: "purchase_request"},
		[]Grant{
			{RoleKey: "viewer", PermissionKeys: []string{"purchase_request:read"}},
			{RoleKey: "finance_approver", PermissionKeys: []string{"purchase_request:approve"}},
		},
	)

	if !d.Allowed {
		t.Fatal("denied")
	}
	// In RBAC the role IS the policy that matched. Phase 4b replaces the
	// content of this field with a policy name and leaves the field alone.
	if d.MatchedPolicy != "finance_approver" {
		t.Errorf("matched_policy is %q", d.MatchedPolicy)
	}
	if len(d.Reasons) == 0 {
		t.Error("an allow with no reasons is useless for support and audit")
	}
	for _, r := range d.Reasons {
		if !strings.Contains(r, "finance_approver") && !strings.Contains(r, "purchase_request:approve") {
			t.Errorf("a reason explains nothing: %q", r)
		}
	}
}

// **The whole point of the endpoint.** A near-miss must not be an allow.
func TestANearMissIsADeny(t *testing.T) {
	req := Request{SubjectUserID: "u", Action: "approve", ResourceType: "purchase_request"}

	for _, grants := range [][]Grant{
		// The right resource, the wrong action.
		{{RoleKey: "r", PermissionKeys: []string{"purchase_request:read"}}},
		// The right action, the wrong resource.
		{{RoleKey: "r", PermissionKeys: []string{"invoice:approve"}}},
		// A prefix, which a careless implementation would match.
		{{RoleKey: "r", PermissionKeys: []string{"purchase_request:approve_all"}}},
		{{RoleKey: "r", PermissionKeys: []string{"purchase:approve"}}},
		// A wildcard, which cannot be stored (P2-01) and must not be honoured
		// if one ever arrives from somewhere else.
		{{RoleKey: "r", PermissionKeys: []string{"purchase_request:*"}}},
		{{RoleKey: "r", PermissionKeys: []string{"*"}}},
		// Nothing at all.
		{},
		{{RoleKey: "r", PermissionKeys: nil}},
	} {
		if d := Decide(req, grants); d.Allowed {
			t.Errorf("allowed by %v", grants)
		}
	}
}

// A subject with no grants and a subject who does not exist reach this function
// identically — the caller passes an empty slice for both — so the equivalence
// is a property of the design rather than of a comparison somewhere.
//
// This pins the half that lives here: every denial reads the same, whatever
// produced it. An endpoint whose reasons distinguished "no such user" from "no
// such role" would answer "does this user exist?" for anybody with a token.
func TestEveryDenialReadsTheSame(t *testing.T) {
	req := Request{SubjectUserID: "u", Action: "approve", ResourceType: "purchase_request"}

	noGrants := Decide(req, nil)
	wrongGrants := Decide(req, []Grant{{RoleKey: "other", PermissionKeys: []string{"invoice:read"}}})

	if !reflect.DeepEqual(noGrants, wrongGrants) {
		t.Errorf("two denials differ:\n  %+v\n  %+v", noGrants, wrongGrants)
	}
	if noGrants.MatchedPolicy != "" {
		t.Errorf("a denial names a policy: %q", noGrants.MatchedPolicy)
	}
	// And the reason must not hint at how many roles were considered.
	for _, r := range noGrants.Reasons {
		if strings.Contains(r, "other") {
			t.Errorf("a denial leaks a role the subject holds: %q", r)
		}
	}
}

func TestAMalformedQuestionIsRefusedRatherThanDenied(t *testing.T) {
	for _, req := range []Request{
		{Action: "approve", ResourceType: "purchase_request"},             // no subject
		{SubjectUserID: "u", ResourceType: "purchase_request"},            // no action
		{SubjectUserID: "u", Action: "approve"},                           // no resource type
		{SubjectUserID: "u", Action: "APPROVE", ResourceType: "invoice"},  // not a permission key
		{SubjectUserID: "u", Action: "approve", ResourceType: "Invoice"},  // ditto
		{SubjectUserID: "u", Action: "app rove", ResourceType: "invoice"}, // ditto
		{SubjectUserID: "u", Action: "*", ResourceType: "invoice"},        // a wildcard
	} {
		if err := req.Validate(); err == nil {
			t.Errorf("accepted %+v", req)
		}
	}

	ok := Request{SubjectUserID: "u", Action: "approve", ResourceType: "purchase_request"}
	if err := ok.Validate(); err != nil {
		t.Errorf("a well-formed question was refused: %v", err)
	}
}

// The log line carries the outcome and the permission, and never the resource
// id or its attributes — `docs/PLAN/13` and `CLAUDE.md` both name the
// attributes, and the id is the consumer's own business identifier.
func TestTheLogSummaryCarriesNothingSensitive(t *testing.T) {
	req := Request{SubjectUserID: "user-1", Action: "approve", ResourceType: "purchase_request"}
	summary := Summarise(req, Decide(req, nil))

	if !strings.Contains(summary, "purchase_request:approve") {
		t.Errorf("the summary does not say what was asked: %q", summary)
	}
	for _, forbidden := range []string{"pr_9931", "department", "finance", "8000000"} {
		if strings.Contains(summary, forbidden) {
			t.Errorf("the summary carries %q: %s", forbidden, summary)
		}
	}
}

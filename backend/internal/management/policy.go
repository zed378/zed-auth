package management

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// The permission every /v1 route demands, in one table (P1-16).
//
// Chain.Handle takes a Requirement per route, which works for a route
// registered by hand. These are registered by the GENERATED router, and
// oapi-codegen's chi server applies its middlewares to every operation
// uniformly — there is nowhere to hang a per-route argument. So the
// requirement is looked up at request time instead, keyed by method and chi
// route pattern.
//
// A table has one advantage over an argument that is worth having anyway:
// **the whole permission surface of the API is readable in one screen.** With
// thirty endpoints, "which of these can an ORG_ADMIN reach" is a question
// somebody will ask during a review, and the answer should not require reading
// thirty registrations.

// Policy maps "METHOD /route/{pattern}" to what a caller must hold.
//
// A route ABSENT from this table gets the zero Requirement, which no caller can
// satisfy. That is deliberate and is the same default-refuse property
// Chain.Handle has: forgetting to annotate an endpoint makes it unreachable
// rather than open.
//
// Unreachable is safe but still broken, so TestEveryRouteHasAPolicy walks the
// routes the router actually registers and fails on any that is missing. The
// failure lands on the first test run rather than as a 403 in production.
var Policy = map[string]Requirement{
	// --- Organizations (P1-16) ---
	//
	// Listing and creating are instance-scoped because they are genuinely
	// cross-tenant: a list spans organizations, and a create happens before the
	// organization it makes exists.
	"GET /v1/organizations":             {Role: InstanceOwner, Scope: ScopeInstance},
	"POST /v1/organizations":            {Role: InstanceOwner, Scope: ScopeInstance},
	"GET /v1/organizations/{org_id}":    {Role: OrgAdmin, Scope: ScopeOrganization},
	"PATCH /v1/organizations/{org_id}":  {Role: OrgOwner, Scope: ScopeOrganization},
	"DELETE /v1/organizations/{org_id}": {Role: InstanceOwner, Scope: ScopeInstance},

	// --- Projects (P1-17) ---
	//
	// All organization-scoped: a project belongs to exactly one tenant, and the
	// organization is in the path. DELETE needs ORG_OWNER because it is the
	// only destructive one — and it still refuses while anything is attached.
	"GET /v1/organizations/{org_id}/projects":                 {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/projects":                {Role: OrgAdmin, Scope: ScopeOrganization},
	"GET /v1/organizations/{org_id}/projects/{project_id}":    {Role: OrgAdmin, Scope: ScopeOrganization},
	"PATCH /v1/organizations/{org_id}/projects/{project_id}":  {Role: OrgAdmin, Scope: ScopeOrganization},
	"DELETE /v1/organizations/{org_id}/projects/{project_id}": {Role: OrgOwner, Scope: ScopeOrganization},

	// --- Applications (P1-18) ---
	//
	// Organization-scoped like everything nested under one. DELETE is the only
	// one raised: deleting a registration stops every login through that client
	// at once, without warning to the consumer application.
	//
	// Rotation deliberately stays at ORG_ADMIN. It is the response to a
	// suspected leak, and a control that needs the organization owner woken up
	// is a control that gets skipped at 3am. It is loud in the audit log instead.
	"GET /v1/organizations/{org_id}/projects/{project_id}/applications":                                 {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/projects/{project_id}/applications":                                {Role: OrgAdmin, Scope: ScopeOrganization},
	"GET /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}":                {Role: OrgAdmin, Scope: ScopeOrganization},
	"PATCH /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}":              {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}/rotate-secret": {Role: OrgAdmin, Scope: ScopeOrganization},
	"DELETE /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}":             {Role: OrgOwner, Scope: ScopeOrganization},

	// --- Users (P1-19) ---
	//
	// Every one is ORG_ADMIN, and nothing is raised to ORG_OWNER. That is a
	// deliberate departure from projects and applications: those raised DELETE
	// because it is irreversible, and there is no irreversible operation here.
	// Deactivation is the REVERSIBLE thing the card asks for instead of
	// deletion, so raising it would make the safe action harder than the unsafe
	// one it replaced — and administrators route around that.
	"GET /v1/organizations/{org_id}/users":                           {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/users":                          {Role: OrgAdmin, Scope: ScopeOrganization},
	"GET /v1/organizations/{org_id}/users/{user_id}":                 {Role: OrgAdmin, Scope: ScopeOrganization},
	"PATCH /v1/organizations/{org_id}/users/{user_id}":               {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/users/{user_id}/deactivate":     {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/users/{user_id}/reactivate":     {Role: OrgAdmin, Scope: ScopeOrganization},
	"POST /v1/organizations/{org_id}/users/{user_id}/password-reset": {Role: OrgAdmin, Scope: ScopeOrganization},
}

// PolicyKey names a route the way Policy does.
func PolicyKey(method, pattern string) string {
	return method + " " + strings.TrimSuffix(pattern, "/")
}

// RequirementFor returns what a request must satisfy, and whether the route was
// annotated at all.
//
// The bool is not decoration. A caller that ignores it and uses the zero value
// gets default-refuse, which is correct; a caller that checks it can say WHY,
// which is the difference between an operator seeing "no grant of
// INSTANCE_OWNER over " and seeing "this route has no declared permission".
func RequirementFor(r *http.Request) (Requirement, bool) {
	rc := chi.RouteContext(r.Context())
	if rc == nil {
		return Requirement{}, false
	}
	req, ok := Policy[PolicyKey(r.Method, rc.RoutePattern())]
	return req, ok
}

// Guard is Require with the requirement looked up rather than passed.
//
// Registered as a middleware on the generated router, where every operation
// gets the same chain and the route pattern is the only thing distinguishing
// them.
func (m *Middleware) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, annotated := RequirementFor(r)
		if !annotated && m.Log != nil {
			// Loud, because the request is about to be refused for a reason
			// that looks like a permission problem and is actually a missing
			// registration. Without this line an operator would go looking at
			// the caller's roles.
			m.Log.Error("a /v1 route has no declared permission and is therefore unreachable",
				"method", r.Method, "pattern", routePattern(r))
		}
		m.Require(req, next).ServeHTTP(w, r)
	})
}

// PolicyRoutes lists the annotated routes, sorted. For tests and for a
// reviewer who wants the permission surface as a list.
func PolicyRoutes() []string {
	routes := make([]string, 0, len(Policy))
	for route := range Policy {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

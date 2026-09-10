package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/management"
)

// newTestServer builds a server the way a test needs one: no Management API
// chain, so /v1 is registered by the generated router and guarded into a 404.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(testHTTPConfig("127.0.0.1:0"), Deps{
		Logger: discardLogger(),
		Health: &Health{},
	})
}

// Every /v1 route the router registers has a declared permission.
//
// `management.Policy` is a map, and a route absent from it gets the zero
// Requirement, which no caller can satisfy. That default is right — forgetting
// to annotate an endpoint makes it unreachable rather than open — but
// unreachable is still broken, and a 403 on a working endpoint is a confusing
// thing to debug.
//
// So the gap is caught here instead, by walking what the router ACTUALLY
// registers rather than by reading a list somebody maintains. Adding an
// operation to openapi.yaml and forgetting its permission fails this test on
// the first run.
func TestEveryV1RouteHasADeclaredPermission(t *testing.T) {
	srv := newTestServer(t)

	var missing []string
	var found int

	err := chi.Walk(srv.mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/v1/") && route != "/v1" {
			return nil
		}
		found++
		if _, declared := management.Policy[management.PolicyKey(method, route)]; !declared {
			missing = append(missing, management.PolicyKey(method, route))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the router: %v", err)
	}

	// Without this, a router that registered no /v1 routes at all would pass
	// silently — which is exactly what happened between P1-15 and P1-16.
	if found == 0 {
		t.Fatal("the router registered no /v1 routes, so this test proves nothing")
	}
	if len(missing) > 0 {
		t.Errorf("these /v1 routes have no entry in management.Policy and are therefore unreachable:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

// And the reverse: nothing in Policy names a route that does not exist.
//
// A stale entry is harmless at runtime and misleading to read — the table is
// meant to be the permission surface of the API, and a permission for an
// endpoint nobody serves makes it a permission surface plus some history.
func TestPolicyNamesNoRouteThatDoesNotExist(t *testing.T) {
	srv := newTestServer(t)

	registered := map[string]bool{}
	err := chi.Walk(srv.mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[management.PolicyKey(method, route)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking the router: %v", err)
	}

	for _, route := range management.PolicyRoutes() {
		if !registered[route] {
			t.Errorf("management.Policy declares %q, which the router does not serve", route)
		}
	}
}

// A /v1 request against a server with no Management API configured is a 404,
// not an unguarded call into a nil handler.
//
// The generated router registers the /v1 routes from the spec whether or not a
// deployment serves them, so "not configured" has to mean something explicit.
func TestV1IsNotFoundWhenTheManagementApiIsNotConfigured(t *testing.T) {
	srv := newTestServer(t) // no V1 chain

	for _, target := range []string{
		"/v1/organizations",
		"/v1/organizations/8f3e6b2a-1c4d-4e5f-9a0b-7c8d9e0f1a2b",
	} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))

		if w.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", target, w.Code)
		}
	}
}

// The probes and the discovery documents stay open. They are fetched
// anonymously by every relying party and by the orchestrator, and a bearer
// check applied to them would be an outage rather than a control.
func TestTheOpenEndpointsAreStillOpen(t *testing.T) {
	srv := newTestServer(t)

	for _, target := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))

		if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Errorf("%s = %d — the /v1 guard is engaging outside /v1", target, w.Code)
		}
	}
}

// The audit log serves exactly one operation, and it is a read (P1-20).
//
// **Asserted against the router**, not against the package that implements it.
// "We did not write a delete handler" is a statement about intent; what a
// caller meets is what the router registers, and that is generated from the
// contract — so the contract growing a write here fails this test rather than
// quietly becoming a capability.
//
// The guarantee underneath is P0-07's: `events` is append-only at the database
// level, where the application role holds no UPDATE and no DELETE on it
// (docs/SECURITY/02 §19). An endpoint offering a way around that would turn a
// privilege guarantee into a matter of trust.
func TestTheAuditLogServesExactlyOneOperation(t *testing.T) {
	srv := newTestServer(t)

	var operations []string
	err := chi.Walk(srv.mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasSuffix(route, "/events") || strings.Contains(route, "/events/") {
			operations = append(operations, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the router: %v", err)
	}

	if len(operations) == 0 {
		t.Fatal("the router registers no audit log route, so this test proves nothing")
	}
	if len(operations) != 1 {
		t.Fatalf("the audit log serves %d operations, want 1:\n  %s",
			len(operations), strings.Join(operations, "\n  "))
	}
	if !strings.HasPrefix(operations[0], http.MethodGet+" ") {
		t.Errorf("the one operation is %q, want a GET", operations[0])
	}
}

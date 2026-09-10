package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
)

// Checker reports whether a dependency is usable. Implemented by the Postgres
// and Redis stores.
type Checker interface {
	// Name identifies the dependency in logs and metrics. It is never returned
	// to an unauthenticated caller.
	Name() string
	// Check returns nil when the dependency is usable.
	Check(ctx context.Context) error
}

// Health holds the readiness dependencies and serves both probes.
//
// It implements api.StrictServerInterface, the interface generated from
// openapi/openapi.yaml. That is the enforcement mechanism rather than a
// stylistic choice: if the spec's probe responses change and this type is not
// updated, the build fails at the assertion below (ADR-013). A CI check that
// compares the two would find the same problem later, after it was pushed.
type Health struct {
	// Checks are evaluated by /readyz. /healthz never touches them.
	Checks []Checker
	// Timeout bounds the whole readiness evaluation. A readiness probe that
	// hangs is worse than one that fails: the orchestrator waits instead of
	// routing traffic elsewhere.
	Timeout time.Duration
}

// Health implements the two probe methods of the generated interface. The
// discovery endpoints are implemented by internal/oidc, and `apiRoutes` in
// server.go asserts the two halves cover it between them — Go cannot express
// "these types satisfy this interface jointly", so the assertion lives on the
// struct that embeds both.

// GetLiveness answers "is this process alive and not wedged?".
//
// It deliberately checks nothing. docs/PLAN/14-DEPLOYMENT.md separates liveness from
// readiness precisely so a transient database problem restarts nothing — if
// /healthz consulted Postgres, a brief database blip would make Kubernetes kill
// every pod at once, turning a recoverable dependency failure into an outage.
func (h *Health) GetLiveness(context.Context, api.GetLivenessRequestObject) (api.GetLivenessResponseObject, error) {
	return api.GetLiveness200JSONResponse{Status: api.Ok}, nil
}

// GetReadiness answers "should this instance receive traffic?".
//
// It verifies every dependency the service needs to serve a request. A rolling
// update must not route traffic to an instance whose connection pool is not up
// (docs/PLAN/14-DEPLOYMENT.md § Deployment Model).
//
// The response body names no dependency, no host, no version, and no error
// detail. A readiness endpoint is reachable from the load balancer and is a
// standard reconnaissance target; telling an anonymous caller "postgres:
// connection refused at 10.0.4.2:5432" hands over infrastructure topology
// (docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §12 Enumeration). The detail
// goes to the logs, which is where an operator can see it.
//
// The returned error is always nil. A dependency failure is a 503, which is a
// documented response and therefore a value, not an error — returning an error
// here would produce an undocumented 500 and lose the distinction between "not
// ready" and "broken".
func (h *Health) GetReadiness(ctx context.Context, _ api.GetReadinessRequestObject) (api.GetReadinessResponseObject, error) {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for _, c := range h.Checks {
		if err := c.Check(ctx); err != nil {
			return api.GetReadiness503JSONResponse{Status: api.Unavailable}, nil
		}
	}

	return api.GetReadiness200JSONResponse{Status: api.Ready}, nil
}

// noStore keeps probe responses out of every cache between here and the
// orchestrator.
//
// Without it a cached readiness response lets an unready instance look ready
// for the life of the cache entry, which is the one failure this endpoint
// exists to prevent. The generated response writers set Content-Type and the
// status code and nothing else, so this is applied as middleware rather than
// per handler — a header that has to be remembered in each handler is a header
// that will be forgotten in one.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

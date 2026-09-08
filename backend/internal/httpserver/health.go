package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
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

// Health holds the readiness dependencies.
type Health struct {
	// Checks are evaluated by /readyz. /healthz never touches them.
	Checks []Checker
	// Timeout bounds the whole readiness evaluation. A readiness probe that
	// hangs is worse than one that fails: the orchestrator waits instead of
	// routing traffic elsewhere.
	Timeout time.Duration
}

// Liveness answers "is this process alive and not wedged?".
//
// It deliberately checks nothing. PLAN/14-DEPLOYMENT.md separates liveness from
// readiness precisely so a transient database problem restarts nothing — if
// /healthz consulted Postgres, a brief database blip would make Kubernetes kill
// every pod at once, turning a recoverable dependency failure into an outage.
func (h *Health) Liveness() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeHealth(w, http.StatusOK, "ok")
	}
}

// Readiness answers "should this instance receive traffic?".
//
// It verifies every dependency the service needs to serve a request. A rolling
// update must not route traffic to an instance whose connection pool is not up
// (PLAN/14-DEPLOYMENT.md § Deployment Model).
//
// The response body names no dependency, no host, no version, and no error
// detail. A readiness endpoint is reachable from the load balancer and is a
// standard reconnaissance target; telling an anonymous caller "postgres:
// connection refused at 10.0.4.2:5432" hands over infrastructure topology
// (SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §12 Enumeration). The detail
// goes to the logs, which is where an operator can see it.
func (h *Health) Readiness() http.HandlerFunc {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		for _, c := range h.Checks {
			if err := c.Check(ctx); err != nil {
				// Logged with the dependency name by the access log middleware's
				// caller; the body stays opaque.
				writeHealth(w, http.StatusServiceUnavailable, "unavailable")
				return
			}
		}

		writeHealth(w, http.StatusOK, "ready")
	}
}

func writeHealth(w http.ResponseWriter, status int, state string) {
	w.Header().Set("Content-Type", "application/json")
	// A cached readiness response would let an unready instance look ready.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": state})
}

package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInstrumentRecordsDurationAndCount(t *testing.T) {
	m := NewMetrics("test", "v0")

	h := m.Instrument(func(*http.Request) string { return "/v1/organizations/{org_id}/users" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	for i := 0; i < 3; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/organizations/abc/users", nil))
	}

	got := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("GET", "/v1/organizations/{org_id}/users", "2xx"))
	if got != 3 {
		t.Errorf("request count = %v, want 3", got)
	}
}

// The route label must be the chi PATTERN, never the concrete path.
//
// A concrete path produces one time series per organization, per user, per id
// — unbounded cardinality, and the usual way an application change takes down
// a Prometheus server without anyone attacking anything.
func TestRouteLabelUsesThePatternNotThePath(t *testing.T) {
	m := NewMetrics("test", "v0")

	h := m.Instrument(func(*http.Request) string { return "/v1/organizations/{org_id}/users/{user_id}" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	// Ten distinct concrete paths.
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		h.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/v1/organizations/org-"+id+"/users/user-"+id, nil))
	}

	body := gather(t, m)
	if strings.Contains(body, "org-a") || strings.Contains(body, "user-a") {
		t.Error("a concrete path leaked into a metric label — this is unbounded cardinality")
	}

	got := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("GET", "/v1/organizations/{org_id}/users/{user_id}", "2xx"))
	if got != 10 {
		t.Errorf("ten requests should collapse into one series with count 10, got %v", got)
	}
}

// An unmatched path is entirely attacker-controlled. A scanner walking a
// wordlist would otherwise create a time series per probe, turning the metrics
// endpoint into the denial-of-service vector.
func TestUnmatchedRoutesCollapseToOneLabel(t *testing.T) {
	m := NewMetrics("test", "v0")

	for _, pattern := range []string{"", "/*"} {
		h := m.Instrument(func(*http.Request) string { return pattern })(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/wp-admin/setup-config.php", nil))
	}

	body := gather(t, m)
	if strings.Contains(body, "wp-admin") {
		t.Error("an attacker-controlled path became a metric label")
	}

	got := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("GET", "unmatched", "4xx"))
	if got != 2 {
		t.Errorf("both unmatched shapes should share one series, got %v", got)
	}
}

// The exact code lives in the logs. Here it would multiply every series by the
// number of distinct codes for no analytical gain — alerts care about the 5xx
// rate, not 502 versus 503.
func TestStatusIsReducedToItsClass(t *testing.T) {
	for code, want := range map[int]string{
		100: "1xx", 200: "2xx", 204: "2xx", 301: "3xx",
		400: "4xx", 404: "4xx", 429: "4xx",
		500: "5xx", 503: "5xx",
	} {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

// docs/PLAN/12's targets run from p50 < 20ms to p99 < 600ms. A histogram whose
// buckets straddle a target badly cannot answer whether the target was met,
// which is the only question these metrics exist to answer.
func TestBucketsCoverThePerformanceTargets(t *testing.T) {
	targets := map[string]float64{
		"authz check p50":    0.020,
		"authz check p95":    0.080,
		"userinfo p95":       0.100,
		"authorize p95":      0.150,
		"token p95":          0.200,
		"token p99":          0.400,
		"management api p99": 0.600,
	}

	for name, target := range targets {
		found := false
		for _, b := range latencyBuckets {
			if b == target {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no bucket boundary at %v (%s) — the quantile would have to be interpolated "+
				"across a wide bucket to judge that target", target, name)
		}
	}
}

func TestInFlightReturnsToZero(t *testing.T) {
	m := NewMetrics("test", "v0")

	h := m.Instrument(func(*http.Request) string { return "/x" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := testutil.ToFloat64(m.RequestsInFlight); got != 1 {
				t.Errorf("in-flight during a request = %v, want 1", got)
			}
			w.WriteHeader(http.StatusOK)
		}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := testutil.ToFloat64(m.RequestsInFlight); got != 0 {
		t.Errorf("in-flight after the request = %v, want 0 — a leak here reads as permanent saturation", got)
	}
}

// A panicking handler must still decrement in-flight, or the gauge climbs
// forever and every saturation reading becomes wrong.
func TestInFlightIsReleasedOnPanic(t *testing.T) {
	m := NewMetrics("test", "v0")

	h := m.Instrument(func(*http.Request) string { return "/x" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}))

	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}()

	if got := testutil.ToFloat64(m.RequestsInFlight); got != 0 {
		t.Errorf("in-flight after a panic = %v, want 0", got)
	}
}

// Every instrument docs/PLAN/13 § Metrics names must exist from the start. Naming
// them once means the dashboard, the alerts, and the code agree — a metric
// renamed after a dashboard is built produces an empty panel, not an error.
func TestEveryMetricNamedInThePlanIsRegistered(t *testing.T) {
	m := NewMetrics("test", "v0")

	// Every vector needs at least one series to appear in a scrape at all —
	// an unobserved HistogramVec or CounterVec produces no output.
	//
	// That is worth knowing beyond this test: a dashboard panel for
	// http_requests_total{status_class="5xx"} shows "no data" rather than zero
	// until the first 5xx occurs, so an alert written as `== 0` never fires and
	// a panel showing nothing is ambiguous between "healthy" and "not
	// reporting". Alert expressions have to account for absence.
	h := m.Instrument(func(*http.Request) string { return "/healthz" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	m.LoginAttempts.WithLabelValues("success").Inc()
	m.TokensIssued.WithLabelValues("authorization_code").Inc()
	m.Lockouts.Inc()
	m.AuthzCheckDuration.WithLabelValues("rbac").Observe(0.01)
	m.AuthzDecisions.WithLabelValues("allow", "rbac").Inc()
	m.PolicyEvalDuration.Observe(0.01)
	m.ProjectGrantChanges.WithLabelValues("created").Inc()
	m.AuditWrites.WithLabelValues("success").Inc()
	m.AuditPartitionRunway.Set(3)
	m.AuditPartitionErrors.Inc()
	m.RedisDuration.WithLabelValues("get").Observe(0.001)
	m.InstanceScopedAccess.WithLabelValues("test").Inc()
	m.RequestsInFlight.Set(0)

	body := gather(t, m)

	required := []string{
		// docs/PLAN/13 § Metrics, item by item.
		"http_request_duration_seconds",      // latency per endpoint
		"http_requests_total",                // error rate per endpoint
		"auth_login_attempts_total",          // failed vs successful login rate
		"auth_tokens_issued_total",           // tokens issued per second
		"authz_check_duration_seconds",       // /v1/authz/check latency
		"authz_policy_eval_duration_seconds", // OPA evaluation duration
		"authz_project_grant_changes_total",  // grant create/revoke rate
		"redis_operation_duration_seconds",   // Redis latency
		// Beyond the plan, from failures this project has already hit.
		"audit_partition_runway_months",
		"audit_partition_maintenance_errors_total",
		"db_instance_scoped_access_total",
		// Runtime.
		"go_goroutines",
	}

	for _, name := range required {
		if !strings.Contains(body, name) {
			t.Errorf("metric %q is not exposed — docs/PLAN/13 names it, or a past failure requires it", name)
		}
	}
}

// Const labels let one dashboard distinguish environments and versions without
// a per-deployment copy.
func TestServiceAndVersionAreConstLabels(t *testing.T) {
	m := NewMetrics("authservice", "v1.2.3")
	m.Lockouts.Inc()

	body := gather(t, m)
	if !strings.Contains(body, `service="authservice"`) || !strings.Contains(body, `version="v1.2.3"`) {
		t.Errorf("service and version should be const labels on every metric:\n%s", firstLines(body, 5))
	}
}

func gather(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics endpoint returned %d", rec.Code)
	}
	return rec.Body.String()
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

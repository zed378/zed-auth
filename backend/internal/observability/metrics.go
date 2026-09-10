package observability

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the instruments this service exposes.
//
// docs/PLAN/13-OBSERVABILITY.md § Metrics lists what must be measurable, and
// docs/PLAN/12-PERFORMANCE.md sets the targets those measurements are judged
// against. The two are deliberately connected: a latency target nobody
// measures is an aspiration, and a metric with no target is a number nobody
// knows how to read.
//
// Instruments for later phases are declared here now rather than added
// piecemeal. Naming them once means the dashboard, the alerts, and the code
// agree from the start — a metric renamed after a dashboard is built silently
// produces an empty panel rather than an error.
type Metrics struct {
	registry *prometheus.Registry

	// --- HTTP (all phases) ---

	// RequestDuration is the source of every p50/p95/p99 in docs/PLAN/12's table.
	//
	// The buckets are chosen around those targets rather than left at the
	// client library's defaults: the defaults top out at 10s, which wastes
	// resolution on an auth service where the interesting range is 5ms to
	// 500ms. A histogram whose buckets straddle the target badly cannot answer
	// whether the target was met.
	RequestDuration  *prometheus.HistogramVec
	RequestsTotal    *prometheus.CounterVec
	RequestsInFlight prometheus.Gauge

	// --- Authentication (P1-12, P1-13) ---

	// LoginAttempts is labelled by outcome. docs/PLAN/13 § Alerting wants a spike
	// in failures from one IP or account to be visible, and a single counter
	// split by outcome is what makes the ratio queryable.
	LoginAttempts *prometheus.CounterVec
	TokensIssued  *prometheus.CounterVec
	Lockouts      prometheus.Counter

	// PasswordPolicyRejections counts refused passwords, labelled by the rule
	// that refused them (P1-02).
	PasswordPolicyRejections *prometheus.CounterVec

	// --- Token endpoint (P1-07) ---

	// TokenErrors is labelled by grant and OAuth error code. "token requests
	// are failing" is not actionable; "invalid_client on client_credentials"
	// names a misconfigured service account.
	TokenErrors *prometheus.CounterVec

	// TokenDuration is bucketed on docs/PLAN/12's targets (p50 < 50ms, p95 < 200ms,
	// p99 < 400ms) so a quantile query answers "did we meet it" without
	// interpolating across a wide bucket — the same reasoning P0-11 applied to
	// the request histogram.
	TokenDuration *prometheus.HistogramVec

	// --- UserInfo endpoint (P1-08) ---

	// UserInfoTotal is labelled by outcome only, and the outcome vocabulary is
	// deliberately coarse: "invalid_token" covers expired, forged, wrong
	// audience and revoked-session alike. Splitting them would move the oracle
	// the response body refuses to be into /metrics, which is scraped,
	// retained, and usually more widely readable than the audit log.
	UserInfoTotal *prometheus.CounterVec

	// UserInfoDuration exists because docs/PLAN/12 gives this endpoint its own
	// target — p95 < 100ms — on the grounds that consumer SPAs call it on
	// every page load. An endpoint with a named budget needs a named
	// measurement, or the budget is a sentence nobody can check.
	UserInfoDuration *prometheus.HistogramVec

	// --- Authorization endpoint (P1-06) ---

	// AuthorizeTotal is labelled by outcome and path. The second label carries
	// the OAuth error code on a denial, which is what turns "authorization is
	// failing" into "this client is sending the wrong redirect_uri".
	AuthorizeTotal *prometheus.CounterVec

	// AuthorizeDuration measures the silent path only. docs/PLAN/12 sets a target
	// for it specifically (p95 < 150ms), and including the interactive path
	// would average in the time a human spends typing a password.
	AuthorizeDuration *prometheus.HistogramVec

	// --- Sessions (P1-11) ---

	SessionsCreated *prometheus.CounterVec
	SessionsRevoked *prometheus.CounterVec

	// SessionLookupDuration is labelled by source, so the cache hit rate is
	// readable from the same metric that shows the latency. docs/PLAN/12 gives
	// /oauth/authorize 150ms at p95 for everything, and this is the part of it
	// that a cache is supposed to make free.
	SessionLookupDuration *prometheus.HistogramVec

	// SessionCacheInvalidationFailures is load-bearing rather than
	// informational. A revoked session stops being served immediately only
	// because the cache entry is deleted; if that delete fails, the session
	// stays usable until the TTL. This counter is the only signal that
	// happened.
	SessionCacheInvalidationFailures prometheus.Counter

	// PasswordBreachChecks counts corpus lookups by outcome: clean, breached,
	// skipped, disabled.
	//
	// This counter is not decoration — it is half of ADR-015. The fail-open
	// decision is defensible only because a skipped check is visible, so a
	// deployment where `skipped` climbs is a deployment where breached
	// passwords are being accepted, and nothing else would say so.
	PasswordBreachChecks *prometheus.CounterVec

	// --- Authorization (P2-06, P4B-02) ---

	AuthzCheckDuration *prometheus.HistogramVec
	AuthzDecisions     *prometheus.CounterVec
	// PolicyEvalDuration is named in docs/PLAN/13 explicitly. It stays at zero
	// until Phase 4b, which is correct and visible.
	PolicyEvalDuration prometheus.Histogram

	// --- Delegation (P4-01) ---

	// ProjectGrantChanges exists because docs/PLAN/13 names an unusual spike in
	// grant creation or revocation as a possible misuse indicator.
	ProjectGrantChanges *prometheus.CounterVec

	// --- Audit (P0-12) ---

	AuditWrites *prometheus.CounterVec
	// AuditPartitionRunway is months of partition runway remaining.
	//
	// This exists because the maintenance goroutine has no supervisor: if it
	// stops, nothing notices until every audited action fails at a month
	// boundary. A gauge that drifts toward zero is visible weeks before that,
	// which is the difference between a ticket and an outage (P0-12).
	AuditPartitionRunway prometheus.Gauge
	AuditPartitionErrors prometheus.Counter

	// --- Dependencies ---

	RedisDuration *prometheus.HistogramVec
	// InstanceScopedAccess counts uses of the cross-tenant database path.
	// docs/PLAN/08 Part B wants that path auditable; a rising count without a
	// matching change in operations is worth a question.
	InstanceScopedAccess *prometheus.CounterVec
}

// Latency buckets in seconds, chosen around docs/PLAN/12's targets: the tightest
// is /v1/authz/check at p50 < 20ms, the loosest Management API CRUD at
// p99 < 600ms. Bucket edges sit near each target so a query can answer
// "are we meeting it" without interpolating across a wide bucket.
var latencyBuckets = []float64{
	0.005, 0.010, 0.020, 0.040, 0.050, 0.080,
	0.100, 0.150, 0.200, 0.300, 0.400, 0.600,
	1.0, 2.5, 5.0,
}

// NewMetrics builds the instruments and their registry.
//
// A dedicated registry rather than the global default: the default is
// package-global mutable state that any dependency can register into, which
// makes the exposed metric set depend on the import graph. An explicit
// registry makes it depend on this file.
func NewMetrics(service, version string) *Metrics {
	reg := prometheus.NewRegistry()

	constLabels := prometheus.Labels{"service": service, "version": version}
	factory := promauto{reg: reg, constLabels: constLabels}

	m := &Metrics{
		registry: reg,

		RequestDuration: factory.histogramVec(
			"http_request_duration_seconds",
			"HTTP request latency. The source of every p50/p95/p99 in docs/PLAN/12's target table.",
			latencyBuckets, "method", "route", "status_class"),

		RequestsTotal: factory.counterVec(
			"http_requests_total",
			"HTTP requests by outcome.",
			"method", "route", "status_class"),

		RequestsInFlight: factory.gauge(
			"http_requests_in_flight",
			"Requests currently being served. A rising floor indicates saturation before latency shows it."),

		LoginAttempts: factory.counterVec(
			"auth_login_attempts_total",
			"Login attempts by outcome. docs/PLAN/13 § Alerting keys the brute-force alert on the failure rate.",
			"outcome"),

		TokenErrors: factory.counterVec(
			"auth_token_errors_total",
			"Token endpoint failures by grant and OAuth error code.",
			"grant", "error"),
		TokenDuration: factory.histogramVec(
			"auth_token_duration_seconds",
			"Token endpoint latency by grant, bucketed on docs/PLAN/12's targets.",
			[]float64{0.01, 0.025, 0.05, 0.1, 0.2, 0.4, 0.8},
			"grant"),

		UserInfoTotal: factory.counterVec(
			"auth_userinfo_total",
			"UserInfo requests by outcome. The outcome is coarse on purpose: every unusable token is one label.",
			"outcome"),
		UserInfoDuration: factory.histogramVec(
			"auth_userinfo_duration_seconds",
			"UserInfo latency. docs/PLAN/12 targets p50 < 30ms and p95 < 100ms; SPAs call this on every page load.",
			[]float64{0.005, 0.01, 0.025, 0.03, 0.05, 0.1, 0.2, 0.4},
			"outcome"),

		AuthorizeTotal: factory.counterVec(
			"auth_authorize_total",
			"Authorization requests by outcome and path. On a denial the second label is the OAuth error code.",
			"outcome", "path"),
		AuthorizeDuration: factory.histogramVec(
			"auth_authorize_duration_seconds",
			"Silent-SSO latency. docs/PLAN/12 targets p95 < 150ms for this path specifically.",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.3, 0.6},
			"path"),

		SessionsCreated: factory.counterVec(
			"auth_sessions_created_total",
			"Browser sessions created, by the factors used.",
			"auth_method"),
		SessionsRevoked: factory.counterVec(
			"auth_sessions_revoked_total",
			"Sessions revoked, by reason. `logout` and `admin` are different facts.",
			"reason"),
		SessionLookupDuration: factory.histogramVec(
			"auth_session_lookup_duration_seconds",
			"Session lookup latency by source. The cache hit rate is the ratio of the counts.",
			[]float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1},
			"source"),
		SessionCacheInvalidationFailures: factory.counter(
			"auth_session_cache_invalidation_failures_total",
			"Revocations that did not reach the cache. Each one is a session that stays usable until its cache entry expires."),

		PasswordPolicyRejections: factory.counterVec(
			"auth_password_policy_rejections_total",
			"Passwords refused by policy, by the rule that refused them.",
			"rule"),
		PasswordBreachChecks: factory.counterVec(
			"auth_password_breach_checks_total",
			"Breached-password corpus lookups by outcome. A rising `skipped` means ADR-015 is failing open and unchecked passwords are being accepted.",
			"outcome"),

		TokensIssued: factory.counterVec(
			"auth_tokens_issued_total",
			"Tokens issued, by grant type.",
			"grant_type"),

		Lockouts: factory.counter(
			"auth_lockouts_total",
			"Accounts temporarily locked by rate limiting (P1-13)."),

		AuthzCheckDuration: factory.histogramVec(
			"authz_check_duration_seconds",
			"Authorization decision latency. docs/PLAN/12: p95 < 80ms for RBAC, < 150ms with ABAC.",
			latencyBuckets, "mode"),

		AuthzDecisions: factory.counterVec(
			"authz_decisions_total",
			"Authorization decisions by result. A sudden shift in the allow ratio is worth investigating.",
			"decision", "mode"),

		PolicyEvalDuration: factory.histogram(
			"authz_policy_eval_duration_seconds",
			"OPA policy evaluation duration. Named in docs/PLAN/13; zero until Phase 4b.",
			latencyBuckets),

		ProjectGrantChanges: factory.counterVec(
			"authz_project_grant_changes_total",
			"Project Grant creations and revocations. docs/PLAN/13 names an unusual spike as a possible misuse indicator.",
			"action"),

		AuditWrites: factory.counterVec(
			"audit_events_written_total",
			"Audit events written, by outcome. A rising failure count means actions are failing too, since the write is in-transaction (P0-12).",
			"outcome"),

		AuditPartitionRunway: factory.gauge(
			"audit_partition_runway_months",
			"Months of events-partition runway remaining. Drifting to zero means every audited action will fail at a month boundary (P0-12)."),

		AuditPartitionErrors: factory.counter(
			"audit_partition_maintenance_errors_total",
			"Failed partition maintenance runs. The goroutine has no supervisor; this is how its failure becomes visible."),

		RedisDuration: factory.histogramVec(
			"redis_operation_duration_seconds",
			"Redis operation latency (docs/PLAN/13).",
			latencyBuckets, "operation"),

		InstanceScopedAccess: factory.counterVec(
			"db_instance_scoped_access_total",
			"Uses of the cross-tenant database path, by reason (docs/PLAN/08 Part B).",
			"reason"),
	}

	// Go runtime and process metrics: goroutine count, heap, file descriptors,
	// GC pauses. Cheap, and the first thing anyone wants during an incident.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// RegisterDBStats exposes connection pool statistics.
//
// docs/PLAN/13 names pool utilisation explicitly, and for good reason: pool
// exhaustion presents as latency at every endpoint at once, which looks like
// a dozen unrelated problems until someone thinks to check the pool.
func (m *Metrics) RegisterDBStats(name string, db *sql.DB) error {
	return m.registry.Register(collectors.NewDBStatsCollector(db, name))
}

// Handler serves the metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		// A scrape failure should be visible in the scrape, not swallowed.
		ErrorHandling: promhttp.HTTPErrorOnError,
		// Metrics are internal, but the endpoint still should not hand an
		// error message to whoever reaches it.
		EnableOpenMetrics: true,
	})
}

// Registry exposes the registry, for tests.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// --- HTTP middleware --------------------------------------------------------

// Instrument records duration, count, and in-flight for every request.
//
// route is the chi route PATTERN, never the concrete path. `/v1/organizations/
// {org_id}/users` is one time series; the concrete path would be one series
// per organization, which is unbounded cardinality — the classic way to take
// down a Prometheus server with an application change rather than an attack.
func (m *Metrics) Instrument(routeOf func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.RequestsInFlight.Inc()
			defer m.RequestsInFlight.Dec()

			start := time.Now()
			rec := &metricsRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			// An unmatched request is almost always a scan or a typo, and its
			// path is entirely attacker-controlled. Bucketing them under one
			// label keeps cardinality bounded — otherwise a scanner walking a
			// wordlist creates a time series per probe, and the metrics
			// endpoint becomes the denial-of-service vector.
			//
			// chi reports an unmatched request as the mount catch-all "/*".
			// This service has no legitimate wildcard route, so treating that
			// pattern as unmatched is unambiguous; adding one later would need
			// this line revisited.
			route := routeOf(r)
			if route == "" || route == "/*" {
				route = "unmatched"
			}

			labels := prometheus.Labels{
				"method":       r.Method,
				"route":        route,
				"status_class": statusClass(status),
			}

			m.RequestDuration.With(labels).Observe(time.Since(start).Seconds())
			m.RequestsTotal.With(labels).Inc()
		})
	}
}

// statusClass reduces a status code to its class.
//
// The exact code is in the logs; here it would multiply every series by the
// number of distinct codes for no analytical gain. Alerts care about the 5xx
// rate, not about 502 versus 503.
func statusClass(code int) string {
	switch {
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

type metricsRecorder struct {
	http.ResponseWriter
	status int
}

func (r *metricsRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *metricsRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *metricsRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// --- registration helpers ---------------------------------------------------

// promauto registers instruments as it builds them, so a metric cannot be
// declared and then silently never registered — which produces a metric that
// records perfectly and is never exposed.
type promauto struct {
	reg         *prometheus.Registry
	constLabels prometheus.Labels
}

func (p promauto) counter(name, help string) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: name, Help: help, ConstLabels: p.constLabels,
	})
	p.reg.MustRegister(c)
	return c
}

func (p promauto) counterVec(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name, Help: help, ConstLabels: p.constLabels,
	}, labels)
	p.reg.MustRegister(c)
	return c
}

func (p promauto) gauge(name, help string) prometheus.Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: name, Help: help, ConstLabels: p.constLabels,
	})
	p.reg.MustRegister(g)
	return g
}

func (p promauto) histogram(name, help string, buckets []float64) prometheus.Histogram {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: name, Help: help, Buckets: buckets, ConstLabels: p.constLabels,
	})
	p.reg.MustRegister(h)
	return h
}

func (p promauto) histogramVec(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: name, Help: help, Buckets: buckets, ConstLabels: p.constLabels,
	}, labels)
	p.reg.MustRegister(h)
	return h
}

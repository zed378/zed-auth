package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/observability"
)

func discardLogger() *slog.Logger {
	return observability.NewLogger(io.Discard, observability.Options{Level: "error", Format: "json"})
}

func testHTTPConfig(addr string) config.HTTPConfig {
	return config.HTTPConfig{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       10 * time.Second,
		ShutdownTimeout:   3 * time.Second,
	}
}

// --- health endpoints (P0-10) ---------------------------------------------

// probes adapts Health to the generated ServerInterface, whose methods are
// plain http.HandlerFuncs. Tests exercise the handler through the same wrapper
// that serves production traffic, so the generated decoding and status mapping
// are covered rather than bypassed.
func probes(h *Health) api.ServerInterface {
	return api.NewStrictHandler(h, nil)
}

// The two probes report different words for success: /healthz says "ok" and
// /readyz says "ready". That is odd, it is what ships, and it is what the spec
// records (ADR-013).
//
// The test exists because the tidying instinct is strong and wrong here: a
// consumer already parsing "ready" breaks if the server is changed to match a
// prettier spec. Changing it is a breaking API change and should have to
// defeat a failing test to happen by accident.
func TestHealth_ProbeVocabularyMatchesTheSpec(t *testing.T) {
	h := &Health{Checks: []Checker{stubChecker{"postgres", nil}}}

	tests := []struct {
		name    string
		serve   func(http.ResponseWriter, *http.Request)
		path    string
		want    string
		wantErr bool
	}{
		{"liveness", probes(h).GetLiveness, "/healthz", "ok", false},
		{"readiness", probes(h).GetReadiness, "/readyz", "ready", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.serve(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			var body struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding %s: %v", tc.path, err)
			}
			if body.Status != tc.want {
				t.Errorf("%s status = %q, want %q — if this is a deliberate change, "+
					"openapi/openapi.yaml must change with it and it is a breaking "+
					"change for any client parsing the old value", tc.path, body.Status, tc.want)
			}
		})
	}

	t.Run("unready", func(t *testing.T) {
		down := &Health{Checks: []Checker{stubChecker{"postgres", errors.New("down")}}}

		rec := httptest.NewRecorder()
		probes(down).GetReadiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		var body struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding /readyz: %v", err)
		}
		if body.Status != "unavailable" {
			t.Errorf("unready status = %q, want %q", body.Status, "unavailable")
		}
	})
}

// A cached readiness response lets an unready instance keep looking ready for
// the life of the cache entry, which is the one thing this endpoint exists to
// prevent. The generated response writers set only Content-Type and the status
// code, so the header comes from middleware and is worth pinning.
func TestHealth_ProbesAreNeverCached(t *testing.T) {
	srv := New(testHTTPConfig("127.0.0.1:0"), Deps{
		Logger: discardLogger(),
		Health: &Health{Checks: []Checker{stubChecker{"postgres", nil}}},
	})

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

type stubChecker struct {
	name string
	err  error
}

func (s stubChecker) Name() string                { return s.name }
func (s stubChecker) Check(context.Context) error { return s.err }

// PLAN/14-DEPLOYMENT.md: liveness stays healthy when a dependency is down, so a
// transient database problem does not make the orchestrator kill every pod.
func TestHealth_LivenessIgnoresDependencies(t *testing.T) {
	h := &Health{Checks: []Checker{stubChecker{"postgres", errors.New("connection refused")}}}

	rec := httptest.NewRecorder()
	probes(h).GetLiveness(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("liveness must stay 200 with a failing dependency, got %d", rec.Code)
	}
}

func TestHealth_ReadinessReflectsDependencies(t *testing.T) {
	t.Run("all healthy", func(t *testing.T) {
		h := &Health{Checks: []Checker{stubChecker{"postgres", nil}, stubChecker{"redis", nil}}}

		rec := httptest.NewRecorder()
		probes(h).GetReadiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if rec.Code != http.StatusOK {
			t.Errorf("want 200 when every dependency is healthy, got %d", rec.Code)
		}
	})

	t.Run("one failing", func(t *testing.T) {
		h := &Health{Checks: []Checker{
			stubChecker{"postgres", nil},
			stubChecker{"redis", errors.New("dial tcp 10.0.4.2:6379: connect: connection refused")},
		}}

		rec := httptest.NewRecorder()
		probes(h).GetReadiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("want 503 when a dependency fails, got %d", rec.Code)
		}
	})
}

// SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §12: the readiness endpoint is
// reachable from the load balancer and is a standard reconnaissance target. It
// must not name dependencies, hosts, versions, or error text.
func TestHealth_ReadinessLeaksNoInfrastructureDetail(t *testing.T) {
	h := &Health{Checks: []Checker{
		stubChecker{"postgres-primary", errors.New("dial tcp 10.0.4.2:5432: connect: connection refused")},
	}}

	rec := httptest.NewRecorder()
	probes(h).GetReadiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	body := rec.Body.String()
	for _, leak := range []string{"postgres", "10.0.4.2", "5432", "connection refused", "dial tcp"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
			t.Errorf("readiness body leaked %q to an unauthenticated caller: %s", leak, body)
		}
	}
}

// A readiness probe that hangs is worse than one that fails: the orchestrator
// waits instead of routing traffic elsewhere.
func TestHealth_ReadinessBoundsSlowChecks(t *testing.T) {
	slow := checkerFunc(func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	h := &Health{Checks: []Checker{slow}, Timeout: 50 * time.Millisecond}

	start := time.Now()
	rec := httptest.NewRecorder()
	probes(h).GetReadiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("readiness must be bounded by its timeout, took %v", elapsed)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a timed-out check must report unready, got %d", rec.Code)
	}
}

type checkerFunc func(context.Context) error

func (f checkerFunc) Name() string                    { return "stub" }
func (f checkerFunc) Check(ctx context.Context) error { return f(ctx) }

// --- middleware ------------------------------------------------------------

func TestRequestID_GeneratedAndEchoed(t *testing.T) {
	var seen string
	h := RequestID(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = observability.RequestIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("a request ID must be present in the handler's context")
	}
	if got := rec.Header().Get(RequestIDHeader); got != seen {
		t.Errorf("response header %s = %q, want the context value %q", RequestIDHeader, got, seen)
	}
}

// An untrusted caller must not choose their own correlation ID: colliding it
// with someone else's deliberately makes an incident timeline unreadable.
func TestRequestID_IgnoresClientHeaderWhenProxyNotTrusted(t *testing.T) {
	var seen string
	h := RequestID(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = observability.RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "attacker-chosen-id")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen == "attacker-chosen-id" {
		t.Error("client-supplied request ID must be ignored when the proxy is not trusted")
	}
}

func TestRequestID_AdoptsHeaderWhenProxyTrusted(t *testing.T) {
	var seen string
	h := RequestID(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = observability.RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "edge-generated-id")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "edge-generated-id" {
		t.Errorf("trusted proxy header should be adopted, got %q", seen)
	}
}

// An unbounded or control-character-bearing header value written into
// structured logs is a log-injection primitive.
func TestRequestID_RejectsMalformedProxyHeader(t *testing.T) {
	malformed := []string{
		strings.Repeat("a", 65), // too long
		"has spaces",            // whitespace
		"line\nbreak",           // log injection
		"tab\there",             //
		`{"json":"injection"}`,  //
		"null\x00byte",          //
	}

	for _, bad := range malformed {
		var seen string
		h := RequestID(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = observability.RequestIDFromContext(r.Context())
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(RequestIDHeader, bad)
		h.ServeHTTP(httptest.NewRecorder(), req)

		if seen == bad {
			t.Errorf("malformed request ID %q was adopted verbatim", bad)
		}
		if seen == "" {
			t.Errorf("a fresh ID should be generated when the header is rejected, got empty")
		}
	}
}

// PLAN/10-THREAT-MODEL.md § Information Disclosure: a panic message or stack
// trace returned to the caller is an information-disclosure bug.
func TestRecover_ReturnsGenericErrorAndKeepsServing(t *testing.T) {
	// The password is the repository's designated placeholder rather than an
	// invented one. The secret-scanning hook cannot tell a test fixture from a
	// real leak and should not try — a hook routinely bypassed with
	// --no-verify stops being a control (deploy/SECRETS.md). The test asserts
	// on the exact string either way, so nothing is weakened.
	const leaked = "local_dev_only"

	h := Recover(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("internal detail: connection string postgres://user:" + leaked + "@db:5432")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("want 500 after a panic, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, leaked) || strings.Contains(body, "postgres://") {
		t.Errorf("panic detail leaked to the caller: %s", body)
	}
	if !strings.Contains(body, "INTERNAL_ERROR") {
		t.Errorf("want the standard error envelope, got: %s", body)
	}
}

func TestSecurityHeaders_AppliedToEveryResponse(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
}

// The access log must not carry the query string: on /oauth/authorize it holds
// code_challenge and state, and on a callback it can hold an authorization code.
func TestAccessLog_DoesNotLogQueryString(t *testing.T) {
	var buf strings.Builder
	log := observability.NewLogger(&buf, observability.Options{Level: "info", Format: "json"})

	h := AccessLog(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?code_challenge=SECRETCHALLENGE&state=SECRETSTATE", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if strings.Contains(out, "SECRETCHALLENGE") || strings.Contains(out, "SECRETSTATE") {
		t.Errorf("query string leaked into the access log: %s", out)
	}
	if !strings.Contains(out, "/oauth/authorize") {
		t.Errorf("path should still be logged, got: %s", out)
	}
}

// --- server lifecycle (P0-04) ---------------------------------------------

func TestServer_ServesHealthEndpoints(t *testing.T) {
	srv := New(testHTTPConfig(":0"), Deps{Logger: discardLogger(), Health: &Health{}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: want 200, got %d", path, resp.StatusCode)
		}
		if resp.Header.Get(RequestIDHeader) == "" {
			t.Errorf("GET %s: response should carry a request ID", path)
		}
	}
}

// PLAN/14-DEPLOYMENT.md: finish in-flight requests before terminating, since
// this service sits on the critical path of many others.
func TestServer_GracefulShutdownCompletesInFlightRequests(t *testing.T) {
	port := freePort(t)

	completed := make(chan struct{})
	srv := New(testHTTPConfig(port), Deps{Logger: discardLogger(), Health: &Health{}})
	srv.mux.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
		close(completed)
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()

	waitForListener(t, port)

	// Start a slow request, then signal shutdown while it is still running.
	respCh := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Get("http://127.0.0.1" + port + "/slow")
		if err == nil {
			respCh <- resp
		}
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case resp := <-respCh:
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(body) != "done" {
			t.Errorf("in-flight request was cut short: status=%d body=%q", resp.StatusCode, body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight request never completed — shutdown dropped it")
	}

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned an error on graceful shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after shutdown")
	}

	select {
	case <-completed:
	default:
		t.Error("handler did not run to completion")
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return ":" + strings.Split(l.Addr().String(), ":")[1]
}

func waitForListener(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+port, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server never started listening")
}

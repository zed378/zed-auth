package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/config"
)

func metricsStub() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("auth_login_attempts_total 42\n"))
	})
}

func TestAdminServesMetricsWithoutATokenWhenNoneIsConfigured(t *testing.T) {
	s := NewAdmin(config.AdminConfig{Addr: "127.0.0.1:0", Enabled: true}, AdminDeps{
		Logger:  discardLogger(),
		Metrics: metricsStub(),
	})

	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("want 200 when no token is configured, got %d", rec.Code)
	}
}

func TestAdminRequiresTheTokenWhenConfigured(t *testing.T) {
	const token = "s3cr3t-scrape-token"

	s := NewAdmin(config.AdminConfig{Addr: "127.0.0.1:0", Enabled: true}, AdminDeps{
		Logger:  discardLogger(),
		Metrics: metricsStub(),
		Token:   token,
	})

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"correct token with Bearer prefix", "Bearer " + token, http.StatusOK},
		{"correct token unprefixed", token, http.StatusOK},
		{"no header", "", http.StatusNotFound},
		{"wrong token", "Bearer wrong-token", http.StatusNotFound},
		{"empty bearer", "Bearer ", http.StatusNotFound},
		{"prefix of the real token", "Bearer s3cr3t", http.StatusNotFound},
		{"token plus suffix", "Bearer " + token + "x", http.StatusNotFound},
		{"wrong scheme", "Basic " + token, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			rec := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want != http.StatusOK && strings.Contains(rec.Body.String(), "auth_login_attempts_total") {
				t.Error("metrics leaked despite the request being rejected")
			}
		})
	}
}

// A rejected request must not advertise that the endpoint exists, what scheme
// it wants, or that a token was merely wrong rather than missing. Anything
// more helps someone probing for it (docs/SECURITY/02 §12).
func TestAdminRejectionRevealsNothing(t *testing.T) {
	s := NewAdmin(config.AdminConfig{Addr: "127.0.0.1:0", Enabled: true}, AdminDeps{
		Logger:  discardLogger(),
		Metrics: metricsStub(),
		Token:   "a-token",
	})

	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Error("a WWW-Authenticate header tells a prober the endpoint exists and what it wants")
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "" {
		t.Errorf("the rejection should have an empty body, got %q", body)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d; 404 is preferred over 401 so the endpoint does not confirm it exists", rec.Code)
	}
}

// Nothing but /metrics is served. pprof in particular is deliberately absent:
// it exposes heap contents, which on this service means tokens and passwords
// in flight.
func TestAdminServesNothingElse(t *testing.T) {
	s := NewAdmin(config.AdminConfig{Addr: "127.0.0.1:0", Enabled: true}, AdminDeps{
		Logger:  discardLogger(),
		Metrics: metricsStub(),
	})

	for _, path := range []string{
		"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/goroutine",
		"/", "/healthz", "/readyz", "/config", "/env",
	} {
		rec := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code == http.StatusOK {
			t.Errorf("%s is served by the admin listener and should not be", path)
		}
	}
}

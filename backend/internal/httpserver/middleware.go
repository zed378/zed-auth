// Package httpserver owns the HTTP router, the middleware chain, and graceful
// shutdown.
//
// The middleware order is deliberate and documented at Chain below. Security
// middleware added by later tasks (rate limiting in P1-13, bearer
// authentication and tenant scoping in P1-15) slots into defined positions
// rather than wherever it happens to be convenient.
package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/zed378/zed-auth/backend/internal/observability"
)

// RequestIDHeader is the header carrying the correlation ID across services.
const RequestIDHeader = "X-Request-Id"

// newRequestID returns a random correlation identifier.
//
// crypto/rand rather than math/rand: a request ID appears in logs that may be
// shared across trust boundaries, and a predictable identifier is a small but
// free information leak about request volume and ordering.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is not a recoverable condition, but a request ID
		// is not worth panicking the process over. Fall back to a timestamp,
		// which is still unique enough to correlate a single request's lines.
		return "ts-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b)
}

// RequestID assigns a correlation ID to every request and echoes it in the
// response, so a client reporting a problem can quote an ID that finds the
// exact request in the logs.
//
// trustProxyHeaders controls whether an inbound X-Request-Id is adopted. It
// must only be true when the service sits behind a proxy that strips or
// overwrites client-supplied values: otherwise any caller can choose their own
// correlation ID, collide it with someone else's deliberately, and make an
// incident timeline unreadable (docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §10
// makes the same argument for rate-limit headers).
func RequestID(trustProxyHeaders bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if trustProxyHeaders {
				id = sanitizeRequestID(r.Header.Get(RequestIDHeader))
			}
			if id == "" {
				id = newRequestID()
			}

			ctx := observability.WithRequestID(r.Context(), id)
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sanitizeRequestID accepts only a bounded, printable-ASCII identifier.
// An unbounded or control-character-bearing header value would be written
// straight into structured logs, which is a log-injection primitive.
func sanitizeRequestID(v string) string {
	const maxLen = 64
	if len(v) == 0 || len(v) > maxLen {
		return ""
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		isAllowed := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if !isAllowed {
			return ""
		}
	}
	return v
}

// statusRecorder captures the response status so the access log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, so
// streaming and hijacking still work through the wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// AccessLog logs one structured line per request.
//
// It deliberately logs the URL *path* and never the raw query string: the query
// string on /oauth/authorize carries code_challenge and state, and on the
// callback it can carry an authorization code (docs/PLAN/13-OBSERVABILITY.md).
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isProbe(r.URL.Path) {
				// Probe traffic arrives every few seconds from the
				// orchestrator and would drown every other line in the log
				// (P0-10). It used to be excluded by living on a sub-router
				// with no access logging at all; that stopped working when the
				// generated router grew /v1 routes, which must be logged.
				//
				// Skipped by PATH rather than by router, so the exclusion is
				// two named endpoints a reader can see rather than a property
				// of where something happens to be mounted.
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			// A 5xx is a system failure. A 4xx is usually the client being
			// wrong, which is expected traffic — logging it at error level is
			// how error dashboards become noise nobody reads
			// (docs/PLAN/13-OBSERVABILITY.md § Logging).
			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			}

			log.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", rec.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// isProbe reports whether a path is a liveness or readiness probe.
func isProbe(path string) bool {
	return path == "/healthz" || path == "/readyz"
}

// Recover turns a panic into a 500 rather than a dropped connection, and logs
// the stack trace.
//
// The response body is deliberately generic: a panic message or stack trace
// returned to the caller is an information-disclosure bug
// (docs/PLAN/10-THREAT-MODEL.md § Information Disclosure).
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// http.ErrAbortHandler is the documented way for a handler
					// to abandon a response; it is not a bug.
					if rec == http.ErrAbortHandler {
						panic(rec)
					}

					log.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
						slog.Any("panic", rec),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("stack", string(debug.Stack())),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred."}}`))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders applies response headers that apply to every route.
//
// The login page tightens these further (P1-12): it must additionally be
// unframable and carry a strict Content-Security-Policy, because it is the one
// page where clickjacking has a credential to steal.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Stop content-type sniffing from turning a JSON error into executable
		// content in a browser context.
		h.Set("X-Content-Type-Options", "nosniff")
		// A referrer leaks the path — and on /oauth/authorize the path's query
		// carries protocol parameters — to whatever the user navigates to next.
		h.Set("Referrer-Policy", "no-referrer")
		// This is an API and an auth surface; nothing here should be framed.
		h.Set("X-Frame-Options", "DENY")
		// No API response should ever be cached by an intermediary. Token
		// responses set this too (docs/PLAN/05-API-CONTRACT.md), but a default here
		// means a new endpoint cannot forget.
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// Timeout bounds how long a handler may run.
//
// Every dependency call must also carry its own timeout — this is a backstop,
// not the primary mechanism. Without it a slow dependency holds a connection
// open indefinitely, which is how a degraded dependency becomes an outage
// (docs/PLAN/13-OBSERVABILITY.md § Graceful Degradation).
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d,
			`{"error":{"code":"TIMEOUT","message":"The request took too long to process."}}`)
	}
}

// HeadAsGet routes a HEAD request to the GET handler for the same path.
//
// chi matches methods exactly, so a router with only GET routes answers 405 to
// HEAD. RFC 9110 says a server supporting GET SHOULD support HEAD, and the
// practical consequences are not theoretical:
//
//   - `curl -I` is the first thing anyone reaches for when checking headers,
//     and a 405 returns the middleware's defaults rather than the handler's.
//     That is not hypothetical either: it reported `Cache-Control: no-store`
//     for the JWKS endpoint during P1-04 and briefly looked like a caching bug
//     in the handler, which was serving `max-age=300` perfectly well on GET.
//   - Caching proxies and uptime monitors use HEAD to revalidate. Answering
//     405 makes an intermediary re-fetch the whole body, or drop the resource.
//
// The request is CLONED with the method changed rather than mutated in place.
// That matters: net/http decides whether a body may be written by looking at
// the ORIGINAL request it captured, so cloning leaves the server correctly
// suppressing the body while the router sees a GET.
func HeadAsGet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		asGet := r.Clone(r.Context())
		asGet.Method = http.MethodGet
		next.ServeHTTP(w, asGet)
	})
}

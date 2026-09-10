package httpserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/config"
)

// AdminServer carries the internal endpoints: metrics, and later profiling.
//
// It is a SEPARATE listener from the public server, not a route on it. docs/PLAN/13
// requires the metrics endpoint not to be reachable from the public ingress,
// and separating the listener makes that a property of the binding rather than
// something an ingress rule has to remember — it keeps holding when the
// ingress is reconfigured by someone who does not know the rule exists.
//
// What the endpoint discloses is worth stating plainly, because "metrics are
// harmless" is a common and wrong assumption: request rates per route, error
// rates, login success and failure counts, in-flight concurrency, database
// pool saturation, and the service version. That is a reconnaissance summary
// and a reliable oracle for whether an attack is working
// (docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §12).
type AdminServer struct {
	cfg  config.AdminConfig
	log  *slog.Logger
	http *http.Server
}

// AdminDeps are the handlers the admin listener serves.
type AdminDeps struct {
	Logger  *slog.Logger
	Metrics http.Handler

	// Token, when non-empty, is required as a bearer token on every request.
	// Prometheus sends it via bearer_token in its scrape config.
	Token string
}

// NewAdmin builds the internal listener.
func NewAdmin(cfg config.AdminConfig, deps AdminDeps) *AdminServer {
	mux := http.NewServeMux()

	if deps.Metrics != nil {
		h := deps.Metrics
		if deps.Token != "" {
			h = requireBearerToken(deps.Token, h)
		}
		mux.Handle("GET /metrics", h)
	}

	// Deliberately minimal: no request logging, because a scrape every fifteen
	// seconds would drown the log the way the health probes would have
	// (P0-10); and no security-header middleware, because nothing here is
	// rendered in a browser context.
	//
	// pprof is deliberately NOT mounted. It is genuinely useful and it exposes
	// heap contents, which on this service means tokens and passwords in
	// flight. If it is ever needed it belongs behind its own flag, off by
	// default, and enabled for a named investigation.

	return &AdminServer{
		cfg: cfg,
		log: deps.Logger,
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			ErrorLog:          slog.NewLogLogger(deps.Logger.Handler(), slog.LevelError),
		},
	}
}

// requireBearerToken gates a handler behind a shared secret.
//
// Compared in constant time. A shared secret compared with == leaks its
// prefix through response timing, and while extracting a token that way over
// a network is slow and noisy, the constant-time comparison costs nothing and
// removes the question.
//
// The failure response is deliberately bare: no WWW-Authenticate challenge
// naming a scheme, no hint about what was wrong. This endpoint should not
// advertise that it exists or what it wants to anyone probing it
// (docs/SECURITY/02 §12).
func requireBearerToken(token string, next http.Handler) http.Handler {
	want := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		got, ok := strings.CutPrefix(header, "Bearer ")
		if !ok {
			// Also accept the token as the whole header value, since some
			// scrapers send it unprefixed.
			got = header
		}

		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Run serves until ctx is cancelled.
//
// A failure here is logged but does not stop the service: losing metrics is a
// visibility problem, and taking authentication down over it would be a worse
// outcome than the one being reported.
func (s *AdminServer) Run(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.log.LogAttrs(ctx, slog.LevelInfo, "admin listener disabled")
		<-ctx.Done()
		return nil
	}

	errCh := make(chan error, 1)

	go func() {
		s.log.LogAttrs(ctx, slog.LevelInfo, "admin listener started",
			slog.String("addr", s.cfg.Addr))

		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin server: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

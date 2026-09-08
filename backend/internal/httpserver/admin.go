package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zed378/zed-auth/backend/internal/config"
)

// AdminServer carries the internal endpoints: metrics, and later profiling.
//
// It is a SEPARATE listener from the public server, not a route on it. PLAN/13
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
// (SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §12).
type AdminServer struct {
	cfg  config.AdminConfig
	log  *slog.Logger
	http *http.Server
}

// AdminDeps are the handlers the admin listener serves.
type AdminDeps struct {
	Logger  *slog.Logger
	Metrics http.Handler
}

// NewAdmin builds the internal listener.
func NewAdmin(cfg config.AdminConfig, deps AdminDeps) *AdminServer {
	mux := http.NewServeMux()

	if deps.Metrics != nil {
		mux.Handle("GET /metrics", deps.Metrics)
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

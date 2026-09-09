package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/oidc"
)

// Server owns the HTTP listener and its lifecycle.
type Server struct {
	cfg  config.HTTPConfig
	log  *slog.Logger
	http *http.Server
	mux  *chi.Mux
}

// Deps are the collaborators the router needs. Later phases add to this:
// the OIDC provider (P1-06/P1-07), the Management API (P1-15), and the
// authorization service (P2-06).
type Deps struct {
	Logger  *slog.Logger
	Health  *Health
	Metrics *observability.Metrics

	// Discovery serves the two documents a consumer configures itself from.
	// Optional: nil means the routes are not registered at all, rather than
	// registered and returning an error. A 404 is the honest answer for a
	// deployment that does not serve them.
	Discovery *oidc.Handler

	// TrustProxyHeaders must be true only when a proxy in front of this service
	// strips client-supplied correlation headers. See RequestID.
	TrustProxyHeaders bool
}

// New builds a Server with the standard middleware chain and the routes that
// exist in this phase.
func New(cfg config.HTTPConfig, deps Deps) *Server {
	if deps.Logger == nil {
		panic("httpserver.New: Logger is required")
	}
	if deps.Health == nil {
		panic("httpserver.New: Health is required")
	}

	mux := chi.NewRouter()

	// --- Middleware chain -------------------------------------------------
	//
	// Order matters, and each position is a decision:
	//
	//  1. RequestID       — first, so everything downstream can correlate,
	//                       including the recovery handler's panic log.
	//  2. Recover         — outside AccessLog, so a panic still produces an
	//                       access-log line with its 500 status rather than
	//                       vanishing.
	//  3. AccessLog       — after Recover so it observes the real status code.
	//  4. Metrics        — after AccessLog so it observes the real status, and
	//                       before SecurityHeaders so it times the handler
	//                       rather than the header-writing wrapper.
	//  5. SecurityHeaders — before any handler can write a response.
	//  6. Timeout         — innermost of the always-on middleware, so the
	//                       timeout bounds handler work rather than the
	//                       logging and recovery wrappers.
	//
	// Later phases insert:
	//  - RateLimit        (P1-13) after SecurityHeaders, before authentication,
	//                     so an unauthenticated flood is rejected as cheaply as
	//                     possible.
	//  - BearerAuth       (P1-15) and TenantScope (P0-08/P2-08) on the /v1
	//                     subrouter only — never globally, since the OIDC
	//                     endpoints authenticate differently.
	mux.Use(RequestID(deps.TrustProxyHeaders))
	mux.Use(Recover(deps.Logger))
	mux.Use(AccessLog(deps.Logger))
	if deps.Metrics != nil {
		// Labelled by the chi route PATTERN, not the concrete path: the
		// pattern gives one time series per endpoint, while the path would
		// give one per organization — unbounded cardinality, and the usual way
		// an application change takes down a Prometheus server.
		mux.Use(deps.Metrics.Instrument(func(r *http.Request) string {
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				return rctx.RoutePattern()
			}
			return ""
		}))
	}
	mux.Use(SecurityHeaders)
	mux.Use(Timeout(cfg.WriteTimeout - time.Second))

	// --- Routes -----------------------------------------------------------
	//
	// Health endpoints are registered on a bare sub-router with no access
	// logging: probe traffic arrives every few seconds from the orchestrator
	// and would otherwise drown every other line in the log
	// (TASKS P0-10). They are also exempt from rate limiting when that arrives.
	//
	// The routes themselves come from the generated router rather than being
	// registered by hand, so the paths, methods and status codes are the ones
	// in openapi/openapi.yaml by construction. A path that exists here but not
	// in the spec cannot be reached, and a path in the spec with no handler
	// fails to compile (ADR-013).
	health := chi.NewRouter()
	health.Use(SecurityHeaders)
	health.Use(noStore)
	// Every route in the spec, from one generated router.
	//
	// The probes and the discovery endpoints are implemented by different
	// packages and registered together, because the generated router is the
	// thing that guarantees the served paths are exactly the documented ones
	// (ADR-013). Registering discovery by hand would have been simpler and
	// would have put these two endpoints outside that guarantee — which is
	// precisely where they must not be, since a consumer's only view of this
	// service is what the spec says.
	routes := apiRoutes{Health: deps.Health, Handler: deps.Discovery}
	api.HandlerFromMux(api.NewStrictHandler(routes, nil), health)
	mux.Mount("/", health)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(deps.Logger.Handler(), slog.LevelError),
	}

	return &Server{cfg: cfg, log: deps.Logger, http: srv, mux: mux}
}

// apiRoutes gathers the implementations of the generated server interface.
//
// The interface covers every documented endpoint, and those are implemented by
// different packages: the probes here, the discovery documents in
// internal/oidc. Go has no way to say "these two types satisfy this interface
// between them", so embedding both in one struct is how the compiler is told —
// and the assertion below is what fails when the spec grows an endpoint
// nothing implements.
type apiRoutes struct {
	*Health
	*oidc.Handler
}

var _ api.StrictServerInterface = apiRoutes{}

// Handler exposes the router, for tests and for mounting additional routes in
// later phases.
func (s *Server) Handler() http.Handler { return s.mux }

// Run serves until ctx is cancelled, then shuts down gracefully.
//
// Graceful shutdown is not optional here: this service is on the critical path
// of every consumer application, so dropping in-flight requests on every deploy
// would surface as intermittent login failures across the whole platform
// (PLAN/14-DEPLOYMENT.md § Deployment Model).
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.log.LogAttrs(ctx, slog.LevelInfo, "http server listening",
			slog.String("addr", s.cfg.Addr))

		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err

	case <-ctx.Done():
		s.log.LogAttrs(context.WithoutCancel(ctx), slog.LevelInfo, "shutdown signal received, draining",
			slog.Int64("timeout_ms", s.cfg.ShutdownTimeout.Milliseconds()))

		// context.WithoutCancel: the shutdown deadline must be independent of
		// the already-cancelled parent, or Shutdown returns immediately and
		// drops exactly the in-flight requests it exists to protect.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
		defer cancel()

		if err := s.http.Shutdown(shutdownCtx); err != nil {
			s.log.LogAttrs(shutdownCtx, slog.LevelError, "graceful shutdown did not complete",
				slog.String("error", err.Error()))
			return fmt.Errorf("graceful shutdown: %w", err)
		}

		s.log.Info("shutdown complete")
		return nil
	}
}

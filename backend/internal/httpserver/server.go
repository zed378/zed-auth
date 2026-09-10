package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/management"
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

	// Authorize serves GET /oauth/authorize.
	//
	// Registered by hand rather than through the generated router, and that is
	// the one exception to ADR-013 in this file. The generated wrapper binds
	// every parameter before the handler runs, which would defeat two-phase
	// validation (a missing `state` rejected before `redirect_uri` is checked,
	// so the error takes the wrong channel) and would silently collapse
	// duplicate parameters. The reasoning is in internal/api/oapi-codegen.yaml,
	// where the exclusion lives.
	//
	// The endpoint is still in openapi.yaml, so it is documented, generated
	// into the public API reference, and checked by openapi-shipped-paths.py.
	// Only the code generation is skipped.
	Authorize http.Handler

	// Token serves POST /oauth/token, hand-registered for the reason above:
	// RFC 6749 puts client credentials in the Authorization header, and the
	// generated strict interface passes a parsed body and no request.
	Token http.Handler

	// Introspect and Revoke serve POST /oauth/introspect and /oauth/revoke
	// (P1-09), hand-registered for the same reason as Token: client
	// credentials live in the Authorization header.
	Introspect http.Handler
	Revoke     http.Handler

	// UserInfo serves GET and POST /oauth/userinfo, hand-registered for the
	// same reason as Token: the bearer credential is in the Authorization
	// header, which the generated strict interface does not hand over — and
	// its RFC 6750 challenge header depends on why the request failed.
	UserInfo http.Handler

	// Logout serves GET and POST /oidc/logout (P1-10). Hand-registered like
	// the other protocol endpoints: it answers with HTML or a redirect rather
	// than the JSON envelope the generated interface produces, and its GET
	// decides between acting and asking from the raw query.
	Logout http.Handler

	// Login serves GET and POST /login, and Forgot serves /login/forgot
	// (P1-12). Hand-registered because they answer with HTML rather than with
	// docs/PLAN/05's JSON envelope, which is what the generated interface produces.
	//
	// They are also the two routes in this service that are not part of the
	// API contract at all: a browser is the only client, and no consumer ever
	// codes against them. openapi.yaml documents them so the served surface is
	// still fully described, and nothing is generated from them.
	Login  http.Handler
	Forgot http.Handler

	// Organizations implements the Management API's organization operations
	// (P1-16). Required whenever the spec documents them, which it now does —
	// a nil here is a nil method call rather than a 404, so the constructor
	// refuses it.
	Organizations Manager

	// ProjectAPI implements the project operations (P1-17). Required for the
	// same reason and on the same condition as Organizations.
	ProjectAPI Projects

	// V1 is the Management API chain (P1-15).
	//
	// The routes themselves arrive with P1-16 onward. What is registered here
	// is the middleware every one of them runs behind, mounted on a subrouter
	// of its own rather than globally: the OIDC endpoints authenticate
	// differently, and a bearer check applied to /oauth/token would break the
	// protocol it is meant to protect.
	//
	// Optional. nil means /v1 is not served at all, which is the honest answer
	// for a deployment that does not offer the Management API.
	V1 *management.Chain

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
	if deps.V1 != nil && deps.Organizations == nil {
		// A deployment that serves the Management API must implement it. The
		// generated router registers the /v1 routes from the spec either way,
		// so a nil implementation would panic on the first request rather than
		// at construction — the wrong end of the deploy to find out.
		//
		// Conditional on the chain, because a deployment WITHOUT the
		// Management API is a supported configuration: guardV1 answers 404 for
		// every /v1 path, so the nil is never reached.
		panic("httpserver.New: Organizations is required when V1 is configured")
	}
	if deps.V1 != nil && deps.ProjectAPI == nil {
		panic("httpserver.New: ProjectAPI is required when V1 is configured")
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
	// The hand-routed endpoint, registered on the main router before the
	// generated one is mounted at "/" so it carries the access log, the
	// metrics and the security headers like everything else.
	if deps.Authorize != nil {
		mux.Method(http.MethodGet, "/oauth/authorize", deps.Authorize)
	}
	if deps.Token != nil {
		mux.Method(http.MethodPost, "/oauth/token", deps.Token)
	}
	if deps.Introspect != nil {
		mux.Method(http.MethodPost, "/oauth/introspect", deps.Introspect)
	}
	if deps.Revoke != nil {
		mux.Method(http.MethodPost, "/oauth/revoke", deps.Revoke)
	}
	if deps.UserInfo != nil {
		// OIDC Core 5.3.1 requires both methods.
		mux.Method(http.MethodGet, "/oauth/userinfo", deps.UserInfo)
		mux.Method(http.MethodPost, "/oauth/userinfo", deps.UserInfo)
	}
	if deps.Logout != nil {
		mux.Method(http.MethodGet, "/oidc/logout", deps.Logout)
		mux.Method(http.MethodPost, "/oidc/logout", deps.Logout)
	}
	if deps.Login != nil {
		// One handler for both methods: the page and its submission share the
		// pending request, the branding and the CSRF token, and splitting them
		// across two registrations would be two places to keep those in step.
		mux.Method(http.MethodGet, "/login", deps.Login)
		mux.Method(http.MethodPost, "/login", deps.Login)
	}
	if deps.Forgot != nil {
		mux.Method(http.MethodGet, "/login/forgot", deps.Forgot)
	}

	// Every route in the spec, from one generated router, on the MAIN mux.
	//
	// It used to be mounted on the bare `health` sub-router, which skipped the
	// access log and the metrics — correct while the spec contained only
	// probes and discovery documents, and wrong the moment it grew /v1. A
	// management request that is neither logged nor timed is a management
	// request nobody can investigate.
	//
	// The probes keep their quiet: AccessLog skips them by path.
	routes := apiRoutes{
		Health:   deps.Health,
		Handler:  deps.Discovery,
		Manager:  deps.Organizations,
		Projects: deps.ProjectAPI,
	}
	// The two error paths the generated wrapper would otherwise answer with
	// http.Error — a bare text/plain body and a status of its choosing.
	//
	// Both are routed through docs/PLAN/05's envelope instead, so that a
	// consumer writes ONE error path. A handler returning a management.Fault
	// gets its class's status and its details; anything else becomes a 500 with
	// a fixed message, because an unexpected error's text is written for a
	// developer and routinely names a table, a column or a query.
	strict := api.NewStrictHandlerWithOptions(routes, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			// A malformed path parameter or an undecodable body, rejected by
			// the wrapper before the handler ran. A 400 either way; the point
			// is the shape of the body.
			management.WriteError(w, management.Fault{
				Class:   management.Invalid,
				Message: "The request could not be read.",
				Reason:  err.Error(),
			})
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			var fault management.Fault
			if !errors.As(err, &fault) && deps.Logger != nil {
				// Only the unexpected ones. A Fault is an answer the handler
				// chose and is already visible in the access log; logging it
				// again at error level is how error dashboards fill with
				// 404s nobody needs to read.
				deps.Logger.Error("a management handler failed",
					"method", r.Method, "path", r.URL.Path, "error", err.Error())
			}
			management.WriteError(w, err)
		},
	})

	api.HandlerWithOptions(strict, api.ChiServerOptions{
		BaseRouter:  mux,
		Middlewares: []api.MiddlewareFunc{noStore, guardV1(deps.V1)},

		// The THIRD error path, and the one that is easy to miss.
		//
		// StrictHTTPServerOptions covers errors raised once the strict wrapper
		// is running. This one covers binding a PATH or QUERY parameter, which
		// happens before that — so setting only the strict handlers leaves a
		// malformed org_id answering text/plain with an unmarshalling error
		// naming *uuid.UUID. The integration test caught exactly that.
		//
		// The message is fixed rather than the library's. The caller's own
		// input is not a disclosure, but the internal type it failed to parse
		// into is, and neither belongs in a response a consumer branches on.
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			management.WriteError(w, management.Fault{
				Class:   management.Invalid,
				Message: "A path or query parameter is not valid.",
				Reason:  err.Error(),
			})
		},
	})
	_ = health

	srv := &http.Server{
		Addr: cfg.Addr,
		// Wrapped OUTSIDE the router, not registered as chi middleware: chi
		// runs middleware after matching a route, and the whole point is that
		// a HEAD request never matches a GET-only route in the first place.
		Handler:           HeadAsGet(mux),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(deps.Logger.Handler(), slog.LevelError),
	}

	return &Server{cfg: cfg, log: deps.Logger, http: srv, mux: mux}
}

// guardV1 applies the Management API's middleware chain to /v1 and nothing else.
//
// The generated router applies its middlewares to EVERY operation, and most of
// them must stay open: the discovery documents are fetched anonymously by every
// relying party, and a probe that required a bearer token would fail the
// orchestrator's health check.
//
// So the chain engages by PATH PREFIX rather than by a list of operations, and
// that is the safer direction of default. A new /v1 endpoint added to
// openapi.yaml is guarded the moment it exists, without anybody remembering to
// add it here. What it still needs is a declared permission, and
// management.Policy's missing-key case refuses it until it has one.
func guardV1(chain *management.Chain) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if chain == nil {
			// No Management API configured. The generated router registers the
			// /v1 routes regardless, so they must not be reachable unguarded.
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if isManagementPath(r.URL.Path) {
					http.NotFound(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		guarded := chain.Guarded(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isManagementPath(r.URL.Path) {
				guarded.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isManagementPath reports whether a path belongs to the Management API.
//
// "/v1" itself is included as well as "/v1/...", so a request to the bare
// prefix cannot slip past on a missing slash.
func isManagementPath(path string) bool {
	return path == "/v1" || strings.HasPrefix(path, "/v1/")
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

	// Manager implements the Management API's operations (P1-16 onward).
	//
	// Embedded as an interface rather than a concrete handler so this package
	// does not import every package that implements part of /v1 — there will
	// be several, and each new one would otherwise be a new import here.
	Manager
	Projects
}

// Manager is the part of the generated interface the Management API implements.
//
// It exists so that "the spec grew an endpoint nothing implements" stays a
// compile error: adding an operation to openapi.yaml under /v1 breaks this
// interface's satisfaction, which breaks apiRoutes, which breaks the build.
type Manager interface {
	ListOrganizations(ctx context.Context, request api.ListOrganizationsRequestObject) (api.ListOrganizationsResponseObject, error)
	CreateOrganization(ctx context.Context, request api.CreateOrganizationRequestObject) (api.CreateOrganizationResponseObject, error)
	GetOrganization(ctx context.Context, request api.GetOrganizationRequestObject) (api.GetOrganizationResponseObject, error)
	UpdateOrganization(ctx context.Context, request api.UpdateOrganizationRequestObject) (api.UpdateOrganizationResponseObject, error)
	DeleteOrganization(ctx context.Context, request api.DeleteOrganizationRequestObject) (api.DeleteOrganizationResponseObject, error)
}

// Projects is the project half of the Management API (P1-17).
//
// A second interface rather than more methods on Manager, because they are
// implemented by different packages and Go has no way to say "these two types
// satisfy this interface between them" — embedding both in apiRoutes is how the
// compiler is told.
type Projects interface {
	ListProjects(ctx context.Context, request api.ListProjectsRequestObject) (api.ListProjectsResponseObject, error)
	CreateProject(ctx context.Context, request api.CreateProjectRequestObject) (api.CreateProjectResponseObject, error)
	GetProject(ctx context.Context, request api.GetProjectRequestObject) (api.GetProjectResponseObject, error)
	UpdateProject(ctx context.Context, request api.UpdateProjectRequestObject) (api.UpdateProjectResponseObject, error)
	DeleteProject(ctx context.Context, request api.DeleteProjectRequestObject) (api.DeleteProjectResponseObject, error)
}

var _ api.StrictServerInterface = apiRoutes{}

// Handler returns what the server actually serves.
//
// The SERVED handler, not the bare router. They differ — HeadAsGet wraps the
// router outside chi — and a test exercising the router while production
// serves the wrapper is testing something nobody deploys. That distinction is
// not hypothetical: the first version of this returned s.mux, and the HEAD
// routing test failed against a server that handles HEAD correctly.
//
// Additional routes are supplied through Deps at construction rather than
// mounted onto this afterwards, so that every served path goes through the
// same middleware chain.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run serves until ctx is cancelled, then shuts down gracefully.
//
// Graceful shutdown is not optional here: this service is on the critical path
// of every consumer application, so dropping in-flight requests on every deploy
// would surface as intermittent login failures across the whole platform
// (docs/PLAN/14-DEPLOYMENT.md § Deployment Model).
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

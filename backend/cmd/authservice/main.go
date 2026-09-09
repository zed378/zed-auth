// Command authservice is the Auth Service: OIDC/OAuth2 provider,
// authentication, authorization, and the Management REST API.
//
// This file does wiring only. Business logic lives in internal/ packages, so
// the modular monolith described in PLAN/07-BACKEND-ARCHITECTURE.md can be
// split later without a rewrite.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/oidc"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// version is set at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)".
var version = "dev"

// partitionMonthsAhead is how much events-partition runway to maintain.
//
// Three months rather than one: a service that is down for a while, or a
// maintenance tick that fails quietly, still has room before the failure
// becomes an outage. The cost of an unused empty partition is nothing.
const partitionMonthsAhead = 3

func main() {
	// -healthcheck exists because the runtime image is distroless: it has no
	// shell and no curl, so a container healthcheck has to be the binary
	// itself (SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §17 — a runtime image
	// carrying a shell just to run a healthcheck is attack surface added for
	// operational convenience).
	healthcheck := flag.Bool("healthcheck", false, "probe the local readiness endpoint and exit 0 if ready")
	flag.Parse()

	if *healthcheck {
		os.Exit(probeReadiness())
	}

	if err := run(); err != nil {
		// Written to stderr rather than through the logger, because the most
		// likely reason we are here is that configuration failed to load and
		// there is no configured logger yet.
		fmt.Fprintf(os.Stderr, "authservice: %v\n", err)
		os.Exit(exitCode(err))
	}
}

func run() error {
	// Fail fast and loudly on invalid configuration. A service that boots with
	// a missing signing-key configuration is worse than one that refuses to
	// boot (TASKS P0-04).
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := observability.NewLogger(os.Stdout, observability.Options{
		Level:   cfg.Log.Level,
		Format:  cfg.Log.Format,
		Service: "authservice",
		Version: version,
	})

	log.Info("starting",
		"environment", string(cfg.Environment),
		"issuer", cfg.Issuer,
	)

	// Every secret is resolved here, before anything is opened or started.
	//
	// The ordering is deliberate. When this ran later, next to the listener
	// that used it, an unreadable secret file produced a confusing cascade: the
	// process returned an error, the deferred pool close ran, and the audit
	// maintenance goroutine — already started — logged "sql: database is
	// closed" on its way out. Three symptoms, one of them wrong, none of them
	// the cause. Resolving up front means a bad secret reference is one error
	// message and nothing else.
	secrets, err := resolveSecrets(cfg)
	if err != nil {
		return err
	}

	// Cancelled on SIGINT or SIGTERM. Kubernetes sends SIGTERM before removing
	// a pod from the load balancer, which is the window graceful shutdown uses
	// to drain in-flight requests (PLAN/14-DEPLOYMENT.md).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	metrics := observability.NewMetrics("authservice", version)

	// Tracing is off unless an OTLP endpoint is configured. The shutdown flush
	// matters: an unflushed exporter drops the spans from the last seconds of
	// a process, which are exactly the ones present when it crashed.
	shutdownTracing, err := observability.InitTracing(ctx, observability.TracingConfig{
		Endpoint:       cfg.Tracing.Endpoint,
		Insecure:       cfg.Tracing.Insecure,
		SampleRatio:    cfg.Tracing.SampleRatio,
		ServiceName:    "authservice",
		ServiceVersion: version,
		Environment:    string(cfg.Environment),
	}, log)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if ferr := shutdownTracing(flushCtx); ferr != nil {
			log.Error("flushing traces", "error", ferr.Error())
		}
	}()

	db, err := postgres.Open(ctx, cfg.Postgres, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Error("closing postgres", "error", cerr.Error())
		}
	}()

	// Refuse to start if the connected role can bypass row-level security.
	//
	// Without this the failure is silent and total: point AUTH_POSTGRES_DSN at
	// auth_owner — entirely plausible while debugging a permissions error —
	// and every RLS policy stops applying. Nothing errors, no test fails, and
	// cross-tenant isolation is gone. Refusing to boot is the only response
	// proportionate to that (PLAN/08 Part B, P0-08).
	if err := db.AssertRoleIsNotPrivileged(ctx); err != nil {
		return fmt.Errorf("database role check: %w", err)
	}

	// PLAN/13 names pool utilisation explicitly: exhaustion presents as latency
	// at every endpoint at once, which looks like a dozen unrelated problems
	// until someone thinks to check the pool.
	if err := metrics.RegisterDBStats("postgres", db.SQL()); err != nil {
		return fmt.Errorf("register db metrics: %w", err)
	}

	// Forwarding to an external SIEM is nil until P5-08 supplies one. The seam
	// exists now because PLAN/09 § Audit wants the log forwarded, and adding
	// the seam later would mean touching every call site.
	auditor := audit.NewWriter(db, log, nil)
	auditor.SetObserver(auditObserver{m: metrics})

	// PLAN/08 Part B requires the cross-tenant database path to be auditable.
	// A hook rather than a direct call, because audit already imports postgres
	// and importing back would be a cycle. It runs outside the scoped
	// transaction: failing to record the access should not roll back the
	// access, which is the opposite trade from business events (ADR-012).
	db.SetInstanceScopeHook(func(hookCtx context.Context, reason string) {
		metrics.InstanceScopedAccess.WithLabelValues(reason).Inc()
	})

	// Partition maintenance runs for the life of the process.
	//
	// Without it the service works fine until the last events partition's range
	// ends, at which point every INSERT into events fails — and since every
	// security-sensitive action writes an audit event (PLAN/09 § Audit), every
	// such action fails with it. At midnight on the first of a month, with no
	// deploy to correlate against (P0-12).
	go auditor.Run(ctx, partitionMonthsAhead)

	// Redis joins this list in P1-11/P1-13.
	health := &httpserver.Health{
		Checks:  []httpserver.Checker{db},
		Timeout: 2 * time.Second,
	}

	admin := httpserver.NewAdmin(cfg.Admin, httpserver.AdminDeps{
		Logger:  log,
		Metrics: metrics.Handler(),
		Token:   secrets.adminToken,
	})
	go func() {
		// A failure here is a visibility problem. Taking authentication down
		// over a metrics listener would be a worse outcome than the one it
		// would be reporting.
		if aerr := admin.Run(ctx); aerr != nil {
			log.Error("admin listener stopped", "error", aerr.Error())
		}
	}()

	// The signing key set, read from the database rather than from a
	// configured path (P1-03).
	//
	// This is what makes an application rollback safe: key state lives in
	// `signing_keys`, so rolling back the binary cannot invalidate tokens
	// signed under a newer key (PLAN/14 § Rollback Strategy). A single key
	// reference in configuration would put that state in the deployment, which
	// is the thing being rolled back.
	keyStore := signing.NewStore(db.SQL(), config.NewSecretResolver(cfg.Environment != config.EnvLocal), signing.PurposeOIDC)

	keys := signing.NewCache(func() (*signing.KeySet, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return keyStore.Load(loadCtx)
	}, signing.DefaultCacheTTL)

	// Load once at startup so a broken key set is a refusal to boot rather
	// than a service that accepts requests and fails every login. The latter
	// is a worse outage and a much harder one to read (P1-03).
	//
	// Absence of ANY key is tolerated here and only here: a fresh deployment
	// has no keys until `keyctl generate && keyctl rotate` runs, and refusing
	// to start would make the service impossible to bootstrap. Discovery then
	// reports an empty key set, which is accurate.
	discoveryCapabilities := oidc.Capabilities{
		Issuer:  cfg.Issuer,
		JWKSURI: cfg.Issuer + "/.well-known/jwks.json",

		// Only what this build actually serves.
		//
		// The authorization and token endpoints arrive in P1-06 and P1-07 and
		// are deliberately absent until then. A discovery document naming an
		// endpoint that 404s is worse than one that omits it: a client
		// configures successfully and fails at the first login, which is the
		// failure P1-04 step 2 exists to prevent.
		SigningAlgorithms: []string{string(signing.RS256), string(signing.ES256)},
	}

	discovery, err := oidc.NewHandler(discoveryCapabilities, keys)
	if err != nil {
		return fmt.Errorf("discovery document: %w", err)
	}

	if _, err := keys.Get(); err != nil {
		log.Warn("no signing keys are available; tokens cannot be issued",
			"error", err.Error(),
			"remedy", "run: keyctl generate && keyctl rotate")
	} else if kid, kerr := signing.NewSigner(keys).CurrentKID(); kerr == nil {
		log.Info("signing key loaded", "kid", kid)
	} else {
		log.Warn("keys are present but none is signing",
			"error", kerr.Error(),
			"remedy", "run: keyctl rotate")
	}

	srv := httpserver.New(cfg.HTTP, httpserver.Deps{
		Logger:    log,
		Health:    health,
		Metrics:   metrics,
		Discovery: discovery,
		// Explicit configuration, not inferred from the environment: see the
		// comment on config.HTTPConfig.TrustProxyHeaders. Defaults to false,
		// so a deployment behind a proxy that forwards client headers
		// untouched is safe by default rather than by accident.
		TrustProxyHeaders: cfg.HTTP.TrustProxyHeaders,
	})

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// probeReadiness requests the local readiness endpoint and reports the result
// as a process exit code, for the container healthcheck.
//
// It talks to 127.0.0.1 rather than to the configured address, because the
// probe runs inside the same container as the server it is checking.
func probeReadiness() int {
	addr := os.Getenv("AUTH_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "8080"
	}

	// Parse the port to an integer and rebuild the URL from that integer rather
	// than from the environment string.
	//
	// AUTH_HTTP_ADDR is configuration, not user input, so this was never a
	// realistic SSRF vector: anyone who can set it already controls the
	// process. But an environment-derived string concatenated into a request
	// URL is the shape of the bug regardless of today's reachability, and
	// rebuilding from a validated integer means no attacker-influenceable
	// string reaches the URL at all — which is a genuine fix rather than a
	// suppressed warning (SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §7).
	portNum, convErr := strconv.Atoi(port)
	if convErr != nil || portNum < 1 || portNum > 65535 {
		fmt.Fprintf(os.Stderr, "healthcheck: AUTH_HTTP_ADDR has an invalid port %q\n", port)
		return 1
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/readyz", portNum)

	client := &http.Client{Timeout: 3 * time.Second}

	// #nosec G704 -- gosec's taint analysis follows portNum back to os.Getenv
	// and cannot see the strconv.Atoi + range check in between. After that
	// validation the URL contains no attacker-influenceable string: the host is
	// the literal 127.0.0.1 and the only variable is an int in [1,65535]
	// formatted with %d. This is a limitation of the analysis, not a reachable
	// SSRF, and it is annotated rather than worked around because contorting
	// the code to satisfy a taint tracker would make it worse to read.
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: readiness returned %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// auditObserver adapts the audit package's Observer to the metric instruments.
// It exists so audit does not import observability, keeping the dependency
// one-directional.
type auditObserver struct{ m *observability.Metrics }

func (o auditObserver) PartitionRunway(months int) {
	o.m.AuditPartitionRunway.Set(float64(months))
}

func (o auditObserver) PartitionMaintenanceFailed() {
	o.m.AuditPartitionErrors.Inc()
}

// exitCode maps an error to a process exit code. Kept for the operational
// distinctions later phases need (a configuration error is not retryable; a
// dependency failure is).
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, config.ErrNotConfigured):
		return 78 // EX_CONFIG, per sysexits.h
	default:
		return 1
	}
}

// serviceSecrets holds every secret value the process needs, resolved once at
// startup.
//
// Values, not references: a reference resolved lazily at the point of use turns
// a configuration error into a runtime failure at an arbitrary later moment,
// which is precisely the failure mode P0-14 exists to remove.
type serviceSecrets struct {
	adminToken string
}

// resolveSecrets reads every configured secret reference.
//
// Permissions are checked strictly outside local development, so a secret file
// that is group- or world-readable is refused rather than used. On the VM
// deployment that means the file must be mode 0400 and owned by the container's
// own uid, since the service is not the operator (deploy/SECRETS.md).
func resolveSecrets(cfg *config.Config) (serviceSecrets, error) {
	var out serviceSecrets

	resolver := config.NewSecretResolver(cfg.Environment != config.EnvLocal)

	if cfg.Admin.TokenRef != "" {
		raw, err := resolver.Resolve(config.SecretRef(cfg.Admin.TokenRef))
		if err != nil {
			return out, fmt.Errorf("resolve admin token: %w", err)
		}
		out.adminToken = string(raw)
	}

	return out, nil
}

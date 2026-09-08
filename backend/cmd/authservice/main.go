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

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/observability"
)

// version is set at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)".
var version = "dev"

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

	// Cancelled on SIGINT or SIGTERM. Kubernetes sends SIGTERM before removing
	// a pod from the load balancer, which is the window graceful shutdown uses
	// to drain in-flight requests (PLAN/14-DEPLOYMENT.md).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Readiness checks are registered as their dependencies are wired in:
	//   - Postgres  P0-07 / P0-08
	//   - Redis     P1-11 / P1-13
	// Until then /readyz reports ready, which is correct — the service has no
	// dependencies yet, so there is nothing that could make it unready.
	health := &httpserver.Health{
		Checks:  nil,
		Timeout: 2 * time.Second,
	}

	srv := httpserver.New(cfg.HTTP, httpserver.Deps{
		Logger: log,
		Health: health,
		// Local development has no proxy in front, so an inbound correlation
		// header is client-controlled and must not be trusted. Deployed
		// environments set this once an ingress that strips the header is in
		// place (P0-20).
		TrustProxyHeaders: cfg.Environment != config.EnvLocal,
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

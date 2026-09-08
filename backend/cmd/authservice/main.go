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
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/observability"
)

// version is set at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)".
var version = "dev"

func main() {
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

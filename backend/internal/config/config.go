// Package config loads and validates the service's configuration.
//
// The governing rule, from TASKS/PHASE-0-FOUNDATION.md P0-04: fail fast and
// loudly on a missing required value. A service that boots with a missing
// signing-key configuration is worse than one that refuses to boot, because the
// failure surfaces later, in production, as a confusing runtime error rather
// than immediately as a clear startup error.
//
// Secrets are read from the environment, which in deployed environments is
// populated by the secret manager (PLAN/07-BACKEND-ARCHITECTURE.md
// § Infrastructure). Configuration values are never read from a committed file.
package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Environment names the deployment context. Every environment has its own
// signing keys and its own database; production values are never shared with
// staging (PLAN/13-OBSERVABILITY.md, PLAN/14-DEPLOYMENT.md).
type Environment string

const (
	EnvLocal      Environment = "local"
	EnvStaging    Environment = "staging"
	EnvProduction Environment = "production"
)

// Valid reports whether e is one of the three environments the plan defines.
func (e Environment) Valid() bool {
	switch e {
	case EnvLocal, EnvStaging, EnvProduction:
		return true
	}
	return false
}

// IsProduction reports whether this is the live environment. Used to refuse
// unsafe defaults that are acceptable locally.
func (e Environment) IsProduction() bool { return e == EnvProduction }

// Config is the fully-validated configuration. A Config value that exists has
// already passed validation; there is no partially-valid Config.
type Config struct {
	Environment Environment

	HTTP     HTTPConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Log      LogConfig

	// Issuer is the OIDC issuer identifier. It must exactly match the `iss`
	// claim the token issuer emits and the `issuer` field in the discovery
	// document — a mismatch breaks every conforming client library, and it is a
	// classic misconfiguration (TASKS P1-04).
	Issuer string
}

type HTTPConfig struct {
	Addr string

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration

	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests to finish. This service sits on the critical path of every
	// consumer application, so dropping requests on deploy is not acceptable
	// (PLAN/14-DEPLOYMENT.md § Deployment Model).
	ShutdownTimeout time.Duration
}

type PostgresConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type LogConfig struct {
	// Level is one of debug, info, warn, error.
	Level string
	// Format is json or text. Deployed environments are always json so logs are
	// machine-parseable and correlatable by request ID
	// (PLAN/13-OBSERVABILITY.md § Logging).
	Format string
}

// LoadError reports every configuration problem at once rather than the first
// one. Fixing configuration one error per restart is needless friction when the
// whole environment can be validated in a single pass.
type LoadError struct {
	Problems []string
}

func (e *LoadError) Error() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("configuration is invalid (%d problem(s)):", len(e.Problems)))
	for _, p := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(p)
	}
	return b.String()
}

// Getenv abstracts the environment so tests do not have to mutate global state.
type Getenv func(key string) string

// Load reads configuration using the process environment.
func Load() (*Config, error) { return LoadFrom(os.Getenv) }

// LoadFrom reads configuration using the supplied lookup function, validates it,
// and returns either a usable Config or a *LoadError listing every problem.
func LoadFrom(getenv Getenv) (*Config, error) {
	l := &loader{getenv: getenv}

	cfg := &Config{
		Environment: Environment(l.optional("AUTH_ENV", string(EnvLocal))),
		Issuer:      l.required("AUTH_ISSUER"),
		HTTP: HTTPConfig{
			Addr:              l.optional("AUTH_HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: l.duration("AUTH_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       l.duration("AUTH_HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      l.duration("AUTH_HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       l.duration("AUTH_HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   l.duration("AUTH_HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
		},
		Postgres: PostgresConfig{
			DSN:             l.required("AUTH_POSTGRES_DSN"),
			MaxOpenConns:    l.integer("AUTH_POSTGRES_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    l.integer("AUTH_POSTGRES_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: l.duration("AUTH_POSTGRES_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Redis: RedisConfig{
			Addr:     l.required("AUTH_REDIS_ADDR"),
			Password: l.optional("AUTH_REDIS_PASSWORD", ""),
			DB:       l.integer("AUTH_REDIS_DB", 0),
		},
		Log: LogConfig{
			Level:  l.optional("AUTH_LOG_LEVEL", "info"),
			Format: l.optional("AUTH_LOG_FORMAT", "json"),
		},
	}

	l.validate(cfg)

	if len(l.problems) > 0 {
		sort.Strings(l.problems)
		return nil, &LoadError{Problems: l.problems}
	}
	return cfg, nil
}

type loader struct {
	getenv   Getenv
	problems []string
}

func (l *loader) problem(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, args...))
}

// required returns the value of key, recording a problem if it is unset or blank.
func (l *loader) required(key string) string {
	v := strings.TrimSpace(l.getenv(key))
	if v == "" {
		l.problem("%s is required but not set", key)
	}
	return v
}

func (l *loader) optional(key, fallback string) string {
	if v := strings.TrimSpace(l.getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (l *loader) duration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(l.getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		l.problem("%s must be a duration such as 30s or 5m, got %q", key, raw)
		return fallback
	}
	if d <= 0 {
		l.problem("%s must be positive, got %q", key, raw)
		return fallback
	}
	return d
}

func (l *loader) integer(key string, fallback int) int {
	raw := strings.TrimSpace(l.getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		l.problem("%s must be an integer, got %q", key, raw)
		return fallback
	}
	return n
}

func (l *loader) validate(cfg *Config) {
	if !cfg.Environment.Valid() {
		l.problem("AUTH_ENV must be one of local, staging, production; got %q", cfg.Environment)
	}

	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		l.problem("AUTH_LOG_LEVEL must be one of debug, info, warn, error; got %q", cfg.Log.Level)
	}

	switch cfg.Log.Format {
	case "json", "text":
	default:
		l.problem("AUTH_LOG_FORMAT must be json or text; got %q", cfg.Log.Format)
	}

	// TLS is mandatory everywhere, with no HTTP fallback (PLAN/09-SECURITY.md
	// § Transport & Storage). TLS itself terminates at the ingress
	// (PLAN/14-DEPLOYMENT.md), but the issuer this service advertises is what
	// consumer applications will actually call — so an http:// issuer outside
	// local development would publish a plaintext endpoint to every client.
	if cfg.Issuer != "" {
		switch {
		case strings.HasPrefix(cfg.Issuer, "https://"):
		case strings.HasPrefix(cfg.Issuer, "http://") && cfg.Environment == EnvLocal:
		case strings.HasPrefix(cfg.Issuer, "http://"):
			l.problem("AUTH_ISSUER must use https outside local development; got %q", cfg.Issuer)
		default:
			l.problem("AUTH_ISSUER must be an absolute URL, got %q", cfg.Issuer)
		}
		if strings.HasSuffix(cfg.Issuer, "/") {
			// A trailing slash silently produces an `iss` claim that does not
			// match the discovery document, which every conforming client
			// rejects — and the resulting error message names neither.
			l.problem("AUTH_ISSUER must not end with a trailing slash; got %q", cfg.Issuer)
		}
	}

	if cfg.Postgres.MaxOpenConns <= 0 {
		l.problem("AUTH_POSTGRES_MAX_OPEN_CONNS must be positive, got %d", cfg.Postgres.MaxOpenConns)
	}
	if cfg.Postgres.MaxIdleConns < 0 {
		l.problem("AUTH_POSTGRES_MAX_IDLE_CONNS must not be negative, got %d", cfg.Postgres.MaxIdleConns)
	}
	if cfg.Postgres.MaxIdleConns > cfg.Postgres.MaxOpenConns {
		l.problem("AUTH_POSTGRES_MAX_IDLE_CONNS (%d) must not exceed AUTH_POSTGRES_MAX_OPEN_CONNS (%d)",
			cfg.Postgres.MaxIdleConns, cfg.Postgres.MaxOpenConns)
	}
	if cfg.Redis.DB < 0 {
		l.problem("AUTH_REDIS_DB must not be negative, got %d", cfg.Redis.DB)
	}

	// Production must never run with debug logging: debug output across an
	// identity provider is exactly where sensitive values leak into logs
	// despite redaction (PLAN/13-OBSERVABILITY.md).
	if cfg.Environment.IsProduction() && cfg.Log.Level == "debug" {
		l.problem("AUTH_LOG_LEVEL must not be debug in production")
	}
	if cfg.Environment.IsProduction() && cfg.Log.Format != "json" {
		l.problem("AUTH_LOG_FORMAT must be json in production so logs stay machine-parseable")
	}
}

// ErrNotConfigured is returned by components asked to start before their
// configuration exists. It is deliberately distinct from a validation failure.
var ErrNotConfigured = errors.New("component is not configured")

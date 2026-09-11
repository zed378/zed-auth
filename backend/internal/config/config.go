// Package config loads and validates the service's configuration.
//
// The governing rule, from TASKS/PHASE-0-FOUNDATION.md P0-04: fail fast and
// loudly on a missing required value. A service that boots with a missing
// signing-key configuration is worse than one that refuses to boot, because the
// failure surfaces later, in production, as a confusing runtime error rather
// than immediately as a clear startup error.
//
// Secrets are read from the environment, which in deployed environments is
// populated by the secret manager (docs/PLAN/07-BACKEND-ARCHITECTURE.md
// § Infrastructure). Configuration values are never read from a committed file.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Environment names the deployment context. Every environment has its own
// signing keys and its own database; production values are never shared with
// staging (docs/PLAN/13-OBSERVABILITY.md, docs/PLAN/14-DEPLOYMENT.md).
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
	Admin    AdminConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Log      LogConfig

	// Password holds the P1-02 password-policy knobs. The policy VALUES are
	// per organization and live in the database; these are the deployment-wide
	// switches that policy cannot express.
	Password PasswordConfig
	Tracing  TracingConfig

	// Mail is ADR-018: plain SMTP by URL, no provider SDK. Unset means no
	// outbound mail, which is a valid deployment — it says so once at startup
	// rather than failing silently at the first invitation.
	Mail MailConfig

	// Issuer is the OIDC issuer identifier. It must exactly match the `iss`
	// claim the token issuer emits and the `issuer` field in the discovery
	// document — a mismatch breaks every conforming client library, and it is a
	// classic misconfiguration (TASKS P1-04).
	Issuer string
}

// MailConfig is outbound email (ADR-018).
//
// A URL rather than a host/port/user/password quartet, because that is what an
// operator is given by every relay and because it keeps the switch between
// providers a configuration change rather than a code one.
//
//	smtp://localhost:1025                    — development, Mailpit, no TLS
//	smtps://user:pass@smtp.example.com:587    — STARTTLS required
type MailConfig struct {
	SMTPURL string
	From    string

	// AllowCleartext permits `smtp://` outside local development.
	//
	// Off by default, and the default is the important part: `smtp://`
	// tolerates a relay with no STARTTLS, and an invitation or reset link is a
	// bearer credential for an account. Against a relay reached over a network
	// that is a mistake worth refusing to boot on.
	//
	// It exists because one arrangement is genuinely safe and the scheme alone
	// cannot express it: a mail sink running as a sidecar on the same host,
	// reached over a private container network that no packet leaves. Staging
	// runs Mailpit that way — without this, staging can send no mail at all,
	// which means no invitation and no password reset can be exercised there,
	// which means the only environment where they are tested is a developer's
	// laptop.
	//
	// Set deliberately, never inferred, and logged at WARN on every start so
	// that an operator who sets it once cannot forget. Same shape as
	// AUTH_TRUST_PROXY_HEADERS: a claim the deployment makes about its own
	// topology, which the service cannot verify and must not guess.
	AllowCleartext bool
}

// Configured reports whether mail can be sent.
func (m MailConfig) Configured() bool {
	return strings.TrimSpace(m.SMTPURL) != "" && strings.TrimSpace(m.From) != ""
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
	// (docs/PLAN/14-DEPLOYMENT.md § Deployment Model).
	ShutdownTimeout time.Duration

	// TrustProxyHeaders controls whether an inbound X-Request-Id is adopted
	// rather than replaced.
	//
	// It is deliberately independent of Environment. "Deployed" and "has a
	// header-stripping proxy in front" are different facts, and conflating
	// them gets the answer wrong for any deployment whose proxy does not
	// strip: a tunnel such as Cloudflare Tunnel forwards client headers
	// through untouched, so trusting them there lets a caller choose their own
	// correlation ID and collide it with someone else's deliberately, making
	// an incident timeline unreadable (docs/SECURITY/02 §10).
	//
	// Default false. Turn it on only when something in front provably
	// overwrites the header — the Caddyfile in deploy/vm does.
	TrustProxyHeaders bool

	// ClientIPHeader and TrustedProxyCIDRs decide where the client's address
	// comes from (P1-13).
	//
	// They exist because the obvious answer is wrong on this service's own
	// deployment. `cloudflared` runs as a host service and reaches the
	// published port, so RemoteAddr is the Docker gateway — the same address
	// for every user in the world. A per-IP rate limit computed from it is not
	// a per-IP limit; it is a global one, and a single attacker could use it
	// to lock every user out of the service.
	//
	// The header is read ONLY when the immediate peer is inside a trusted
	// range, which is what makes it unforgeable: a client that sets
	// `CF-Connecting-IP` itself is not connecting from a trusted proxy, so its
	// header is ignored (docs/SECURITY/02 §10 Rate-Limit Bypass).
	//
	// Both or neither. Either alone falls back to RemoteAddr, because a header
	// believed from anywhere is a header anybody can write, and trusted peers
	// with no header have nothing to read.
	ClientIPHeader    string
	TrustedProxyCIDRs []string
}

// AdminConfig is the internal listener carrying metrics.
//
// A SEPARATE listener from the public one, not a route on it. docs/PLAN/13 requires
// the metrics endpoint not to be reachable from the public ingress, and a
// separate port makes that a property of the binding rather than something an
// ingress rule has to remember. It also survives the ingress being
// reconfigured by someone who does not know the rule exists.
type AdminConfig struct {
	// Addr defaults to loopback. Deployed environments that scrape from
	// another host set it explicitly, which is a visible decision.
	Addr string

	// Enabled turns the listener off entirely.
	Enabled bool

	// TokenRef points at a bearer token required on every request to the
	// metrics endpoint. Empty disables the check.
	//
	// Defence in depth, and it exists because of how this endpoint gets
	// exposed in practice. Reaching it from a scraper on another host means
	// either binding beyond loopback or routing it through a tunnel, and at
	// that point the only thing between an unauthenticated reconnaissance
	// summary and whoever can reach the address is one piece of network
	// configuration. Tunnel configs get edited and access policies get
	// misapplied; a token makes a leak require two mistakes rather than one.
	//
	// Prometheus supports bearer_token natively, so this costs nothing
	// operationally.
	TokenRef string
}

// TracingConfig configures OTLP export. Empty endpoint disables tracing.
type TracingConfig struct {
	Endpoint    string
	Insecure    bool
	SampleRatio float64
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
	// (docs/PLAN/13-OBSERVABILITY.md § Logging).
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

// PasswordConfig covers what a per-organization policy cannot decide.
//
// Deliberately small. min_length and the rest belong to the organization
// (docs/PLAN/08 Part B) and are read from the database, so nothing here duplicates
// a policy value — a knob that could disagree with the database would be a
// second source of truth for the same rule.
type PasswordConfig struct {
	// BreachCheckEnabled turns the corpus lookup on and off.
	//
	// ADR-015's rollback path: the check can be disabled without a deploy if
	// the corpus service becomes a problem. Turning it off is visible in the
	// same metric as it failing, because `disabled` is its own outcome rather
	// than an absence of data — "somebody switched it off" and "it has been
	// broken for three weeks" must not look alike.
	BreachCheckEnabled bool

	// BreachAPI overrides the corpus endpoint. Empty means the default.
	//
	// Exists for a self-hosted corpus later (ADR-015 § Alternatives), and for
	// a deployment that must reach the service through a specific egress.
	BreachAPI string

	// BreachTimeout bounds one lookup. Measured at 273ms against the live
	// service from the staging VM.
	BreachTimeout time.Duration
}

// LoadFrom reads configuration using the supplied lookup function, validates it,
// and returns either a usable Config or a *LoadError listing every problem.
func LoadFrom(getenv Getenv) (*Config, error) {
	l := &loader{getenv: getenv}

	cfg := &Config{
		Environment: Environment(l.optional("AUTH_ENV", string(EnvLocal))),
		Issuer:      l.required("AUTH_ISSUER"),
		Mail: MailConfig{
			SMTPURL:        l.optional("AUTH_SMTP_URL", ""),
			From:           l.optional("AUTH_MAIL_FROM", ""),
			AllowCleartext: l.boolean("AUTH_SMTP_ALLOW_CLEARTEXT", false),
		},
		HTTP: HTTPConfig{
			Addr:              l.optional("AUTH_HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: l.duration("AUTH_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       l.duration("AUTH_HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      l.duration("AUTH_HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       l.duration("AUTH_HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   l.duration("AUTH_HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			TrustProxyHeaders: l.boolean("AUTH_TRUST_PROXY_HEADERS", false),
			ClientIPHeader:    l.optional("AUTH_CLIENT_IP_HEADER", ""),
			TrustedProxyCIDRs: l.list("AUTH_TRUSTED_PROXY_CIDRS"),
		},
		Admin: AdminConfig{
			Addr:     l.optional("AUTH_ADMIN_ADDR", "127.0.0.1:9090"),
			Enabled:  l.boolean("AUTH_ADMIN_ENABLED", true),
			TokenRef: l.optional("AUTH_ADMIN_TOKEN_REF", ""),
		},
		Tracing: TracingConfig{
			Endpoint:    l.optional("AUTH_OTLP_ENDPOINT", ""),
			Insecure:    l.boolean("AUTH_OTLP_INSECURE", false),
			SampleRatio: l.float("AUTH_TRACE_SAMPLE_RATIO", 0.05),
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
		Password: PasswordConfig{
			BreachCheckEnabled: l.boolean("AUTH_PASSWORD_BREACH_CHECK_ENABLED", true),
			BreachAPI:          l.optional("AUTH_PASSWORD_BREACH_API", ""),
			BreachTimeout:      l.duration("AUTH_PASSWORD_BREACH_TIMEOUT", 2*time.Second),
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

// list reads a comma-separated setting.
//
// Empty entries are dropped rather than kept as "", because a trailing comma
// in an environment file is a typo and an empty CIDR is not a value anybody
// meant. Whether the remaining entries parse is the caller's question — it is
// the one that can report a useful error.
func (l *loader) list(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (l *loader) boolean(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(l.getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		l.problem("%s must be true or false, got %q", key, raw)
		return fallback
	}
}

func (l *loader) float(key string, fallback float64) float64 {
	raw := strings.TrimSpace(l.getenv(key))
	if raw == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		l.problem("%s must be a number, got %q", key, raw)
		return fallback
	}
	return f
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

	// TLS is mandatory everywhere, with no HTTP fallback (docs/PLAN/09-SECURITY.md
	// § Transport & Storage). TLS itself terminates at the ingress
	// (docs/PLAN/14-DEPLOYMENT.md), but the issuer this service advertises is what
	// consumer applications will actually call — so an http:// issuer outside
	// local development would publish a plaintext endpoint to every client.
	// AUTH_JWT_SIGNING_KEY_REF was how a single signing key was configured
	// before P1-03. Keys now live in the `signing_keys` table, which is what
	// makes an application rollback safe — key state must not be part of the
	// thing being rolled back (docs/PLAN/14 § Rollback Strategy).
	//
	// Refused rather than ignored. A variable that is still set, still looks
	// meaningful, and no longer does anything is worse than one that is gone:
	// an operator rotating a key by editing it would believe they had, and
	// nothing would contradict them until a token failed to verify somewhere
	// else entirely.
	if l.getenv("AUTH_JWT_SIGNING_KEY_REF") != "" {
		l.problem("AUTH_JWT_SIGNING_KEY_REF is set, but signing keys have been " +
			"read from the signing_keys table since P1-03. Remove it from the " +
			"environment; manage keys with `keyctl` (deploy/vm/RUNBOOK-key-rotation.md).")
	}

	if cfg.Issuer != "" {
		switch {
		case strings.HasPrefix(cfg.Issuer, "https://"):
		case strings.HasPrefix(cfg.Issuer, "http://") && cfg.Environment == EnvLocal:
		case strings.HasPrefix(cfg.Issuer, "http://"):
			l.problem("AUTH_ISSUER must use https outside local development; got %q", cfg.Issuer)
		default:
			l.problem("AUTH_ISSUER must be an absolute URL, got %q", cfg.Issuer)
		}
		if strings.TrimSpace(cfg.Mail.SMTPURL) != "" && strings.TrimSpace(cfg.Mail.From) == "" {
			// Half-configured is worse than unconfigured: it looks set up and
			// fails per message instead of at startup.
			l.problem("AUTH_MAIL_FROM is required when AUTH_SMTP_URL is set")
		}
		if strings.HasPrefix(cfg.Mail.SMTPURL, "smtp://") &&
			cfg.Environment != EnvLocal && !cfg.Mail.AllowCleartext {
			// smtp:// tolerates a relay with no STARTTLS, and an invitation or
			// reset link is a bearer credential for an account. Outside local
			// development that is a mistake worth refusing to boot on —
			// unless the operator has stated that the hop does not leave the
			// host, which is what AUTH_SMTP_ALLOW_CLEARTEXT asserts. See
			// MailConfig.AllowCleartext for when that is true and when it is
			// somebody silencing a warning.
			l.problem("AUTH_SMTP_URL uses smtp:// outside local development; use smtps:// " +
				"so links are not sent over a cleartext hop, or set " +
				"AUTH_SMTP_ALLOW_CLEARTEXT=true if the server is a sidecar on this host")
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
	if cfg.Tracing.SampleRatio < 0 || cfg.Tracing.SampleRatio > 1 {
		l.problem("AUTH_TRACE_SAMPLE_RATIO must be between 0 and 1, got %v", cfg.Tracing.SampleRatio)
	}

	// A metrics endpoint reachable beyond loopback discloses request rates,
	// error rates and login outcomes to whoever can reach it — a
	// reconnaissance summary, and during an attack a reliable oracle for
	// whether the attack is working (docs/PLAN/13, docs/SECURITY/02 §12).
	//
	// Binding beyond loopback is allowed, because a scraper on another host is
	// a real need. Doing it WITHOUT a token is not: that combination is the
	// one where a single network-configuration mistake exposes everything.
	if cfg.Admin.Enabled && cfg.Admin.TokenRef == "" && !isLoopbackAddr(cfg.Admin.Addr) {
		l.problem("AUTH_ADMIN_ADDR is %q, which is reachable beyond loopback, but "+
			"AUTH_ADMIN_TOKEN_REF is not set. The metrics endpoint discloses request "+
			"rates, error rates and login outcomes; require a bearer token, or bind it "+
			"to 127.0.0.1 and reach it through a tunnel", cfg.Admin.Addr)
	}

	if cfg.Redis.DB < 0 {
		l.problem("AUTH_REDIS_DB must not be negative, got %d", cfg.Redis.DB)
	}

	// Production must never run with debug logging: debug output across an
	// identity provider is exactly where sensitive values leak into logs
	// despite redaction (docs/PLAN/13-OBSERVABILITY.md).
	if cfg.Environment.IsProduction() && cfg.Log.Level == "debug" {
		l.problem("AUTH_LOG_LEVEL must not be debug in production")
	}
	if cfg.Environment.IsProduction() && cfg.Log.Format != "json" {
		l.problem("AUTH_LOG_FORMAT must be json in production so logs stay machine-parseable")
	}
}

// isLoopbackAddr reports whether a listen address is reachable only from the
// local host.
//
// An empty host (":9090") means all interfaces, which is the case most likely
// to be written by accident — it looks local and is not.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// ErrNotConfigured is returned by components asked to start before their
// configuration exists. It is deliberately distinct from a validation failure.
var ErrNotConfigured = errors.New("component is not configured")

package config

import (
	"strings"
	"testing"
	"time"
)

// env builds a Getenv from a map, so tests never mutate process state.
func env(m map[string]string) Getenv {
	return func(k string) string { return m[k] }
}

// valid returns the minimum environment that must produce a usable Config.
func valid() map[string]string {
	return map[string]string{
		"AUTH_ISSUER":       "https://auth.example.com",
		"AUTH_POSTGRES_DSN": "postgres://auth_app:local_dev_only@localhost:5432/auth?sslmode=disable",
		"AUTH_REDIS_ADDR":   "localhost:6379",
	}
}

func TestLoadFrom_MinimalValidEnvironment(t *testing.T) {
	cfg, err := LoadFrom(env(valid()))
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if cfg.Environment != EnvLocal {
		t.Errorf("Environment: want %q (the default), got %q", EnvLocal, cfg.Environment)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("HTTP.Addr: want default %q, got %q", ":8080", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ShutdownTimeout != 20*time.Second {
		t.Errorf("HTTP.ShutdownTimeout: want default 20s, got %v", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format: want default %q, got %q", "json", cfg.Log.Format)
	}
}

// The core P0-04 requirement: a missing required value fails at startup with a
// message naming the variable — never with a nil dereference at first use.
func TestLoadFrom_MissingRequiredValuesAreNamed(t *testing.T) {
	for _, key := range []string{"AUTH_ISSUER", "AUTH_POSTGRES_DSN", "AUTH_REDIS_ADDR"} {
		t.Run(key, func(t *testing.T) {
			m := valid()
			delete(m, key)

			cfg, err := LoadFrom(env(m))
			if err == nil {
				t.Fatalf("expected an error when %s is unset, got a usable config: %+v", key, cfg)
			}
			if cfg != nil {
				t.Errorf("expected a nil config alongside the error, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error must name the missing variable %q, got: %v", key, err)
			}
		})
	}
}

func TestLoadFrom_BlankRequiredValueIsTreatedAsMissing(t *testing.T) {
	m := valid()
	m["AUTH_ISSUER"] = "   "

	if _, err := LoadFrom(env(m)); err == nil {
		t.Fatal("a whitespace-only required value must be rejected, not accepted as set")
	}
}

// All problems are reported at once. Fixing configuration one error per restart
// is needless friction when the whole environment can be checked in one pass.
func TestLoadFrom_ReportsEveryProblemAtOnce(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{}))
	if err == nil {
		t.Fatal("expected an error for an entirely empty environment")
	}

	var le *LoadError
	if !asLoadError(err, &le) {
		t.Fatalf("expected *LoadError, got %T", err)
	}
	if len(le.Problems) < 3 {
		t.Errorf("expected at least 3 problems (issuer, dsn, redis), got %d: %v", len(le.Problems), le.Problems)
	}
}

func TestEnvironment_Validation(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"local", true},
		{"staging", true},
		{"production", true},
		{"prod", false},
		{"dev", false},
		{"", false}, // empty falls back to local, so this is exercised via the default
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			if got := Environment(tc.value).Valid(); got != tc.valid {
				t.Errorf("Environment(%q).Valid(): want %v, got %v", tc.value, tc.valid, got)
			}
		})
	}
}

// PLAN/09-SECURITY.md: TLS is mandatory everywhere with no HTTP fallback. The
// issuer is what consumer applications actually call, so a plaintext issuer
// outside local development publishes an insecure endpoint to every client.
func TestLoadFrom_IssuerMustUseHTTPSOutsideLocal(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		issuer  string
		wantErr bool
	}{
		{"https in production", "production", "https://auth.example.com", false},
		{"http in production", "production", "http://auth.example.com", true},
		{"http in staging", "staging", "http://auth.example.com", true},
		{"http in local is allowed", "local", "http://localhost:8080", false},
		{"not a URL", "local", "auth.example.com", true},
		{"trailing slash", "local", "http://localhost:8080/", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			m["AUTH_ENV"] = tc.envName
			m["AUTH_ISSUER"] = tc.issuer

			_, err := LoadFrom(env(m))
			if tc.wantErr && err == nil {
				t.Errorf("issuer %q in %s must be rejected", tc.issuer, tc.envName)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("issuer %q in %s must be accepted, got: %v", tc.issuer, tc.envName, err)
			}
		})
	}
}

// Debug logging across an identity provider is exactly where sensitive values
// leak into logs despite redaction (PLAN/13-OBSERVABILITY.md).
func TestLoadFrom_ProductionRefusesUnsafeLogging(t *testing.T) {
	t.Run("debug level", func(t *testing.T) {
		m := valid()
		m["AUTH_ENV"] = "production"
		m["AUTH_LOG_LEVEL"] = "debug"

		if _, err := LoadFrom(env(m)); err == nil {
			t.Error("production must refuse debug logging")
		}
	})

	t.Run("text format", func(t *testing.T) {
		m := valid()
		m["AUTH_ENV"] = "production"
		m["AUTH_LOG_FORMAT"] = "text"

		if _, err := LoadFrom(env(m)); err == nil {
			t.Error("production must require json log format")
		}
	})

	t.Run("debug is allowed locally", func(t *testing.T) {
		m := valid()
		m["AUTH_LOG_LEVEL"] = "debug"

		if _, err := LoadFrom(env(m)); err != nil {
			t.Errorf("debug logging must be allowed locally, got: %v", err)
		}
	})
}

func TestLoadFrom_MalformedValuesAreRejected(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
	}{
		{"non-duration timeout", "AUTH_HTTP_READ_TIMEOUT", "thirty seconds"},
		{"negative duration", "AUTH_HTTP_READ_TIMEOUT", "-5s"},
		{"zero duration", "AUTH_HTTP_SHUTDOWN_TIMEOUT", "0s"},
		{"non-integer pool size", "AUTH_POSTGRES_MAX_OPEN_CONNS", "many"},
		{"zero pool size", "AUTH_POSTGRES_MAX_OPEN_CONNS", "0"},
		{"negative redis db", "AUTH_REDIS_DB", "-1"},
		{"unknown log level", "AUTH_LOG_LEVEL", "verbose"},
		{"unknown log format", "AUTH_LOG_FORMAT", "xml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			m[tc.key] = tc.val

			if _, err := LoadFrom(env(m)); err == nil {
				t.Errorf("%s=%q must be rejected", tc.key, tc.val)
			}
		})
	}
}

// An idle pool larger than the open pool is a misconfiguration the database
// driver will silently clamp — better to say so at startup.
func TestLoadFrom_IdleConnectionsCannotExceedOpenConnections(t *testing.T) {
	m := valid()
	m["AUTH_POSTGRES_MAX_OPEN_CONNS"] = "5"
	m["AUTH_POSTGRES_MAX_IDLE_CONNS"] = "10"

	_, err := LoadFrom(env(m))
	if err == nil {
		t.Fatal("idle connections exceeding open connections must be rejected")
	}
	if !strings.Contains(err.Error(), "MAX_IDLE_CONNS") {
		t.Errorf("error should name the offending variable, got: %v", err)
	}
}

// The error message is what an operator reads at 3am. It must list the problems.
func TestLoadError_MessageListsEveryProblem(t *testing.T) {
	e := &LoadError{Problems: []string{"first problem", "second problem"}}
	msg := e.Error()

	if !strings.Contains(msg, "first problem") || !strings.Contains(msg, "second problem") {
		t.Errorf("error message must contain every problem, got: %s", msg)
	}
	if !strings.Contains(msg, "2 problem(s)") {
		t.Errorf("error message should state the problem count, got: %s", msg)
	}
}

func asLoadError(err error, target **LoadError) bool {
	le, ok := err.(*LoadError)
	if ok {
		*target = le
	}
	return ok
}

// TrustProxyHeaders is deliberately independent of Environment: "deployed" and
// "has a header-stripping proxy in front" are different facts. A tunnel such as
// Cloudflare Tunnel forwards client headers untouched, so inferring trust from
// the environment would get it wrong there (SECURITY/02 §10).
func TestLoadFrom_TrustProxyHeadersIsExplicitAndDefaultsToFalse(t *testing.T) {
	t.Run("defaults to false even in production", func(t *testing.T) {
		m := valid()
		m["AUTH_ENV"] = "production"

		cfg, err := LoadFrom(env(m))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.HTTP.TrustProxyHeaders {
			t.Error("must default to false: a deployment behind a non-stripping proxy should be safe by default, not by accident")
		}
	})

	t.Run("accepted truthy and falsy spellings", func(t *testing.T) {
		for raw, want := range map[string]bool{
			"true": true, "1": true, "yes": true, "on": true, "TRUE": true,
			"false": false, "0": false, "no": false, "off": false,
		} {
			m := valid()
			m["AUTH_TRUST_PROXY_HEADERS"] = raw

			cfg, err := LoadFrom(env(m))
			if err != nil {
				t.Fatalf("%q: %v", raw, err)
			}
			if cfg.HTTP.TrustProxyHeaders != want {
				t.Errorf("%q: got %v, want %v", raw, cfg.HTTP.TrustProxyHeaders, want)
			}
		}
	})

	t.Run("a malformed value is rejected rather than silently false", func(t *testing.T) {
		m := valid()
		m["AUTH_TRUST_PROXY_HEADERS"] = "maybe"

		if _, err := LoadFrom(env(m)); err == nil {
			t.Error("a malformed boolean must be an error: silently defaulting to false would hide a typo in a security-relevant setting")
		}
	})
}

// A metrics endpoint reachable beyond loopback discloses request rates, error
// rates and login outcomes to whoever can reach it. Allowing that bind is
// reasonable — a scraper on another host is a real need — but allowing it
// WITHOUT a token is the combination where one network-configuration mistake
// exposes everything (SECURITY/02 §12).
func TestLoadFrom_MetricsBeyondLoopbackRequiresAToken(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"loopback without a token is fine", "127.0.0.1:9090", "", false},
		{"localhost without a token is fine", "localhost:9090", "", false},
		{"ipv6 loopback without a token is fine", "[::1]:9090", "", false},
		{"private address without a token is refused", "10.1.200.13:9090", "", true},
		{"all interfaces without a token is refused", "0.0.0.0:9090", "", true},
		{"empty host means all interfaces, and is refused", ":9090", "", true},
		{"private address with a token is allowed", "10.1.200.13:9090", "file:/etc/zed-auth/secrets/metrics-token", false},
		{"all interfaces with a token is allowed", "0.0.0.0:9090", "file:/etc/zed-auth/secrets/metrics-token", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			m["AUTH_ADMIN_ADDR"] = tc.addr
			if tc.token != "" {
				m["AUTH_ADMIN_TOKEN_REF"] = tc.token
			}

			_, err := LoadFrom(env(m))
			if tc.wantErr && err == nil {
				t.Errorf("addr %q with token %q must be refused", tc.addr, tc.token)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("addr %q with token %q must be accepted, got: %v", tc.addr, tc.token, err)
			}
		})
	}
}

// ":9090" looks local and is not — it binds every interface. It is the shape
// most likely to be written by accident.
func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:9090": true,
		"localhost:9090": true,
		"[::1]:9090":     true,
		"0.0.0.0:9090":   false,
		":9090":          false,
		"10.1.200.13:90": false,
		"example.com:90": false,
		"garbage":        false,
	} {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// A retired variable is refused, not ignored.
//
// AUTH_JWT_SIGNING_KEY_REF configured a single signing key before P1-03. Keys
// now live in the signing_keys table, which is what makes an application
// rollback safe — key state must not be part of the thing being rolled back.
//
// Ignoring it would be worse than removing it: an operator rotating a key by
// editing the variable would believe they had, and nothing would contradict
// them until a token failed to verify somewhere else entirely.
func TestRetiredSigningKeyVariableIsRefused(t *testing.T) {
	m := valid()
	m["AUTH_JWT_SIGNING_KEY_REF"] = "file:/etc/zed-auth/secrets/jwt-signing.pem"

	_, err := LoadFrom(env(m))
	if err == nil {
		t.Fatal("AUTH_JWT_SIGNING_KEY_REF must be refused, not silently ignored")
	}
	if !strings.Contains(err.Error(), "signing_keys") {
		t.Errorf("the error should say where keys live now, got: %v", err)
	}
	if !strings.Contains(err.Error(), "keyctl") {
		t.Errorf("the error should name the tool that replaced it, got: %v", err)
	}
}

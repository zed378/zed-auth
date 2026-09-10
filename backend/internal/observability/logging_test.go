package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func decode(t *testing.T, b *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	line := strings.TrimSpace(b.String())
	if line == "" {
		t.Fatal("no log output produced")
	}
	// Take the last record if several were written.
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, line)
	}
	return m
}

// docs/PLAN/13-OBSERVABILITY.md § Logging: structured JSON, every line correlatable.
func TestNewLogger_EmitsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, Options{Level: "info", Format: "json", Service: "authservice"})

	log.Info("service started", slog.String("addr", ":8080"))

	m := decode(t, &buf)
	if m["msg"] != "service started" {
		t.Errorf("msg: want %q, got %v", "service started", m["msg"])
	}
	if m["service"] != "authservice" {
		t.Errorf("service attribute missing, got %v", m["service"])
	}
}

// The core P0-09 requirement, and CLAUDE.md's hard rule: a struct containing a
// token field emits a redacted placeholder, not the value.
func TestNewLogger_RedactsSensitiveKeys(t *testing.T) {
	secrets := []string{
		"password", "new_password", "current_password",
		"token", "access_token", "refresh_token", "id_token", "id_token_hint",
		"client_secret", "code_verifier", "code_challenge",
		"authorization", "cookie", "set-cookie",
		"private_key", "totp_secret", "recovery_code",
		"api_key", "credential", "assertion", "saml_response",
		"attributes", "resource_attributes",
	}

	for _, key := range secrets {
		t.Run(key, func(t *testing.T) {
			var buf bytes.Buffer
			log := NewLogger(&buf, Options{Format: "json"})

			log.Info("handling request", slog.String(key, "super-secret-value"))

			out := buf.String()
			if strings.Contains(out, "super-secret-value") {
				t.Errorf("secret value leaked into log output for key %q: %s", key, out)
			}
			if !strings.Contains(out, Redacted) {
				t.Errorf("expected %s placeholder for key %q, got: %s", Redacted, key, out)
			}
		})
	}
}

// A namespaced key must be caught too — http.request.authorization is exactly
// as sensitive as authorization.
func TestIsSensitiveKey_MatchesNamespacedKeys(t *testing.T) {
	sensitive := []string{
		"Authorization", "AUTHORIZATION",
		"http.request.authorization",
		"request/cookie",
		"oauth:client_secret",
		"user.password",
	}
	for _, k := range sensitive {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", k)
		}
	}

	safe := []string{
		"user_id", "org_id", "client_id", "status", "method", "path",
		"duration_ms", "event_type", "authorization_server", "codebase",
	}
	for _, k := range safe {
		if IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = true, want false — over-redaction hides useful data", k)
		}
	}
}

// Redaction must survive nesting: an attribute inside a group is just as
// capable of leaking a credential.
func TestNewLogger_RedactsInsideGroups(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, Options{Format: "json"})

	log.Info("token exchange",
		slog.Group("oauth",
			slog.String("client_id", "app-123"),
			slog.String("client_secret", "leak-me"),
		),
	)

	out := buf.String()
	if strings.Contains(out, "leak-me") {
		t.Errorf("secret inside a group leaked: %s", out)
	}
	if !strings.Contains(out, "app-123") {
		t.Errorf("non-sensitive sibling attribute was lost: %s", out)
	}
}

// Correlation: docs/PLAN/13 requires a request_id on every line so a request can be
// followed across services.
func TestNewLogger_AttachesCorrelationIDsFromContext(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, Options{Format: "json"})

	ctx := WithRequestID(context.Background(), "req-abc123")
	ctx = WithTraceID(ctx, "trace-xyz789")

	log.InfoContext(ctx, "handled request")

	m := decode(t, &buf)
	if m["request_id"] != "req-abc123" {
		t.Errorf("request_id: want %q, got %v", "req-abc123", m["request_id"])
	}
	if m["trace_id"] != "trace-xyz789" {
		t.Errorf("trace_id: want %q, got %v", "trace-xyz789", m["trace_id"])
	}
}

func TestNewLogger_NoCorrelationIDsWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, Options{Format: "json"})

	log.InfoContext(context.Background(), "no context ids")

	m := decode(t, &buf)
	if _, present := m["request_id"]; present {
		t.Error("request_id should be absent when not set, not empty-string present")
	}
}

// docs/PLAN/13: a failed login is WARN, not ERROR. It is expected behavior, and
// treating it as an error trains everyone to ignore errors.
func TestNewLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		level    string
		logDebug bool
		logInfo  bool
		logWarn  bool
	}{
		{"debug", true, true, true},
		{"info", false, true, true},
		{"warn", false, false, true},
		{"error", false, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			var buf bytes.Buffer
			log := NewLogger(&buf, Options{Level: tc.level, Format: "json"})

			log.Debug("d")
			gotDebug := strings.Contains(buf.String(), `"msg":"d"`)
			log.Info("i")
			gotInfo := strings.Contains(buf.String(), `"msg":"i"`)
			log.Warn("w")
			gotWarn := strings.Contains(buf.String(), `"msg":"w"`)

			if gotDebug != tc.logDebug {
				t.Errorf("debug emitted=%v, want %v", gotDebug, tc.logDebug)
			}
			if gotInfo != tc.logInfo {
				t.Errorf("info emitted=%v, want %v", gotInfo, tc.logInfo)
			}
			if gotWarn != tc.logWarn {
				t.Errorf("warn emitted=%v, want %v", gotWarn, tc.logWarn)
			}
		})
	}
}

func TestNewLogger_UnknownLevelFallsBackToInfo(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, Options{Level: "verbose", Format: "json"})

	log.Debug("should not appear")
	log.Info("should appear")

	out := buf.String()
	if strings.Contains(out, "should not appear") {
		t.Error("unknown level must not enable debug output")
	}
	if !strings.Contains(out, "should appear") {
		t.Error("unknown level should fall back to info, not silence the logger")
	}
}

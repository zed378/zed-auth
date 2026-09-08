// Package observability provides structured logging, and later metrics and
// tracing, for the Auth Service.
//
// The redaction layer here exists because PLAN/13-OBSERVABILITY.md and
// CLAUDE.md both state the same rule: never log tokens, passwords, or raw
// resource attributes sent to /v1/authz/check. That rule is enforced by the
// logger itself rather than by developer discipline, because discipline fails
// silently and a leaked credential in a log is indistinguishable from a leaked
// credential anywhere else.
package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

// Redacted replaces any value whose key matches the deny list. It is a fixed
// string rather than an empty value so that a reader can tell "this field was
// present and withheld" from "this field was absent" — the difference matters
// when reconstructing an incident.
const Redacted = "[REDACTED]"

// sensitiveKeys are logged as Redacted regardless of their value's type.
//
// Sources:
//   - PLAN/13-OBSERVABILITY.md § Logging: tokens, passwords, raw /v1/authz/check
//     resource attributes.
//   - PLAN/09-SECURITY.md § Passwords: never log passwords, even failed attempts.
//   - SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §16: secret exposure.
//
// Matching is case-insensitive and also matches the final segment of a dotted
// or slash-separated key, so `http.request.authorization` is caught as readily
// as `authorization`.
var sensitiveKeys = map[string]struct{}{
	"password":            {},
	"password_hash":       {},
	"new_password":        {},
	"current_password":    {},
	"secret":              {},
	"client_secret":       {},
	"client_secret_hash":  {},
	"token":               {},
	"access_token":        {},
	"refresh_token":       {},
	"id_token":            {},
	"id_token_hint":       {},
	"token_hash":          {},
	"code":                {},
	"code_verifier":       {},
	"code_challenge":      {},
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"session_id":          {},
	"private_key":         {},
	"privatekey":          {},
	"totp_secret":         {},
	"recovery_code":       {},
	"recovery_codes":      {},
	"api_key":             {},
	"apikey":              {},
	"credential":          {},
	"credentials":         {},
	"assertion":           {},
	"saml_response":       {},
	"attributes":          {}, // raw /v1/authz/check resource attributes
	"resource_attributes": {},
	"subject_attributes":  {},
}

// IsSensitiveKey reports whether a log attribute key must be redacted.
// Exported so tests and the CI lint rule can share one definition.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if _, ok := sensitiveKeys[k]; ok {
		return true
	}
	// Match the last segment of a namespaced key: http.request.authorization,
	// request/cookie, oauth:client_secret.
	if i := strings.LastIndexAny(k, "./:-"); i >= 0 && i+1 < len(k) {
		if _, ok := sensitiveKeys[k[i+1:]]; ok {
			return true
		}
	}
	return false
}

// redactAttr is slog's ReplaceAttr hook. It runs on every attribute of every
// record, including attributes nested inside groups.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	return a
}

// Options configures the logger. It mirrors config.LogConfig without importing
// it, so the observability package stays usable from tests that have no config.
type Options struct {
	// Level is one of debug, info, warn, error. Anything else falls back to info.
	Level string
	// Format is json or text. Deployed environments always use json.
	Format string
	// Service is attached to every record so logs from several services can be
	// distinguished once they reach a shared aggregator.
	Service string
	// Version is the build version, attached to every record.
	Version string
}

// NewLogger builds a redacting structured logger writing to w.
func NewLogger(w io.Writer, opts Options) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{
		Level:       parseLevel(opts.Level),
		ReplaceAttr: redactAttr,
	}

	var h slog.Handler
	if strings.EqualFold(opts.Format, "text") {
		h = slog.NewTextHandler(w, handlerOpts)
	} else {
		h = slog.NewJSONHandler(w, handlerOpts)
	}

	// contextHandler pulls the request ID out of the context so no call site has
	// to remember to attach it. A log line without a request ID cannot be
	// correlated, which is most of the value of structured logging.
	logger := slog.New(&contextHandler{Handler: h})

	var base []slog.Attr
	if opts.Service != "" {
		base = append(base, slog.String("service", opts.Service))
	}
	if opts.Version != "" {
		base = append(base, slog.String("version", opts.Version))
	}
	if len(base) > 0 {
		logger = slog.New(logger.Handler().WithAttrs(base))
	}
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// --- request-scoped correlation -------------------------------------------

type contextKey int

const (
	requestIDKey contextKey = iota
	traceIDKey
)

// WithRequestID returns a context carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext returns the request ID, or "" if none is set.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// WithTraceID returns a context carrying the trace ID.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFromContext returns the trace ID, or "" if none is set.
func TraceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

// contextHandler attaches request-scoped correlation IDs to every record.
type contextHandler struct{ slog.Handler }

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if id := TraceIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("trace_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

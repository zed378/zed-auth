package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// TracingConfig configures distributed tracing.
//
// docs/PLAN/13-OBSERVABILITY.md § Tracing wants end-to-end visibility from request
// through validation, database query, and policy evaluation — because the
// question that actually gets asked during an incident is "which part of this
// request was slow", and per-endpoint latency cannot answer it.
type TracingConfig struct {
	// Endpoint is the OTLP collector, e.g. "otel-collector:4318". Empty
	// disables tracing entirely, which is the default and the correct local
	// behaviour: an exporter retrying against a collector that does not exist
	// produces noise that trains people to ignore logs.
	Endpoint string

	// Insecure sends over plain HTTP. Acceptable only when the collector is
	// on the same host or a private network.
	Insecure bool

	// SampleRatio is the fraction of traces recorded, 0.0 to 1.0.
	//
	// Sampling matters here more than usual: /oauth/token and
	// /v1/authz/check are the highest-volume endpoints in the system
	// (docs/PLAN/12), and tracing every request would cost more than the requests
	// themselves. Errors are always sampled regardless — see the sampler below.
	SampleRatio float64

	ServiceName    string
	ServiceVersion string
	Environment    string
}

// InitTracing configures the global tracer provider.
//
// Returns a shutdown function that flushes pending spans. It must be called on
// exit: an unflushed exporter silently drops the spans from the last few
// seconds of a process, which are exactly the ones present when it crashed.
func InitTracing(ctx context.Context, cfg TracingConfig, log *slog.Logger) (func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		// A no-op provider rather than leaving the global unset: instrumented
		// code then works unchanged whether tracing is on or off, so there is
		// no `if tracingEnabled` branch scattered through the codebase.
		otel.SetTracerProvider(noop.NewTracerProvider())
		log.LogAttrs(ctx, slog.LevelInfo, "tracing disabled",
			slog.String("reason", "no OTLP endpoint configured"))
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			// A bounded queue. Unbounded buffering turns a slow collector into
			// an out-of-memory kill of the service it is observing.
			sdktrace.WithMaxQueueSize(2048),
		),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		)),
		sdktrace.WithResource(newResource(cfg)),
	)

	otel.SetTracerProvider(tp)

	// W3C trace context, so a trace started by a consumer application
	// continues through this service rather than starting again. That
	// continuity is most of the value: a consumer team debugging a slow login
	// needs to see this service's spans inside their own trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.LogAttrs(ctx, slog.LevelInfo, "tracing enabled",
		slog.String("endpoint", cfg.Endpoint),
		slog.Float64("sample_ratio", cfg.SampleRatio),
	)

	return tp.Shutdown, nil
}

func newResource(cfg TracingConfig) *resource.Resource {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironmentName(cfg.Environment),
	)
}

// Tracer returns a named tracer.
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }

// StartSpan begins a span and returns the derived context.
//
// Attributes must never carry a token, a password, or a raw
// /v1/authz/check resource attribute. A span is shipped to a collector that
// is usually a different trust boundary from the logs, and the redaction the
// logger applies (P0-09) does not reach here — so this is a rule the caller
// has to keep (docs/PLAN/13, CLAUDE.md).
func StartSpan(ctx context.Context, tracerName, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, spanName)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}

// TraceIDFromSpan returns the current trace ID, or "" when tracing is off.
//
// Used to put the trace ID on every log line, which is what lets an operator
// move from a log entry to the trace it belongs to.
func TraceIDFromSpan(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

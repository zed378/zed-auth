# 04 - Distributed Tracing

> Category: **Observability** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-11, P5-05, P5-08 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State exactly what tracing infrastructure exists, and exactly what it does not yet do — because "wired" and "working" are different claims here, and this document exists so nobody assumes the second from the first.

## Scope

Covers `backend/internal/observability/tracing.go` and its wiring in `backend/cmd/authservice/main.go`. Does not cover metrics (`03-METRICS-AND-SLO-TRACKING.md`) or the audit log (`01-AUDIT-LOGGING-SPECIFICATION.md`), neither of which depends on tracing.

## As Built

**What exists and is real:**

- An OpenTelemetry SDK tracer provider, an OTLP-over-HTTP exporter (`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`), a batch span processor with a bounded queue (`MaxQueueSize: 2048` — unbounded buffering would turn a slow collector into an out-of-memory kill of the service it observes), a parent-based ratio sampler, and a W3C `TraceContext` + `Baggage` propagator, all assembled in `observability.InitTracing` (`backend/internal/observability/tracing.go`).
- `InitTracing` is called once at startup in `backend/cmd/authservice/main.go`, with a `defer` that flushes on shutdown — an unflushed exporter would silently drop the spans from the last seconds of a process, which are exactly the ones present when it crashed.
- When `AUTH_OTLP_ENDPOINT` is empty (the default), `InitTracing` installs a no-op tracer provider rather than leaving the global tracer unset, specifically so that instrumented code works unchanged whether tracing is on or off — there is no `if tracingEnabled` branch anywhere else in the codebase, because there does not need to be.
- Helper functions exist for the day something calls them: `observability.Tracer(name)`, `observability.StartSpan(ctx, tracerName, spanName, attrs...)`, `observability.TraceIDFromSpan(ctx)`.

**What does not exist:**

- **No code outside `backend/internal/observability` ever calls `StartSpan` or `Tracer(...)`.** A repository-wide search finds zero call sites in `backend/cmd` or any other `backend/internal` package. This means that even with `AUTH_OTLP_ENDPOINT` configured and a collector reachable, the service creates **no spans at all** — nothing for an incoming HTTP request, nothing for a database query, nothing for a policy evaluation. The exporter, sampler, and propagator are correctly configured machinery with nothing feeding it.
- **No HTTP middleware extracts or injects trace context.** `backend/internal/httpserver` has no `otelhttp` (or equivalent) instrumentation; the global propagator set by `InitTracing` is consequently never invoked in either direction. A trace started by a consumer application does **not** currently continue through this service, despite that being the propagator's whole purpose per the code's own comment.
- **`observability.WithTraceID`/`TraceIDFromContext` are never called from request handling.** The `contextHandler` in `logging.go` will attach a `trace_id` attribute to a log line if one is present in the context, but nothing ever puts one there — so today's JSON log lines never carry a `trace_id`, only a `request_id`.

In short: this is SDK-and-exporter wiring with the application-side instrumentation not yet written. Turning on `AUTH_OTLP_ENDPOINT` today changes the startup log line from "tracing disabled" to "tracing enabled" and establishes a connection to a collector, and produces no visible traces because nothing generates any.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Tracing enabled | Only when `AUTH_OTLP_ENDPOINT` is non-empty | `backend/internal/observability/tracing.go` `InitTracing` |
| Sample ratio default | `0.05` (parent-based ratio sampler) | `backend/internal/config/config.go` (`AUTH_TRACE_SAMPLE_RATIO`) |
| Insecure transport | Plain HTTP only via `AUTH_OTLP_INSECURE=true`; intended for a collector on the same host | `backend/internal/config/config.go`, `tracing.go` |
| Exporter queue bound | 2048 spans | `tracing.go` `InitTracing` (`sdktrace.WithMaxQueueSize`) |
| Batch timeout | 5 seconds | `tracing.go` `InitTracing` (`sdktrace.WithBatchTimeout`) |
| Propagation format | W3C `TraceContext` + `Baggage` (set globally; not currently invoked by any handler) | `tracing.go` |
| Span attribute content | Must never carry a token, password, or raw `/v1/authz/check` resource attribute | Documentation-only rule; **not enforced by code**, since the logger's redaction (`02-PRIVACY-PRESERVING-LOGGING.md`) does not reach span attributes |

## Interfaces

- Configuration: `AUTH_OTLP_ENDPOINT` (empty by default), `AUTH_OTLP_INSECURE` (`false`), `AUTH_TRACE_SAMPLE_RATIO` (`0.05`).
- Code seam for future instrumentation: `observability.Tracer(name)`, `observability.StartSpan(ctx, tracerName, spanName, attrs...) (context.Context, trace.Span)`, `observability.TraceIDFromSpan(ctx) string`.

## Security Considerations

- Because span attributes bypass the logger's key-based redaction entirely, **any future call site that adds `StartSpan` attributes must independently avoid tokens, passwords, and raw `/v1/authz/check` resource attributes** — the code comment on `StartSpan` states this explicitly, but nothing currently checks it, since nothing currently calls it. `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`'s Information Disclosure concerns therefore extend to any tracing instrumentation added later.
- A trace collector is typically a different trust boundary from the log aggregator; the code comment on `StartSpan` makes this explicit as the reason the rule needs restating rather than being assumed inherited from the logger.

## Verification

- No test exercises span creation, propagation, or content, because no code creates a span. There is nothing to test yet beyond configuration parsing and the no-op-provider fallback, neither of which has a dedicated test file today.

## Not Yet Built / Open Questions

- HTTP request spans (incoming request → validation → DB query → policy evaluation → response), the exact chain `docs/PLAN/13-OBSERVABILITY.md` § Tracing asks for, do not exist.
- `otelhttp` (or equivalent) middleware for context extraction/injection does not exist.
- Database query spans do not exist (no wrapping in `backend/internal/storage/postgres`).
- Policy-evaluation spans do not exist (moot until Phase 4b/ABAC ships an evaluator to instrument).
- Attaching `trace_id` to log lines via `observability.WithTraceID` is unused.
- Degraded-dependency and end-to-end latency attribution work that depends on tracing (`TASKS/PHASE-5-HARDENING.md` § P5-05, § P5-08) cannot start until the above exists.

## Related Documents

- `docs/PLAN/13-OBSERVABILITY.md` § Tracing
- [`02-PRIVACY-PRESERVING-LOGGING.md`](./02-PRIVACY-PRESERVING-LOGGING.md), [`03-METRICS-AND-SLO-TRACKING.md`](./03-METRICS-AND-SLO-TRACKING.md)
- `deploy/observability/README.md` § Tracing

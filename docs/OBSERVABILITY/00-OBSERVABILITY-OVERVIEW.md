# 00 - Observability Overview

> Category: **Observability** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-11, P0-12, P1-20, P5-08 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe what the running service actually emits about itself — structured logs, Prometheus metrics, the append-only audit log, and (partially) distributed traces — and what an operator or an incident responder can and cannot see today.

## Scope

This document is the map; the other four documents in this folder go deep on one pillar each:

- [`01-AUDIT-LOGGING-SPECIFICATION.md`](./01-AUDIT-LOGGING-SPECIFICATION.md) — the `events` table, a **product feature**, not an operational log.
- [`02-PRIVACY-PRESERVING-LOGGING.md`](./02-PRIVACY-PRESERVING-LOGGING.md) — the redaction rules that apply to both operational logs and the audit log.
- [`03-METRICS-AND-SLO-TRACKING.md`](./03-METRICS-AND-SLO-TRACKING.md) — Prometheus instruments, the alert rules, and what SLO tracking does not yet exist.
- [`04-DISTRIBUTED-TRACING.md`](./04-DISTRIBUTED-TRACING.md) — the OpenTelemetry wiring, and why it currently exports nothing.

Performance targets that these signals are judged against live in `docs/PERFORMANCE/`. Design intent for both categories is `docs/PLAN/13-OBSERVABILITY.md` and `docs/PLAN/12-PERFORMANCE.md`; this folder does not restate them, only what the code actually does against them.

## As Built

Two things share the word "log" in this codebase and must not be confused:

| | Operational logs | Audit log |
|---|---|---|
| Mechanism | `log/slog`, JSON to stdout | `INSERT INTO events`, inside the caller's own transaction |
| Package | `backend/internal/observability` (`logging.go`) | `backend/internal/audit`, read via `backend/internal/auditlog` |
| Audience | Whoever collects container stdout | Organization administrators (`GET /v1/organizations/{org_id}/events`), and instance operators for instance-level rows |
| Retention | Whatever the log collector is configured to keep — not controlled by this service | 24 months hot per row, per `docs/PLAN/04-DATA-MODEL.md` § Retention and Growth (not yet enforced by any archival job — see `01-AUDIT-LOGGING-SPECIFICATION.md`) |
| Mutability | N/A | Append-only at the database level; the application role has no `UPDATE`/`DELETE` on `events` (`backend/migrations/20260908000006_events_audit_log.up.sql`, reasserted by `20260910000019_reassert_events_append_only.up.sql`) |

Three pillars exist in code today, one only partially:

1. **Structured logging** (`backend/internal/observability/logging.go`) — a redacting `slog` logger, JSON by default, with request-ID correlation. Implemented; see `02-PRIVACY-PRESERVING-LOGGING.md`.
2. **Metrics** (`backend/internal/observability/metrics.go`) — a dedicated Prometheus registry with instruments for every phase built so far, served from a separate internal listener (`backend/internal/httpserver/admin.go`). Implemented; see `03-METRICS-AND-SLO-TRACKING.md`.
3. **Tracing** (`backend/internal/observability/tracing.go`) — an OpenTelemetry SDK, OTLP HTTP exporter, sampler and W3C propagator are wired and configurable, but no code anywhere in `backend/` ever starts a span. **Partially implemented**; see `04-DISTRIBUTED-TRACING.md`.

The audit log is covered separately because it is not part of "observability" in the operations sense — it is a product feature with its own API, its own retention rule, and its own database privileges — but it shares the same redaction code as the logger (`backend/internal/audit/audit.go`'s `redactPayload`, which calls `observability.IsSensitiveKey`).

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Metrics/admin listener bind | `127.0.0.1:9090` by default; refused non-loopback in production without a bearer token | `backend/internal/config/config.go` (`isLoopbackAddr` check), `backend/internal/httpserver/admin.go` |
| Metrics endpoint auth | Bearer token, constant-time compare, `404` (not `401`) on failure | `backend/internal/httpserver/admin.go` `requireBearerToken` |
| Tracing default | Off (`AUTH_OTLP_ENDPOINT` empty → no-op tracer provider) | `backend/internal/observability/tracing.go` `InitTracing` |
| Trace sample ratio default | `0.05` | `backend/internal/config/config.go` (`AUTH_TRACE_SAMPLE_RATIO`) |
| Log format | JSON to stdout; `text` available for local development | `backend/internal/observability/logging.go` `NewLogger` |
| Sensitive log/audit keys | Redacted to the literal string `[REDACTED]` | `backend/internal/observability/logging.go` `sensitiveKeys`, `IsSensitiveKey` |

## Interfaces

- `GET /metrics` — Prometheus exposition, on the admin listener only, never on the public listener (`deploy/observability/README.md`).
- `GET /v1/organizations/{org_id}/events` (`operationId: listEvents`) — the audit log read API. See `01-AUDIT-LOGGING-SPECIFICATION.md`.
- `X-Request-Id` — accepted or generated per request, echoed in the response and attached to every log line and audit event (`backend/internal/httpserver/middleware.go` `RequestID`).

## Security Considerations

- The metrics endpoint is a reconnaissance summary and a reliable oracle for whether an attack is working (request rates, login failure counts, pool saturation, service version) — this is why it is a separate, authenticated, non-public listener rather than a route (`backend/internal/httpserver/admin.go`; `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12).
- Neither logs nor traces nor the audit log may carry a token, a password, or a raw `/v1/authz/check` resource attribute (`docs/PLAN/13-OBSERVABILITY.md` § Logging; CLAUDE.md). This is enforced for logs and the audit log by shared redaction code, and is a documentation-only rule for spans (see `04-DISTRIBUTED-TRACING.md`).
- `pprof` is deliberately not mounted on the admin listener: it would expose heap contents, which on this service means tokens and passwords in flight (`backend/internal/httpserver/admin.go`).

## Verification

- `backend/internal/observability/logging_test.go` — `TestNewLogger_RedactsSensitiveKeys`, `TestNewLogger_RedactsInsideGroups`, `TestNewLogger_AttachesCorrelationIDsFromContext`.
- `backend/internal/observability/metrics_test.go` — `TestEveryMetricNamedInThePlanIsRegistered`, `TestBucketsCoverThePerformanceTargets`, `TestRouteLabelUsesThePatternNotThePath`, `TestUnmatchedRoutesCollapseToOneLabel`.
- `backend/internal/httpserver/admin_test.go` — admin listener auth behavior.
- Staging: `MEMORY/records/2026-09-11-P1-28-threat-model-review.md` §19 re-confirms live that `UPDATE` on `events` as `auth_app` is refused.

## Not Yet Built / Open Questions

- Distributed tracing exports nothing yet — no code calls `observability.StartSpan` outside its own package (see `04-DISTRIBUTED-TRACING.md`).
- A security/observability dashboard (Grafana or equivalent) does not exist anywhere in the repository — owned by `TASKS/PHASE-5-HARDENING.md` § P5-08.
- Forwarding the audit log to an external SIEM is a documented no-op seam (`audit.Forwarder`), not a working integration — owned by P5-08.
- Degraded-dependency behavior (Redis slow, DB replica lagging, OPA slowed) is asserted in code for the authz cache and rate limiter, but has not been systematically tested per `docs/PLAN/12`'s list — owned by `TASKS/PHASE-5-HARDENING.md` § P5-05.

## Related Documents

- `docs/PLAN/13-OBSERVABILITY.md`, `docs/PLAN/12-PERFORMANCE.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12, §19
- `docs/PERFORMANCE/00-PERFORMANCE-TARGETS.md`

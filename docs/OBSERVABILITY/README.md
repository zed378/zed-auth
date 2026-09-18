# Observability

What the running Auth Service actually emits about itself, and how each signal is verified: structured operational logs, the append-only audit log (a product feature, not an ops log), Prometheus metrics with Alertmanager-style rules, and the current, partial state of distributed tracing. This category documents the code as built; `docs/PLAN/13-OBSERVABILITY.md` is the design intent it is measured against, and `docs/SECURITY/` covers the threat model these signals help detect. Related latency targets live in `docs/PERFORMANCE/`.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-OBSERVABILITY-OVERVIEW.md`](./00-OBSERVABILITY-OVERVIEW.md) | The map: logs vs. audit log vs. metrics vs. tracing | Partially implemented |
| [`01-AUDIT-LOGGING-SPECIFICATION.md`](./01-AUDIT-LOGGING-SPECIFICATION.md) | The `events` table, the one writer, the read API, the audit guard | Partially implemented |
| [`02-PRIVACY-PRESERVING-LOGGING.md`](./02-PRIVACY-PRESERVING-LOGGING.md) | Key-based redaction, the CI log-hygiene gate | Implemented |
| [`03-METRICS-AND-SLO-TRACKING.md`](./03-METRICS-AND-SLO-TRACKING.md) | Prometheus instruments and the 22 alert rules; no dashboards yet | Partially implemented |
| [`04-DISTRIBUTED-TRACING.md`](./04-DISTRIBUTED-TRACING.md) | OTel SDK wired, zero spans created anywhere | Partially implemented |

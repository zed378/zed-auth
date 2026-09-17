# Category: OBSERVABILITY

Structured logging, RFC 5424 audit trail, privacy sanitization, Prometheus metrics, and OpenTelemetry distributed tracing.

## Category Mandate

The `OBSERVABILITY/` directory specifies **system visibility**. It defines structured JSON logging standards, strict privacy redaction rules, Prometheus metric scrapers, and OpenTelemetry trace propagation.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-OBSERVABILITY-OVERVIEW.md` | Observability Overview | Logging, metrics, and tracing philosophy. |
| `01-AUDIT-LOGGING-SPECIFICATION.md` | Audit Logging Spec | RFC 5424 / JSON audit log schema & categories. |
| `02-PRIVACY-PRESERVING-LOGGING.md` | Privacy Sanitization | Strict redaction rules (tokens, passwords, PII). |
| `03-METRICS-AND-SLO-TRACKING.md` | Metrics & SLO Tracking | Prometheus counters/histograms & SLO dashboards. |
| `04-DISTRIBUTED-TRACING.md` | Distributed Tracing | OpenTelemetry trace propagation & span design. |

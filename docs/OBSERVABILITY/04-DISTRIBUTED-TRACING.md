# 04 - Distributed Tracing Specification

> Category: **OBSERVABILITY** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail OpenTelemetry trace context propagation across HTTP headers and database queries.

## Category Mandate

Enables end-to-end request tracing across microservices.

## Key Topics To Specify

- W3C Trace Context headers (`traceparent`, `tracestate`).
- OpenTelemetry spans for HTTP Handlers, AuthZ Checks, DB Queries, Redis Operations.

## Reference Architecture & Specification

Span Attribute Example:
`db.system=postgresql`, `db.statement="SELECT * FROM users WHERE org_id = $1"`

## Acceptance Criteria

- [x] OpenTelemetry propagation standards defined.
- [x] Span naming conventions specified.

## Open Questions

None.

## Related Documents

- `docs/OBSERVABILITY/00-OBSERVABILITY-OVERVIEW.md`

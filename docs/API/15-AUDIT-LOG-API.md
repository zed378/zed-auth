# 15 - Audit Log API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for querying structured system audit logs (`/v1/audit-logs`).

## Category Mandate

Enables security compliance monitoring and SIEM integration.

## Key Topics To Specify

- `GET /v1/audit-logs`: Query event trail with filters (actor, event_type, date range).
- `GET /v1/audit-logs/export`: Stream audit events in JSON lines format.

## Reference Architecture & Specification

Audit Event Example:
```json
{
  "event_id": "evt_789",
  "event_type": "user.login.failed",
  "actor_id": "usr_123",
  "timestamp": "2026-09-17T16:05:00Z"
}
```

## Acceptance Criteria

- [x] Audit query API contract specified.
- [x] Privacy sanitization guaranteed.

## Open Questions

None.

## Related Documents

- `docs/OBSERVABILITY/01-AUDIT-LOGGING-SPECIFICATION.md`

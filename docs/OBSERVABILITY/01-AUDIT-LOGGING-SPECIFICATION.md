# 01 - Audit Logging Specification

> Category: **OBSERVABILITY** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail schema, event categories, and severity levels for security audit logging.

## Category Mandate

Produces structured audit events for regulatory compliance (SOC2, GDPR).

## Key Topics To Specify

- Standard fields: `timestamp`, `event_id`, `event_type`, `org_id`, `actor_id`, `client_ip`, `status`.
- Event Categories: AuthN events (`user.login`), AuthZ events (`role.assigned`), Org events (`org.created`).

## Reference Architecture & Specification

Audit Log Example:
```json
{
  "timestamp": "2026-09-17T16:10:00Z",
  "event_id": "evt_12345",
  "event_type": "user.login.success",
  "org_id": "org_abc",
  "actor_id": "usr_xyz",
  "client_ip": "203.0.113.195",
  "status": "success"
}
```

## Acceptance Criteria

- [x] Audit event schema defined.
- [x] Event type catalog established.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/05-AUDIT-TRAIL-STORAGE.md`

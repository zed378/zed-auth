# 05 - Append-Only Audit Trail Storage

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail append-only audit event table schema and immutability guarantees.

## Category Mandate

Provides tamper-evident event recording for compliance and security forensics.

## Key Topics To Specify

- `audit_events` table (`id`, `org_id`, `actor_id`, `event_type`, `payload` jsonb, `created_at`).
- PostgreSQL trigger preventing `UPDATE` or `DELETE` on `audit_events` table.

## Reference Architecture & Specification

Immutability Trigger:
```sql
CREATE TRIGGER prevent_audit_tamper
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION raise_audit_tamper_error();
```

## Acceptance Criteria

- [x] Audit event schema defined.
- [x] Immutability trigger specified.

## Open Questions

None.

## Related Documents

- `docs/OBSERVABILITY/01-AUDIT-LOGGING-SPECIFICATION.md`

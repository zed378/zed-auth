# 01 - API Standards & Conventions

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify standard formatting rules for JSON bodies, field naming, timestamps, and UUID identifiers.

## Category Mandate

Ensures uniform request and response payloads across all endpoints.

## Key Topics To Specify

- Snake_case JSON key convention.
- ISO-8601 UTC timestamps (`2026-09-17T16:00:00Z`).
- UUIDv4 resource identifiers.
- Null vs absent field semantics.

## Reference Architecture & Specification

```json
{
  "id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "org_id": "11111111-2222-3333-4444-555555555555",
  "email": "user@example.com",
  "status": "active",
  "created_at": "2026-09-17T16:00:00Z"
}
```

## Acceptance Criteria

- [x] JSON key format enforced.
- [x] Timestamp format enforced.

## Open Questions

None.

## Related Documents

- `docs/API/00-API-OVERVIEW.md`

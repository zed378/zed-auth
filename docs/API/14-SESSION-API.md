# 14 - Session & Token API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for session inspection, active session revocation, and token management (`/v1/sessions`).

## Category Mandate

Provides administrators and users control over active login sessions.

## Key Topics To Specify

- `GET /v1/sessions`: List active sessions for user or organization.
- `DELETE /v1/sessions/{id}`: Revoke single session.
- `DELETE /v1/users/{id}/sessions`: Revoke all sessions for a user (force logout).

## Reference Architecture & Specification

Session Response Example:
```json
{
  "id": "sess_abc123",
  "user_id": "usr_456",
  "ip_address": "192.168.1.1",
  "user_agent": "Mozilla/5.0...",
  "created_at": "2026-09-17T15:00:00Z"
}
```

## Acceptance Criteria

- [x] Session management API specified.
- [x] Immediate token revocation enforced.

## Open Questions

None.

## Related Documents

- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`

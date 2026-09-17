# 01 - Event Types & Payload Schemas

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Catalog all published webhook event types and their JSON payload schemas.

## Category Mandate

Provides predictable event payload schemas to webhook consumers.

## Key Topics To Specify

- `user.created`: Triggered when a new user registers or is invited.
- `user.deactivated`: Triggered when a user account is disabled.
- `role.assigned`: Triggered when role grants are updated.
- `org.updated`: Triggered when organization settings change.

## Reference Architecture & Specification

Webhook Payload Format:
```json
{
  "id": "evt_wh_123",
  "event_type": "user.created",
  "timestamp": "2026-09-17T16:15:00Z",
  "data": {
    "user_id": "usr_456",
    "org_id": "org_789",
    "email": "newuser@acme.com"
  }
}
```

## Acceptance Criteria

- [x] Webhook event catalog enumerated.
- [x] Payload JSON schemas documented.

## Open Questions

None.

## Related Documents

- `docs/WEBHOOK/00-WEBHOOK-OVERVIEW.md`

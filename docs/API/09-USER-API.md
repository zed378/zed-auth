# 09 - User Management API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for managing organization users (`/v1/users`).

## Category Mandate

Governs CRUD operations, status management, password reset, and role assignment for users.

## Key Topics To Specify

- `GET /v1/users`: List organization users (paginated).
- `POST /v1/users`: Invite or create new user.
- `GET /v1/users/{id}`: Fetch user details.
- `PATCH /v1/users/{id}`: Update user profile / status.
- `DELETE /v1/users/{id}`: Deactivate or delete user.

## Reference Architecture & Specification

JSON Payload (`POST /v1/users`):
```json
{
  "email": "jane.doe@example.com",
  "username": "janedoe",
  "roles": ["editor"],
  "send_invite": true
}
```

## Acceptance Criteria

- [x] User CRUD API contract specified.
- [x] Multi-tenant org scoping enforced.

## Open Questions

None.

## Related Documents

- `docs/API/00-API-OVERVIEW.md`
- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`

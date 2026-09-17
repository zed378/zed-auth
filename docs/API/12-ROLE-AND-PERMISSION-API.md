# 12 - Role & Permission API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for managing RBAC Roles and Permissions (`/v1/roles`).

## Category Mandate

Allows tenant administrators to define custom roles and assign string permission keys.

## Key Topics To Specify

- `GET /v1/projects/{id}/roles`: List project roles.
- `POST /v1/projects/{id}/roles`: Create project role with `permission_keys` array.
- `PATCH /v1/roles/{id}`: Update role permissions.
- `DELETE /v1/roles/{id}`: Delete custom role (builtin roles protected).

## Reference Architecture & Specification

Role Definition Payload:
```json
{
  "key": "billing_admin",
  "display_name": "Billing Administrator",
  "permission_keys": ["billing:read", "billing:write", "invoices:export"]
}
```

## Acceptance Criteria

- [x] Role management endpoints specified.
- [x] Built-in role deletion protection enforced.

## Open Questions

None.

## Related Documents

- `docs/AUTHORIZATION/01-MULTI-TENANT-RBAC.md`

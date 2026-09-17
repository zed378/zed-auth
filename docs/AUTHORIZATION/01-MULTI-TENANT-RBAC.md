# 01 - Multi-Tenant RBAC Specification

> Category: **AUTHORIZATION** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail multi-tenant Role-Based Access Control, role keys, permission strings, and scoping.

## Category Mandate

Ensures isolated, granular permission management per project and organization.

## Key Topics To Specify

- Permission keys formatted as `resource:action` (e.g. `user:read`, `project:write`).
- Role definitions scoped per project (`key`, `permission_keys[]`).
- `user_grants` mapping (`user_id`, `project_id`, `role_keys[]`).
- Built-in system roles (`org_owner`, `org_admin`, `org_member`).

## Reference Architecture & Specification

RBAC Rule: A user holds permission `P` if any role assigned in their `user_grant` for target project `PR` contains `P`.

## Acceptance Criteria

- [x] Permission key naming convention specified.
- [x] Built-in vs custom role rules documented.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`

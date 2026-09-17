# 00 - Authorization Architecture

> Category: **AUTHORIZATION** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify top-level authorization engine design, unified decision tree, and server-side enforcement.

## Category Mandate

Combines RBAC, Project Grants, and ABAC into a single deterministic evaluation pipeline.

## Key Topics To Specify

- Layered decision tree (Tenant Scope -> RBAC User Grants -> Project Grants Delegation -> ABAC Policy).
- Server-side enforcement middleware (`/v1/authz/check`).
- Zero-trust evaluation semantics (default DENY).

## Reference Architecture & Specification

```
Request -> Extract Subject & Resource -> Check Tenant RLS -> Evaluate RBAC Roles -> Evaluate Project Grants -> Evaluate ABAC Policies -> DENY / ALLOW
```

## Acceptance Criteria

- [x] Decision tree evaluation order specified.
- [x] Default DENY policy enforced.

## Open Questions

None.

## Related Documents

- `docs/AUTHORIZATION/01-MULTI-TENANT-RBAC.md`
- `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`

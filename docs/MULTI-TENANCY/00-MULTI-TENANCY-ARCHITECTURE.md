# 00 - Multi-Tenancy Architecture Overview

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify multi-tenant entity hierarchy and tenant isolation boundaries.

## Category Mandate

Establishes a logical hierarchy ensuring zero data bleed between tenant organizations.

## Key Topics To Specify

- Entity Hierarchy: Deployment Instance -> Organization -> Project -> Application.
- Shared database architecture with row-level tenant partitioning (`org_id`).
- Isolated configuration per organization (MFA policies, password rules, branding).

## Reference Architecture & Specification

```
Instance
  └── Organization A (org_123)
       ├── Project 1
       │    └── Application (Client ID)
       └── User A
  └── Organization B (org_456)
       └── ...
```

## Acceptance Criteria

- [x] Tenant hierarchy model defined.
- [x] Data partitioning strategy established.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/00-DATABASE-ARCHITECTURE.md`
- `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`

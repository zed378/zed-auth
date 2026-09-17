# 02 - PostgreSQL Row-Level Security (RLS) Isolation

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail PostgreSQL Row-Level Security (RLS) policies and session variable enforcement.

## Category Mandate

Guarantees that database queries are constrained to the active tenant at the DB engine level.

## Key Topics To Specify

- DB Session Variable: `SET LOCAL app.current_org_id = '<uuid>';`.
- PostgreSQL RLS policy on all tenant-scoped tables:
  `CREATE POLICY tenant_isolation_policy ON users USING (org_id = current_setting('app.current_org_id')::uuid);`.
- Prevents developer code mistakes from leaking cross-tenant data.

## Reference Architecture & Specification

RLS Enforcement Example:
```sql
SET LOCAL app.current_org_id = '11111111-2222-3333-4444-555555555555';
SELECT * FROM users; -- Automatically returns ONLY rows where org_id matches
```

## Acceptance Criteria

- [x] RLS session variable mechanism specified.
- [x] SQL policy definitions provided for all tenant tables.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`

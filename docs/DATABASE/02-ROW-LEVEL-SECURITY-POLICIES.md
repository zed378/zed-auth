# 02 - Row-Level Security DDL Policies

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Provide explicit SQL statements enabling Row-Level Security across all multi-tenant tables.

## Category Mandate

Ensures database-level data isolation for every query executed under a tenant session variable.

## Key Topics To Specify

- `ALTER TABLE <tablename> ENABLE ROW LEVEL SECURITY;`.
- Policy definition checking `org_id = current_setting('app.current_org_id', true)::uuid`.

## Reference Architecture & Specification

DDL Script:
```sql
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_org_isolation ON users
    FOR ALL
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
```

## Acceptance Criteria

- [x] RLS DDL statements provided for all tenant tables.
- [x] Bypass protection for admin tasks specified.

## Open Questions

None.

## Related Documents

- `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`

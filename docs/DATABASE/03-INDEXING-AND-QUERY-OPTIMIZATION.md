# 03 - Indexing & Query Optimization Strategy

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail B-Tree indexes, composite indexes, and GIN indexes for JSONB columns.

## Category Mandate

Guarantees fast query execution (<5ms DB latency) on critical lookup paths.

## Key Topics To Specify

- Unique composite index on `users (org_id, email)`.
- Index on `user_grants (user_id, project_id)`.
- GIN index on `roles USING gin (permission_keys)`.
- GIN index on `organizations USING gin (settings)`.

## Reference Architecture & Specification

Index Definitions:
```sql
CREATE UNIQUE INDEX idx_users_org_email ON users(org_id, email);
CREATE INDEX idx_roles_permissions ON roles USING GIN(permission_keys);
```

## Acceptance Criteria

- [x] All primary and secondary indexes specified.
- [x] GIN indexes for array and JSONB columns documented.

## Open Questions

None.

## Related Documents

- `docs/PERFORMANCE/00-PERFORMANCE-TARGETS.md`

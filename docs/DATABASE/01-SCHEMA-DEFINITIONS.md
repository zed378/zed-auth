# 01 - Relational Schema Definitions

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail SQL schema definitions for all core entities: `organizations`, `users`, `projects`, `applications`, `roles`, `user_grants`, `project_grants`, `sessions`, `refresh_tokens`.

## Category Mandate

Provides the complete DDL reference for the database tables.

## Key Topics To Specify

- Primary keys UUIDv4 (`gen_random_uuid()`).
- `organizations` (`id`, `name`, `domain`, `settings` jsonb).
- `users` (`id`, `org_id`, `email`, `password_hash`, `status`).
- `roles` (`id`, `project_id`, `key`, `permission_keys` text[]).
- `project_grants` (`id`, `project_id`, `granting_org_id`, `granted_org_id`, `granted_role_keys` text[]).

## Reference Architecture & Specification

DDL Example (`users` table):
```sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    password_hash TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, email)
);
```

## Acceptance Criteria

- [x] SQL schema DDL for all core tables defined.
- [x] Foreign key constraints and cascade rules specified.

## Open Questions

None.

## Related Documents

- `docs/PLAN/04-DATA-MODEL.md`
- `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`

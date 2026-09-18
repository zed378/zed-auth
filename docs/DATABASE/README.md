# DATABASE

This category documents how the Auth Service actually uses PostgreSQL: the connection and role architecture, the full table-by-table schema, the row-level security policies and triggers that enforce tenant isolation and data-model invariants at the database level, index design, migration tooling and its expand/contract discipline, and the append-only audit trail. It describes the code as it exists in this repository; where the design plan (`docs/PLAN/`) and the code differ, each document says so explicitly and cites the reason rather than silently following one or the other. See `docs/MULTI-TENANCY/` for the tenant-hierarchy and tenant-resolution story that sits on top of this storage layer.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md) | Connection layer, role split, `WithTenant`/`WithInstanceScope`, pool settings | Partially implemented |
| [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md) | Every table: columns, constraints, indexes, foreign keys, owning Go package | Implemented |
| [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md) | Every RLS policy verbatim, triggers, `SECURITY DEFINER` functions, the `manager_roles` gap | Implemented |
| [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md) | Index catalogue by purpose, RLS-at-scale plan verification | Implemented |
| [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md) | `cmd/migrate`, expand/contract rule, the `migration-safety` CI gate, a real drift incident | Implemented |
| [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md) | The partitioned, append-only `events` table and its Go writer | Partially implemented |

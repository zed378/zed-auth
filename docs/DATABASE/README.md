# Category: DATABASE

PostgreSQL relational schema definitions, Row-Level Security policies, indexing strategies, migration workflows, and append-only audit trail storage.

## Category Mandate

The `DATABASE/` directory details the **relational persistence model**. It specifies PostgreSQL table schemas, foreign key constraints, RLS policies, migration tools (`golang-migrate`), and index performance optimization.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-DATABASE-ARCHITECTURE.md` | DB Architecture | PostgreSQL configuration, connection pooling (`pgxpool`). |
| `01-SCHEMA-DEFINITIONS.md` | Schema Definitions | Table definitions for users, orgs, projects, roles, grants, sessions. |
| `02-ROW-LEVEL-SECURITY-POLICIES.md` | RLS Policies | DDL scripts for PostgreSQL Row-Level Security. |
| `03-INDEXING-AND-QUERY-OPTIMIZATION.md` | Indexing Strategy | Composite indexes, B-Tree & GIN indexes for JSONB columns. |
| `04-MIGRATIONS-STRATEGY.md` | Migrations Strategy | Forward/Backward SQL migrations via `golang-migrate`. |
| `05-AUDIT-TRAIL-STORAGE.md` | Audit Trail Storage | Append-only event store table design & immutability. |

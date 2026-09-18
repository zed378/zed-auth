# 00 - Database Architecture Overview

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-07, P0-08, P0-12, P0-15, P0-20 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how the Auth Service actually talks to PostgreSQL: the driver, the connection pool, the two-database-role split, the mechanism that makes every query tenant-scoped, and where PostgreSQL's authority ends and Redis's begins.

## Scope

This document covers the storage engine and connection layer. It does not repeat:

- table-by-table DDL — see [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md);
- the row-level security policies, triggers and `SECURITY DEFINER` functions — see [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md);
- index design — see [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md);
- migration tooling and the expand/contract rule — see [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md);
- the `events` audit table specifically — see [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md);
- how the tenant hierarchy and tenant resolution work from the multi-tenancy side — see `docs/MULTI-TENANCY/00-MULTI-TENANCY-ARCHITECTURE.md`.

Design intent that predates the code lives in `docs/PLAN/04-DATA-MODEL.md`, `docs/PLAN/07-BACKEND-ARCHITECTURE.md` and `docs/PLAN/08-AUTHORIZATION.md` Part B; this document says how the plan was actually carried out, and names it explicitly where the two diverge.

## As Built

**Driver and connection handling.** The service connects through `database/sql` using the `pgx/v5/stdlib` driver, not `pgxpool` (`backend/internal/storage/postgres/postgres.go`). `postgres.DB` wraps `*sql.DB` and deliberately does not embed it — `SQL()` is the one escape hatch, reserved for the connection-pool-statistics collector, named so a reader notices it and told in its own comment that using it for queries bypasses tenant scoping entirely.

**No unscoped query path.** There is no exported method that runs a statement without first establishing a tenant. Every read or write goes through one of two functions:

- `DB.WithTenant(ctx, orgID, fn)` — opens a transaction, runs `SELECT set_config('app.current_org_id', $1, true)` with `orgID` as a bind parameter (never string-interpolated), then calls `fn` with a `*Tx` scoped to that transaction. `fn` returning an error rolls back; a panic rolls back and re-panics.
- `DB.WithInstanceScope(ctx, reason, fn)` — the same transaction shape, but sets the tenant to the empty string, which makes `current_org_id()` return `NULL` (see [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md)). It requires a non-empty `reason`, which is logged, and calls an optional hook (`SetInstanceScopeHook`) so cross-tenant access can be audited. `docs/PLAN/08` Part B requires this path to be "explicit, documented, and auditable — not the normal path with the filter omitted"; the differently-named function is what makes using it a visible choice in a diff.

`SET LOCAL` (via `set_config(..., true)`) rather than a plain `SET` is load-bearing: a plain `SET` persists on the pooled connection and would leak one request's tenant into whichever request the pool hands that connection to next — the exact cross-tenant leak the mechanism exists to prevent, and one that would appear only under concurrency, intermittently, in production.

**Two database roles.** `deploy/postgres/init/01-roles.sh` creates `auth_app` at first initialization of an empty data directory (deployed environments provision it separately, per `P0-20`):

| Role | Purpose | Attributes |
|---|---|---|
| `auth_owner` | Owns the schema. Runs migrations (`backend/cmd/migrate`). Never used by the running service. | superuser-equivalent within the database it owns |
| `auth_app` | The service's runtime connection (`AUTH_POSTGRES_DSN`). | `NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS NOINHERIT`, owns no tables |

Table privileges are granted per migration as tables are created, not blanket-granted, so a future table starts with no assumed access. The one standing default (`ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner`) grants `auth_app` `SELECT, INSERT, UPDATE, DELETE` on new tables and `USAGE, SELECT` on sequences — `events` then has `UPDATE, DELETE` explicitly revoked in its own migration (see [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md)).

The split exists because row-level security is silently bypassed by a table's owner and by any role with `BYPASSRLS`. Running the service as the owner would disable every cross-tenant isolation guarantee while every application-level test still passed — "the worst kind of security failure, because it is invisible" (`deploy/postgres/init/01-roles.sh`).

**Startup self-check.** `DB.AssertRoleIsNotPrivileged(ctx)` runs once at boot and queries `pg_roles`/`pg_tables` to confirm the connected role is not a superuser, does not have `BYPASSRLS`, and owns no tables in `public`. It exists because pointing `AUTH_POSTGRES_DSN` at `auth_owner` is "entirely plausible while debugging a permissions error", and every RLS policy would then silently stop applying with nothing failing and no test able to catch it (the integration suite would be inspecting the owner too).

**PostgreSQL is authoritative; Redis is a cache, never a second source of truth** (ADR-003, `MEMORY/DECISIONS.md`). Sessions are the concrete case: PostgreSQL holds the durable `sessions` row backing the sessions screen, admin session management and audit; Redis holds a short-TTL lookup copy for the silent-SSO hot path. A revocation writes `revoked_at` in PostgreSQL and invalidates the Redis entry in the same operation; a Redis miss falls through to PostgreSQL and is never read as "no session". OAuth authorization codes and the `/v1/authz/check` decision-input cache (ADR-022) live in Redis only and are out of this document's scope — they hold no durable identity state.

**Connection pool.** `backend/internal/config/config.go` reads three settings, applied to `*sql.DB` in `postgres.Open`:

| Setting | Env var | Default |
|---|---|---|
| Max open connections | `AUTH_POSTGRES_MAX_OPEN_CONNS` | 25 |
| Max idle connections | `AUTH_POSTGRES_MAX_IDLE_CONNS` | 5 |
| Connection max lifetime | `AUTH_POSTGRES_CONN_MAX_LIFETIME` | 30 minutes |

Config validation refuses a non-positive max-open value and refuses idle connections exceeding open connections (`backend/internal/config/config.go`).

**Extensions.** `pgcrypto` is enabled in the baseline migration (`backend/migrations/20260908000001_baseline.up.sql`) to provide `gen_random_uuid()` for every primary key.

**Single instance today.** `instances` is the deployment root; `organizations.instance_id` references it. `organization_create()` (`backend/migrations/20260910000016_organizations_soft_delete.up.sql`) deliberately takes no `instance_id` parameter and raises an exception if more than one `instances` row exists, so a future multi-instance deployment fails loudly rather than silently picking one. Nothing in the current codebase creates a second `instances` row.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Driver | `pgx/v5/stdlib` over `database/sql` | `backend/internal/storage/postgres/postgres.go` |
| Every query is tenant-scoped or explicitly instance-scoped | No exported unscoped query path except `DB.SQL()` (pool stats only) | `backend/internal/storage/postgres/postgres.go`; enforced mechanically by `TestOnlyNamedPlacesBypassTheTenantScope` (`backend/tests/security/tenancy_test.go`) |
| Tenant context propagation | `SET LOCAL` via `set_config('app.current_org_id', $1, true)`, transaction-scoped | `postgres.go` `withScope` |
| Runtime role | `auth_app`: not owner, `NOBYPASSRLS`, no superuser | `deploy/postgres/init/01-roles.sh`; asserted at boot by `AssertRoleIsNotPrivileged` |
| Migration role | `auth_owner`, used only by `backend/cmd/migrate` | `backend/cmd/migrate/main.go` |
| Max open / idle connections, lifetime | 25 / 5 / 30m, overridable by env | `backend/internal/config/config.go` |
| Sessions: authoritative store | PostgreSQL; Redis is a cache with the same-operation invalidation rule | ADR-003 (`MEMORY/DECISIONS.md`) |
| Single-instance assumption | `organization_create()` refuses to run against more than one `instances` row | `backend/migrations/20260910000016_organizations_soft_delete.up.sql` |

## Interfaces

Configuration keys read by `backend/internal/config/config.go`:

| Key | Used by | Purpose |
|---|---|---|
| `AUTH_POSTGRES_DSN` | the running service | `auth_app` connection string |
| `AUTH_POSTGRES_MAX_OPEN_CONNS` / `AUTH_POSTGRES_MAX_IDLE_CONNS` / `AUTH_POSTGRES_CONN_MAX_LIFETIME` | the running service | pool sizing |
| `AUTH_MIGRATE_DSN` | `backend/cmd/migrate` only | `auth_owner` connection string |
| `AUTH_POSTGRES_APP_PASSWORD` | `deploy/postgres/init/01-roles.sh` | sets `auth_app`'s password at first container init |

## Security Considerations

- **Owner-as-runtime-role misconfiguration** is the abuse case this document's mechanisms exist to close: `AssertRoleIsNotPrivileged` at boot, plus the CI job's mechanical check that every tenant-scoped table has row-level security enabled (`.github/workflows/ci.yml`, step "Every tenant-scoped table has row-level security policy"). See `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` for the broader threat model this sits inside.
- **String-built tenant context** would reopen the door SQL parameterization exists to close. `withScope` passes `orgID` as a bind parameter to `set_config` specifically because `SET LOCAL` itself is not parameterizable — noted in the source as the one place in the package a value would otherwise reach a statement as text.
- **A forgotten `WHERE org_id = …`** is the failure class row-level security is meant to remove rather than merely reduce; see [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md) for the policies and [`docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](../MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md) for the tenancy-guarantee framing and its verification.

## Verification

- `TestRuntimeRoleCannotBypassRLS` (`backend/tests/security/isolation_test.go`) — asserts the test connection is `auth_app`, not superuser, not `BYPASSRLS`.
- `TestOnlyNamedPlacesBypassTheTenantScope` (`backend/tests/security/tenancy_test.go`) — scans every non-test `.go` file for `.SQL()` and fails if a caller outside a named, justified allowlist bypasses tenant scoping.
- CI job step "Every tenant-scoped table has row-level security policy" (`.github/workflows/ci.yml`) queries `pg_class`/`information_schema.columns` for any table carrying `org_id` or `granting_org_id` with `relrowsecurity` false.
- CI step "Migrations roll back and re-apply cleanly" (`.github/workflows/ci.yml`) exercises `cmd/migrate down 6` then `up` against a freshly-provisioned `auth_owner`/`auth_app` pair, which is also where the role-creation script above is exercised end to end.
- `backend/migrations/20260908000006_events_audit_log.up.sql` through `20260908000009_partition_maintenance_privileges.up.sql` and `20260910000019_reassert_events_append_only.up.sql` are a recorded incident where a database migrated before a privilege fix kept the weaker privileges — found by a `P1-19` staging smoke test rather than by any test in the suite. See [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md) and `MEMORY/records/` for the P1-19/P1-20 records.

## Not Yet Built / Open Questions

- **High availability / failover.** The current deployment is a single VM with no database failover (`docs/PLAN/14` § Scalability assumes a primary/replica pair that does not exist yet). Tracked as an accepted deviation in `TASKS/BACKLOG.md` (the single-VM entry); closes with the planned Kubernetes migration.
- **Multi-instance deployments.** The schema supports more than one `instances` row; nothing creates one, and `organization_create()` actively refuses to. This is a real "not yet", not a design gap.
- **GDPR erasure / pseudonymization of audit rows.** `docs/PLAN/04` § Retention and Growth describes pseudonymizing `events.actor_user_id` rather than deleting on an erasure request; no code implements this (see [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md)).

## Related Documents

- `docs/PLAN/04-DATA-MODEL.md`, `docs/PLAN/07-BACKEND-ARCHITECTURE.md`, `docs/PLAN/08-AUTHORIZATION.md` Part B — design intent
- [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md), [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md), [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md), [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md), [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md)
- `docs/MULTI-TENANCY/00-MULTI-TENANCY-ARCHITECTURE.md`
- `MEMORY/DECISIONS.md` ADR-003 (sessions authority), ADR-022 (authz cache)

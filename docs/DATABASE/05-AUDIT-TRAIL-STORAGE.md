# 05 - Audit Trail Storage

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-12, P1-19, P1-20 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the `events` table's actual structure, its append-only enforcement mechanism (a revoked database privilege, not a trigger or application convention), its monthly partitioning and the scheduler that maintains it, and the Go writer that is the single entry point for every audit event.

## Scope

Source: `backend/migrations/20260908000006_events_audit_log.up.sql`, `20260908000008_audit_instance_events_and_partitions.up.sql`, `20260908000009_partition_maintenance_privileges.up.sql`, `20260910000019_reassert_events_append_only.up.sql`, and `backend/internal/audit/audit.go`. Table-level column/index/FK detail duplicates [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md) only where needed for context; the RLS policies and `SECURITY DEFINER` partition functions are catalogued in full in [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md). The migration-drift incident that motivated re-verifying append-only privileges is also covered from the migration-tooling angle in [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md).

## As Built

### Table shape

```sql
CREATE TABLE events (
    id             bigint GENERATED ALWAYS AS IDENTITY,
    org_id         uuid,                    -- nullable since 008; NULL = instance-level event
    actor_user_id  uuid,                     -- nullable; no actor for a failed login against a
                                              -- nonexistent account, or a system-initiated event
    event_type     text NOT NULL,            -- noun.verb.outcome, e.g. user.login.success
    payload        jsonb NOT NULL DEFAULT '{}'::jsonb,   -- redacted before storage
    ip             inet,
    request_id     text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
```

**No foreign key on `org_id` or `actor_user_id`, deliberately.** `docs/PLAN/04` § Retention and Growth resolves the tension between an immutable audit trail and a data-subject erasure request by *pseudonymizing* rather than deleting: personal data in `users` would be erased while `actor_user_id` is retained as an opaque identifier that no longer resolves to a person. A foreign key would force a cascade or block the erasure entirely, which defeats that design — "the audit trail must outlive the rows it refers to" (`20260908000006_events_audit_log.up.sql`).

### Partitioning

Partitioned `RANGE (created_at)`, one partition per calendar month, named `events_YYYY_MM`. The primary key is `(id, created_at)` rather than `id` alone because Postgres requires the partition key to be part of the primary key on a partitioned table.

Partition creation went through two iterations:

1. **`006`** created `ensure_events_partition(target date)` as an ordinary (non-`SECURITY DEFINER`) `plpgsql` function and seeded the current and next month.
2. **`008`** replaced it with a concurrency-safe version — the original had a race between the existence check and the `CREATE TABLE`, which two simultaneously-starting service instances could both lose — using `pg_advisory_xact_lock(hashtext('ensure_events_partition:' || part_name))` to serialize. It also added `ensure_events_partitions_ahead(months_ahead int DEFAULT 3)`, which creates the current partition through `months_ahead` out.
3. **`009`** made both functions `SECURITY DEFINER` with a pinned `search_path`, because `auth_app` has no `CREATE` privilege on schema `public` — see [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md) for the full reasoning. `009` also bounds `months_ahead` to `0..24`.

**Why this matters operationally**: a missing partition means every `INSERT INTO events` fails, and because every security-sensitive action writes an audit event, every such action fails with it — "at midnight on the 1st, with no deploy and no code change to point at" (`20260908000008_audit_instance_events_and_partitions.up.sql`). Running several months ahead, and re-running daily rather than monthly, means a missed scheduler tick or a multi-day outage still has runway.

### Append-only enforcement

Append-only is enforced by **revoking the `UPDATE`/`DELETE` table privilege from `auth_app`**, not by a trigger and not by application-layer convention:

```sql
GRANT SELECT, INSERT ON events TO auth_app;
REVOKE UPDATE, DELETE ON events FROM auth_app;
```

Each partition must inherit this explicitly — a Postgres partition does not automatically inherit privilege grants/revokes made on the parent after the partition already exists — so `ensure_events_partition()` re-issues the same `GRANT`/`REVOKE` pair on every partition it creates.

**Incident: this drifted on staging.** `20260910000019_reassert_events_append_only.up.sql` documents that the staging database's `events` table and its four live partitions carried `auth_app=arwd` (full read/write/delete), discovered by a `P1-19` smoke test rather than by any test in the suite. The cause was not a bug in either `006` or `009` individually — it was that a database already migrated through the earlier, weaker-privileged version of the logic never re-ran it, because `golang-migrate` does not re-apply an already-recorded version. `019` re-asserts the grant/revoke pair on the parent and every existing partition, idempotently, and then queries `has_table_privilege('auth_app', ..., 'UPDATE'|'DELETE')` a second time inside its own transaction and **raises an exception if anything is still writable** — so the migration cannot report success against a database it did not actually fix. See [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md) for the full incident narrative and the CI checks it motivated.

### Row-level security on `events`

Two policies, replacing the original tenant-only pair in `008` to admit instance-level rows:

```sql
CREATE POLICY events_read ON events
    FOR SELECT
    USING (org_id = current_org_id() OR (org_id IS NULL AND current_org_id() IS NULL));

CREATE POLICY events_insert ON events
    FOR INSERT
    WITH CHECK (org_id = current_org_id() OR (org_id IS NULL AND current_org_id() IS NULL));
```

No policy governs `UPDATE`/`DELETE` — the revoked privilege makes one unnecessary. The `008` migration's own comment is explicit about what the instance-level branch does **not** do: "it does not let the instance-scoped path read every organization's events. `current_org_id()` is `NULL` there, so `org_id = current_org_id()` is still false for every tenant row." Reading a cross-organization audit view is a separate, deliberately-designed capability (the instance audit log screen, `P1-20`), not a side effect of this policy.

### The Go writer — one entry point

`backend/internal/audit/audit.go` is the sole writer for `events`. `docs/PLAN/09-SECURITY.md` § Audit requires every identity- or permission-changing event to be recorded; the package comment states the rationale for a single entry point: "six subtly different shapes for 'a role was assigned' destroys [uniform reasoning] long before any single one of them is wrong."

Three write paths, each matched to whether a business transaction already exists:

| Method | When used | Mechanism |
|---|---|---|
| `Write(ctx, tx, event)` | The primary path — a business transaction is already open | Takes the caller's `*postgres.Tx` rather than opening its own. The event commits **inside** the same transaction as the action it records (ADR-012). If the insert fails, the action fails with it — a deliberate, accepted trade: "the service refuses to act rather than acting unrecorded." |
| `WriteStandalone(ctx, event)` | No business transaction exists (e.g. a failed login) | Opens its own `WithTenant` transaction — still tenant-scoped, since RLS reads `SET LOCAL` state. |
| `WriteInstanceLevel(ctx, reason, event)` | An event belongs to no single organization (signing-key rotation, cross-tenant database access) | Forces `org_id = ""`, runs under `WithInstanceScope(reason, ...)`. |

`Write` also refuses a mismatch between the event's declared `org_id` and the transaction's actual tenant, rather than letting row-level security silently reject the insert with a less informative error.

**Redaction happens in the writer, not at call sites.** `redactPayload` applies `observability.IsSensitiveKey` to every payload key before the `INSERT`, because "a call site that forgets produces a credential sitting in an append-only table that nothing can delete" (`backend/internal/audit/audit.go`). This is also why `docs/PLAN/13-OBSERVABILITY.md`'s and this project's `CLAUDE.md`'s rule ("never log tokens, passwords, or raw resource attributes") is enforced structurally rather than by convention for this table specifically — though call sites are still responsible for every other log surface.

**Partition maintenance is scheduled by the same package.** `Writer.Run(ctx, monthsAhead)` calls `EnsurePartitions` once at startup and then every 24 hours via `time.Ticker`, reporting the actual runway (partitions successfully confirmed, minus one) to an `Observer` metric so a silently-failing scheduler shows up as a shrinking gauge before it becomes an outage, not after.

**Forwarding to an external system** (`Writer.forward`) is a no-op today — the `Forwarder` interface exists as a seam for `P5-08` and nothing implements it yet. A forwarding failure is logged but never fails the write, the opposite trade from the database write itself, because the database copy is the authoritative record and an unreachable external system must not be able to stop logins from working.

### Reading the log

`backend/internal/auditlog/handler.go` is the Management API reader (`Writer.Query`), tenant-scoped like every other read.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Partitioning | Monthly, `RANGE (created_at)` | `006`, `008` |
| Partition runway maintained | current month + 3 months ahead, refreshed daily | `Writer.Run`, `009`'s `ensure_events_partitions_ahead` |
| Append-only mechanism | `REVOKE UPDATE, DELETE ... FROM auth_app`, on the parent and every partition | `006`, `009`, re-asserted `019` |
| Audit write commits with its action | `Write(ctx, tx, event)` — same transaction | ADR-012, `audit.go` |
| Payload redaction | applied in the writer (`redactPayload`), never trusted to call sites | `audit.go` |
| Retention | 24 months hot, then archived — **design intent, not yet implemented** (see below) | `docs/PLAN/04` § Retention and Growth; `TASKS/BACKLOG.md` OQ-09 |
| Erasure handling | pseudonymize `actor_user_id` rather than delete rows — **design intent, not yet implemented** | `docs/PLAN/04` § Retention and Growth |

## Interfaces

| Event-type examples | Package |
|---|---|
| `user.login.success`, `user.login.failed`, `user.mfa.success`, `user.mfa.failed`, `session.revoked`, `organization.mfa_required.enabled`, `delegated_role.assigned` | `backend/internal/audit/audit.go` (constants), written by the packages that raise each event |

Configuration: none beyond the `monthsAhead` parameter passed to `Writer.Run` at startup.

## Security Considerations

- **Tampering with the historical record** is the abuse case append-only-by-privilege closes; `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19 names this directly. A privilege, unlike a trigger or a code-review convention, "cannot be forgotten by a future code path" (`backend/tests/security/isolation_test.go`).
- **A credential recorded into an append-only table** is treated as effectively permanent, which is why redaction is centralized in the writer rather than delegated to call sites (see above).
- **A partition-creation failure taking down every audited action at once** is an availability risk with a security dimension — a service that cannot audit is a service `docs/PLAN/09` § Audit says should not act. The advisory-lock/idempotent design and the multi-month runway are the controls.

## Verification

- `TestAuditLogCannotBeAltered` (`backend/tests/security/isolation_test.go`) — attempts `UPDATE`, `DELETE` and `TRUNCATE` against `events` as `auth_app` and asserts each fails with a permission-denied error specifically (not merely "fails for any reason").
- `backend/internal/audit/audit_integration_test.go` exercises the writer's three paths and redaction.
- CI step "the audit log is append-only for the application role" (`.github/workflows/ci.yml`; mirrored in `scripts/check.sh`) queries `has_table_privilege` against the parent and every partition on every run.
- `20260910000019_reassert_events_append_only.up.sql`'s own closing `DO` block is itself a verification step, run as part of applying migrations.

## Not Yet Built / Open Questions

- **Retention (24-month hot window, archival) and pseudonymization on erasure** are described in `docs/PLAN/04` § Retention and Growth as the resolution to the GDPR-versus-immutable-log tension, but no code implements either — no scheduled archival job, no erasure endpoint, no pseudonymization routine exist in `backend/internal`. This is recorded as an open question in the plan itself (`TASKS/BACKLOG.md` OQ-09, "Confirm the audit log retention period") and should not be read as built.
- **Forwarding to an external system** (SIEM) is a defined interface (`audit.Forwarder`) with no implementation; tracked for `P5-08`.
- **The cross-organization instance audit log screen** (`P1-20`) is a read surface over instance-level events; this document describes the storage and RLS shape it depends on, not the screen itself.

## Related Documents

- [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md), [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md), [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md), [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md)
- `docs/PLAN/04-DATA-MODEL.md` § Retention and Growth, `docs/PLAN/09-SECURITY.md` § Audit, `docs/PLAN/13-OBSERVABILITY.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19
- `MEMORY/DECISIONS.md` ADR-012; `TASKS/BACKLOG.md` OQ-09

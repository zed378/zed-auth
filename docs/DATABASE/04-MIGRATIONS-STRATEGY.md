# 04 - Database Migrations Strategy

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-07, P0-15, P1-19, P1-20 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the actual migration tool, the expand/contract rule it is built around, and the two automated CI gates that enforce that rule mechanically rather than by review discipline — plus a real incident where the rule's *intent* held (no application ever broke) while a *related* guarantee (append-only privilege) silently drifted, because nothing was checking it yet.

## Scope

Source: `backend/cmd/migrate/main.go`, all 37 `backend/migrations/*.up.sql`/`*.down.sql` pairs, `.github/workflows/ci.yml` (jobs `migration-safety` and the `db`/integration job's migration steps), `scripts/check.sh`. Schema content produced by these migrations is in [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md); the RLS/trigger/`SECURITY DEFINER` objects several migrations redefine are catalogued in [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md). Design intent for zero-downtime deployment is `docs/PLAN/14-DEPLOYMENT.md` § Rollback Strategy; this document describes how that intent is actually enforced.

## As Built

### Tooling

`backend/cmd/migrate` is a separate binary from the service, built on `golang-migrate/migrate/v4` with an embedded filesystem source (`backend/migrations`, via `iofs`). It is deliberately not run implicitly on service startup: `docs/PLAN/14-DEPLOYMENT.md` requires migrations to run as a reviewable step before rollout, because a service that migrates as it boots would, during a rolling update, have several instances racing to alter the schema while older instances still serve traffic against the old one.

It connects as `AUTH_MIGRATE_DSN` — the schema owner (`auth_owner`), never the application role — because the application role is deliberately not the table owner, which is what lets row-level security apply to it (see [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md)).

Commands: `migrate new <name>` (scaffolds a timestamped `.up.sql`/`.down.sql` pair), `migrate up`, `migrate down [n]` (step-limited, default 1 — there is deliberately no "down to zero" shortcut, so an accidental full teardown of an environment's schema is hard to type by accident), `migrate status`, `migrate force <version>` (clears the dirty flag after a manually-repaired failed migration).

Migration files are named `<UTC-timestamp>_<description>.up.sql` / `.down.sql`, applied in filename order — 37 pairs as of this commit, from `20260908000001_baseline` to `20260917000037_delegated_user_grants`.

### The expand/contract rule

`docs/PLAN/14-DEPLOYMENT.md`'s expand/contract pattern requires a migration to leave the **previous** application version able to run against the **new** schema — so an application rollback during a bad deploy never requires a database rollback in turn. Concretely: add nullable columns or columns with defaults; widen constraints; never drop a column, rename a column/table, or narrow a type in the same release that stops using it.

This codebase's migrations follow the rule overwhelmingly by addition:

- **Additive columns with defaults or nullability** — `password_changed_at`, `email_verified_at`, `deleted_at`, `token_hash`, `scope`, `allowed_origins` are all nullable or default-backed additions that a previous application version simply never reads or writes (migrations `010`, `017`, `016`, `011`, `014`, `020`).
- **Widened `CHECK` constraints** — `20260913000033_report_not_me_token.up.sql` adds one allowed value (`report_not_me`) to `user_tokens.purpose_valid`; every row that satisfied the old constraint still satisfies the new one, so the `DROP`/`ADD` pair (run together in one transaction) cannot fail against existing data.
- **New tables, functions, triggers and indexes** — the large majority of migrations from `022` onward add a rule (a trigger, a `SECURITY DEFINER` function, a new table) that a previous application version never invokes, so its presence is invisible to that version.

### The one migration that deliberately did **not** follow expand/contract

`20260912000028_align_factors_with_plan.up.sql` renames `user_factors` to `user_mfa_factors` and `secret` to `secret_encrypted`, in the same migration that also drops `confirmed_at` in favor of a `status` column. Its own comment states the justification precisely:

> This migration is deliberately **NOT** expand/contract, and the justification is that the rule has nothing to protect here. ... The previous version of this table was created in the immediately preceding commit; **no deployed instance has ever read or written it**, because nothing calls the code that would. There is no running reader to break.

This is the rule's own escape valve used correctly: expand/contract protects a **running previous version**, and there was none for this table yet. The commit log and the CI gate below still required this migration to justify itself in writing — the exemption is explicit, not silent.

### The CI gate: `migration-safety`

`.github/workflows/ci.yml`, job `migration-safety`, runs on every pull request and enforces two rules mechanically:

**1. Destructive migrations require a written justification.** For every newly-added `backend/migrations/*.up.sql` file in the PR's diff, the job greps for `DROP COLUMN`, `DROP TABLE`, `RENAME COLUMN`, `RENAME TO`, or `ALTER COLUMN ... TYPE` (case-insensitive). If found, the file must also contain a line starting with `EXPAND/CONTRACT:` explaining why the previous application version still runs against the resulting schema — or the job fails with `::error file=...::Destructive change without justification`. This is the mechanism behind every `EXPAND/CONTRACT:` comment block quoted throughout this document and [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md) — the comment is not a courtesy, it is what makes the migration pass CI.

**2. Every `.up.sql` has a matching `.down.sql`.** The job fails with `::error file=...::No matching down migration` if one is missing. The job's own comment is explicit that this is a weaker guarantee than a *tested* rollback: "An untested down migration is not a rollback plan; a missing one is not even a claim."

**3. The down/up round trip is actually exercised**, in the separate integration job (`.github/workflows/ci.yml`, step "Migrations roll back and re-apply cleanly"): `go run ./cmd/migrate down 6` followed by `go run ./cmd/migrate up`, against a freshly-provisioned database, on every CI run — not only on PRs touching migrations.

### `NOT VALID` is not used — and why

Postgres allows adding a `CHECK` or foreign-key constraint as `NOT VALID` (skipping validation against existing rows) and validating it later without holding a long lock. None of these 37 migrations use it. `20260912000022_role_rules.up.sql`, which adds several `CHECK` constraints to `roles`, states the reasoning directly: at the time those constraints were added, nothing had ever written a role (`P2-02`'s write API had not shipped yet), so every constraint could be added already-validated. The migration's own comment: "There is no data to be incompatible with, and a constraint carried as `NOT VALID` is one nobody remembers to validate later." This is a property of *when* these particular constraints were introduced relative to feature rollout, not a blanket policy against `NOT VALID` — a future constraint added against a populated table would need it.

### `SECURITY DEFINER` as an alternative to schema privilege expansion

Several migrations solve "the application needs to do X, which requires a privilege it should not hold generally" with a narrowly-scoped `SECURITY DEFINER` function rather than a `GRANT`. `20260908000009_partition_maintenance_privileges.up.sql` states the tradeoff explicitly: granting `CREATE ON SCHEMA public` to `auth_app` would let it create arbitrary tables — "a permanent privilege expansion to solve a narrow, scheduled maintenance need" — versus a function whose only caller-supplied value is a date and whose only possible statement is `CREATE TABLE ... PARTITION OF events`. The full catalogue of these functions and the bound each respects is in [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md).

### Incident: a corrected migration does not fix an already-migrated database

`20260910000019_reassert_events_append_only.up.sql` exists because of a gap in `golang-migrate`'s model rather than in any single migration's SQL: **a migration whose effect is corrected by a later migration leaves a database that already applied the earlier version in the weaker state**, because `golang-migrate` records a version as applied and never re-runs it.

Concretely: `20260908000006_events_audit_log.up.sql` granted `auth_app` broad privileges on `events` via `ALTER DEFAULT PRIVILEGES`, then attempted to revoke `UPDATE`/`DELETE` in the same file; `20260908000009` corrected the partition-creation function to reassert the revoke on every new partition. Both files are individually correct. But the staging database had been migrated through `006`'s window before the revoke logic in that same file executed cleanly relative to `ALTER DEFAULT PRIVILEGES`'s ordering, and `events` plus its four live partitions ended up with `auth_app=arwd` (read, insert, update, delete) — the audit log was not append-only there at all, and nothing in the test suite noticed, because the integration tests provision a fresh database where the ordering issue does not reproduce.

It was found by a `P1-19` staging smoke test that queried `has_table_privilege` directly, not by any migration or unit test. `20260910000019` fixes it by re-granting `SELECT, INSERT` and re-revoking `UPDATE, DELETE` on `events` and every existing partition, idempotently, and then — the part that makes it a gate rather than a hope — **queries `has_table_privilege` again at the end of its own transaction and raises an exception if any relation is still writable**, so the migration cannot silently report success against a database it did not actually fix.

This incident is why the CI job now includes the step "Every up migration has a matching down" and why the integration job separately runs a live `has_table_privilege` check against `events` and its partitions (`.github/workflows/ci.yml`, step "the audit log is append-only for the application role"; also present in `scripts/check.sh`'s local equivalent) — a mechanical, repeated check rather than a one-time fix. See [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md) for the append-only mechanism this protects.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Migration file naming | `<UTC timestamp>_<description>.{up,down}.sql`, applied in order | `backend/migrations/` |
| Migration connects as | `auth_owner` via `AUTH_MIGRATE_DSN`, never the application role | `backend/cmd/migrate/main.go` |
| `down` has no "to zero" shortcut | step count required, positive integer only | `backend/cmd/migrate/main.go` |
| Destructive migration requires `EXPAND/CONTRACT:` comment | `DROP COLUMN\|TABLE`, `RENAME COLUMN\|TO`, `ALTER COLUMN ... TYPE` | `.github/workflows/ci.yml` job `migration-safety` |
| Every `.up.sql` has a `.down.sql` | mechanically checked | `.github/workflows/ci.yml` job `migration-safety` |
| Down/up round trip exercised in CI | `down 6` then `up`, every run | `.github/workflows/ci.yml` |
| `events` append-only privilege re-verified live, not assumed | `has_table_privilege` query against parent + every partition | `.github/workflows/ci.yml`; `scripts/check.sh` |

## Interfaces

| Command | Effect |
|---|---|
| `go run ./cmd/migrate new <name>` | scaffolds a new timestamped migration pair (developer-run only) |
| `go run ./cmd/migrate up` | applies every pending migration |
| `go run ./cmd/migrate down [n]` | rolls back `n` migrations (default 1) |
| `go run ./cmd/migrate status` | reports the current applied version |
| `go run ./cmd/migrate force <version>` | clears the dirty flag after manual repair — destructive if used carelessly |

## Security Considerations

- A migration that silently leaves a database in a weaker state than the repository describes (the `019` incident above) is itself a security-relevant defect class — "the repository described one thing and the deployment was another, silently, for the property that says an attacker cannot edit the record of what they did" (`20260910000019_reassert_events_append_only.up.sql`). The self-verifying `RAISE EXCEPTION` pattern in that migration, and the CI privilege check it motivated, are the controls.
- See `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19 (Logging / Audit Integrity) for the broader threat this sits inside.

## Verification

- `.github/workflows/ci.yml` job `migration-safety` — destructive-migration justification and down-migration presence, on every PR.
- `.github/workflows/ci.yml` integration job — "Apply migrations", "Migrations roll back and re-apply cleanly" (`down 6` / `up`), "Every tenant-scoped table has row-level security policy", "the audit log is append-only for the application role".
- `scripts/check.sh` § Migrations — the same checks, runnable locally before pushing (per its own header comment, "the same gates `.github/workflows/ci.yml` runs, available before a push rather than after one").

## Not Yet Built / Open Questions

None identified — the migration tooling, expand/contract discipline and CI enforcement described above are fully built and exercised on every change.

## Related Documents

- [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md), [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md), [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md), [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md)
- `docs/PLAN/14-DEPLOYMENT.md` § Rollback Strategy (design intent)
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19

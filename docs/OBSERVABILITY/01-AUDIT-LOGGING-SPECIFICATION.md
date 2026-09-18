# 01 - Audit Logging Specification

> Category: **Observability** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-12, P1-14, P1-19, P1-20, P2-01, P2-03, P3-03..P3-10, P4-01, P4-02, P4-03, P5-08 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Specify the audit log as it is actually built: the `events` table, the one writer that populates it, the one read endpoint that serves it, and the guarantee that a security-sensitive action which is not audited is treated as a defect rather than an oversight.

## Scope

This is a **product feature**, not an operations log — it is per-organization (with an instance-level slice for actions that belong to no single tenant), it is read through the Management API by `ORG_ADMIN`s, and it is designed to be relied on during an incident investigation. Operational logging (stdout, `slog`) is covered in `02-PRIVACY-PRESERVING-LOGGING.md`. Metrics that observe the audit writer itself (`audit_events_written_total`, `audit_partition_runway_months`) are covered in `03-METRICS-AND-SLO-TRACKING.md`.

## As Built

**The table.** `events` (`backend/migrations/20260908000006_events_audit_log.up.sql`) is partitioned by month on `created_at`, with primary key `(id, created_at)`. Columns: `org_id` (nullable since `20260908000008`, for instance-level events), `actor_user_id` (nullable — a failed login against a nonexistent address has no authenticated actor), `event_type text`, `payload jsonb`, `ip inet`, `request_id text`, `created_at timestamptz`. No foreign keys on `org_id` or `actor_user_id`, deliberately: `docs/PLAN/04-DATA-MODEL.md` § Retention and Growth resolves the GDPR-versus-immutable-log tension by pseudonymizing rather than deleting, and a foreign key would force a cascade or block erasure.

**Append-only, at the database level.** The `auth_app` application role holds `SELECT, INSERT` on `events` and every partition, and explicitly not `UPDATE`/`DELETE` (`20260908000006_events_audit_log.up.sql`). A partition created later inherits the same grants via the `ensure_events_partition` function. Migration `20260910000019_reassert_events_append_only.up.sql` exists because staging's partitions were once found writable (`auth_app=arwd`) due to a `golang-migrate` property: a migration whose effect is corrected later leaves an already-migrated database in the weaker state. That migration re-asserts the privilege on the parent and every existing partition and **fails the migration** if any relation is still writable afterward.

**One writer.** `backend/internal/audit/audit.go`'s `Writer.Write` is the only path that inserts into `events`. It takes an existing `*postgres.Tx` and writes inside it — the event commits with the action that caused it (`ADR-012`). If the audit write fails, the action fails; this is an accepted, intentional cost. `WriteStandalone` opens its own tenant-scoped transaction for events with no surrounding business transaction (a failed login). `WriteInstanceLevel` writes with a `NULL org_id` through `db.WithInstanceScope`, for actions that belong to no single organization (signing key rotation, use of the cross-tenant database path).

**Redaction before storage.** `redactPayload` in `audit.go` applies the same rule as the logger (`observability.IsSensitiveKey`) to every event payload, recursively through nested maps, before the JSON is written. This runs inside the writer rather than at call sites, because a call site that forgets would put a credential in an append-only table nothing can delete.

**Event types.** `audit.go` defines the full catalog as typed constants, `noun.verb.outcome` (e.g. `user.login.failed`, `role.assigned`, `project_grant.revoked`, `delegated_role.assigned`, `manager_role.assigned`). Constants rather than free strings, so the console's filters and any future alert rule cannot silently diverge from what the writer actually emits.

**The audit guard.** `backend/internal/management/audit.go` is the "unchanged" mechanism referenced in this document's title:

- `management.Audit(ctx, recorder, tx, event)` fills in actor, organization, IP and request ID from context, writes the event, then marks a per-request trail as written — only after a successful write, never before.
- `management.Unchanged(ctx)` is a deliberate declaration that a successful mutating request changed nothing and so has nothing to audit (a rename to the same name, ending an already-ended session). It must be called on the branch that established nothing changed, never defensively at the top of a handler.
- `AuditGuard.Wrap` installs the trail on every mutating request (`POST`/`PUT`/`PATCH`/`DELETE`), and after the handler runs, checks: did a `2xx` response leave the trail unmarked? If so, it logs `ERROR` and increments `auth_management_unaudited_mutations_total`, labelled by route pattern (never a concrete path, to avoid unbounded cardinality). A refused request (`4xx`) is never reported — demanding an event for every 403 would let a caller fill the log at will.

**Partition maintenance.** `Writer.Run` (`audit.go`) calls `EnsurePartitions` at startup and every 24 hours, maintaining 3 months of runway (`partitionMonthsAhead` in `backend/cmd/authservice/main.go`). The underlying SQL function `ensure_events_partitions_ahead` (`20260908000008_audit_instance_events_and_partitions.up.sql`) is concurrency-safe via an advisory lock, because several service instances call it simultaneously. Its absence would be a scheduled outage: when the last partition's range ends, every `INSERT` into `events` fails, and because every security-sensitive action writes an audit event, every such action fails with it — at a month boundary, with no deploy to correlate against.

**The read API.** `GET /v1/organizations/{org_id}/events` (`operationId: listEvents`, `openapi/openapi.yaml`), served by `backend/internal/auditlog/handler.go`. Requires `ORG_ADMIN` over the organization. Newest-first (`ORDER BY created_at DESC, id DESC`), keyset-paginated on `(created_at, id)` rather than `OFFSET` — an `OFFSET` on a table that grows without bound scans and discards, and skips or repeats rows as new events arrive mid-pagination. Filterable by up to `MaxEventTypes = 20` event types, one actor, and a `from`/`to` time range (an empty range is refused as a caller error, per `auditlog/handler.go`). Row-level security supplies the tenant filter; the query deliberately adds no `org_id` predicate itself, so every filter combination is tenant-safe by construction rather than by someone remembering to add the clause.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Application role privilege on `events` | `SELECT, INSERT` only | `backend/migrations/20260908000006*.up.sql`, reasserted by `20260910000019*.up.sql` |
| Partitioning | Monthly range partitions on `created_at` | `backend/migrations/20260908000006*.up.sql` |
| Partition runway maintained | 3 months ahead, checked daily | `backend/cmd/authservice/main.go` (`partitionMonthsAhead`), `backend/internal/audit/audit.go` `Run` |
| Audit write transaction scope | Inside the caller's own transaction; failure fails the action | `backend/internal/audit/audit.go` `Write` (`ADR-012`) |
| Payload redaction | Applied recursively before storage | `backend/internal/audit/audit.go` `redactPayload` |
| List page size / event-type filter bound | `DefaultLimit=50`, `MaxLimit=500` (writer); `MaxEventTypes=20` (read API) | `backend/internal/audit/audit.go`; `backend/internal/auditlog/handler.go` |
| Documented retention | 24 months hot, then archived (not yet enforced by code) | `docs/PLAN/04-DATA-MODEL.md` § Retention and Growth |
| Unaudited-mutation alert threshold | Any non-zero value is a defect | `backend/internal/observability/metrics.go` `UnauditedMutations` |

## Interfaces

- `GET /v1/organizations/{org_id}/events` — `operationId: listEvents`; requires `ORG_ADMIN`; query params `page_size`, `page_token`, `event_type` (repeatable), `actor_id`, `from`, `to`; response `EventList` (`openapi/openapi.yaml`).
- Audit event type catalog (`backend/internal/audit/audit.go`): authentication (`user.login.success/failed`, `user.logout`, `user.lockout`, `session.created/revoked`), MFA (`user.mfa.*`), password lifecycle (`user.password.*`), authorization (`role.*`, `project_grant.*`, `delegated_role.*`, `manager_role.assigned/revoked`), organization/instance (`organization.*`, `policy.*`, `project.*`, `application.*`, `signing_key.rotated`, `instance.scoped_access`).
- Metric `auth_management_unaudited_mutations_total{route}` — the guard's own signal; see `03-METRICS-AND-SLO-TRACKING.md`.

## Security Considerations

- **Log/audit forgery via the application**: closed by the database-level `REVOKE UPDATE, DELETE`, not by convention — `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19 (Logging / Audit Integrity).
- **A missing audit record for a real change** is the class of gap `docs/PLAN/09-SECURITY.md` § Audit warns is invisible by nature. The `AuditGuard` turns it into a loud `ERROR` line and a metric on the first request that trips it, rather than something found during an incident.
- **Credential storage inside the audit log itself**: closed by `redactPayload` applying the logger's redaction rules before every write, so a call site that forgets to redact cannot put a credential in a table nothing can later delete.
- **Cross-tenant reads of another organization's audit log**: `auditlog.Handler.inScope` refuses to run without a resolved tenant, and the query relies on row-level security rather than an application-level filter for every combination of caller-supplied parameters.
- **Manager-role assignment has no notification channel.** `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §3 expects manager-role writes to be rare and alerted on. They are audited (`manager_role.assigned`/`.revoked`) and distinguishable, but nothing currently notifies an organization's owners when one happens — `MEMORY/records/2026-09-17-P4-03-project-grant-owner.md` records this gap explicitly, owned by `TASKS/PHASE-5-HARDENING.md` § P5-08.

## Verification

- `backend/internal/audit/audit_integration_test.go` — `TestWriteAndQuery`, `TestPayloadIsRedactedBeforeStorage`, `TestEventRollsBackWithItsTransaction`, `TestEventCannotClaimAnotherTenant`, `TestInstanceLevelEvent`, `TestQueryFiltersAndPagination`, `TestEnsurePartitionsCreatesRunway`, `TestEnsurePartitionsIsConcurrencySafe`, `TestNewPartitionsAreAppendOnly`, `TestForwardingFailureDoesNotFailTheAction`.
- `backend/internal/auditlog/auditlog_integration_test.go` — `TestEveryWriteMethodIsRefused`, `TestTheApplicationRoleCannotAlterTheLog`, `TestNoFilterCombinationReachesAnotherTenant`, `TestFilteringByEventType/Actor/TimeRange`, `TestAnEmptyTimeRangeIsRefused`, `TestTheLogIsOrderedNewestFirst`, `TestANonAdminIsRefused`.
- `backend/internal/auditlog/tenancy_integration_test.go` — `TestNoFilterCombinationCrossesTenants`.
- `backend/internal/management/audit_test.go` — `TestASuccessfulMutationThatAuditsNothingIsReported`, `TestAnAuditedMutationIsNotReported`, `TestADeclaredNoOpIsNotReported`, `TestAFailedAuditWriteDoesNotSatisfyTheGuard`, `TestARefusedMutationIsNotReported`, `TestEveryMutatingMethodIsGuarded`, `TestNoRequestContentReachesAnEventByDefault`, `TestTheMetricLabelCarriesNoIdentifiers`.
- `backend/tests/security/isolation_test.go` — `TestAuditLogCannotBeAltered`.
- Staging: `MEMORY/records/2026-09-11-P1-28-threat-model-review.md` §19 re-confirms live that `UPDATE` on `events` as `auth_app` is refused; `MEMORY/records/2026-09-17-P4-03-project-grant-owner.md` verifies `manager_role.assigned`/`.revoked` on staging with a throwaway grant.

## Not Yet Built / Open Questions

- **External SIEM forwarding.** `audit.Forwarder` is a real interface with a working call site (`Writer.forward`), but `backend/cmd/authservice/main.go` passes `nil` for it — there is no forwarder implementation. Owned by `TASKS/PHASE-5-HARDENING.md` § P5-08.
- **Archival to cold storage after 24 months.** No code moves or drops old partitions; only creation of future partitions is automated. `docs/PLAN/04-DATA-MODEL.md` § Retention and Growth names this as intent, `OQ-09`.
- **Pseudonymization on erasure.** No user-erasure code path exists in `backend/internal/user`; only deactivation (`user.deactivated`). The table comment's claim that "erasure requests pseudonymize rather than delete" describes an intended design, not a built one.
- **Notification on manager-role assignment**, see Security Considerations above.

## Related Documents

- `docs/PLAN/04-DATA-MODEL.md` § events, § Retention and Growth
- `docs/PLAN/09-SECURITY.md` § Audit
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §3, §19
- [`02-PRIVACY-PRESERVING-LOGGING.md`](./02-PRIVACY-PRESERVING-LOGGING.md), [`03-METRICS-AND-SLO-TRACKING.md`](./03-METRICS-AND-SLO-TRACKING.md)
- `MEMORY/DECISIONS.md` ADR-004, ADR-012

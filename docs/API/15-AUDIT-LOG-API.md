# 15 - Audit Log API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-20, P0-12 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the one read-only route onto the append-only audit log: `GET /v1/organizations/{org_id}/events`.

## Scope

`/v1/organizations/{org_id}/events`. What each operation across the API writes to the log is listed in that operation's own document (`08`–`14`, `16`) under "Audit events"; the full vocabulary of event types is `backend/internal/audit/audit.go`, not restated here. The underlying storage design (partitioning, retention) is `docs/DATABASE/05-AUDIT-TRAIL-STORAGE.md`.

## As Built

**This resource has exactly one operation, and that is a property, not an omission.** There is no create, update, or delete on `events` through this API — `events` is append-only at the database level; the application's own database role holds no `UPDATE` or `DELETE` privilege on the table (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19). An API that offered any write path here would turn a privilege-level guarantee into a matter of trust in application code instead.

**Ordered newest first — the one list in this entire API that is.** Every other list endpoint in `09`–`14`/`16` is oldest-first (see `06-PAGINATION.md`); an audit log is read from the end, because the question asked of it is almost always "what just happened," and paging forward from the oldest row of a table that grows without bound would answer that slowly and last.

**Payloads are stored already redacted, not redacted at read time.** `docs/PLAN/13-OBSERVABILITY.md` / `P0-12`'s redaction rules run before a row is ever written (`backend/internal/audit/audit.go` `redactPayload`), applying the same sensitive-key rules the structured logger uses. Consequently nothing here — ever — carries a token, a password, or the raw resource attributes sent to `/v1/authz/check` (`CLAUDE.md`'s non-negotiable constraint on exactly this point).

**`actor_user_id` is legitimately and commonly null.** A failed login against an address that has no account has no authenticated actor; neither does a scheduled maintenance job. A consumer that assumes an actor is always present will fail precisely on the entries an incident review needs most.

**Filtering is exact-match, not full-text.** `event_type` may be repeated to match any of several types (`style: form, explode: true`); `actor_id` matches an exact user id; `from`/`to` bound `occurred_at` (inclusive lower, exclusive upper). There is no substring search over payloads.

**`id` is a string, not an integer, because the underlying primary key is `(id, created_at)` on a partitioned table.** A bare integer would not be unique across partitions on its own; a client treating it as one would eventually collapse two distinct events into one.

## Rules and Defaults

| Setting | Value | Enforced in |
|---|---|---|
| Sort order | Newest first (`created_at DESC, id DESC`) | `backend/internal/audit/audit.go` `Query` |
| Default page size (this endpoint's underlying query) | 50 (`audit.DefaultLimit`); the `/v1` HTTP layer still applies its own `page_size` default of 20 and max of 100 (`06-PAGINATION.md`) | `backend/internal/audit/audit.go` |
| Absolute query cap | 500 rows per underlying query (`audit.MaxLimit`) | `backend/internal/audit/audit.go` |
| `event_type` filter | Exact match, repeatable, max 100 chars each | `openapi/openapi.yaml` `listEvents` |
| `actor_id` filter | Exact match, `ResourceId` | `openapi/openapi.yaml` |
| `from`/`to` filters | Inclusive lower / exclusive upper bound on `occurred_at` | `openapi/openapi.yaml` |
| Payload redaction | Applied before storage, not at read | `backend/internal/audit/audit.go` `redactPayload` |
| Write operations on this resource | None exist | — (enforced by database privilege, `docs/SECURITY/02` §19) |
| Required role | `ORG_ADMIN` over the organization | `backend/internal/management/policy.go` |

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET /v1/organizations/{org_id}/events` | `listEvents` | `ORG_ADMIN` |

Query parameters: `page_size`, `page_token`, `event_type` (repeatable), `actor_id`, `from`, `to`. Full schema: `public-site/docs/api-reference/list-events.api.mdx`.

Response shape (`EventList` → `Event`): `id`, `event_type`, `actor_user_id` (nullable), `occurred_at`, `ip` (nullable), `request_id` (nullable), `payload` (object, shape varies by `event_type`, deliberately unconstrained by the schema — the console renders it as data rather than parsing a per-type shape).

## Security Considerations

- `events` is append-only at the database privilege level, not merely by API convention — see `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19 and `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`.
- Payload redaction happens at write time so that a call site which forgets to redact cannot put a credential into a table nothing can later delete — the append-only guarantee makes a redaction mistake permanent, which is why it is centralized in `audit.Write` rather than left to each caller.
- Cross-tenant visibility follows `02-AUTHENTICATION-AND-AUTHORIZATION.md`'s general rule: only `ORG_ADMIN` over the organization in the path, or `INSTANCE_OWNER`, may read a given tenant's log — there is no "read every organization's events in one call" endpoint (see `16-ADMIN-API.md`).

## Verification

- `backend/internal/management/audit_test.go` (the `AuditGuard`'s own unit behaviour) and `TestAMutatingCallWritesAnAuditEvent`, `TestAMutatingCallThatAuditsNothingIsReported`, `TestAReplayIsNotReportedAsUnaudited` — `backend/internal/management/chain_integration_test.go`.
- `backend/internal/audit/audit_integration_test.go` (redaction, partitioning, query pagination against real Postgres).

## Not Yet Built / Open Questions

- No cross-tenant ("instance-wide") audit query endpoint exists. An `INSTANCE_OWNER` reads any single organization's log by calling this same route for that organization (their `INSTANCE_OWNER` grant satisfies the `ORG_ADMIN` requirement — `02-AUTHENTICATION-AND-AUTHORIZATION.md`); there is no endpoint that returns events across every organization in one page. See `16-ADMIN-API.md`.
- Forwarding to an external SIEM (`audit.Forwarder`) is a wired seam with no real implementation yet — `docs/PLAN/09-SECURITY.md` § Audit says the log should "ideally" be forwarded; `P5-08` is the owning future task.

## Related Documents

- `docs/DATABASE/05-AUDIT-TRAIL-STORAGE.md`
- `docs/PLAN/13-OBSERVABILITY.md`, `docs/PLAN/09-SECURITY.md` § Audit
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §19
- `docs/API/16-ADMIN-API.md`

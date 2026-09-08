# Changelog

Chronological summary of changes at a coarser grain than the individual records in [`records/`](./records/). If you want to know what happened and roughly when, read this. If you want to know why it was done that way, follow the link to the record.

This is the **internal** changelog. The public-facing `/changelog` on the marketing site (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`) is a separate, user-facing artifact that never describes an unshipped capability.

Format follows Keep a Changelog conventions, grouped by release once releases exist. Before the first release, entries are grouped by date.

---

## Unreleased

### 2026-09-08

**Added**
- `TASKS/` — the execution layer: task conventions and global definition of done, seven phase files covering 124 tasks, a progress board, and a backlog. Every task cites the plan documents it implements and names its abuse cases. ([P0-21](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md), [ADR-001](./DECISIONS.md#adr-001--establish-tasks-and-memory-as-the-execution-layer))
- `MEMORY/` — the record layer: change records, decision log, this changelog, and templates. A task is not done until its record exists. ([P0-21](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md))

**Found**
- Two contradictions between existing plan documents, recorded as `PG-08` and `PG-09` in `TASKS/BACKLOG.md`. Neither plan document has been edited — both are flagged for the deliberate plan-change process (`AGENTS.md` rule 9).
- Eleven plan gaps: capabilities the plan requires functionally but does not model, most of them missing tables in `PLAN/04-DATA-MODEL.md` (signing keys, MFA factors, invite and reset tokens, federated identity links, webhook endpoints, role permission keys). Each is recorded in `TASKS/BACKLOG.md` against the task it blocks.
- Eight open questions requiring a decision from the project owner — deployment target, email provider, RPO/RTO values, capacity assumptions, and others. Recorded in `TASKS/BACKLOG.md`.

**Changed** — plan amendments, made deliberately at the user's instruction under `AGENTS.md` rule 9
- `PLAN/04-DATA-MODEL.md` (158 → 295 lines): added `signing_keys`, `user_mfa_factors`, `user_recovery_codes`, `user_tokens`, `user_identities`, `webhook_endpoints`, `webhook_deliveries`; extended `roles` (permission keys), `sessions` (org scoping, revocation, and the Redis-versus-PostgreSQL authority note), `refresh_tokens` (rotation families); added retention/partitioning policy and a "what is deliberately not stored" table. ([record](./records/2026-09-08-plan-gap-remediation.md), [ADR-002](./DECISIONS.md), [ADR-003](./DECISIONS.md), [ADR-004](./DECISIONS.md))
- `PLAN/05-API-CONTRACT.md`: SAML 2.0 corrected from Phase 2 to Phase 4, matching `PLAN/03`, `PLAN/16`, and `PLAN/17`.
- `PLAN/18-RISK-REGISTER.md`: R-04's mitigation corrected to state that Project Grant subset validation happens **on every request**, not only at grant creation — matching `CLAUDE.md`, `AGENTS.md` rule 3, `PLAN/08` Part C, and `PLAN/19`. The weaker wording described a system where a narrowed or revoked grant would keep working.
- `PLAN/07-BACKEND-ARCHITECTURE.md`: Redis clarified as a cache in front of PostgreSQL for sessions, not a second source of truth.
- `UI-UX/08-PAGE-SPECIFICATIONS.md`: four screens added that the IA included but the "full inventory" omitted — Organization Overview, Organization Settings, Instance-wide policies, Instance audit log.

**Added** — frontend track
- `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`: 53 tasks across seven tracks covering every component in `UI-UX/07`, all 21 console screens, the hosted authentication screens, the public site, and the frontend quality suite. Foundation tasks are phase-independent; every page task carries a binding gate naming the backend task that unblocks it, which enforces `PLAN/16`'s lockstep rule per screen rather than per phase. ([record](./records/2026-09-08-phase-f-frontend-track.md), [ADR-005](./DECISIONS.md))
- Project total: 124 → 177 tasks.

**Added** — implementation begins ([record](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md))
- Go service skeleton with fail-fast configuration, a documented middleware chain, and graceful shutdown that provably completes in-flight requests. Health endpoints separate liveness from readiness and disclose no infrastructure detail. (`P0-04`, `P0-10`)
- Structured JSON logging with redaction enforced by the logger across 23 sensitive key names, plus per-request correlation IDs. (`P0-09`)
- Local Docker stack — PostgreSQL, Redis, Mailpit — with a distroless non-root runtime image whose healthcheck is the binary itself. The Postgres init script creates the application role as `NOSUPERUSER NOBYPASSRLS` and non-owner, which is the precondition for row-level security. (`P0-05`)
- Migration tooling as a separate binary with embedded SQL, and the full 14-table Phase 0/1 schema including month-partitioned, append-only `events`. (`P0-06`, `P0-07`)
- CI pipeline: commit-convention enforcement, build and race-enabled tests, integration tests against real Postgres and Redis, migration round-trip verification, destructive-migration justification, gosec, govulncheck, secret scanning, and a non-root image assertion. Third-party actions pinned by SHA. (`P0-13`)
- ADR-006 through ADR-010 record the backend stack, embedded migrations, the distroless runtime, text-plus-CHECK over native enums, and why `events` has no foreign keys. (`P0-01`)

**Added** — cross-tenant isolation ([record](./records/2026-09-08-P0-08-row-level-security.md))
- Row-level security on 11 tables, keyed to a transaction-scoped `app.current_org_id`. A query with no tenant context returns zero rows rather than every row — fail-closed as a consequence of how SQL evaluates NULL, not as a check someone has to remember. (`P0-08`)
- A storage API with no exported way to query outside a declared scope: every path goes through `WithTenant` or `WithInstanceScope`, both of which set the tenant inside a transaction before returning a queryable handle. `SET LOCAL` rather than `SET`, so a pooled connection cannot carry one request's tenant into the next. (`P0-08`)
- The service refuses to boot if its database role is a superuser, has `BYPASSRLS`, or owns a table — closing the hole flagged in the previous record, where pointing `AUTH_POSTGRES_DSN` at the owner would silently disable every policy with no test failure. (`P0-08`)
- A CI gate failing the build when a table with an `org_id` has no RLS enabled. (`P0-08`)
- `/readyz` now genuinely checks PostgreSQL; it previously reported ready with no dependencies wired.
- Deployed and verified on the VM; `https://auth.zedth.my.id` healthy throughout.

**Status**
- Phase 0: 14 of 21 tasks done. 14 of 177 overall.
- Open deviations: DV-01 (single-VM production vs. Multi-AZ), DV-02 (`manager_roles` has no tenant policy until `P2-05` provides a user context).
- Remaining in Phase 0: metrics and tracing (`P0-11`), the audit writer (`P0-12`), secrets conventions (`P0-14`), the test harness (`P0-15`), OpenAPI (`P0-16`), console and public-site skeletons (`P0-17` … `P0-19`), and staging (`P0-20`, blocked on `OQ-03`).
- The OIDC provider library remains undecided by design: confirming JWKS rotation with overlap and refresh-token reuse detection requires building against it, so it moves to `P1-03`.
- Open questions now number nine; `OQ-09` (audit log retention period and the erasure approach) is new and should be confirmed before `P0-07` writes the partitioning migration.

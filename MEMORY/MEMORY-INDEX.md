# Memory Index

Every change record, newest first. One line each: date, task ID, title, and the hook that tells you whether this is the record you need.

Add a line here as part of writing the record — an unindexed record is a record nobody finds.

---

## Records

| Date | Task | Record | Hook |
|---|---|---|---|
| 2026-09-08 | P0-11 | [Metrics, tracing, and alerting](./records/2026-09-08-P0-11-metrics-and-tracing.md) | 16 instruments on a separate internal listener, 15 promtool-validated alerts. Closes both P0-12 follow-ups. The principle worth keeping: absence is not health |
| 2026-09-08 | P0-11 | [Metrics endpoint authentication](./records/2026-09-08-P0-11-metrics-endpoint-authentication.md) | A bearer token in front of the metrics endpoint, and three deployment failures: a guard catching its own compose file, `chmod 400` on a directory the service cannot traverse, and a startup ordering that reported a closed database instead of an unreadable secret |
| 2026-09-08 | P0-16 | [The API contract becomes executable](./records/2026-09-08-P0-16-api-contract.md) | Spec-first, generating Go server interfaces so a contract mismatch is a compile error rather than a CI message. Found a probe vocabulary inconsistency by writing the spec against the running code, and a shell function shadowing `head` that had been discarding two failure paths' diagnostic output |
| 2026-09-08 | P0-17 | [The console shell](./records/2026-09-08-P0-17-console-skeleton.md) | Tokens, routing, navigation and accessibility, with the token discipline enforced by lint. Every type token was defined, named correctly, and generated no CSS at all — the tests passed because they read the source file rather than the compiler's output |
| 2026-09-08 | P0-12 | [Audit event writer](./records/2026-09-08-P0-12-audit-writer.md) | Events commit with the action that caused them. Kills the partition time bomb flagged two records ago. Six stdlib vulnerabilities found and fixed. A check script that was silently discarding uncommitted work |
| 2026-09-08 | P0-08 | [Row-level security](./records/2026-09-08-P0-08-row-level-security.md) | Cross-tenant isolation becomes a database guarantee: 11 policies, a storage API with no unscoped query path, a startup assertion refusing an RLS-bypassing role, and a CI gate. Opens DV-02 |
| 2026-09-08 | P0-14, P0-20 | [Secrets conventions and VM deployment](./records/2026-09-08-P0-14-P0-20-secrets-and-vm-deployment.md) | OQ-03 answered (self-managed VM). Secret-reference abstraction, rotation runbooks, full VM deploy. Found and fixed an env_file bug handing the service RLS-bypassing owner credentials. Opens DV-01: single VM misses PLAN/14's Multi-AZ requirement |
| 2026-09-08 | P0-01…P0-13 | [Phase 0 foundation, first eleven tasks](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md) | Repo goes from docs-only to a running, tested service: Go skeleton, local stack, full 14-table schema, CI. 11/21 of Phase 0 done |
| 2026-09-08 | — | [Phase F frontend track added](./records/2026-09-08-phase-f-frontend-track.md) | 53 frontend tasks; resolves the tension with PLAN/16's lockstep rule by gating each page on its backend task; found a 4-screen omission in UI-UX/08 |
| 2026-09-08 | — | [Plan gap remediation](./records/2026-09-08-plan-gap-remediation.md) | All 11 gaps and both contradictions closed by amending the plan; PLAN/04 grew 158 → 295 lines; 2 further gaps found while fixing them |
| 2026-09-08 | P0-21 | [TASKS and MEMORY scaffolding](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md) | Execution layer established; 124 tasks across 7 phases; 2 plan contradictions and 11 plan gaps found while writing it |

---

## By Phase

### Pre-Phase-0 — Planning and plan amendments
- [Plan gap remediation](./records/2026-09-08-plan-gap-remediation.md) — closes PG-01…PG-11 plus two found during the work
- [Phase F frontend track added](./records/2026-09-08-phase-f-frontend-track.md) — 53 tasks covering every component and page

### Phase 0 — Foundation
- `P0-01`…`P0-13` — [Foundation, first eleven tasks](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md) — service skeleton, local stack, schema, CI
- `P0-11` — [Metrics, tracing, and alerting](./records/2026-09-08-P0-11-metrics-and-tracing.md) — measurement before the thing measured
- `P0-11` — [Metrics endpoint authentication](./records/2026-09-08-P0-11-metrics-endpoint-authentication.md) — the endpoint authenticates on its own behalf, not only by where it is bound
- `P0-16` — [The API contract becomes executable](./records/2026-09-08-P0-16-api-contract.md) — the spec generates the code, so drift is impossible rather than merely detectable
- `P0-17` — [The console shell](./records/2026-09-08-P0-17-console-skeleton.md) — a test that reads the input to a compiler cannot tell you what the compiler did
- `P0-12` — [Audit event writer](./records/2026-09-08-P0-12-audit-writer.md) — append-only log, partition maintenance
- `P0-08` — [Row-level security](./records/2026-09-08-P0-08-row-level-security.md) — isolation enforced by the database
- `P0-14`, `P0-20` — [Secrets conventions and VM deployment](./records/2026-09-08-P0-14-P0-20-secrets-and-vm-deployment.md)
- `P0-21` — [TASKS and MEMORY scaffolding](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md)

### Phase 1 — MVP: Core Auth + SSO
_No records yet._

### Phase 2 — RBAC & Multi-Tenancy
_No records yet._

### Phase 3 — Advanced Security
_No records yet._

### Phase 4 — Enterprise Interoperability
_No records yet._

### Phase 4b — ABAC
_Conditional phase; not started._

### Phase 5 — Hardening
_No records yet._

---

## Phase Summaries

Written at each phase completion from `templates/PHASE-SUMMARY-TEMPLATE.md`.

| Phase | Summary | Completed |
|---|---|---|
| Phase 0 | — | — |
| Phase 1 | — | — |
| Phase 2 | — | — |
| Phase 3 | — | — |
| Phase 4 | — | — |
| Phase 4b | — | — |
| Phase 5 | — | — |
| Phase F (track) | — | — |

---

## Operational Events

Key rotations, DR drills, pentests, load tests, and production incidents — anything with lasting consequence that is not a code change.

| Date | Event | Record |
|---|---|---|
| — | — | — |

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

**Status**
- No implementation code exists yet. Phase 0 begins at `P0-01`.
- Open questions now number nine; `OQ-09` (audit log retention period and the erasure approach) is new and should be confirmed before `P0-07` writes the partitioning migration.

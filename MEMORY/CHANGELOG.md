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

**Status**
- No implementation code exists yet. Phase 0 begins at `P0-01`.

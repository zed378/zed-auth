# P0-21 — TASKS and MEMORY Scaffolding

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Task** | `TASKS/PHASE-0-FOUNDATION.md` § P0-21 |
| **Phase** | Phase 0 — Foundation |
| **Surface** | docs |
| **Author** | Claude Code, at the project owner's request |
| **Commits / PR** | Not yet committed — repository is not under version control (see `P0-03`) |
| **Status** | Completed, with one item pending user approval |

---

## What Changed

Two new top-level folders. `TASKS/` turns `PLAN/16-IMPLEMENTATION-ROADMAP.md`'s six phase checklists into 124 individually-specified tasks across seven phase files, each with dependencies, plan references, implementation steps, a definition of done, and its abuse cases. `MEMORY/` establishes the record layer — change records, an ADR log, an internal changelog, and templates — with the rule that a task is not done until its record exists.

No implementation code was written and no existing document was modified.

## Why

The repository held 49 planning documents and zero lines of code. The plan is unusually thorough on *what* to build and *why*; what it lacked was an execution layer.

`PLAN/16`'s phase items are the right shape for a roadmap and the wrong shape for execution — "Basic OIDC provider implementation" is roughly a month of work containing a dozen security-critical decisions. Nobody can pick that up, finish it, and verify it. `PLAN/17`'s acceptance criteria are per-phase, so there is no per-task equivalent of "done," which is precisely how a phase gets declared complete with three of its eight criteria unverified.

`AGENTS.md` rule 9 makes `PLAN/`, `UI-UX/`, and `SECURITY/` reference-only, so mutable execution state could not live inside them without making it impossible to distinguish a design change from a progress update in a diff.

The `MEMORY/` half follows from the same reasoning that makes `events` append-only at the database level (`PLAN/04`, `SECURITY/02` §19). A project whose entire premise is that audit trails matter benefits from its development process having one.

## How

Read the full corpus first — `README.md`, `CLAUDE.md`, `AGENTS.md`, all 21 `PLAN/` documents, and the relevant parts of `UI-UX/` and `SECURITY/` — then decomposed each roadmap phase into tasks, working backwards from `PLAN/17`'s acceptance criteria so every criterion has at least one task that produces its evidence.

Four rules governed the decomposition:

1. **No task invents design.** Every task cites the plan documents that already made its decisions. Where the plan does not answer a question, that became a `BACKLOG.md` entry rather than an assumption — following `CLAUDE.md`'s instruction to flag gaps rather than guess.
2. **Security requirements attach to the task that must satisfy them.** Abuse cases from `SECURITY/02` and `PLAN/10` are listed on the specific task, so `CLAUDE.md`'s rule that security-sensitive features ship with abuse-case tests is enforceable per task rather than remembered per phase.
3. **The phase rule is structural.** Task dependencies encode `PLAN/16`'s sequencing, and the one sanctioned exception — the public site shipping ahead of Phase 1, per `PLAN/20` — is confined to `P0-17` through `P0-19` and stated as an exception where it appears.
4. **Conditional phases stay conditional.** Phase 4b opens with a justification gate (`P4B-00`) that is `BLOCKED` by default, because `PLAN/08` Part D and `PLAN/00` both say to build ABAC only on concrete need. The phase never running is a valid outcome, and the file says so.

Tasks that touch authentication, authorization, sessions, tokens, grants, or the data model are marked `Spec required: Yes`, routing them through `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` per `CLAUDE.md`'s mandatory workflow.

## Files and Components Touched

| Path | Change |
|---|---|
| `TASKS/README.md` | New — how to use the folder, and the phase rule |
| `TASKS/00-TASK-CONVENTIONS.md` | New — IDs, statuses, task card anatomy, global DoD, definition of ready, deviation protocol |
| `TASKS/PHASE-0-FOUNDATION.md` | New — 21 tasks |
| `TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` | New — 28 tasks |
| `TASKS/PHASE-2-RBAC-MULTITENANCY.md` | New — 17 tasks |
| `TASKS/PHASE-3-ADVANCED-SECURITY.md` | New — 15 tasks |
| `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` | New — 16 tasks |
| `TASKS/PHASE-4B-ABAC.md` | New — 10 tasks, gated |
| `TASKS/PHASE-5-HARDENING.md` | New — 16 tasks |
| `TASKS/PROGRESS.md` | New — status board and cross-phase watch list |
| `TASKS/BACKLOG.md` | New — 8 open questions, 11 plan gaps, 12 deferrals |
| `MEMORY/README.md` | New — what gets recorded, and the honesty rules |
| `MEMORY/MEMORY-INDEX.md` | New — record index |
| `MEMORY/CHANGELOG.md` | New — internal changelog |
| `MEMORY/DECISIONS.md` | New — ADR log, ADR-001, and 13 pending decisions |
| `MEMORY/templates/CHANGE-RECORD-TEMPLATE.md` | New |
| `MEMORY/templates/PHASE-SUMMARY-TEMPLATE.md` | New |
| `MEMORY/records/` | New — this record |

Nothing under `PLAN/`, `UI-UX/`, or `SECURITY/` was touched.

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| Two separate folders rather than one | Forward-looking plans get edited; backward-looking records must not. Separation keeps the record trustworthy | [ADR-001](../DECISIONS.md#adr-001--establish-tasks-and-memory-as-the-execution-layer) |
| File-based rather than an issue tracker | Lives in the repository, reviewable in a PR, readable from a checkout. Compatible with generating issues later | ADR-001 |
| Phase structure mirrors `PLAN/16` exactly | The roadmap is the authority on sequencing; re-deriving it would create a second, competing source of truth | ADR-001 |
| Documents written in English | Matches the existing 49 documents so the corpus reads as one body of work | See `BACKLOG.md` OQ-02 |
| SAML placed in Phase 4 | Three documents say Phase 4, one says Phase 2 — see PG-08 below | — |
| Subset validation specified as "every request" | Four documents say every request, one says at creation — see PG-09 below | — |

## Deviations from the Plan

None. No plan document was modified, and no plan decision was overridden.

Two apparent contradictions **within** the existing plan were found and resolved by following the majority reading, with both flagged for correction rather than silently normalized:

**PG-08 — SAML phase placement.** `PLAN/05-API-CONTRACT.md`'s standards table lists SAML 2.0 as Phase 2. `PLAN/16`, `PLAN/17`, and `PLAN/03` all place it in Phase 4. `TASKS/` follows the three-document majority. `PLAN/05`'s table is most likely a leftover from an earlier phase numbering.

**PG-09 — Project Grant subset validation timing.** `PLAN/18-RISK-REGISTER.md` R-04's mitigation says validation happens "on every grant creation." `CLAUDE.md`, `AGENTS.md` rule 3, `PLAN/08` Part C, and `PLAN/19`'s worked example all say **on every request**, not just at creation. `TASKS/` implements the stricter, four-document reading.

This second one matters more than a wording nit. The difference is exactly the attack: a grant narrowed or revoked after creation must stop working immediately, and R-04's phrasing describes a system where it would not. Left uncorrected, the risk register becomes the document someone cites later when arguing that creation-time validation is sufficient.

Both need correcting in the plan documents through the deliberate, code-owner-reviewed process, which is the user's call rather than a side effect of this work.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | N/A — documentation only |
| Integration | N/A |
| E2E | N/A |
| Security | N/A |

Verification was by cross-reference instead: every `PLAN/17` acceptance criterion was traced to at least one task that produces its evidence, and every abuse-case category in `SECURITY/02` was traced to at least one task that must test it.

## Abuse Cases Covered

N/A for this task. Abuse cases from `SECURITY/02` and `PLAN/10` were distributed across the tasks that must satisfy them, so that `CLAUDE.md`'s rule — no security-sensitive feature is done without a corresponding abuse-case test — is checkable at the task level rather than only at the phase level.

## Definition of Done Verification

- [x] `TASKS/` exists with conventions, seven phase files, progress board, and backlog
- [x] `MEMORY/` exists with README, index, changelog, decision log, and templates
- [x] This record documents the creation of both folders
- [x] ADR-001 records the decision and its alternatives
- [x] `MEMORY-INDEX.md` and `CHANGELOG.md` are updated
- [ ] **Pending** — `CLAUDE.md` and `AGENTS.md` reference `TASKS/` and `MEMORY/` in their documentation maps

The pending item is deliberate, not an oversight. Both files govern how every agent behaves in this repository; editing them is a decision the user should make explicitly rather than receive as a side effect of a scaffolding task. Raised as `OQ-01`.

Items 1–6 of the global DoD do not apply to a documentation-only change; item 7 (a MEMORY record) is this file; item 8 (`PROGRESS.md` updated) is done.

## What Did Not Work

Two dead ends worth recording.

**Writing the phase files through shell heredocs.** The first attempt piped file content through `bash`, which failed with `ENAMETOOLONG` once a file exceeded roughly 20KB. Switched to direct file writes. Worth knowing for anyone scripting large document generation in this environment.

**Reading the corpus in large batches.** Concatenating seven documents in one command produced 33KB of output that had to be spooled to a file and re-read, costing more than reading them in smaller groups. Three to four documents per read was the practical limit.

## Follow-Ups and Open Questions

All recorded in `TASKS/BACKLOG.md` with the task each one blocks:

- **8 open questions** requiring the project owner's decision: agent-file updates (OQ-01), document language (OQ-02), deployment target (OQ-03), email provider (OQ-04), concrete RPO/RTO (OQ-05), capacity assumptions (OQ-06), organization count at launch (OQ-07), and which two applications serve as the MVP consumers (OQ-08).
- **11 plan gaps** needing a plan amendment or a recorded implementation decision. Six are missing tables in `PLAN/04-DATA-MODEL.md`: signing keys (PG-02, blocks `P1-03`), MFA factors (PG-03, blocks `P3-02`/`P3-05`), invite and reset tokens (PG-04, blocks `P1-19`), federated identity links (PG-05, blocks `P4-10`), webhook endpoints (PG-06, blocks `P4-12`), and role permission keys (PG-01, blocks `P2-01`). The remaining five are the two contradictions above plus authorization code storage (PG-07), `events` retention (PG-10), and the Redis/Postgres session split (PG-11).
- **13 decisions pending** an ADR, listed in `DECISIONS.md`.

`PG-01` and `PG-02` are the ones to resolve soonest: they block `P2-01` and `P1-03` respectively, and `P1-03` is on Phase 1's critical path.

## What to Watch

**`PROGRESS.md` going stale.** It is only useful if updated in the same commit as the work, which is why that is global DoD item 8. A progress board nobody trusts is worse than none, because it produces confident wrong answers.

**Task granularity in later phases.** Phase 0 through 2 were decomposed with more confidence than Phases 4b and 5, simply because they are closer and better specified by the plan. Expect some `L` tasks in the later phases to turn out to be `XL` and need splitting on contact.

**The record-per-task overhead.** If it starts feeling like paperwork, that is a signal the records have drifted toward restating diffs rather than capturing reasoning. The "What Did Not Work" and "Decisions Made" sections carry most of the value; if those are consistently thin, the practice needs revisiting rather than grinding on.

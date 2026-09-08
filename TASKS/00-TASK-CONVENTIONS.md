# 00 — Task Conventions

How every task in this folder is written, tracked, and closed. Read once; then it applies to all phases.

## Task IDs

```
P<phase>-<nn>[.<sub>]
```

| Example | Meaning |
|---|---|
| `P0-04` | Phase 0, task 4 |
| `P1-12.3` | Phase 1, task 12, sub-task 3 |
| `P4B-02` | Phase 4b (conditional ABAC phase), task 2 |

IDs are **permanent and never reused**. If a task is dropped, its ID is retired with a `DROPPED` status and a one-line reason — a gap in the numbering is a lost audit trail, and this project's whole premise is that audit trails matter.

Task IDs are the join key across the repo: they appear in branch names (`feat/P1-07-token-endpoint`), commit messages, PR titles, MEMORY change records, and `PROGRESS.md`.

## Status Values

| Status | Meaning |
|---|---|
| `TODO` | Not started. Dependencies may or may not be met. |
| `BLOCKED` | Cannot start — either a dependency is incomplete or an open question in `BACKLOG.md` must be answered first. The blocker is always named. |
| `SPEC` | A feature spec (`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`) is being written; no implementation code yet. |
| `WIP` | Implementation in progress. |
| `REVIEW` | Implementation complete, under review / awaiting test results. |
| `DONE` | Every line of the task DoD and the global DoD below is satisfied, **and** a MEMORY record exists. |
| `DROPPED` | Deliberately abandoned. Requires a one-line reason and a MEMORY decision record. |

## Task Card Anatomy

Every task in a phase file uses this structure:

```
### P1-07 — Implement POST /oauth/token

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-05, P1-06 |
| **Plan refs** | PLAN/05-API-CONTRACT.md Part A, PLAN/09-SECURITY.md |
| **Spec required** | Yes — touches authentication |
| **Surface** | backend |

**Goal** — one sentence stating the observable outcome.

**Steps** — the ordered implementation sequence.

**Definition of Done** — checkable, objective conditions.

**Abuse cases to test** — cross-referenced from SECURITY/02 and PLAN/10.
```

Field meanings:

- **Depends on** — task IDs that must be `DONE` first. Empty means it can start as soon as the phase starts.
- **Plan refs** — the authoritative documents. If implementation and these documents disagree, the documents win unless the deviation is escalated (see below).
- **Spec required** — `Yes` for anything touching authentication, authorization, sessions, tokens, grants, or the data model, per `CLAUDE.md`. `No` for config, scaffolding, and copy work.
- **Surface** — `backend`, `console`, `public-site`, `infra`, `docs`, or a combination. Used to parallelize work across people.

## Global Definition of Done

Inherited by **every** task. A task's own DoD is *in addition* to this, never instead of it.

1. **Tests exist at the right layer** of `PLAN/11-TESTING.md`'s pyramid — unit for pure logic, integration against real Postgres/Redis, E2E for user-visible flows.
2. **Every abuse case listed on the task has an automated test**, per `CLAUDE.md`'s hard rule 7. A security-sensitive feature with no abuse-case test is not done, regardless of how well the happy path works.
3. **The OpenAPI spec is updated** if the task touches an API surface (`PLAN/05-API-CONTRACT.md`), and the generated console client + generated public API reference both still build.
4. **Sensitive actions write an audit event** to `events` (`PLAN/04-DATA-MODEL.md`, `PLAN/09-SECURITY.md`).
5. **Nothing sensitive is logged** — no tokens, no passwords, no raw `/v1/authz/check` resource attributes (`PLAN/13-OBSERVABILITY.md`).
6. **CI is green**: build, unit + integration tests, `gosec` SAST, dependency CVE scan, lint, OpenAPI validation.
7. **A MEMORY change record exists** (`MEMORY/records/`), the index and changelog are updated, and any architectural decision or plan deviation has an ADR.
8. **`PROGRESS.md` and the phase file checkbox are updated** in the same commit as the work.

## Definition of Ready

A task should not move to `WIP` unless:

- All `Depends on` tasks are `DONE`.
- Every document in `Plan refs` has actually been read for this task (not remembered from a previous one).
- If `Spec required: Yes`, the spec exists in `TASKS/specs/` and covers all 22 template sections.
- No unanswered `BACKLOG.md` open question blocks it.

## Deviation Protocol

`AGENTS.md` rule 9 makes `PLAN/`, `UI-UX/`, and `SECURITY/` reference documentation, not implementation output. So when reality and the plan conflict — a library can't do what `PLAN/07` assumed, a UX spec is impossible at a given breakpoint, an endpoint shape doesn't survive contact with OIDC conformance:

1. **Stop.** Do not silently pick a different approach.
2. Write an ADR in `MEMORY/DECISIONS.md` describing the conflict, the options, and the recommendation.
3. Raise it with the user.
4. Only after a decision: implement, and note in the ADR whether the plan document itself should be amended (a separate, deliberate action).

A deviation that is documented is a decision. A deviation that is not documented is a bug that nobody has found yet.

## Estimation

Tasks carry **relative size**, not calendar dates, because staffing is unknown:

| Size | Rough meaning |
|---|---|
| `S` | Under half a day for someone familiar with the area |
| `M` | One to two days |
| `L` | Several days; consider splitting into sub-tasks |
| `XL` | Too large — must be split before it enters `WIP` |

Sizes are recorded in `PROGRESS.md`, not repeated on every card.

## Branch, Commit, PR

- Branch: `feat/P1-07-token-endpoint`, `fix/P2-03-role-claim-scope`, `chore/P0-11-ci-pipeline`.
- Commit message subject: `P1-07: implement POST /oauth/token with PKCE verification`.
- Commit trailer: reference the plan documents implemented, per `AGENTS.md`'s PR instructions.
- PR body must state: the task ID, the plan sections implemented, which test layers were added and run, and any deviation (with its ADR link).

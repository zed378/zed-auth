# <Task ID> — <Short Title>

| | |
|---|---|
| **Date** | YYYY-MM-DD |
| **Task** | `TASKS/PHASE-N-....md` § <task id> |
| **Phase** | Phase N |
| **Surface** | backend / console / public-site / infra / docs |
| **Author** | |
| **Commits / PR** | |
| **Status** | Completed / Partially completed / Reverted |

---

## What Changed

A factual summary of the change in two or three sentences. What exists now that did not before, or behaves differently than it did.

## Why

The reason this was done now, and the plan documents it implements. Link them — `docs/PLAN/05-API-CONTRACT.md` § Core Endpoints, not "the API doc."

If the task exists because of something discovered during other work rather than because the roadmap said so, say that — it is the more useful information.

## How

The implementation approach, at the level of detail a future reader needs to orient themselves before opening the code. Not a line-by-line description; the diff already covers that.

Name anything non-obvious: a workaround, an unusual pattern, a place where the straightforward approach was wrong for a reason that is not visible locally.

## Files and Components Touched

| Path | Change |
|---|---|
| | |

## Decisions Made

Decisions taken during this work. Anything architectural, or any deviation from the plan, also gets a full ADR in `DECISIONS.md` — link it here.

| Decision | Rationale | ADR |
|---|---|---|
| | | |

## Deviations from the Plan

Per the deviation protocol in `TASKS/00-TASK-CONVENTIONS.md`: state the conflict, what was chosen, whether the user approved it, and whether the plan document should now be amended.

If there were none, write "None" — an empty section reads as an oversight.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | |
| Integration | |
| E2E | |
| Security | |

## Abuse Cases Covered

Cross-referenced from `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` and `docs/PLAN/10-THREAT-MODEL.md`. Every abuse case listed on the task must appear here with the test that covers it.

| Abuse case | Source | Test |
|---|---|---|
| | | |

## Definition of Done Verification

Both the task's own DoD and the global DoD from `TASKS/00-TASK-CONVENTIONS.md`. **If an item was waived, say which one, why, and who agreed** — a silently unmet DoD item is the failure mode this section exists to prevent.

- [ ] Tests at the appropriate pyramid layer
- [ ] Every abuse case has an automated test
- [ ] OpenAPI spec updated (if API-facing) and generated clients rebuilt
- [ ] Sensitive actions write audit events
- [ ] Nothing sensitive is logged
- [ ] CI green: build, tests, SAST, dependency scan, lint, OpenAPI validation
- [ ] `TASKS/PROGRESS.md` and the phase file updated
- [ ] Task-specific DoD items (list them)

## What Did Not Work

Approaches tried and abandoned, and why. This is the section most likely to be skipped and most likely to save someone a day later — a dead end that is not recorded gets walked into again.

## Follow-Ups and Open Questions

Anything left undone, discovered mid-work, or deferred. Every item here should also exist in `TASKS/BACKLOG.md` or as a task, so it has a consequence rather than only a mention.

## What to Watch

Things that could go wrong in production because of this change, what the symptom would look like, and which metric or alert would show it first.

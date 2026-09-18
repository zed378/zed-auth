# 02 - Task Conventions and Workflow

> Category: **Developer** (`docs/DEVELOPER/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: all &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how work moves from a task card to a merged, deployed, recorded change — and why each step exists.

## Scope

The workflow this repository actually uses. The canonical rules are `TASKS/00-TASK-CONVENTIONS.md`; this document summarises them and points at the evidence.

## As Built

### Task identity

Task IDs (`P<phase>-<nn>`, e.g. `P4-02`, `P4B-01`) are permanent and never reused; a dropped task keeps its ID with a `DROPPED` status and a reason, because a gap in the numbering is a lost audit trail. The ID is the join key across branch names, commit subjects, the progress board, feature specs and change records.

### The loop

1. **Pick the card** from `TASKS/PHASE-*.md`; check its dependencies on `TASKS/PROGRESS.md`.
2. **Write the feature spec** in `MEMORY/specs/` when the card touches authentication, authorization or the data model — the template is `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`, and it includes abuse cases drawn from `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.
3. **Branch**: one branch per card, `feat/<task-id>-<slug>`.
4. **Implement with tests**, including a test per abuse case the spec names.
5. **Prove the tests can fail**: break each control in turn and confirm a specific test turns red. This mutation step is a convention rather than a tool, and it has repeatedly caught tests that passed vacuously.
6. **Run the gate**: `bash scripts/check.sh`, zero failures.
7. **Merge** with `--no-ff`, so the branch remains visible in history.
8. **Deploy to staging and verify against the running service** — not just that the container started.
9. **Write the record** in `MEMORY/records/`: what shipped, what was decided, what was verified, what was deliberately left undone.
10. **Update `TASKS/PROGRESS.md`** in the same change as the work it describes.

A card is `DONE` only when every line of its own Definition of Done and the global one is satisfied **and** a record exists.

### Deviations and gaps

- A decision that changes the design is an ADR in `MEMORY/DECISIONS.md`.
- A place where the plan is silent, ambiguous or contradicted by reality is a plan gap (`PG-xx`) in `TASKS/BACKLOG.md`, raised rather than decided silently.
- The plan documents themselves are amended only deliberately, through a separate reviewed change (`.github/CODEOWNERS`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Commit subject begins with the task ID | Required | `commit-convention` CI job; `scripts/hooks/commit-msg` |
| One branch per card, merged `--no-ff` | Required | convention; visible in `git log` |
| Abuse-case tests for security features | Required before `DONE` | `CLAUDE.md`, card DoD |
| Record before `DONE` | Required | `TASKS/00-TASK-CONVENTIONS.md` |
| Plan edits | Separate, reviewed change | `.github/CODEOWNERS` |

## Security Considerations

The two conventions that carry the most weight are the abuse-case tests and the mutation step. Together they answer a question a passing suite cannot: *would this test have failed if the control were missing?* Several defects in this repository were found exactly there, and the pattern is recorded in the project's memory as a recurring defect class.

## Verification

- `TASKS/PROGRESS.md` — the current state of every card.
- `MEMORY/records/` — one record per completed card, including staging evidence.
- CI enforces the commit convention and the gate.

## Not Yet Built / Open Questions

- The Phase F (frontend track) rows in `TASKS/PROGRESS.md` have not been reconciled with the console screens that shipped.

## Related Documents

- `TASKS/00-TASK-CONVENTIONS.md`, `TASKS/BACKLOG.md`, `CLAUDE.md`, `AGENTS.md`.
- `docs/TESTING/04-ABUSE-CASE-SECURITY-TESTING.md` (abuse cases and mutation verification).

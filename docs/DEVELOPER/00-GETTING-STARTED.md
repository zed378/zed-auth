# 00 - Getting Started

> Category: **Developer** (`docs/DEVELOPER/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P0-15, P1-26 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Orient someone new to this repository: the concepts the product is built from, where the code is, and what to read before changing anything.

## Scope

Onboarding for people working **on** this service. Integrating an application **with** it is `03-INTEGRATION-GUIDE.md`.

## As Built

### The five concepts

| Concept | What it is | Table |
|---|---|---|
| **Instance** | The deployment. Everything else lives inside one | `instances` |
| **Organization** | A tenant. Its data is isolated by row-level security | `organizations` |
| **Project** | A container for applications and roles inside one organization | `projects` |
| **Application** | An OIDC client — the thing that signs users in | `applications` |
| **Role and grant** | A role belongs to a project; a grant gives a user that role in that project | `roles`, `user_grants` |

Two further concepts appear once delegation is in play: a **Project Grant** lends a project's roles to another organization, and **manager roles** decide who may administer the service itself (`docs/AUTHORIZATION/`).

### Repository layout

```
backend/        Go service: cmd/ binaries, internal/ packages, migrations/, tests/
console/        React management console (+ its own docs/ and e2e/)
public-site/    Marketing and documentation site, incl. the generated API reference
openapi/        The contract every surface is generated from
deploy/         Compose files, VM units, demo applications, runbooks
scripts/        check.sh (the gate), e2e-up.sh, acceptance and load tests
docs/           This documentation
TASKS/          The execution plan: phases, cards, progress board, backlog
MEMORY/         Decisions (ADRs), feature specs, and a record per completed task
```

### What to read before changing code

1. `CLAUDE.md` — the documentation map and the non-negotiable constraints.
2. `TASKS/PROGRESS.md` — what phase the project is in and what is in scope.
3. The `docs/PLAN/` document your area names, then the matching document here.
4. For anything touching authentication, authorization or sessions: `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| One branch per task card | `feat/<task-id>-<slug>`, merged `--no-ff` | repository convention, `TASKS/00-TASK-CONVENTIONS.md` |
| Commit subject | Begins with the task ID | `commit-convention` CI job, `scripts/hooks/commit-msg` |
| A task is done | Gate green, deployed and verified, and a record exists in `MEMORY/records/` | `TASKS/00-TASK-CONVENTIONS.md` |
| Feature specs | Required for anything touching auth, authorization or the data model | `CLAUDE.md`, `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` |

## Interfaces

- Local stack: `02-...` in `docs/DEVOPS/` and `01-LOCAL-DEVELOPMENT-SETUP.md` here.
- The gate: `bash scripts/check.sh`.

## Verification

Nothing in this document is a behavioural claim; the conventions above are checked by the commit-message hook, the CI convention job, and the progress board itself.

## Not Yet Built / Open Questions

- There is no published SDK to start from; `docs/SDK/` explains what exists instead.

## Related Documents

- `CLAUDE.md`, `AGENTS.md`, `TASKS/00-TASK-CONVENTIONS.md`.
- `01-LOCAL-DEVELOPMENT-SETUP.md`, `02-TASK-CONVENTIONS-AND-WORKFLOW.md`, `03-INTEGRATION-GUIDE.md`.

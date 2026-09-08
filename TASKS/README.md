# TASKS/ — Execution Plan

`PLAN/`, `UI-UX/`, and `SECURITY/` describe **what** to build and **why**. This folder describes **what to do next, in what order, and how to know it's finished**.

Nothing in this folder invents new architecture. Every task points back to the plan document that already decided the design. If a task needs a decision the plan doesn't contain, it is not a task — it is an entry in [`BACKLOG.md`](./BACKLOG.md) under "Open Questions", to be raised with the user.

## Files in This Folder

| File | Purpose |
|---|---|
| [`00-TASK-CONVENTIONS.md`](./00-TASK-CONVENTIONS.md) | Task ID scheme, status values, the anatomy of a task card, and the Definition of Done that every task inherits |
| [`PROGRESS.md`](./PROGRESS.md) | The single status board — phase-level and task-level completion at a glance |
| [`PHASE-0-FOUNDATION.md`](./PHASE-0-FOUNDATION.md) | Repo, toolchain, schema, CI/CD, observability baseline, public site skeleton |
| [`PHASE-1-MVP-CORE-AUTH-SSO.md`](./PHASE-1-MVP-CORE-AUTH-SSO.md) | OIDC provider, password login, sessions/SSO, Management API CRUD, console MVP, audit log, rate limiting |
| [`PHASE-2-RBAC-MULTITENANCY.md`](./PHASE-2-RBAC-MULTITENANCY.md) | Roles/permissions, role claims, `/v1/authz/check`, multi-org activation, per-org policies |
| [`PHASE-3-ADVANCED-SECURITY.md`](./PHASE-3-ADVANCED-SECURITY.md) | TOTP, WebAuthn, refresh token rotation, anomaly detection, session self-service |
| [`PHASE-4-ENTERPRISE-INTEROP.md`](./PHASE-4-ENTERPRISE-INTEROP.md) | SAML IdP, social login, Project Grants, webhooks, SCIM (optional) |
| [`PHASE-4B-ABAC.md`](./PHASE-4B-ABAC.md) | Embedded OPA, `user_attributes`/`policies`, dry-run and versioned rollout — **conditional phase** |
| [`PHASE-5-HARDENING.md`](./PHASE-5-HARDENING.md) | Pentest, load testing, DR drill, accessibility audit, public trust page, launch readiness |
| [`PHASE-F-FRONTEND-IMPLEMENTATION.md`](./PHASE-F-FRONTEND-IMPLEMENTATION.md) | **A track, not a sequence position** — every component and every page of the console, hosted auth screens, and public site. Foundation tasks run early and continuously; each page task is gated on the backend task that unblocks it |
| [`BACKLOG.md`](./BACKLOG.md) | Open questions, deferred items, and gaps discovered in the plan |

## How to Use This Folder

1. **Before starting work**, open [`PROGRESS.md`](./PROGRESS.md) and find the lowest-numbered task in the current phase that is `TODO` and whose dependencies are all `DONE`.
2. **Read every document listed in that task's `Plan refs` row.** These are not decoration — they contain the decisions the task implements.
3. **If the task is marked `Spec required`**, write the feature spec from [`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`](../PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md) before writing code. Save it to `TASKS/specs/<task-id>-<slug>.md`.
4. **Implement**, satisfying every line of the task's Definition of Done plus the inherited global DoD in [`00-TASK-CONVENTIONS.md`](./00-TASK-CONVENTIONS.md).
5. **Record the change** in [`../MEMORY/`](../MEMORY/README.md) — a change record per task, an index line, a changelog entry, and an ADR if an architectural decision was made or deviated from.
6. **Update `PROGRESS.md`** and tick the checkbox in the phase file.

## The Phase Rule

From `CLAUDE.md` and `PLAN/16-IMPLEMENTATION-ROADMAP.md`:

> **Never build a Phase N+1 feature while Phase N is incomplete.**

Phases are sequential because each one's security posture depends on the previous one being sound. There is exactly one deliberate exception, already stated in `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`: the public site ships ahead of the backend phases, because it is how stakeholders evaluate the project before there is anything to log into. That exception is encoded as tasks `P0-17` through `P0-19`, and nowhere else.

**Phase F is not an exception to this.** `PLAN/16` requires the console to be built in lockstep with the backend phases rather than as a separate track, and Phase F preserves that: its foundation tasks (design system, app shell, cross-cutting behavior, test infrastructure) are genuinely phase-independent and should run early, while every page task carries a binding **Gate** naming the backend task that must be `DONE` first. Phase F exists so the whole frontend surface is visible in one document, not so screens can be built ahead of the APIs they call.

Phase 3 and Phase 4 may be swapped as a whole if business need demands it (roadmap note in `PLAN/16`), but never interleaved task-by-task.

## Relationship to MEMORY/

`TASKS/` is forward-looking (what will be done). `MEMORY/` is backward-looking (what was done, and why it ended up that way). They are updated in the same commit: a task is not `DONE` until its MEMORY record exists.

# 03 - Attribute-Based Access Control

> Category: **Authorization** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: Phase 4b (`P4B-00` … `P4B-10`)

## Purpose

Record what ABAC would add on top of RBAC, and what has already been decided so that a future implementation fits the code that exists.

## Why It Is Not Built Yet

Phase 4b is **conditional**. `TASKS/PHASE-4B-ABAC.md` gates the whole phase behind `P4B-00`: a concrete requirement that RBAC cannot express. `TASKS/PROGRESS.md` shows 0 of 11 tasks done and the phase marked CONDITIONAL. Nothing in the service evaluates an attribute today.

## Constraints Already Decided

- **The request shape already accommodates it.** `/v1/authz/check` accepts `resource.id` and `resource.attributes` and ignores them (`backend/internal/authz/decide.go`), so a consumer that sends attributes today needs no change when policies arrive.
- **The response field already exists.** `Decision.MatchedPolicy` carries a role key under RBAC; the plan is for a policy name to replace the content without changing the field.
- **The engine is intended to be OPA embedded as a Go library** (`docs/PLAN/07-BACKEND-ARCHITECTURE.md`, `CLAUDE.md`). No dependency on it exists in `backend/go.mod` yet.
- **Policies are per organization and versioned**, with dry-run and rollback in the console (`docs/UI-UX/04-USER-FLOWS.md` Flow 5, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` Policies — ABAC tab).
- **RBAC stays the floor.** `docs/PLAN/08-AUTHORIZATION.md` Part D describes ABAC as layered on top: a policy narrows what a role already allows rather than granting something no role carries.

## Key Topics To Specify

- Which attributes are in scope (subject, resource, environment) and where each comes from; how an attribute the caller supplies is distinguished from one the service knows.
- The storage and lifecycle of a policy: table, versioning, activation, rollback, and who may write one.
- Evaluation order against RBAC, and whether a policy may ever *grant* rather than only *deny*.
- Latency budget: `/v1/authz/check` has a p95 target (`docs/PLAN/12-PERFORMANCE.md`) that policy evaluation must fit inside, and the current cache (`backend/internal/authz/cache.go`) keys on user and project only.
- Caching: whether a decision that depends on request attributes can be cached at all, and what must be excluded from the key.
- What is logged. `docs/PLAN/13-OBSERVABILITY.md` and `CLAUDE.md` forbid logging raw resource attributes; a policy engine that logs its inputs would break that.
- Failure mode when a policy cannot be evaluated: refuse, or fall back to RBAC. (The service's convention elsewhere is to refuse.)

## Acceptance Criteria

- [ ] `P4B-00` records a requirement RBAC demonstrably cannot express, before any other 4b task starts.
- [ ] A policy can be written, dry-run against real requests, activated and rolled back, each step audited.
- [ ] A policy cannot widen access beyond the subject's roles, proven by a test that tries.
- [ ] Raw attributes appear in no log line; the log-hygiene gate in `scripts/check.sh` still passes.
- [ ] `/v1/authz/check` p95 stays within `docs/PLAN/12`'s target with policies active, measured by the load test in `scripts/loadtest/`.
- [ ] Evaluation is deterministic and explainable: the response names the policy that decided.

## Open Questions

- Does an organization with no policy pay any evaluation cost at all?
- Are policies authored in Rego directly, or through a constrained form the console can validate?
- How are policies versioned relative to roles they reference, given role keys are immutable but roles can be deleted?

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md` Part D; `TASKS/PHASE-4B-ABAC.md`.
- `04-PERMISSION-EVALUATION-ENGINE.md` — the RBAC decision path a policy layer would extend.
- `docs/UI-UX/04-USER-FLOWS.md` Flow 5.

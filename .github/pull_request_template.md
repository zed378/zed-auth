## Task

<!-- Required. The task ID from TASKS/, e.g. P1-07. A PR without one will be rejected by CI. -->

Task ID:

## Plan Documents Implemented

<!-- Required. Which PLAN/, UI-UX/, or SECURITY/ sections this implements, so a reviewer can check
     against the spec rather than only against the diff (AGENTS.md § PR / Change Instructions). -->

-

## Tests Added and Run

<!-- Required. Which layers of PLAN/11-TESTING.md's pyramid. Name the tests, not just the layer. -->

| Layer | What it covers |
|---|---|
| Unit | |
| Integration | |
| E2E | |
| Security | |

## Abuse Cases Covered

<!-- Required for anything touching auth, authz, sessions, tokens, or grants.
     Cross-reference SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md and PLAN/10-THREAT-MODEL.md.
     "None applicable" is an acceptable answer; a blank section is not. -->

| Abuse case | Source | Test |
|---|---|---|

## Deviation From the Plan

<!-- Required. Write "None" if there is none. If there is one, link the ADR in MEMORY/DECISIONS.md.
     A documented deviation is a decision; an undocumented one is a bug nobody has found yet. -->

None

## Definition of Done

<!-- The global DoD from TASKS/00-TASK-CONVENTIONS.md. If an item was waived,
     say which one, why, and who agreed — do not silently leave it unchecked. -->

- [ ] Tests exist at the appropriate pyramid layer
- [ ] Every abuse case listed above has an automated test
- [ ] `openapi/openapi.yaml` updated (if API-facing) and generated clients rebuild
- [ ] Sensitive actions write an audit event
- [ ] No tokens, passwords, or raw `/v1/authz/check` attributes are logged
- [ ] CI green: build, tests, SAST, dependency scan, lint, OpenAPI validation
- [ ] A `MEMORY/records/` change record exists, with the index and changelog updated
- [ ] `TASKS/PROGRESS.md` and the phase file are updated in this PR

# PF-01 — Motion Tokens and Reduced-Motion Contract

| | |
|---|---|
| **Date** | 2026-11-25 |
| **Task** | `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` § PF-01 |
| **Phase** | Phase F — Frontend Implementation |
| **Surface** | console |
| **Author** | Codex |
| **Commits / PR** | Recorded in git history |
| **Status** | Completed |

---

## What Changed

The console stylesheet now provides token-driven motion primitives for panels, modals, directional steps, changed table rows, and toast entry. Reduced-motion preference disables animations and transitions; confirmation dialogs and table sorting remain motionless by default.

## Why

`docs/UI-UX/10-MOTION-DESIGN.md` defines the permitted functional motion patterns and `docs/UI-UX/13-ACCESSIBILITY.md` requires instant changes for reduced-motion users. PF-01 makes both enforceable before more animated components are built.

## How

Named CSS primitives refer to the existing `--duration-quick` and `--ease-standard` tokens. Panel and modal state selectors define their open/closed endpoints; the step, toast and changed-row patterns use named keyframes. A global reduced-motion rule sets animation and transition to `none`, which makes state changes immediate rather than merely shortening them.

## Files and Components Touched

| Path | Change |
|---|---|
| `console/src/styles/tokens.css` | Added shared motion selectors/keyframes and structural no-motion rules. |
| `console/src/styles/tokens.test.ts` | Asserted token use and reduced-motion/no-motion source rules. |
| `console/src/styles/utilities.test.ts` | Verified the shared rules survive Tailwind compilation. |
| `console/package.json` | Limited the Vitest worker count to four to avoid timeouts under the default parallel load. |
| `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` | Closed PF-01 and added its required frontend implementation-chain table. |
| `TASKS/PROGRESS.md` | Updated the task count and board. |
| `MEMORY/MEMORY-INDEX.md`, `MEMORY/CHANGELOG.md` | Indexed and summarized the completed task. |

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| Keep the motion patterns as named CSS primitives, with duration and easing supplied centrally. | Components should select an interaction without inventing per-component timing. | Not architectural; recorded in source. |
| Run Vitest with four workers by default. | The unrestricted run timed out three tests; isolated reruns and the full suite with four workers passed. | Not architectural. |

## Deviations from the Plan

None. The task reuses the duration and easing tokens already present in the P0-17 design system.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | CSS token, reduced-motion, confirmation, and table-motion rules. |
| Integration | Tailwind CLI compiles and retains the motion selectors and reduced-motion rules. |
| E2E | Not applicable; no interactive component is introduced in PF-01. |
| Security | Not applicable; no security-sensitive behavior is changed. |

## Abuse Cases Covered

Not applicable. PF-01 adds presentation behavior only and does not grant access or handle untrusted input.

## Definition of Done Verification

- [x] Tests at the appropriate layer; all 302 console tests pass.
- [x] No abuse cases apply to this presentation-only task.
- [x] No API surface changed; generated clients are unaffected.
- [x] No sensitive action or logging path changed.
- [x] Console lint, typecheck, production build, and test suite pass.
- [x] `TASKS/PROGRESS.md` and the phase file were updated.
- [x] All five PF-01 task-specific Definition of Done items are checked.
- [x] The frontend implementation chain is complete in the task card.

## What Did Not Work

The default Vitest worker count caused three unrelated page tests to exceed the five-second limit during a full-suite run. The same test files passed in isolation, and the full suite passed with four workers; the standard test script now uses that stable limit.

## Follow-Ups and Open Questions

No open question blocks PF-01. Later component tasks should use the named motion classes rather than defining local durations.

## What to Watch

Future components could bypass the shared primitives with ad-hoc animation utilities. The compiled-CSS tests ensure the shared classes and reduced-motion rules remain present, while component review should check that implementations use these classes.

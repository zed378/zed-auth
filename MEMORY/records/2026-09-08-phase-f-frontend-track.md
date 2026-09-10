# Phase F — Frontend Implementation Track Added

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Task** | `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` (new) |
| **Phase** | Pre-Phase-0 (planning) |
| **Surface** | docs |
| **Author** | Claude Code, at the user's request ("tambahkan fase untuk implementasi frontend dengan segala halaman dan komponennya") |
| **Commits / PR** | Not yet committed — repository is not under version control (`P0-03`) |
| **Status** | Completed |

---

## What Changed

Added `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`: 53 tasks across seven tracks covering every component in `docs/UI-UX/07-COMPONENT-SPECIFICATION.md`, all 21 console screens in `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, four hosted-authentication screen groups, five public-site page groups, and the frontend quality suite. Project total went from 124 to 177 tasks.

## Why

The user asked for a frontend phase with all its pages and components. The existing breakdown had frontend work distributed across Phases 0–5 as fourteen coarse tasks — "Console — Users list and detail" is one task covering two screens, a two-step side-panel flow, four tabs, and every state each of those has. The whole frontend surface was never visible in one place, and the component library had no home at all: `docs/UI-UX/07` specifies eight components with usage rules, and nothing in `TASKS/` owned building them.

## How

### The tension I had to resolve first

`docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` says plainly: "The management console is built **in lockstep with these phases, not as a separate track**." A frontend phase is, on its face, exactly the separate track that sentence forbids.

I raised this rather than quietly ignoring it, and resolved it by splitting Phase F into two kinds of task with different rules:

- **Foundation** (Tracks A, B, C, G — design system, app shell, cross-cutting behavior, test infrastructure). Genuinely phase-independent: a Button does not depend on any endpoint. These should run early and continuously.
- **Pages** (Tracks D, E, F). Each carries a binding **Gate** row naming the backend task that must be `DONE` first — `PF-31` gates on `P2-03`, `PF-36` on `P4-01`, and so on.

That makes the lockstep rule *stricter*, not looser: it moves from per-phase to per-screen. A screen cannot be built ahead of its API, and now the document says which API, by task ID.

### Avoiding duplication with the existing console tasks

The fourteen existing console tasks in Phases 1–5 were not deleted or superseded. They remain the **phase gates** that verify screens against `docs/PLAN/17`'s per-phase acceptance criteria; Phase F holds the implementation detail. A mapping table at the top of the file states the relationship for every one of them, so nobody has to guess whether `P2-12` and `PF-31` are the same work described twice. A page task is done when both its own DoD and its gate task's criteria are met.

### Sourcing the content

Every task was written from the `docs/UI-UX/` documents rather than from general frontend practice. The specificity in those documents is unusual and worth preserving:

- `docs/UI-UX/19` specs the Sessions tab revoke button down to its screen-reader label ("Revoke session on [device/browser]", not "Revoke") as a worked example. `PF-33` says to follow that row by row rather than re-derive it.
- `docs/UI-UX/11` notes that on mobile a user may be trying to scan a TOTP QR code displayed on the very device that must scan it — impossible — so a manual entry code is required. That is in `PF-34` and `PF-40`.
- `docs/UI-UX/11` says a role a receiving organization cannot assign is **absent**, never a disabled checkbox, because a disabled checkbox implies "you could have this, but not yet." That is `PF-37`'s hardest requirement.
- `docs/UI-UX/07` says a `destructive` button must not visually dominate a screen whose common path is non-destructive — "Deactivate user" is `secondary` with danger-colored text. That is encoded in `PF-03` and applied in `PF-24`.

The twelve console-wide rules at the top of the file are collected from across `docs/UI-UX/` so they are enforceable per task rather than remembered per reader, and `docs/UI-UX/19`'s twelve-step implementation chain is made a mandatory, inherited deliverable for every component and page task.

### A gap found while doing this

`docs/UI-UX/08` presents itself as the "full screen inventory" and is cited that way throughout `TASKS/`, but it omitted four screens that `docs/UI-UX/03` and `docs/PLAN/06` both include — including Organization Overview, which `docs/UI-UX/18` separately specs in full detail. Amended `docs/UI-UX/08` to add all four; recorded in the plan gap remediation record.

## Files Touched

| Path | Change |
|---|---|
| `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` | New — 53 tasks, seven tracks, mapping table, twelve inherited rules |
| `TASKS/PROGRESS.md` | Phase F section added (five sub-tables); total 124 → 177; six frontend entries added to the cross-phase watch list |
| `TASKS/README.md` | Phase F listed; the phase rule now explains why a frontend track is not an exception to it |
| `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` | Four missing screens added to the inventory |

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| A track with per-page gates, not a sequenced phase | Preserves `docs/PLAN/16`'s lockstep rule while giving the whole surface one home; makes the rule per-screen instead of per-phase | [ADR-005](../DECISIONS.md) |
| Existing console tasks kept as phase gates, not superseded | They verify against `docs/PLAN/17`'s per-phase criteria, which Phase F does not replace | ADR-005 |
| `PF-12` is the only frontend task marked `Spec required` | "Genuinely unreachable, not merely hidden" is an authorization property, and it is where the UI can disagree with the API about what a user may do | ADR-005 |
| Public site duplicates brand tokens rather than importing console components | `docs/PLAN/20` requires the two surfaces to share no codebase; sharing components would force compromises in both | ADR-005 |

## Deviations from the Plan

One, raised explicitly and resolved rather than absorbed: `docs/PLAN/16`'s "not as a separate track" sentence. The resolution above keeps the rule's intent (no screen ahead of its API) and strengthens its enforcement (per screen, by task ID), so I do not read this as a deviation requiring a plan amendment. If the user disagrees, the fix is a sentence in `docs/PLAN/16` acknowledging the track — flagged here rather than assumed either way.

## Tests Added

None — planning documents. Test *requirements* are specified: `PF-48` through `PF-52` cover component tests, E2E, accessibility CI, visual regression, and a performance budget, and `PF-50` is what actually satisfies `docs/PLAN/17`'s Phase 5 WCAG 2.1 AA gate.

## Definition of Done Verification

- [x] Every component in `docs/UI-UX/07` has an owning task
- [x] All 21 screens in the amended `docs/UI-UX/08` have an owning task
- [x] Hosted auth screens covered — these appear in `docs/UI-UX/16` but were in no inventory
- [x] Public site pages from `docs/UI-UX/20` covered
- [x] Every page task carries a gate naming its backend dependency
- [x] The relationship to the existing console tasks is stated explicitly, not left ambiguous
- [x] `PROGRESS.md` and `README.md` updated in the same change

## What Did Not Work

Nothing failed outright, but the **task-count arithmetic was wrong twice**, in both directions. Phase 4b has eleven tasks (`P4B-00` through `P4B-10`), not ten, and the earlier total of 108 was simply miscounted — it should have been 124, and is now 177. Both times the error survived being written into five files before a `grep | wc -l` caught it. Derived numbers in prose should be computed, not typed.

## Follow-Ups and Open Questions

- The hosted authentication screens (`PF-39`–`PF-42`) have no dedicated `docs/UI-UX/` page specification. `docs/UI-UX/16` puts them in mobile scope and `P1-12` specifies their security properties, but nothing specs their layout the way `docs/UI-UX/18` specs the Users list. Their tasks are written from the security and form-UX requirements instead. Worth a `docs/UI-UX/` addition if these screens get design attention — they are, after all, the only screens every end user sees.
- `PF-38` (Rego editor) depends on Phase 4b, which may never run. Its accessibility requirement is the notable one: the editor component must be chosen for keyboard and screen-reader support **up front**, because discovering it fails during the `PF-50` audit means replacing the editor late.

## What to Watch

**The gates are only as good as the discipline behind them.** Phase F makes it easy to see all the frontend work at once, which is exactly what makes it tempting to build a screen before its endpoint exists. A screen built against a mock diverges from the API it eventually meets, and the divergence surfaces as integration pain much later. If a page task starts before its gate is `DONE`, that is the failure mode to name early.

**Foundation tasks are on the critical path for everything in Track D**, but they have no phase gate, so nothing forces them to happen first. `PF-02` slipping means every page task inherits an incomplete component library and starts inventing components locally — which is the exact inconsistency `docs/UI-UX/05`'s governance rule exists to prevent.

**The role-source badge (`PF-07`) renders only one of its two values until `PF-37`**, which is deep in Phase 4. A component whose second value is unexercised for months is a component whose second value is probably broken. It deserves a test and a workbench story from day one, not just a prop that exists.

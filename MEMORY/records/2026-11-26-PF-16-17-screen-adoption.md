# PF-16/17 — Projects, Audit Log and role form adoption

Date: 2026-11-26. Status: implementation slice; parent tasks remain WIP.

## Scope and references

Parallel continuation of the existing frontend foundation, following PF-16 and
PF-17 in `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`,
`docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/15-FORM-UX.md`,
`docs/UI-UX/13-ACCESSIBILITY.md` and the existing Management API contract.
No backend, API contract or authorization policy change.

## Changes

- Projects and Audit Log adopt `CollectionStateBoundary`: pending requests do
  not flash empty states; filtered empty results offer filter recovery; stale
  failure retains mounted content and request-local retry. A denied API response
  suppresses cached rows and open project creation/audit payload content.
- Audit errors use the existing failure conversion, including HTTP 403 and
  network failure treatment. Cursor/filter navigation remains intact.
- Role creation and editing use `FormSystem`/`useExplicitForm`, generated key
  validators, blur/submit validation, field-level server errors and first-error
  focus. Drafts survive failure and cancel/reopen; org/project remounts isolate
  drafts and late mutation completions. Successful writes remain successful if
  subsequent cache refresh fails.
- Added 16 fetch-backed page regressions across Projects, Audit Log and Roles;
  existing role assertions now follow the documented blur/submit behavior.
- Validation exposed an eager users query from the closed Granted Projects
  panel. It is now disabled until a grant is selected, so retrying the received
  grants list does not also fetch unrelated users.
- Registered the existing responsive grid fixture in the workbench and catalog
  contract. Browser target measurement waits for the modal's opening transform
  to finish before checking the minimum clickable dimensions.

## Verification

- Initial full component run: 507 passed, 1 failed. The failure identified the
  closed-panel users query described above; all 16 new page regressions passed.
- Initial Chromium workbench run: 7 passed, 6 failed. Five failures were missing
  responsive fixture registration; one measured the animated target too early.
- Final full component rerun: **516 passed across 38 files**, zero failures.
- Production build (including TypeScript) and lint passed after integration.
- Second browser run: 9 passed, 4 failed. The remaining assertions expected a
  shortened `grid-column` serialization; Chromium returned the complete
  `span N / span N`. Assertions now check start and end spans independently;
  the actual track counts and gutters already passed.
- Final full Chromium workbench rerun: **13 passed**, zero failures, including
  four viewport grids, width boundary, touch targets, form focus/review, typed
  confirmation, dialog geometry and reduced motion.
- `git diff --check` passed. Full backend tests were not run: this slice changes
  console presentation and tests, not authentication, authorization or data rules.
- Documentation validation passed: 188 documents, zero problems.

## Remaining and rollback

PF-16/17 remain WIP: remaining screen adoption, live API field-error acceptance,
manual accessibility review and shared redirect URI format validation remain
outstanding. PF-18 remains a gated foundation preparation, not page adoption.
No deployment or commit was performed. Rollback is limited to the page, fixture,
regression-test and execution-record changes in this slice; preserve pre-existing
uncommitted primitive work.

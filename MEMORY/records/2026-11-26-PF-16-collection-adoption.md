# PF-16 — Users, Roles and Project Grants collection adoption

Date: 2026-11-26. Status: implementation slice; parent PF-16 remains WIP.

## Scope and references

User requested parallel implementation. Three agents owned separate page files,
regression suites and implementation-chain documents. Root integrated HTTP error
normalization, verification and tracking.

References: task PF-16, `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`,
`docs/UI-UX/09-INTERACTION-DESIGN.md`, `docs/UI-UX/13-ACCESSIBILITY.md`,
`docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md`, and `docs/PLAN/11-TESTING.md`.
No backend, API contract or server authorization policy changed.

## Changes

- Users, Roles and Project Grants use `CollectionStateBoundary`. Loading withholds
  empty messages; stale failures retain mounted rows and drafts with request-local
  retry. Denial hides cached details, management actions and open dialogs.
- Users distinguishes an empty server search from an initially empty collection.
  Invitation panels defer their projects query until open and isolate drafts by
  organization. Existing bounded pagination and incomplete-result hints remain.
- Roles preserves the existing shared create/edit forms and their draft behavior;
  denied project context also hides cached role contents.
- Role, project-grant and user collection reads pass HTTP status to `asFailure`.
  A 403 takes precedence over an unknown envelope code; other call sites retain
  their existing normalization.
- Added 22 fetch-backed component regressions: seven Users, nine Roles, six
  Project Grants. These use the generated API client with mocked transport;
  they are not live API integration evidence.

## Verification

Focused suites passed: Users plus existing pagination/screens 27/27; Roles plus
existing form/role suites 31/31; Project Grants plus existing suite 21/21.
Integrated validation: `npm run check` passed lint, TypeScript and all **538
tests across 41 files**. `npm run build` passed. Chromium workbench checks
passed **13/13**. Documentation validation passed across **188 documents**;
`git diff --check` passed. The workbench checks verify shared primitives and
responsive fixtures, not live API page acceptance. Backend tests were not run
because this slice changes console presentation and client error classification
only. Existing jsdom canvas-not-implemented notices did not fail the suite.

## Remaining acceptance

Other collection screens still require adoption. Live API and manual
assistive-technology acceptance remain pending. No completed-task count is raised,
and no commit or deployment is performed.

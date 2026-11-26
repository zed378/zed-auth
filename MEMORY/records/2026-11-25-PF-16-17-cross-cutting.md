# PF-16 / PF-17 - Cross-cutting console state and forms

Implemented bounded foundation slices for the Phase F console.

- PF-16 adds a central collection-state priority resolver, loading skeleton region,
  distinct initial and filtered empty states, actionable network/server errors,
  permission denial, stale-data retry, and focused tests.
- PF-17 adds explicit-save form primitives with blur and submit validation,
  browser-autofill reads, generated role/permission validators, API detail-field
  mapping, first-invalid focus, live summaries, draft preservation, save-pattern
  guarding, and multi-step review support.
- Workbench entries and implementation-chain tables cover both slices.
- CI installs Chromium and runs the standalone workbench browser suite. The
  catalog inventory assertion checks every component family required by
  `docs/UI-UX/07` independently of the entries themselves.
- PF-19 now has a repeatable NVDA/Chrome procedure in
  `console/docs/accessibility-verification.md`; no manual audit is claimed.
- Granted Projects adopts the collection boundary, including hiding cached
  management content after permission denial. Project creation adopts the form
  system through the generated API client, with field mapping, org-isolated
  drafts and refresh-failure protection after committed creation. Their mocked
  HTTP regressions are separate from live API acceptance.

Both remain WIP until real screens adopt the primitives and CI/browser/manual
acceptance is recorded. No backend or authorization behavior changed.

Validation so far: 13 focused FormSystem/state tests passed, as did typecheck.
Full console and browser validation remains in progress with the testing agent.

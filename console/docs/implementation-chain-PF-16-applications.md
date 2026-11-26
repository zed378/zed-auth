# PF-16 — Applications collection states

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/09-INTERACTION-DESIGN.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` Applications tab.

| Step | Implementation |
|---|---|
| Design | Factual initial empty state with registration action for managers; shared actionable error and permission copy. |
| Component | `ApplicationsPage` uses `CollectionStateBoundary` around the shared applications table. |
| State | Permission refusal takes priority over cached rows. Definitive errors precede loading and empty. This tab has no search/filter control, so no filtered-empty state is invented. |
| Interaction | Retry refetches only applications, preserving table identity and open registration input on ordinary failures. |
| API Dependency | Existing generated Management API client, `useApplications`, and `useProjects`; existing query failure mapping treats HTTP 403 as permission refusal even for unknown error codes. |
| Loading | Announced busy region with the applications table skeleton, withholding the empty action until completion. |
| Error | Shared inline banner with stale table retained and request-local retry. No server detail is rendered in the permission treatment. |
| Empty | `No applications yet` with the first registration action when permitted. |
| Permission | Refusal from applications or project metadata hides cached project/application text, table and manager actions, registration dialog, SAML settings and issued-secret dialog. Drafts remain in memory within the same scope and are absent from the DOM during refusal. Server authorization remains independent. |
| Responsive | Existing table column priorities and shared modal token sizing are retained. |
| Accessibility | Shared loading announcement, error alert, permission guidance and modal focus behavior. |
| Scope | Organization/project identity keys a local page component, clearing drafts, selections and issued secrets when the scope changes. Late mutation callbacks belong to their original mounted scope. |
| Test | `applications.states.test.tsx` exercises pending-to-empty, stale retry, registration input retention, unknown-code HTTP 403, refusal during registration/SAML editing/issued-secret display, and project navigation through the generated API client with fetch fixtures. |

Validation: full console suite passes 572 tests across 46 files; lint, typecheck/build and 13 Chromium workbench checks pass. These fetch-backed fixtures are not live API or manual accessibility acceptance; PF-16 remains WIP. No backend or authorization policy changes.

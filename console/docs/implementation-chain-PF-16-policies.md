# PF-16 — Policies state handling

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`,
`docs/UI-UX/09-INTERACTION-DESIGN.md` (Error Recovery), and task PF-16.

| Step | Implementation |
|---|---|
| Design | Policy settings keep their last-known values during a transient refresh failure, while an authorization refusal removes protected settings and unsaved drafts from the rendered page. |
| Component | The initial request uses the shared `LoadingRegion` around the policy skeleton. Existing inline `ErrorState` handles absent-data failure and stale-data recovery. |
| State | A successful organization settings response seeds the form. Organization changes reseed it. Permission denial clears local draft, confirmation, save feedback, and cached settings from view. |
| Interaction | Retry refetches only the organization settings query. Saving uses the shared mutation invalidation policy. |
| API Dependency | Existing organization settings API and query; no API contract or authorization policy change. HTTP status is retained for permission classification. |
| Loading | Announced busy region with policy-form geometry; default settings remain the successful baseline. |
| Error | With no successful data, render only the error and retry. On a transient refresh error, retain the last-known policy values and draft and show an inline recovery banner. |
| Empty | Not applicable: a successful response is rendered against the settings defaults rather than as a collection. |
| Permission | HTTP 403 suppresses cached policy values and drafts and closes the confirmation. The server remains authoritative. |
| Responsive | Existing settings form remains constrained to its content width and wraps naturally. |
| Accessibility | Loading uses the shared busy announcement; errors use the shared alert and retry control. |
| Test | `policies.states.test.tsx` covers initial loading, refresh failure with retained settings/draft, request-local retry, and cached-content suppression after permission denial. |

Validation on 2026-11-26: policy suites pass 22 tests across the page and settings tests; targeted ESLint passes. The full console suite passes 584 tests across 49 files, production build and typecheck pass, and Chromium workbench checks pass 13/13. Live API and manual accessibility acceptance remain pending.

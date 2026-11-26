# PF-16 — Roles collection states

References: `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Project detail — Roles tab), `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/09-INTERACTION-DESIGN.md` (Error Recovery), and PF-16.

| Step | Implementation |
|---|---|
| Design | Keeps project-scoped role keys, permission disclosure, assignment counts, built-in restrictions and existing management actions. Distinguishes initial and filtered empty results. |
| Component | `RolesPage` uses `CollectionStateBoundary` around its shared table. Existing PF-17 create/edit forms and the delete confirmation remain in place. |
| State | Shared state priority withholds empty results during loading and keeps stale rows after a refresh failure. A permission failure from either roles or project context hides cached contents. |
| Interaction | Clear the search resets the local filter without fetching. Retry invokes only `roles.refetch()`; it does not navigate, reload, or call a scroll API. |
| API Dependency | Existing `useRoles` and `useProjects` with the generated Management API client; no route or server authorization policy changes. The shared query hook classifies HTTP 403 even without a recognized envelope. |
| Loading | Announced busy region with the roles-table skeleton. No initial or filtered empty state appears while the first request is pending. |
| Error | Native fetch failures receive connection guidance. Server failures receive factual retry guidance. A failed refresh retains the table node, search and mounted form draft. |
| Empty | Initial empty offers Define the first role to an authorized manager. A nonblank search offers Clear the search, including when the underlying collection is empty. |
| Permission | Generic administrator recovery guidance replaces cached role rows, project name, search, management buttons and any open create/edit/delete dialog. Drafts remain in scoped memory, absent from the DOM while denied. Server authorization remains authoritative. |
| Responsive | Existing table secondary columns and token-based form/modal layout remain. No breakpoint changes. |
| Accessibility | Shared busy announcements and alert messages; labelled search and the existing keyboard-accessible form and confirmation controls. |
| Test | `roles.states.test.tsx` uses fetch fixtures with the generated client to cover pending-to-empty, filter recovery, retained stale-table identity and request-local retry, draft retention, open-dialog suppression for create/edit/delete, unclassified HTTP 403, and project-context denial. Existing role and role-form suites protect validation and mutation behavior. |

Focused validation on 2026-11-26: all 31 tests passed across `roles.states.test.tsx` (9), `roles.forms.test.tsx` and `roles.test.tsx`, including the shared HTTP-status fallback. The integrating agent records the full console validation separately.

This is a bounded PF-16 adoption slice. Live API acceptance and manual assistive-technology checks remain pending; PF-16 stays WIP.

# PF-16 — Users collection states

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/09-INTERACTION-DESIGN.md` (Error Recovery), `docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` (Users List), and task PF-16.

| Step | Implementation |
|---|---|
| Design | Keeps the existing search, URL status filter, user links, status badges and invitation flow. Empty server searches offer filter recovery instead of suggesting a first invitation. |
| Component | `UsersPage` wraps its existing `Table` in `CollectionStateBoundary`. The loading skeleton uses the same columns and caption. |
| State | Shared priority hides empty results while pending, retains mounted stale rows after refresh failure, and suppresses cached contents after permission denial. |
| Interaction | Clear the search resets both search and URL status. Retry invokes only `users.refetch()` with the current search. The invitation panel fetches project information only while open. |
| API Dependency | Existing generated users endpoint and `useUsers` query; server search and bounded page collection remain intact. HTTP status normalization is handled by the shared query layer. No API contract or authorization policy changes. |
| Loading | Announced busy region and user-table skeletons; no initial empty flash. |
| Error | Inline retry banner with last-known-data notice when visible cached rows exist. A rejected fetch receives connection recovery copy. Search and open invitation drafts survive a list refresh failure. |
| Empty | Initial empty offers the first invitation to permitted administrators. Search or status filtered empty offers Clear the search even when the server returned zero users. |
| Permission | Cached names, email addresses, incomplete-result counts, filters, invitation actions and any open invitation draft disappear from the DOM. Server error detail is not displayed. API authorization remains authoritative. |
| Responsive | Existing secondary columns, wrapping filter layout and side-panel behavior remain. |
| Accessibility | Shared loading announcements, error alerts and keyboard-accessible recovery controls apply. Existing input labels and panel focus management remain. |
| Test | `users.states.test.tsx` exercises deferred loading, server-search and status recovery, request-local stale retry, network rejection, cached-content denial, unclassified HTTP 403 denial, and draft retention followed by denied-access suppression. Existing `pagination.test.tsx` verifies later-page users and incomplete collection disclosure. |

The invitation panel is keyed to the organization, and its open state is scoped to that organization, so navigation cannot expose another organization's draft. A denied list hides the panel without discarding an unsaved draft from memory. The existing invitation form's validation and later-phase role-assignment work remain outside this PF-16 slice.

Validation on 2026-11-26: users, pagination and existing screen suites passed 27 tests across three files, including all seven new users regressions. Diff whitespace checks pass. Full console checks remain with the parent integration run. Fetch fixtures exercise the real generated client; these tests do not claim live API or manual accessibility acceptance. PF-16 remains WIP for other screens and remaining acceptance.

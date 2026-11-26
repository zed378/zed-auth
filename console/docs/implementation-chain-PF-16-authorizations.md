# PF-16 — Authorizations collection states

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/09-INTERACTION-DESIGN.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, and the existing P2-12 implementation chain.

| Step | Implementation |
|---|---|
| Design | Search chooses a person; this screen does not claim to list a project's entire authorization roster. |
| Component | Search results, selected-user grants, and assignment role choices use `CollectionStateBoundary`. The page uses `PermissionDeniedState` for authoritative read denial. |
| State | Loading precedes empty; definitive failure precedes loading. Refetch failures retain last-known results, access and an open role selection. |
| Interaction | Filtered empty results offer Clear the search. Retry refetches only the failing users, grants or roles query. |
| API Dependency | Existing generated Management API reads and mutations remain unchanged. HTTP 403 is normalized by the query layer. No API or authorization policy is added. |
| Loading | Search rows retain their skeleton geometry and Searching announcement; selected-user access and role choices have their own loading announcements. |
| Error | Stale results stay mounted under the inline recovery banner. An absent grants response cannot become a false No access statement. Failed role refresh prevents submission until retry succeeds. |
| Empty | An unfiltered successful empty search invites typing; a filtered empty search uses the shared factual filtered state. A successful empty grant list retains the explicit No access explanation. |
| Permission | A denied projects, users, roles or selected-user grants read hides the whole resource context, cached names, search and open assignment/revoke dialogs. Server messages are not displayed. |
| Responsive | Existing wrapping controls and tokens are retained. No fixed viewport layout is introduced. |
| Accessibility | Shared busy regions announce loading; inline failures use alerts. Existing labelled search, keyboard selection and dialog focus management remain. |
| Test | `authorizations.states.test.tsx` exercises fetch-backed pending/filter recovery, stale users/grants retry, retained role selections, cached-data/open-dialog suppression for four HTTP 403 sources, and project navigation isolation. The existing empty-search assertion follows shared copy. Root runs the full validation suite. |

The page remounts its local search, selected user and dialogs when organization or project changes. These tests use generated API calls with fetch fixtures; live API and manual accessibility acceptance remain pending. PF-16 remains WIP.

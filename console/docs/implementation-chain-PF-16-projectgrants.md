# PF-16 — Project Grants collection states

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/09-INTERACTION-DESIGN.md`, `docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` (Project Grants Tab), and `docs/UI-UX/04-USER-FLOWS.md` Flow 2.

| Step | Implementation |
|---|---|
| Design | Keeps named shared/withheld roles, partner IDs, holder counts, status and the existing consequence previews. |
| Component | `ProjectGrantsPage` uses `CollectionStateBoundary` around its shared `Table`. |
| State | Pending requests announce loading with a table skeleton. Definitive failure takes priority over empty; permission denial suppresses cached contents. |
| Interaction | Retry calls only `grants.refetch()` without reloading or navigating. An ordinary list failure retains the mounted table and open creation draft. |
| API Dependency | Existing organization/project-scoped grants query and generated Management API client. No endpoint or authorization policy changes. |
| Loading | Table skeleton with the same columns and a polite loading announcement; no first-grant action flashes before completion. |
| Error | A stale list remains visible with the shared last-known-data banner. Network failures use connection recovery copy. |
| Empty | A genuinely empty collection offers Create the first grant to an eligible manager. This screen has no search filter, so it has no filtered-empty branch. |
| Permission | A denied list hides partner rows, cached project breadcrumb text, management actions, open creation drafts and revocation confirmations; server error detail stays hidden. Drafts remain in memory while hidden. Independent server authorization remains authoritative. |
| Responsive | Existing fluid layout and wrapping role lists remain. No breakpoint changes. |
| Accessibility | Shared loading announcements and alert semantics; existing labelled creation controls and typed revocation confirmation remain. |
| Test | `projectgrants.states.test.tsx` covers pending-to-empty, stale table preservation and request-local retry, connection failure with retained creation draft, cached-content denial with an unclassified 403 envelope, and denial while creation or revocation is open. Existing `projectgrants.test.tsx` checks role subsets, consequence previews, typed confirmation and mutation payloads. |

Validation on 2026-11-26: the focused state and existing Project Grants suites passed all 21 tests. Root runs the integrated console checks. This is bounded PF-16 page adoption; live API acceptance, browser/manual accessibility acceptance, other page adoption and full PF-16 completion remain pending.

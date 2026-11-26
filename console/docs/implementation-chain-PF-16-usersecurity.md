# PF-16 — User security tab states

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, `docs/UI-UX/09-INTERACTION-DESIGN.md`, `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`.

| Step | Implementation |
|---|---|
| Design | Sessions and member/self MFA use the shared collection state priority. Existing revoke, reset, enrolment and recovery behavior is retained. |
| Component | `SessionsTab`, `MemberFactors` and `MyFactors` wrap their contents and confirmations in `CollectionStateBoundary`. |
| State | Pending reads announce loading without empty flashes. Tabs wait for the current identity before choosing a self/member read, avoiding an eager member request for the current user during auth restoration. Definitive failures show an inline banner; successful cached contents remain mounted after refresh failure. MFA counts the returned response as one record because recovery metadata, availability and mandate guidance remain meaningful with zero factors; `FactorTable` retains the factual factor-empty guidance. |
| Interaction | Retry refetches only the failed sessions or MFA read. Switching organization/user scope remounts the tab's local action state. |
| API dependency | Existing generated management client and existing self/member routes. Sessions read normalizes HTTP status through `asFailure`; MFA hooks already do so. |
| Loading | Announced skeletons for initial sessions and MFA reads; action spinners stay local. |
| Error | Stale tables remain mounted with last-known-data guidance. No cached response means error and retry without management controls. |
| Empty | Sessions uses shared factual empty state; member and self MFA keep their distinct existing no-factor guidance and applicable setup controls. These tabs have no search/filter operation. |
| Permission | HTTP 403 suppresses cached devices, factor labels, recovery metadata, actions and open confirmations/enrolment/recovery dialogs. No retry or raw server detail renders. API authorization remains authoritative. The parent UserDetailPage detail read/header is outside this slice. |
| Responsive | Existing fluid table, skeleton and dialog layouts and tokens are retained. |
| Accessibility | Busy region and polite loading announcement; generic permission recovery; existing labelled action buttons and confirmation focus handling. |
| Test | `usersecurity.states.test.tsx`: fetch-backed pending/empty, self-read selection after auth restoration, stable cached-table stale failure with request-local retry, unknown-code HTTP 403 including open confirmations, and scope changes. Generated API client stays in the path. Focused suite passed 12/12; root runs full validation. |

PF-16 remains WIP. Live API and manual accessibility acceptance are pending; these fixtures do not constitute a live permission/authorization audit. No backend or authorization policy changes are introduced.

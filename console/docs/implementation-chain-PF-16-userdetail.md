# PF-16 — User detail header and profile states

References: `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`.

| Step | Implementation |
|---|---|
| Design | The user detail endpoint is a single-resource read: pending, unavailable/error, permission refusal, and loaded profile are distinct. Cached profile fields remain useful after a transient refresh failure, but never after HTTP 403. |
| Component | `UserDetailPage` owns the scoped detail query and header; `ProfileTab` presents pending, missing/unavailable, error, and profile content. Existing tab behavior remains unchanged. |
| State | Initial profile load uses an announced skeleton. A failed read without data shows an actionable error. HTTP 403 is normalized by status even when the error code is unknown; a 404 follows the API client's existing non-disclosure treatment. |
| Interaction | Retry refetches the current user's detail query. A permission refusal removes stale profile fields and closes any open deactivation dialog. |
| API dependency | Existing `GET /v1/organizations/{org_id}/users/{user_id}`. No API or authorization behavior changed. |
| Loading | Profile skeleton is inside a polite busy region; blank fields are never presented as a successful profile. |
| Error | A transient failure with no cached profile offers retry. With cached data it shows last-known-data guidance and preserves the profile layout while retrying. A null/absent result cannot become a blank profile. |
| Empty | User detail has no successful empty profile; missing/unavailable users render an error/access state rather than empty field values. |
| Permission | HTTP 403 suppresses cached header and profile values, deactivation control, retry, and any open confirmation. Raw API error text is not rendered. |
| Responsive | Existing fluid detail grid and shared state styles are retained. |
| Accessibility | Loading is announced; errors use the shared alert and retry control; confirmation dismissal follows permission changes. |
| Test | `userdetail.states.test.tsx`: pending/loaded profile, missing-user response, server error without data, stale-data retention and scoped retry, and 403 suppression with open-dialog dismissal. |

PF-16 remains WIP pending remaining screens and acceptance. These fetch-backed UI tests do not replace live API or manual accessibility acceptance.

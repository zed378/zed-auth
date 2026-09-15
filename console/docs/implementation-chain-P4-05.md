# Implementation chain — P4-05

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for the Project Grants tab.

"Not applicable" is an acceptable answer; silence is not.

---

## Screen: Project detail — Project Grants tab

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/18` § Project Grants Tab, `docs/UI-UX/04` Flow 2, `docs/UI-UX/08`'s row naming the typed-confirmation variant. One deviation, decided rather than drifted: Flow 2's "select target organization (search/select)" is **an organization ID entered by hand** (ADR-026), because a search would let any administrator enumerate every organization on the instance |
| **Component** | Shared `Table`, `Badge`, `Button`, `Modal`, `ConfirmDialog` (typed variant), `RoleList`, and `ProjectNav` gaining a fourth link. No new shared component |
| **State** | Loading, loaded, refused, genuinely empty, creating (entering; no roles ticked; invalid ID; own ID; ready to review), reviewing, create refused by the server (shown on the review step, choices kept on Back), revoking with nobody affected, revoking with holders (locked until typed), revoke refused, revoked grant (listed, ended, no control), no permission to manage |
| **Interaction** | Create opens a two-step modal: choose, then read back and confirm. Ticking a role rewrites the summary sentence immediately (`docs/UI-UX/11`). Review is disabled until the ID is well-formed and at least one role is ticked. Revoke opens a confirmation stating the holder count; when it is above zero, the partner's exact name (case-sensitive) must be typed first |
| **API Dependency** | `GET`/`POST .../projects/{project_id}/grants`, `DELETE .../grants/{grant_id}`, `GET .../roles` for the full role set, `GET .../projects` for the breadcrumb. `holder_count` was added to `ProjectGrant` by this task (migration 036) because the console cannot see the partner's users and the blast radius has to come from the server |
| **Loading** | Three skeleton rows in the table. Inside the form, the role list says "Loading this project's roles…" rather than showing an empty list, which would read as "this project has no roles" |
| **Error** | `ErrorState` in place of the table, with retry only where it can help. A refused create renders the server's `details[0].issue` on the review step, and Back returns to the form with the ID and ticks intact. The server answers an unknown, suspended and self organization with one sentence (P4-01 A-5), and the console repeats it rather than guessing which |
| **Empty** | "No Project Grants yet" with a create action. A project with **no roles** is a different emptiness inside the form: it says there is nothing to share and where to define a role, instead of offering an empty checklist |
| **Permission** | Route and controls gated to `ORG_ADMIN`/`ORG_OWNER`/`INSTANCE_OWNER`, which satisfy `PROJECT_OWNER` at project scope (the policy table). A `PROJECT_OWNER` who is not an organization administrator cannot reach the route, the same limitation every project tab has today. UX only; the API refuses independently |
| **Responsive** | Organization and roles cells stack their second line (ID; "Not shared") rather than adding columns. Role badges wrap (`flex-wrap`) and are never truncated, per `docs/UI-UX/18`'s tablet rule. Below 768px is not supported for this admin screen |
| **Accessibility** | Table caption; roles in a real `fieldset`/`legend`; each checkbox labelled by its key and name; the "Shared/Not shared" word is `aria-hidden` because the checkbox state already says it; the live summary is `aria-live="polite"`; ID errors use `aria-invalid` + `aria-describedby` and replace the help text; the typed-confirmation input takes focus on open (`docs/UI-UX/18` § Keyboard); axe runs over the table, the form, the review step and the revoke dialog |
| **Test** | 15 tests in `src/pages/projectgrants.test.tsx`; 6 mutations (typed confirmation, reset on cancel, focus, warning colour, withheld roles, review step) each turned a test red. `e2e/projectgrants.spec.ts` drives create and revoke against the real service and checks the API both times |

---

## Colour

- `color-warning`: only the "This Project Grant has no roles selected yet" notice, `docs/UI-UX/05`'s own example.
- `color-danger`: the row's Revoke control (`danger-text`) and the dialog's confirm. The form's field error uses the error text colour the other forms use; it is not an action.
- Status is carried by badge text ("Active", "Revoked"), with colour as reinforcement only.

## A shared component fixed

`ConfirmDialog` had two defects that only its first typed-confirmation caller could expose:

1. **What was typed survived a cancel.** Reopening the dialog, for the same row or another with the same name, arrived already unlocked. The friction had been spent before the new consequence was read.
2. **The typed input never received focus.** `Modal` focuses its panel on open, so a keyboard user had to find the field.

Both are fixed in the component, not in this page, and both have a test here that fails without the fix.

## What this screen cannot show yet

`holder_count` is zero for every grant until `P4-02` lets a partner assign delegated roles. The typed-confirmation branch is therefore covered with a stubbed count in the component tests. The E2E suite covers the path a real grant takes today and should gain the typed branch in `P4-02`.

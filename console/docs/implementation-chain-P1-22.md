# Implementation chain — P1-22

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for every screen and every new component in `P1-22`.

The rule that makes this worth writing down: **"not applicable" is an acceptable answer; silence is not.** The failure this prevents is a screen whose Design and Interaction columns were specified and whose Loading, Error, Empty, Permission and Accessibility were invented by whoever implemented it — which is how two screens end up with two answers to the same question.

---

## Screen: Organization Overview

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/18` § Organization Overview, in its stated order: attention banner (conditional), three KPI cards, activity trend. Objective is "operational status in ≤5 seconds", so nothing above the fold is decorative |
| **Component** | `Skeleton`; KPI card is local to this screen — it appears nowhere else, and a design-system component with one caller is a generalisation nobody asked for |
| **State** | Loading (per card, independently), loaded, per-card error, org-name loading, activity empty, activity error, banner present / absent |
| **Interaction** | KPI cards link **only** where a destination exists (`docs/UI-UX/18`); the activity trend is deliberately not clickable and carries no hover affordance. Per-card retry is a button inside the card |
| **API Dependency** | `GET /v1/organizations/{org_id}`, `.../projects`, `.../users`, `.../events` — four independent queries, so one failure does not blank the page |
| **Loading** | Skeletons matching each element's geometry (`docs/UI-UX/09`): a heading-sized bar for the org name, a number-sized block per KPI, a block for the trend |
| **Error** | **Per card**, inline, with retry — `docs/UI-UX/18` is explicit that a failed KPI fetch must not produce a full-page error. The trend degrades to a sentence saying the rest of the page is unaffected |
| **Empty** | An organization with no activity in seven days gets a sentence, not a chart of zeros. Zero users or zero projects render as `0` — a real answer, not an empty state |
| **Permission** | Any signed-in member. The counts come from endpoints that require `ORG_ADMIN`; a caller without it sees the per-card error, which is the honest rendering of a refusal |
| **Responsive** | 3 columns at ≥1024px, 2 per row at 768–1023px, and below 768px the shell replaces the layout entirely — so there is no single-column case to design (`docs/UI-UX/12`) |
| **Accessibility** | One `h1`; `section` elements labelled by their headings; the KPI grid has a visually hidden heading so it is a named region; the trend is a real `table` with a caption and a per-row number, because a bar alone is unreadable; the banner is `role="status"` |
| **Test** | Component tests for the four states and the conditional banner; the E2E suite covers landing after login |

---

## Screen: Project list

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Projects: search, primary action, table, in that order |
| **Component** | Shared `Table`, `Button`, `Modal`. **No table of its own** (`docs/UI-UX/08` § Cross-Screen Requirements) |
| **State** | Loading, loaded, error, genuinely empty, filtered to empty, creating, create failed, duplicate name caught before submit |
| **Interaction** | Search filters as you type, no submit; the row name and a row action both lead to the project's applications; create opens a modal |
| **API Dependency** | `GET .../projects?page_size=100`; `POST .../projects` with `{name}` |
| **Loading** | Three skeleton rows inside the table, header intact — not a spinner over the table (`docs/UI-UX/07` § Table states) |
| **Error** | `ErrorState` in place of the table, distinguishing network / server / permission; retry offered only where retrying can work |
| **Empty** | **Both kinds** (`docs/UI-UX/14`): "No projects yet" with a create action, versus "No projects match that search" with a clear-search action. Showing the first to somebody who typed a typo is the failure this prevents |
| **Permission** | The create action is hidden without `ORG_ADMIN`/`ORG_OWNER`/`INSTANCE_OWNER`, and the route itself is guarded. Both are UX; the API refuses regardless (`docs/PLAN/08`) |
| **Responsive** | The `Created` column is `secondary` and drops below 1024px; the table scrolls horizontally only as a last resort |
| **Accessibility** | Table `caption`; a labelled search input; modal traps focus and restores it; the duplicate-name error uses `aria-invalid` and `aria-describedby`, and **replaces** the helper text rather than stacking under it (`docs/UI-UX/07` § Form Field) |
| **Test** | Component tests for both empty kinds, the loading rows, the error state, and the duplicate-name guard |

---

## Screen: Applications tab

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Applications tab, under a breadcrumb reflecting the real hierarchy |
| **Component** | Shared `Table`, `Badge`, `Button`, `Modal`, and one new component: `ClientSecretModal` |
| **State** | Loading, loaded, error, empty, creating, create failed with a per-field reason, secret issued, secret acknowledged |
| **Interaction** | Type selection changes the helper text to say whether a secret will be issued — before the user commits, not after |
| **API Dependency** | `GET`/`POST .../projects/{project_id}/applications` |
| **Loading** | Skeleton rows, as the project list |
| **Error** | The server's per-field `details[0].issue` where there is one — a wildcard redirect URI comes back with a sentence explaining why exact matching means a wildcard matches nothing, which is more useful than "invalid" |
| **Empty** | Genuinely empty only; this list has no filter, so the filtered case is **not applicable** rather than unconsidered |
| **Permission** | As the project list |
| **Responsive** | `Redirect URIs` is `secondary` and drops below 1024px — it is the longest column and the least needed at a glance |
| **Accessibility** | Breadcrumb is a labelled `nav` with `aria-current="page"`; badges carry text, never colour alone (`docs/UI-UX/07` § Badge rule) |
| **Test** | Component tests for the table states; the secret modal has its own below |

---

## Component: `ClientSecretModal`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Applications tab: shown once, copy action, unmistakable warning |
| **Component** | Composes `Modal` and `Button` |
| **State** | Open with a secret, copied, acknowledged, closed. There is no "reopen": the secret is gone from the page and from the service |
| **Interaction** | **Not dismissible by Escape or by clicking away**, and `Done` is disabled until a checkbox is ticked. This is the design system's one deliberate friction point outside a destructive confirmation, and it is here because an accidental dismissal costs a rotation and a redeploy |
| **API Dependency** | None. It renders what the `201` already returned — there is no endpoint that could fetch it again, which is the whole point (`P1-18`) |
| **Loading** | Not applicable: nothing is fetched |
| **Error** | Not applicable for the dialog itself. A clipboard write that fails leaves the value selectable in a readable input, so the copy button is a convenience rather than the only route |
| **Empty** | Not applicable — it is not opened for a public client, because a warning about a secret that does not exist is a warning about nothing |
| **Permission** | Whoever could create the application. It shows nothing they did not just cause |
| **Responsive** | Single column at every width; the secret input wraps rather than truncating, because a truncated credential is one somebody copies wrong |
| **Accessibility** | `role="dialog"`, `aria-modal`, labelled by its title, focus taken on open and restored on close, focus trapped while open; the warning is `role="alert"`; the copy confirmation is `aria-live` so it is not a visual-only acknowledgement |
| **Test** | Component tests for the acknowledgement gate, the undismissability, and the secret being rendered readably |

---

## Component: `Table`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/07` § Table anatomy |
| **Component** | The design-system table itself. Every list in the console composes from this one |
| **State** | Loading, ready, error, genuinely empty, filtered to empty |
| **Interaction** | Row actions are rendered by the caller in a trailing cell |
| **API Dependency** | None — it renders what it is given |
| **Loading** | Three skeleton rows, header intact, so nothing moves when data arrives |
| **Error** | `ErrorState` in place of the table. `docs/UI-UX/07` asks for last-known-good data where there is some; on a first load there is none, and a blank table under an error banner reads as "there are none" |
| **Empty** | Delegates to `EmptyState`, which takes `filtered` — the distinction is structural rather than per-caller |
| **Permission** | Not applicable: the table has no permissions of its own |
| **Responsive** | Columns marked `secondary` are hidden below 1024px; the wrapper scrolls horizontally as a fallback |
| **Accessibility** | `caption` for the accessible name; `scope="col"` on headers; the actions column has a visually hidden header, because a column with no name is a column a screen reader cannot describe |
| **Test** | Exercised through the screens that use it, and directly for the state matrix |

---

## Component: `Button`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/07` § Button; three variants plus `danger-text` for the "secondary with danger-coloured text" case the specification's rule describes |
| **Component** | The design-system button |
| **State** | Default, hover, focus, disabled, loading |
| **Interaction** | A loading button is also disabled — a second click is a second request, and on a create that is two objects |
| **API Dependency** | None |
| **Loading** | The label is replaced by a spinner and kept in the accessibility tree, and the button holds its width so nothing beside it moves |
| **Error** | Not applicable |
| **Empty** | Not applicable |
| **Permission** | Not applicable — callers decide whether to render it |
| **Responsive** | Identical at every width |
| **Accessibility** | `aria-busy` while loading; focus ring uses `color-accent`; the loading state keeps an accessible name, so it never becomes an unnamed button |
| **Test** | Covered through the screens; the loading name is asserted directly |

---

## Component: `Badge`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/06`'s status table |
| **Component** | The design-system badge |
| **State** | Four tones. **`color-danger` is not among them**: a status is a statement about what something is, and danger is reserved for destructive actions (`CLAUDE.md`). A deactivated user is not a destructive action |
| **Interaction** | None; it is not interactive |
| **API Dependency** | None |
| **Loading / Error / Empty** | Not applicable |
| **Permission** | Not applicable |
| **Responsive** | Identical at every width |
| **Accessibility** | Text always present — a badge is never the sole carrier of critical information (`docs/UI-UX/07` § Badge rule) |
| **Test** | Asserted through the Applications tab, where the secret state is a badge |

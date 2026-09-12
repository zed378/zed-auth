# Implementation chain — P2-11

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for the Roles tab and for the one new component it introduces.

The rule that makes this worth writing down: **"not applicable" is an acceptable answer; silence is not.** The failure it prevents is a screen whose Design and Interaction columns were specified and whose Loading, Error, Empty, Permission and Accessibility were invented by whoever implemented it.

---

## Screen: Project detail — Roles tab

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Project detail — Roles tab: "Create/edit role, define permission keys", using Table and form. Under the same breadcrumb as the Applications tab, with the project sub-navigation between them |
| **Component** | Shared `Table`, `Badge`, `Button`, `Modal`, `ConfirmDialog`, and one new component: `ProjectNav`. **No table of its own** (`docs/UI-UX/08` § Cross-Screen Requirements) |
| **State** | Loading, loaded, error (network / server / refusal), genuinely empty, filtered to empty, creating, editing, create/edit failed with the server's reason, deleting, delete refused by the server, no permission to manage |
| **Interaction** | Search filters as you type, no submit. Create and Edit open the same modal; the key field is typed on create and read-only on edit. Delete opens a confirmation carrying the grant count. A built-in role offers no delete control and says why in its place |
| **API Dependency** | `GET`/`POST .../projects/{project_id}/roles`, `PATCH`/`DELETE .../roles/{role_id}`, plus `GET .../projects` for the breadcrumb name. All through the generated client |
| **Loading** | Three skeleton rows inside the table, header intact — not a spinner over the table (`docs/UI-UX/07` § Table states) |
| **Error** | `ErrorState` in place of the table, distinguishing network / server / permission; retry offered only where retrying can work. A failed save renders the server's `details[0].issue` where there is one — a reserved key comes back with a sentence naming the reason, which beats "invalid". A refused delete stays in the dialog rather than closing over the failure |
| **Empty** | **Both kinds** (`docs/UI-UX/14`): "No roles yet" with a create action, versus "No roles match that search" with a clear-search action. A third emptiness is distinguished inside the table — a role with **no permissions** reads "None — a label only", because the API permits it deliberately and a blank cell would read as missing data |
| **Permission** | The create, edit and delete controls are hidden without `ORG_ADMIN`/`ORG_OWNER`/`INSTANCE_OWNER`, and the route is guarded. Both are UX; the API refuses independently on every request (`docs/PLAN/08`, `CLAUDE.md`) and a hidden button is never the control |
| **Responsive** | `Origin` is `secondary` and drops below 1024px — it is the least load-bearing column, and the same information is repeated in the row's own delete affordance. The search input is `max-w-sm` so it does not stretch across a wide viewport |
| **Accessibility** | Table `caption`; a labelled search input; the permission **count** carries the full list in a screen-reader-only span and in `title`, so the number is not a dead end; the key field on edit is `readOnly`, **not** `disabled`, so it stays focusable and its explanation is announced; errors use `aria-invalid` + `aria-describedby` and **replace** the helper text rather than stacking under it (`docs/UI-UX/07` § Form Field); the confirmation button carries the verb ("Delete role"); axe runs over the table and over the open form |
| **Test** | 17 tests in `src/pages/roles.test.tsx` — both empty kinds, the refusal, the four validation rules, the read-only key, the server's own error text, the grant count in both its forms, a refused delete, the permission-hidden case, and axe over two states. Five controls were reverted one at a time and each turned its own test red |

---

## Component: `ProjectNav`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` describes the project detail as tabs — Applications, Roles, and Authorizations from `P2-12`. `docs/UI-UX/07` specifies **no tab component**, which is recorded as `PG-34` rather than resolved by inventing one |
| **Component** | New, and deliberately small: a labelled `nav` of `NavLink`s. It is not an ARIA tablist, because these are not tabs — each is a route with its own data, its own loading state and its own back-button behaviour. `role="tab"` would promise arrow-key navigation, a single tab stop and an `aria-controls` panel that this screen does not implement, which is worse for a screen-reader user than plain links (`docs/UI-UX/13`) |
| **State** | Active / inactive per link. No loading or error state — it renders from the route, not from data |
| **Interaction** | Ordinary navigation. The Applications link is `end` so it does not stay active on `/roles` |
| **API Dependency** | None |
| **Loading** | Not applicable — nothing is fetched |
| **Error** | Not applicable |
| **Empty** | Not applicable — the set of tabs is fixed in code |
| **Permission** | Not applicable in Phase 2: both destinations are guarded by the same roles, so a link that leads somewhere refused cannot arise here. `P2-12` adds a third and should re-answer this |
| **Responsive** | A flex row that wraps; each target is at least 44px tall at the default type scale (`docs/UI-UX/12` § touch targets) |
| **Accessibility** | `aria-label="Project sections"` so the region is named and distinct from the breadcrumb `nav`; `aria-current="page"` comes from `NavLink`; active state is carried by a border **and** by font weight, never colour alone (`docs/UI-UX/06`); a visible focus ring on each link |
| **Test** | Covered indirectly by both screens' axe passes. It has no behaviour of its own to test beyond what React Router provides |

---

## One shared component changed

`Modal`'s focus effect depended on the identity of its `onClose` prop. A caller that passes an inline arrow — which is any caller closing over its own state — hands it a new function on every render, so the effect tore down and re-ran after **every keystroke**, and its cleanup calls `focus()`. Typing in a field moved focus back to the dialog after the first character.

The two existing callers happened to be safe because their `onClose` was owned by a component that did not re-render on each keystroke. This screen's was not, and the symptom was a form that accepted one character per field.

A shared component that breaks depending on how the caller spells a prop is a trap rather than an API, so the dependency was removed (the callback is held in a ref, updated in its own effect) rather than documented as a caller's obligation.

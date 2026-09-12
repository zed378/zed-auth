# Implementation chain — P2-12

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for the Authorizations tab, for the User detail Grants tab it mirrors, and for the one new component both depend on.

"Not applicable" is an acceptable answer; silence is not.

---

## Screen: Project detail — Authorizations tab

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Project detail — Authorizations tab: "Search user, assign/revoke role", with a search input, a table and the role-source badge. `docs/UI-UX/04` Flow 2/3 is the source of the role-source requirement |
| **Component** | Shared `Button`, `Badge`, `ConfirmDialog`, `Skeleton`, `ErrorState`, `ProjectNav`, and one new: `RoleSourceBadge`/`RoleList`. The search results are a `ul` of buttons rather than a `Table` — they are choices, and a table with one action column implies columns worth scanning |
| **State** | Untouched (no search yet), searching, no matches, results, a user selected, their grants loading, grants failed, **no access**, access held, assigning, assignment refused, revoking, revocation refused, no roles defined in the project at all, caller cannot manage |
| **Interaction** | Search debounced 250ms server-side; the input itself is never debounced. Picking a user replaces the results with their access. Assignment is a checkbox list of the project's roles with permission keys beneath each. Revocation is a separate, explicitly total action |
| **API Dependency** | `GET .../users?search=`, `GET .../users/{user_id}/grants`, `POST .../users/{user_id}/grants`, `PATCH`/`DELETE .../grants/{project_id}`, plus `.../roles` and `.../projects`. **Search-then-act is the API's shape**: grants are exposed per user, and no endpoint lists a project's roster — recorded as `PG-36` rather than faked with a request per search result |
| **Loading** | Skeleton rows with the geometry of the result rows, inside a `min-h-48` region so results appearing do not push the page down (step 2's "does not shift layout"). `aria-busy` + a visually hidden "Searching" |
| **Error** | `ErrorState`, distinguishing network / server / refusal, for the search and for the grants read separately — one failing does not blank the other. A refused write stays **in its dialog** with the server's own sentence, because closing over a failure leaves the administrator believing it worked |
| **Empty** | Three distinct kinds. An untouched search box is **not an empty state** — it says "type an address above", because "no users" would be a false statement about the organization. A search matching nobody names what was searched for. And a user with no grant gets "No access" plus the reason it is not a bug (`docs/PLAN/08` § Least Privilege) |
| **Permission** | Assignment and revocation controls are hidden without `ORG_ADMIN`/`ORG_OWNER`/`INSTANCE_OWNER`; the route is guarded. Both are UX. The API refuses independently, including the self-grant refusal that no scope can express |
| **Responsive** | The search column is `max-w-md`; the header row and the action row both `flex-wrap`; role badges wrap rather than overflow. Nothing here is a table, so no column-dropping decision arises |
| **Accessibility** | The selected user's panel is a labelled `section`; result counts are `aria-live="polite"` so a screen-reader user hears the list change; each result is a real `button` with a visible focus ring; the assignment list is a `fieldset` with a `legend`; the revoke button carries the verb; the delegated badge's origin is in the accessibility tree, not only in a `title` |
| **Test** | 20 tests in `src/pages/authorizations.test.tsx` covering every state above, plus 3 Playwright tests asserting assignment and revocation against **both** the screen and the Management API |

---

## Screen: User detail — Grants tab

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § User detail — Grants tab: "View all project roles this user holds", table plus role-source badge. Step 7 asks for it to mirror the Authorizations tab from the same components, and `docs/UI-UX/08`'s cross-screen rule is why: two screens rendering grants two ways is how one ends up without the badge |
| **Component** | Shared `Table` and the same `RoleList`. Nothing new |
| **State** | Loading, loaded, error, no grants at all |
| **Interaction** | Read-only. The project name links to that project's Authorizations tab, which is where granting happens — a role only means something inside a project, and that is where the project's roles and their permission keys are in front of you |
| **API Dependency** | `GET .../users/{user_id}/grants` and `.../projects` for the names. The project **name**, never its id: an id is not something an administrator recognises |
| **Loading** | Skeleton rows in the table |
| **Error** | `ErrorState` in place of the table, with retry where retrying can work |
| **Empty** | "No grants yet" from the shared empty state, **plus a sentence saying what that means** — no implicit or default role, so every check is denied. The count alone would be ambiguous with a broken load |
| **Permission** | The tab is inside a route already guarded to organization administrators. It offers no writes, so there is nothing further to gate |
| **Responsive** | Two columns; neither is droppable, because naming the project and naming the roles are both the point |
| **Accessibility** | Inherited from `Table` — caption, real header cells. The tab itself is part of the existing ARIA tablist, which this task turned from "(later)" to real |
| **Test** | 2 tests in `authorizations.test.tsx`, plus the Playwright test that grants through the API and reads it here |

---

## Component: `RoleSourceBadge` / `RoleList`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/06` § Role-Source Visual Treatment: a plain badge for a direct grant; for a delegated one, the badge plus a small link mark and the source organization "on hover/tap, so the origin is always one interaction away" |
| **Component** | New. Not a variant of `Badge`, because `Badge` takes a `string` child and this needs a mark and a hidden sentence beside the text |
| **State** | Direct, delegated. `RoleList` adds the empty case |
| **Interaction** | Hover or long-press reveals the source. No click target — the origin is information, not a destination, until Phase 4 has a Granted Projects screen to point at |
| **API Dependency** | None; it renders what a grant already carries |
| **Loading** | Not applicable |
| **Error** | Not applicable |
| **Empty** | `RoleList` with no roles renders "No access" as a sentence, not a blank cell — a blank reads as missing data |
| **Permission** | Not applicable |
| **Responsive** | Badges `flex-wrap`; the mark is `shrink-0` so it never squashes |
| **Accessibility** | **The delegated origin is duplicated as screen-reader text.** `docs/UI-UX/06` says "on hover/tap", and hover is an affordance for a mouse and nothing else. The mark is `aria-hidden` because the text beside it already says what it means |
| **Test** | 2 tests directly on the component, including the delegated branch — which is **unreachable in Phase 2** (the database refuses a non-null `project_grant_id` until `P4-01`) and is built and tested now so Phase 4 turns it on rather than auditing every screen that shows a role |

---

## What this task did not build

**A project's roster.** "Who has access to this project?" is a reasonable question and the API cannot answer it — grants are exposed only under a user. The screen is honest about that rather than fanning out a grants request per search result, which would be `N+1` requests to produce a list that is still not the roster (it would only cover users matching the search). Recorded as `PG-36`.

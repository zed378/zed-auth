# Implementation chain — P2-13

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for the organization switcher and for the context that sits behind it.

"Not applicable" is an acceptable answer; silence is not.

---

## Component: `OrgSwitcher` (in the shell, on every screen)

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Organization switcher: search input plus list, "relevant only once multi-org is active". Placed at the top of the side navigation, directly under the wordmark — an administrator looks at the top-left to answer "where am I" |
| **Component** | New, in the shell rather than the design system: it has exactly one instance and a design-system component with one caller is a generalisation nobody asked for. Uses the shared `Skeleton` |
| **State** | Signed out (renders nothing), list loading, **one organization** (a label, no control), more than one (a control), open, filtered, filtered to nothing, switched, list failed |
| **Interaction** | Click to open; the filter takes focus; Escape or a click away closes; choosing one clears the cache and rewrites the URL. Choosing the current one is a no-op rather than a cache clear |
| **API Dependency** | `GET /v1/me/organizations` — **added by this task**, because none existed. `GET /v1/organizations` is `INSTANCE_OWNER` by nature, and the token's `urn:authservice:manager_roles` claim carries role names without their scopes and is a snapshot up to ten minutes stale |
| **Loading** | A skeleton the width of a name, in place of the name. Nothing else moves, because the control's geometry does not depend on the list |
| **Error** | The list failing does **not** blank the active organization — the switcher's more important job is saying where you are, and it can do that from `GET /v1/organizations/{org_id}`. There is no error banner in the chrome: a failure to enumerate other organizations is not something to interrupt the page for |
| **Empty** | Not applicable in the usual sense — a caller always administers at least the organization whose console they are in. A filter matching nothing says so and names what was typed |
| **Permission** | The endpoint requires no role, deliberately: it reports the caller's own roles, and an endpoint that required a role to report your roles is one nobody can bootstrap from. The switcher shows what that endpoint returns and nothing else |
| **Responsive** | Inside the side navigation, which is a full-width bar below tablet width and a fixed track above it. The popup is `absolute inset-x-3`, so it matches the track rather than overflowing it; the name truncates |
| **Accessibility** | `aria-haspopup`/`aria-expanded` on the trigger; the trigger's accessible name is "Organization" plus the active name, so it is not just "Acme Corporation" with no indication of what that is; focus moves into the filter on open; Escape closes; the current entry is marked with `aria-current` **and** the word "(current)", never a tick alone. **Not `role="listbox"`** — see below |
| **Test** | 13 tests in `src/app/shell/orgswitcher.test.tsx`, plus 4 Playwright tests including the one that forces an unauthorized organization into the URL |

### Why it is not a listbox

The first version used `role="listbox"` with `role="option"`, and axe failed it on structure — a listbox's children must be options, and these were `<li>` wrapping buttons.

Fixing the structure would have been the wrong fix. The listbox pattern promises arrow-key navigation, a single tab stop and typeahead, and this implements none of them: a screen-reader user told they are in a listbox will press Down and nothing will happen. So it is marked up as what it is — a list of buttons behind a disclosure — which is the same call `ProjectNav` made in `P2-11` for the same reason.

---

## Context: `OrgProvider`

| Step | Answer |
|---|---|
| **Design** | Not a screen. `docs/PLAN/06` § Information Architecture, plus `P2-13` steps 3 and 5 |
| **Component** | New. A React context over `useSearchParams`, so the URL is the state rather than a copy of it |
| **State** | No session, acting in the token's organization, acting elsewhere |
| **Interaction** | `switchTo` clears the query cache, then rewrites `?org=`. An effect re-attaches the parameter after any internal navigation that dropped it — React Router drops search parameters on `<Link to="/users">`, and every link in this console is written that way |
| **API Dependency** | None of its own. It decides what `useOrgId` returns, which is what every other query key is built from |
| **Loading / Error / Empty** | Not applicable — it holds no server state |
| **Permission** | **None, and that is the point.** Setting `?org=` to anything is allowed; the API refuses every request that follows. `docs/UI-UX/08` § Cross-Screen Requirements: the switcher is UI, never a control |
| **Responsive / Accessibility** | Not applicable — it renders nothing |
| **Test** | Covered through the switcher's tests, which assert the re-scoping, the cache clear and the URL round-trip; and by the E2E test that forces an unauthorized id |

---

## What this task added outside the console

`GET /v1/me/organizations`, with the migration behind it. The card's surface is "console" and its Definition of Done asks the switcher to list "exactly the organizations the caller administers, **per server-side truth**" — which no endpoint could answer. Building the switcher off the token's claim would have satisfied the sentence and broken the requirement.

Recorded as `PG-37`. The endpoint, the `ScopeSelf` authorization scope it needed, and its seven integration tests are described in the task record.

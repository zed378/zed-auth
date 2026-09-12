# P2-13 — Console: Organization Switcher

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-13 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | console **and backend** — see below |
| **Branch** | `feat/P2-13-org-switcher` |
| **Status** | Complete — end-to-end verified against a local stack; not yet on staging |

**Spec**: none required — chain committed at [`console/docs/implementation-chain-P2-13.md`](../../console/docs/implementation-chain-P2-13.md)

---

## A console task that could not be done in the console

The card's surface is `console`, it says "Spec required: No", and its first Definition of Done line is:

> The switcher lists exactly the organizations the caller administers, **per server-side truth**.

Nothing could answer that. `GET /v1/organizations` requires `INSTANCE_OWNER` by nature — a list spanning tenants cannot be scoped to one. An `ORG_ADMIN` can read only their own organization. And the token's `urn:authservice:manager_roles` claim carries **role names without their scopes**, so it cannot name an organization at all, quite apart from `internal/management/store.go`'s standing rule that this API does not trust a role in a token.

So the card had two readings: add an endpoint, or build the switcher off something that is not server-side truth. The second satisfies the sentence and breaks the requirement.

`GET /v1/me/organizations` was added, with the `ScopeSelf` authorization scope it needed and the `organizations_administered_by` function behind it. Recorded as [`PG-37`](../../TASKS/BACKLOG.md), because the planning gap outlives this task: two more Phase 2 console cards sit near the same line, and a console card that needs a new endpoint is not a console card.

## The one route that requires no role

`ScopeSelf`. It is in `management.Policy` as `{Role: Member, Scope: ScopeSelf}` rather than absent from the table, because an unlisted route is refused and "this needs no permission" should be a sentence somebody wrote rather than a gap somebody left.

It can be unauthorized because it **grants nothing**: the response is derived from the caller's own `manager_roles` rows, so it can only describe access they already hold. An endpoint that required a role in order to report which roles you have is one nobody can bootstrap from.

Still authenticated, and there is a test for exactly that.

## The function takes a user, never an organization

`organizations_administered_by(who, after, after_id, limit)` is `SECURITY DEFINER`, for the reason `organizations_page` is: `organizations` is the one table whose RLS policy keys on `id` rather than `org_id`, so under instance scope a plain `SELECT` matches nothing and under tenant scope it matches exactly one row.

Running with the owner's privileges is safe here because **there is no argument that could steer it**. It takes a user id — the authenticated subject — and derives the organizations from that user's own rows. There is no organization parameter to guess at.

`PROJECT_OWNER` is deliberately excluded. Its `scope_id` is a project, and `management.Policy` requires `ORG_ADMIN` for every organization endpoint — so offering a switch there would produce a context in which every screen answers 403. A switcher whose entries lead to refusals is worse than one that omits them. The contract says so and a test asserts it.

## A `text[]` that scanned as a string, and four tests that did not notice

The endpoint answered 500 for every caller who administered anything, because `rows.Scan(&row.Roles)` cannot decode a Postgres array into `[]string` without `pq.Array`.

Two tests failed. **Four passed** — including "a caller who administers nothing gets an empty list" and "a PROJECT_OWNER is not offered the containing organization" — because a 500 renders as an empty list, which is exactly what those tests assert.

That is the project's named defect class arriving in a new costume. The fix is in the shared decoder: it refuses to read a body that did not come with a 200. Every negative assertion in the file now means something.

## Not a listbox

The switcher's first version used `role="listbox"` with `role="option"`, and axe failed it: a listbox's children must be options, and these were `<li>` wrapping buttons.

Fixing the structure would have been the wrong fix. That pattern promises arrow-key navigation, a single tab stop and typeahead, and this implements none of them — a screen-reader user told they are in a listbox will press Down and nothing will happen. It is marked up as what it is: a list of buttons behind a disclosure, with `aria-current` on the active one. The same call `ProjectNav` made in `P2-11`.

## The URL is the context

`?org=`, not memory and not storage. A context held only in memory turns "send me that page" into "send me that page and also click the switcher first"; one held only in storage makes two tabs fight over which organization the administrator is in.

React Router drops search parameters on `<Link to="/users">`, and every link in this console is written that way — so an effect re-attaches it. Without that, switching and then clicking anything would silently drop back to the token's organization, which is a context change nobody asked for in a console where the next click might be a delete.

When acting in the token's own organization the parameter is removed entirely. A parameter that is always present is one nobody reads.

## Switching clears everything, not just the old keys

Query keys are already scoped per organization, so the previous tenant's rows could not be *served* under the new one's key. `queryClient.clear()` runs anyway, because that makes it a property of the cache rather than a property of every future hook author remembering — which is why `PF-20` is on the watch list.

## What the E2E run found

Forcing `?org=<an organization the caller holds nothing over>` produced the refusals it should, and a console that said nothing about why. The error states were correct and unexplained.

So the switcher now distinguishes two situations it had been collapsing: a deliberate switch ("you are acting in another organization") and a context the caller does not administer ("you do not administer this organization, so this console cannot read it"). The second is only shown once the list has actually loaded — saying it while still finding out would be wrong more often than right.

The create button stays visible in that state, and the test says why rather than asserting it away: it is gated on the token's manager-role claim, which is a property of the caller rather than of the context, and the API refuses the POST regardless. A hidden button was never the control.

## Verified

| | |
|---|---|
| Backend integration | 7 new, through the real router and real `manager_roles` |
| Backend mutation | the caller filter removed → two cross-tenant tests red |
| Console component | 13 new, in `src/app/shell/orgswitcher.test.tsx` |
| Console mutation | 6 controls reverted one at a time, each turning its own test red |
| **End-to-end** | **4 new Playwright tests against a real service, including the forced-organization refusal. 24/24 green** |
| Accessibility | axe over the switcher, closed and open |
| Everything else | 214 console tests, the full Go unit suite, `tsc`, `eslint`, `go vet` — all clean |

## A note on my own tooling

Half an hour was lost to a self-inflicted failure: `npm run build` without the stack's `VITE_AUTH_CLIENT_ID` replaced the E2E bundle with one that has no client configured, and 19 previously-passing tests failed at the sign-in step. Nothing was wrong with the code. `scripts/e2e-up.sh` builds the console with the right variables; rebuilding by hand has to pass them too.

## Staging

Unchanged: the VM has been unreachable since the deploy key went with the session scratchpad. Like `P2-12`, this is verified against a real running service locally, which is stronger than `P2-01`…`P2-11` managed, and is still not staging.

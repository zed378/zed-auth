# P2-12 — Console: Authorizations Tab

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-12 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | console |
| **Branch** | `feat/P2-12-console-authorizations` |
| **Status** | Complete — **end-to-end verified locally**, not yet on staging |

**Spec**: none required — chain committed at [`console/docs/implementation-chain-P2-12.md`](../../console/docs/implementation-chain-P2-12.md)

---

## Search-then-act, because that is the API's shape

The card asks for "user search with debounced server-side lookup", and the reason it is a search rather than a roster only becomes clear from the API: **grants are exposed one user at a time**. `GET /v1/organizations/{org_id}/users/{user_id}/grants` exists; nothing lists everyone with access to a project.

So the screen answers "what access does this person have here", which is the question an administrator usually arrives with. It does not answer "who can do anything here", which is the question an access review starts from — and that absence is real, not a design choice. Recorded as [`PG-36`](../../TASKS/BACKLOG.md) with the endpoint it needs.

The alternative was to fake it: issue a grants request per search result and render a table. That is `N+1` requests producing something that still is not the roster — it would cover only the users matching whatever was typed. Faking a capability in the UI is how a gap stops being visible.

## The input is never debounced; only the request is

250ms on the query, and the field stays controlled by the raw value. Debouncing the input itself makes typing feel broken, and it is a surprisingly common way to implement this.

The mutation run checks both halves independently: removing the debounce turns the request-count test red, and making the delay zero turns the same test red from the other direction.

Without it, every keystroke is a request — and they can return out of order, so the list settles on the results for `budi@ex` after the results for `budi@example.test` have already rendered.

## The badge that has nothing to show yet

`docs/UI-UX/08` § Cross-Screen Requirements makes the role-source badge mandatory on **every** screen showing roles, and says so explicitly because it is exactly the kind of requirement each screen would otherwise answer for itself.

Every grant in Phase 2 is direct. Project Grants arrive in Phase 4, and until `P4-01` the database refuses a non-null `project_grant_id`. So the delegated branch is **unreachable today**, and it is built and tested anyway — step 4's instruction, and the reasoning holds: adding the distinction later means auditing every screen that renders a role for a treatment it was never given.

One deviation from the letter of `docs/UI-UX/06`, which asks for the source organization "on hover/tap". Hover is an affordance for a mouse. The same sentence is also in the accessibility tree, because a screen-reader user and a keyboard user get nothing from a `title` alone.

## The two dialogs say the thing people get wrong

Revocation states that it takes effect immediately — the grant row is deleted rather than flagged — **and** that an access token already issued keeps the roles it was minted with until it expires. The second half is the one administrators are surprised by, and it is the reason `/v1/authz/check` exists.

Assignment refuses to be the way access is removed. The API rejects an empty `role_keys` — "a grant with no roles grants nothing and should not exist" — so deselecting everything gets a sentence pointing at the revoke action instead of a refusal to interpret.

## Three kinds of nothing, and one that is not an empty state

An untouched search box is **not** an empty state. "No users" would be a false statement about the organization; it says "type an address above".

A search matching nobody names what was searched for. And a user with no grant gets "No access" plus the reason it is normal — `docs/PLAN/08` § Least Privilege means no grant is no access, not a default, and a blank cell is ambiguous with a failed load.

## The User detail Grants tab is real now

It has said "not available yet — arrives with the authorization work in Phase 2" since `P1-23`. This is that work, so it renders, from the same components (step 7).

The test that asserted the tab was unavailable moved to Multi-factor rather than being deleted: `P1-23` step 3's rule — an empty tab and an unavailable one look similar and mean opposite things — outlives any particular tab.

## A bug in the E2E script, found by running it

`scripts/e2e-up.sh` writes `.e2e.env` from an **unquoted** heredoc, because it has to expand `$ISSUER`. That also makes backticks inside it command substitution, comment or not — so three words in a prose comment were being executed:

```
scripts/e2e-up.sh: line 420: manager_roles: command not found
scripts/e2e-up.sh: line 420: P2: command not found
scripts/e2e-up.sh: line 420: PG-26: command not found
```

Harmless as written, and it would not have stayed harmless. Escaped, with a note at the heredoc saying why the escapes are there.

## Verified

| | |
|---|---|
| Component tests | 20 new, in `console/src/pages/authorizations.test.tsx` |
| **End-to-end** | **3 new Playwright tests against a real stack — service, database, browser — all green, plus the existing 17: 20/20** |
| Mutation | 7 controls reverted one at a time, each turning its own test red |
| Accessibility | axe (WCAG 2.1 AA) over the search results and the assignment dialog |
| Existing suites | 201 component tests across 10 files; `tsc --noEmit` and `eslint --max-warnings=0` clean |

**One test was vacuous and the mutation run caught it.** "Shows only this project's grant" passed with the project filter replaced by `() => true`, because the fixture listed this project's grant first and `.find()` returned it anyway. The other project's grant now comes first, so a filter that ignores the project id picks up the wrong row.

Every E2E assertion is made twice — once against the screen and once against the Management API — because a console that wrote a grant somewhere the API cannot see would satisfy every on-screen assertion. That is `docs/PLAN/02` FR-14's no-private-path rule, and only a cross-surface test can make the claim.

## Staging

Still unreachable: the deploy key went with the session scratchpad. What is new is that this task **was** verified against a real running service — locally, through `scripts/e2e-up.sh` — which is a stronger result than `P2-01`…`P2-11` have. It is not staging, and `P2-17` still cannot close without it.

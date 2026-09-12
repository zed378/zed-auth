# P2-11 — Console: Roles Tab

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-11 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | console |
| **Branch** | `feat/P2-11-console-roles` |
| **Status** | Complete — not yet on staging |

**Spec**: none required — but `docs/UI-UX/19`'s chain is mandatory, and it is committed alongside the code at [`console/docs/implementation-chain-P2-11.md`](../../console/docs/implementation-chain-P2-11.md)

---

## The one source of truth was already there

Step 3 asks for client-side validation "matching the server's rule exactly — a client rule that is merely similar produces confusing rejections."

That was already solved, in `P2-01`. `console/src/lib/api/patterns.gen.ts` and `backend/internal/role/pattern.gen.go` are both generated from the `RoleKey` and `PermissionKey` schemas in `openapi/openapi.yaml`, and `scripts/check.sh` fails if either has drifted. This screen's job was to **use** them rather than to write a regex that looked right.

The distinction shows up in the mutation run. Replacing `isValidRoleKey` with `/^[a-zA-Z0-9_-]+$/` — a rule any careful person would write from the field's help text, and which accepts `Cashier` — turns the test red. Without the shared pattern nothing would have caught it, because the form would look correct and the server would refuse a value the form accepted.

## The count before the refusal

The service refuses to delete a role any grant still references (`P2-02`), rather than cascading — a cascade removes access from everybody holding the role in response to a request that looks like tidying up.

That is the right server behaviour and it makes a specific demand of the console: an administrator who cannot see the count discovers the rule by hitting it, and then has to work out what the refusal means. `grant_count` is on every role in the list response precisely so the dialog can say **"3 users currently hold this role"** before the request, and say it in the same dialog that would otherwise carry only "this cannot be undone".

Both branches are tested, because "Nobody currently holds this role" is the half that is easy to leave out and the half that turns silence into an answer.

## Built-in roles: the card and the API disagree, and the API won

Step 5 asks for built-in roles to render as **non-editable**. The API says only their identity is frozen — `updateRole` is explicit that a built-in role's display name and permissions may still be edited.

Both were deliberate; the card predates `P2-02` settling what "built-in" costs. The console matches the **API**: the key is read-only with the reason stated, the delete control is replaced by the reason it is absent, and the name and permissions are editable like any other role's. A console stricter than its own API produces a capability reachable only by `curl`, which is the inverse of the API-first rule.

Recorded as [`PG-34`](../../TASKS/BACKLOG.md) rather than resolved here, because amending a task card is a plan change. Nothing is broken today: no built-in roles are seeded at all (`PG-30`), so the divergence has no live behaviour behind it yet.

## A disabled control is not an explanation

Both places this screen refuses something, it refuses in text rather than with a greyed-out button.

The built-in role's delete control is **absent**, with "Built-in — cannot be deleted" in its place. The key field when editing is `readOnly`, **not** `disabled`.

That is an accessibility decision as much as a copy one. A disabled button is removed from the tab order, so a keyboard or screen-reader user never lands on it and never hears why it is there — the explanation is visible only to somebody who can see it greyed out and guess. `readOnly` keeps the field focusable and its `aria-describedby` sentence announced.

## The bug that was not in this screen

The form accepted one character per field, and the cause was in `Modal`.

Its focus effect listed `onClose` in its dependencies. Any caller passing an inline arrow — which is any caller closing over its own state — hands it a new function every render, so the effect tore down and re-ran on every keystroke, and its cleanup calls `focus()`. Focus went back to the dialog after the first character and the rest went nowhere.

The two existing callers were safe by accident: their `onClose` belonged to a component that did not re-render per keystroke. A shared component that works or not depending on how the caller spells a prop is a trap rather than an API, so the dependency was removed — the callback lives in a ref updated in its own effect — instead of being written down as a caller's obligation.

Found by a test, not by clicking. It is exactly the failure a component test catches and a screenshot does not.

## Three kinds of nothing

`docs/UI-UX/14` asks for "genuinely empty" and "filtered to empty" to be different. This screen has a third: a role with **no permissions at all**, which the API permits deliberately — "a role with no permissions is a label, and labels are useful before the permissions exist."

A blank cell would read as missing data. It says "None — a label only", and the form does not block saving one.

## Tabs that are not tabs

`docs/UI-UX/08` calls the project detail's sections tabs. `docs/UI-UX/07` specifies no tab component, and `UserDetailPage` already implements real ARIA tabs — for panels inside one document, which is what that pattern describes.

These are routes. Each has its own data, its own loading and error states, and its own back-button behaviour. `role="tab"` would promise arrow-key navigation, a single tab stop and an `aria-controls` panel that is not there, which is worse for a screen-reader user than a plain link.

So `ProjectNav` is a labelled `nav` of `NavLink`s. The codebase now has two tabbed screens that disagree on the mechanism and no document saying when each applies — recorded as [`PG-35`](../../TASKS/BACKLOG.md).

## Verified

| | |
|---|---|
| Component tests | 17 new, in `console/src/pages/roles.test.tsx` |
| Mutation | 5 controls reverted one at a time, each turning its own test red |
| Accessibility | axe (WCAG 2.1 AA rule set) over the populated table and over the open form |
| Existing suites | 181 tests across 9 files, all green; `tsc --noEmit` and `eslint --max-warnings=0` clean |

The shared screen-test harness was extracted to `console/src/test/harness.tsx` when this became the second suite to need it. Two copies of `stubApi` would be two definitions of what "the API" means in a test — and the `String(request)` bug already fixed once in `screens.test.tsx` would have had to be found again here.

## Not yet on staging

The VM has been unreachable since the session restart took the deploy key with the scratchpad. Everything above ran locally. Like `P2-01`…`P2-10`, this is complete in the repository and unverified on staging, and `P2-17` cannot close until that is fixed.

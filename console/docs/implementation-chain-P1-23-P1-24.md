# Implementation chain — P1-23 and P1-24

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, for the users screens and the audit log. `P1-23` step 7 makes it mandatory; `P1-24` inherits the same discipline.

The components these compose from — `Table`, `Button`, `Badge`, `Modal`, the four state components — were run through the chain in [`implementation-chain-P1-22.md`](./implementation-chain-P1-22.md) and are not repeated. Two are new: `SidePanel` and `ConfirmDialog`.

---

## Screen: Users list

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/18` § Users List, in its stated order: search and filter always visible and never behind a click, then the primary action, then the table. It is the most visited screen in the console (`docs/UI-UX/01`, Budi) |
| **Component** | Shared `Table`, `Badge`, `Button`, and the new `SidePanel` |
| **State** | Loading, loaded, error, genuinely empty, filtered to empty (by search, by status, or by both), inviting |
| **Interaction** | Search filters as typed; the status filter writes to the **URL**, so a filtered list is a link somebody can send — and so the dashboard's "pending invites" card has a destination |
| **API Dependency** | `GET .../users?page_size=100&search=…`; `POST .../users` |
| **Loading** | Skeleton rows in the shared table |
| **Error** | `ErrorState`, kind-aware. A `403` renders as a refusal **with no retry button**, because retrying a refusal produces a refusal |
| **Empty** | Both kinds. Either control counts as a filter: somebody who typed a search *and* picked a status needs "nothing matches", not "invite your first user" |
| **Permission** | `ORG_ADMIN` and above, guarded at the route; the invite action is additionally hidden without it. Both are UX — the API refuses regardless |
| **Responsive** | `Email` and `Address` are `secondary` and drop below 1024px; the name column carries the link, so the row stays useful |
| **Accessibility** | Labelled search and filter (visually hidden labels, because the placeholder is not a label); status badges carry text; the table has a caption |
| **Test** | Component tests for the badge text, the "not asserted" wording, both empty kinds, and the refusal-without-retry |

---

## Flow: Invite user (`docs/UI-UX/04` Flow 1)

| Step | Answer |
|---|---|
| **Design** | A side panel, not a modal — `docs/UI-UX/07`'s rule is that a multi-step, detail-heavy flow is a panel and an interrupting confirmation is a modal |
| **Component** | New `SidePanel` |
| **State** | Step 1, step 2, submitting, field-level failure, general failure |
| **Interaction** | **One flow in two steps** with a "Step 1 of 2" indicator, which is Flow 1's first safeguard — two separate screens is what it forbids. The indicator is `aria-live`, so it is announced rather than only shown |
| **API Dependency** | `POST .../users` with `{email, display_name?, send_invite_email: true}` |
| **Loading** | The submit button shows a spinner and holds its width |
| **Error** | Field-level errors from `docs/PLAN/05`'s `details[]`, rendered **against their own fields** with `aria-invalid` and `aria-describedby` (`docs/UI-UX/15`). A per-field detail rendered as one banner is a form the user has to re-read to fix. The panel returns to step 1, where the field is |
| **Empty** | Not applicable |
| **Permission** | `ORG_ADMIN` and above |
| **Responsive** | Full-width panel below tablet; capped at `max-w-md` above |
| **Accessibility** | `role="dialog"`, `aria-modal`, focus taken on open, trapped while open, restored on close; Escape closes |
| **Test** | Component tests for the two-step progression, the explicit no-access choice, and the field error landing on its field |

**The second safeguard, and how Phase 1 honours it.** Flow 1 requires role assignment in step 2, with "no access yet" as an **explicit, visible choice rather than a skip** — so an admin never walks away believing they granted access when they did not.

Role assignment is `P2`'s: `manager_roles` has no API in Phase 1. So step 2 exists (the flow stays one flow), says plainly that roles arrive later, states how many projects the invitee will have access to — none — and **gates the send button behind a checkbox** that says so. The choice is made deliberately rather than skipped past, which is the safeguard's actual purpose.

---

## Screen: User detail

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08`, with a breadcrumb reflecting the real hierarchy |
| **Component** | `Badge`, `Button`, the new `ConfirmDialog`, and a real tablist |
| **State** | Loading, loaded, error, profile tab, three unavailable tabs, confirming a deactivation, deactivating |
| **Interaction** | Tabs use `role="tab"` with roving `tabIndex`; deactivation opens a confirmation, never acting on the first click |
| **API Dependency** | `GET .../users/{user_id}`; `POST .../users/{user_id}/deactivate` |
| **Loading** | A skeleton in place of the profile block |
| **Error** | `ErrorState`, kind-aware, with retry |
| **Empty** | Not applicable for a detail screen. The three **unavailable** tabs are not an empty state and are labelled as unavailable — `P1-23` step 3, and the distinction matters: an empty "Sessions" tab says this user has none; an unavailable one says we cannot tell you |
| **Permission** | `ORG_ADMIN` and above; the deactivate action is hidden without it and hidden for an already-deactivated user |
| **Responsive** | The profile becomes one column below tablet |
| **Accessibility** | A real tablist with `aria-selected`, `aria-controls` and roving tabindex; the unavailable tabs say "(later)" in the tab itself, so somebody scanning learns which are real without opening each; the confirmation traps focus |
| **Test** | Component tests for the unavailable-tab wording, the consequence text, and an axe check |

---

## Component: `ConfirmDialog`

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/07` § Confirmation Dialog |
| **Component** | Composes `Modal` and `Button` |
| **State** | Open, confirming, typed-confirmation locked / unlocked |
| **Interaction** | The confirm button carries the **actual verb** — "Deactivate", never "OK". A button labelled with the verb cannot be clicked without reading it. Typed confirmation exists and is reserved for the highest-consequence actions; using it everywhere would make it furniture |
| **API Dependency** | None — the caller owns the request |
| **Loading** | The confirm button spins and holds its width |
| **Error** | The caller renders it. A dialog that swallows the failure is one the user closes believing it worked |
| **Empty** | Not applicable |
| **Permission** | Not applicable |
| **Responsive** | Identical at every width |
| **Accessibility** | Inherits `Modal`'s dialog semantics and focus behaviour |
| **Test** | Exercised through the deactivation flow |

**Deactivation's consequence text is the point.** `docs/UI-UX/07` asks for a plain-language consequence rather than "are you sure?", and the consequence here is the part people do not expect: `P1-19` revokes sessions, refresh tokens **and** live invitation links inside the request. The dialog says all three, and says the action is reversible — which it is, except for the sessions.

---

## Screen: Audit log

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Audit Log. Deliberately basic: `docs/PLAN/17`'s Phase 1 criterion is that logins appear with the correct actor and timestamp, and filtering polish and export are Phase 5 |
| **Component** | Shared `Table`, `Button` |
| **State** | Loading, loaded, error, genuinely empty, filtered to empty, a row expanded, first page / later page |
| **Interaction** | Three filters, each resetting pagination — a cursor from one filter is meaningless under another. Detail expands in place with `aria-expanded` and `aria-controls` |
| **API Dependency** | `GET .../events` with `event_type`, `from`, `to`, `page_size`, `page_token` |
| **Loading** | Skeleton rows |
| **Error** | `ErrorState`, kind-aware |
| **Empty** | Both kinds — "no events yet" versus "none match these filters" (`P1-24` step 6) |
| **Permission** | `ORG_ADMIN` and above |
| **Responsive** | `From` (the IP) is `secondary` and drops below 1024px; the timestamp column does not wrap |
| **Accessibility** | Timestamps are `<time>` with the UTC value in `dateTime` **and** in `title`, so the underlying value is inspectable while local time is displayed (`P1-24` step 5 — incident timelines are reconstructed across timezones); the detail region is labelled; the expand control announces its state |
| **Test** | Component tests for the actorless event's wording, the UTC value being present, the redaction note, and both empty kinds |

**Two things this screen deliberately does not do.**

It does not filter the payload. `P1-24` step 3 says the console must never render a field the API should not have returned — and the way to honour that is *not* a second redaction policy in the browser, which would drift from the real one in `P0-12`. It renders what arrived and says where the redaction happened.

It does not offer page numbers. A keyset cursor has none, and inventing them would mean an offset query against a table with no ceiling. Forward and back, with a cursor stack so "back" returns to the exact page rather than re-querying from the start.

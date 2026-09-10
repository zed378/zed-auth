# P1-23 & P1-24 — Console: Users, User Detail, Audit Log

**Date**: 2026-09-10
**Branch**: `feat/P1-23-console-users`
**Chain**: [`console/docs/implementation-chain-P1-23-P1-24.md`](../../console/docs/implementation-chain-P1-23-P1-24.md)

---

## What this is

Three screens and two components, built on `P1-22`'s design system. Recorded together because they share every component and the same two chains would otherwise repeat each other.

The screens took a fraction of `P1-22`'s effort, which is the shared-table rule paying its second instalment: these add screens rather than tables.

## Flow 1's safeguard, honoured rather than deferred

`docs/UI-UX/04` Flow 1 requires role assignment in step 2, with **"no access yet" as an explicit, visible choice rather than a skip** — so an admin never walks away believing they granted access when they did not.

Role assignment is `P2`'s: `manager_roles` has no API in Phase 1. Two ways to handle that, and only one is honest:

- Drop step 2, making invite a single form. That breaks the *first* safeguard — "one flow, not two screens" — and quietly removes the moment where access is considered at all.
- Keep step 2, say plainly that roles arrive later, state how many projects the invitee will have access to (none), and **gate the send button behind a checkbox that says so**.

The second. The safeguard's purpose is that the choice is made deliberately, and a checkbox reading "Invite with no access yet. I will grant access separately." is that choice — it is not a placeholder for one.

## Unavailable is not empty

`P1-23` step 3 asks for the Grants, Sessions and MFA tabs to be "clearly-labelled unavailable states rather than broken or empty tabs".

**An empty Sessions tab and an unavailable one look similar and mean opposite things.** One says this user has no sessions; the other says we cannot tell you. So each unavailable tab says which phase it belongs to, and the Sessions one says the thing a reader would otherwise assume wrongly: the service *does* track and revoke sessions today — deactivation ends them — it is the screen that is missing, not the capability.

The tabs also carry "(later)" in the tab itself, so somebody scanning learns which are real without opening each one.

## The consequence, not the warning

`docs/UI-UX/07` asks a confirmation dialog for a plain-language consequence rather than "are you sure?", and the consequence for deactivation is the part people do not expect:

> Every session they have open ends immediately, and any application holding a refresh token for them stops working at once. Any invitation or password-reset link they hold stops working too.

All three are true because `P1-19` does all three inside the request. A dialog saying "this cannot be undone" would be both less informative **and wrong** — it can be undone, except for the sessions, and the dialog says that too.

The trigger is `danger-text`, not a full destructive button: `docs/UI-UX/07`'s rule is that a destructive button must not dominate a screen whose expected path is not destructive. The full weight belongs to the dialog, where it is the only thing on the screen.

## Two things the audit log deliberately does not do

**It does not filter the payload.** `P1-24` step 3 says the console must never render a field the API should not have returned — and the way to honour that is *not* a second redaction policy in the browser, which would drift from the real one in `P0-12`. It renders what arrived and says where the redaction happened.

**It does not offer page numbers.** A keyset cursor has none, and inventing them means an offset query against a table with no ceiling. Forward and back, with a cursor stack so "back" returns to the exact page rather than re-querying from the start.

Timestamps show local time with the UTC value in both `dateTime` and `title`, because `P1-24` step 5 is right that incident timelines are reconstructed across timezones — and a log that shows only one of the two forces the reader to do the arithmetic.

## Wording that came from the service's own restraint

- **"Not asserted", not "Unverified".** `PG-18`: an absent `email_verified` means the service has not checked, not that it checked and failed. `P1-08` omits the OIDC claim for exactly this reason, and a console that renders "Unverified" makes the stronger claim the service deliberately withholds.
- **"None — no authenticated actor", not blank.** A failed login against an address that does not exist genuinely has no actor. Blank reads as an incomplete record, which is the opposite of what an audit log should convey.

## Found while building it

**The API client captured `fetch` at module load, and that is a production bug as well as a test one.** `openapi-fetch` reads `globalThis.fetch` once, when the client is created — so anything installed afterwards is ignored: instrumentation, a service worker registering late, a polyfill loaded after this module. Resolved per call now, which costs nothing and means the transport in use is the one currently installed.

**An empty base URL produces relative request paths, which nothing outside a browser can parse.** The failure is `Invalid URL` from a layer naming neither the console nor the endpoint. Same-origin is now spelled `window.location.origin` — the same destination, and one that works in both places.

**And then my own test harness lied in exactly the shape this repository keeps finding.** With the client passing a `Request` object, the stub's `String(input)` produced `"[object Request]"`, which matched no branch of any handler and answered **every** call with the success body — including the `POST` whose failure the test existed to check. The test failed rather than passing vacuously, but only because it asserted on the error's *presence*; a test asserting "no error appeared" would have been green and meaningless.

## Verification

- **Component, 14 new tests** (164 total): the status badge carrying text; "Not asserted" rather than "Unverified"; both empty kinds on the users list, where either control counts as a filter; a refusal rendering with no retry; the invite flow's two announced steps; the send button gated behind the explicit no-access choice; a field error landing on its own field with `aria-invalid`; the unavailable tabs saying *why*; the deactivation consequence naming sessions and refresh tokens, with the verb on the button; an axe check on the user detail; the audit log's actorless wording, its inspectable UTC value, its redaction note, and both empty kinds.
- The API is stubbed at `fetch` rather than at the query layer, so the generated client, the bearer middleware and the envelope handling all stay in the path. A test that mocks `useUsers` proves the component renders an array, which is not the thing that breaks.

## What these tasks did not build

- **Sorting, server-side search and page numbers.** The lists fetch a page and filter in the browser; the audit log pages forward and back. Honest at Phase 1 volumes, and the API already paginates, so the change is in the screen rather than the contract.
- **The E2E for Flow 1.** `P1-23`'s DoD asks for it and `P1-27` owns CI wiring for the E2E suite; the fixtures it needs exist from `P1-21`.
- **Audit log export.** Phase 5, per the card.

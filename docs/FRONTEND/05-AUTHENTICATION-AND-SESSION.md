# 05 - Authentication and Session

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-21, P3-10, P3-12, ADR-019 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Describe how the console obtains, holds, renews and discards a session — and which parts
of that are user experience rather than security.

## Scope

`console/src/lib/auth/`. The hosted login pages themselves are served by the Go service
(`backend/internal/login/`) and are not part of this application.

## As Built

### The console is an ordinary public client

Authorization Code with PKCE against this service, exactly as any `type: spa` client
would do it (`console/src/lib/auth/oidc.ts`, `console/src/lib/auth/pkce.ts`). No endpoint,
parameter or exemption of its own. `docs/PLAN/02` § Constraints and `docs/PLAN/06` both
require it, and the practical test is stated in the code: if something here needs a
special case on the server, that is a bug in this file rather than a feature to add there.

It holds no client secret, because it cannot keep one.

### The access token lives in memory only

A module-scoped variable in `console/src/lib/auth/tokens.ts`. Not `localStorage`, not
`sessionStorage`, not a cookie (ADR-019). A Management API token in `localStorage`
outlives the tab, the browser restart and the incident response, and any script on the
origin can read it by a well-known key.

The cost is real and accepted: **every reload passes through a restoring state**, which is
why `RequireAuth` renders "Checking your session" rather than a blank screen.

The one thing the console does persist is the pending authorization request — a PKCE
verifier and a `state`, in `sessionStorage`. Those are single-use values for one in-flight
login, useless afterwards, and they must survive a full navigation to the identity
provider and back. `sessionStorage` is per tab, so two tabs logging in at once do not
overwrite each other's verifier; `localStorage` would.

### Renewal is silent, in a hidden iframe

`/auth/silent` performs a prompt-less authorization in an iframe, using the SSO session
cookie. It starts **60 seconds** before expiry (`RENEW_MARGIN_MS`): long enough for the
renewal to complete and to fall back to an interactive login before anything breaks, short
enough not to renew constantly. A token issued with a lifetime shorter than the margin
renews immediately, which is correct rather than a bug.

Silent renewal creates no new session, so it does **not** make the session any more recent.
`signedInWithin` tracks the last *interactive* sign-in for that reason.

### A 401 mid-action does not lose the user's work

`console/src/lib/api/client.ts` registers an `onResponse` hook: a 401 triggers renewal
once. **The retry belongs to the caller, not to the middleware.** Replaying a request
inside the middleware would replay a `POST` as well as a `GET`, and silently repeating a
mutation the server may already have applied is worse than the 401 —`Idempotency-Key`
exists precisely because that decision belongs to whoever knows what the request was.

A request with no token is sent with **no** `Authorization` header rather than an empty
one: the service answers a missing header with a clean 401 and `WWW-Authenticate`, while
an empty bearer is a malformed request, and the difference is what the console can tell
the user.

### Re-authentication is a distinct outcome, not a failure

The API answers `REAUTHENTICATION_REQUIRED` when an action changes how an account is
protected and the session behind the token is too old (`P3-10`). The console responds by
starting a login with `prompt=login`: the user signs in again — password and second
factor — even with a live SSO session. A fresh interactive sign-in creates a new session
server-side, so the token that comes back is tied to it.

### The claims the console reads decide what to *show*

`console/src/lib/auth/tokens.ts` decodes the token without verifying it, deliberately.
Nothing it reads is a security decision: the API enforces every permission independently
(`docs/PLAN/08`, `docs/UI-UX/08` § Cross-Screen Requirements). Verifying a token the
console received from the service it is about to call would prove nothing it does not
already assume.

What the claims drive: which navigation items render, which buttons appear, and which
routes `RequireAuth` admits. All user experience. See
[`06-ROUTING-AND-PERMISSIONS.md`](./06-ROUTING-AND-PERMISSIONS.md).

### Sign-out ends the shared session

Signing out of the console ends the SSO session, so another application relying on it asks
again. The E2E suite asserts both directions (`console/e2e/sso.spec.ts`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Access token storage | module memory | `console/src/lib/auth/tokens.ts` (ADR-019) |
| Pending login state | `sessionStorage`, per tab | `console/src/lib/auth/oidc.ts` |
| Renewal margin | 60 s before expiry | `console/src/lib/auth/tokens.ts` |
| 401 recovery | renew once; the caller retries | `console/src/lib/api/client.ts` |
| Token verification in the console | none, deliberately | `console/src/lib/auth/tokens.ts` |
| Client secret | none; public client | `console/src/lib/auth/oidc.ts` |

## Verification

- `console/src/lib/auth/auth.test.ts` — PKCE, state handling, renewal margin, claim reading.
- `console/e2e/login.spec.ts` — a real login against a real service, and a role-gated
  route refusing.
- `console/e2e/sso.spec.ts` — single sign-on across two applications, sign-out ending the
  shared session, and application B refusing a token minted for A.
- `console/e2e/account.spec.ts`, `console/e2e/mfa.spec.ts` — re-authentication paths.

## Not Yet Built / Open Questions

- **Cross-organization sign-in** (ADR-025) does not exist. A user who holds delegated
  roles in another organization's project cannot sign in to that organization's
  applications; the roles are visible to its `/v1/authz/check` and nowhere else. No task
  card yet.
- **No idle-timeout UI.** Session lifetime is a server setting; the console does not warn
  before it expires.

## Related Documents

- [`04-STATE-AND-DATA-LAYER.md`](./04-STATE-AND-DATA-LAYER.md)
- [`06-ROUTING-AND-PERMISSIONS.md`](./06-ROUTING-AND-PERMISSIONS.md)
- [`../SESSION-MANAGEMENT/`](../SESSION-MANAGEMENT/)
- [`../IDENTITY-PROTOCOL/`](../IDENTITY-PROTOCOL/)

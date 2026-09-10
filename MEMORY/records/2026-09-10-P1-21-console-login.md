# P1-21 — Console: OIDC Login (Dogfooding)

**Date**: 2026-09-10
**Branch**: `feat/P1-21-console-login`
**Spec**: [`MEMORY/specs/P1-21-console-login.md`](../specs/P1-21-console-login.md)
**ADR**: [ADR-019](../DECISIONS.md) — token storage

---

## What this is

The console signs in as an ordinary `type: spa` OIDC client: Authorization Code with PKCE, through the same hosted login page, against the same endpoints, with the same parameters any other client would send.

**The clearest measure of whether this task succeeded is that it changed no backend code.** `docs/PLAN/02` § Constraints requires the console to have no private path, and the way that requirement fails is quietly — one endpoint that only the console calls, one parameter only it sends. If this task had needed a server change, that would have been the signal.

## Token storage decided, not defaulted

ADR-019, in full, because `P1-21` step 3 asks for the decision and its reasoning.

**In memory only.** Not `localStorage`, which is the obvious choice and the wrong one here: the token is for the **Management API** — the surface that creates users, rotates client secrets and grants roles — and in `localStorage` it outlives the tab, the browser restart and the incident response, readable by any script on the origin under a well-known key.

The claim is stated precisely rather than oversold. In-memory is **not** immune to XSS: a script on the page can call the API while the page is open. What changes is that exfiltration must happen during that window, from a closure rather than a known key, and a stored payload's reach shrinks from "every future visit" to "the visits during which it runs".

The one thing that *is* persisted is the pending request — a verifier and a `state`, in `sessionStorage`, for one in-flight login. Single-use values rather than credentials, and they have to survive a full navigation. `sessionStorage` rather than `localStorage` so two tabs logging in at once do not overwrite each other's verifier.

Rejected, with reasons in the ADR: a BFF (the console is a static bundle by design, and building it a server for token storage is exactly the private path the constraint forbids) and a refresh token in the browser (a long-lived credential in the place least able to hold one; the durable credential is the `HttpOnly` session cookie, which an administrator can revoke and script cannot read).

## Silent renewal is what makes in-memory viable

A reload has no token. The console recovers one with `prompt=none` in a hidden iframe, against the SSO session cookie — and `P1-06` already implements `prompt=none`, answering `login_required` when there is no session, which is what makes this a design rather than a hope.

Four states, and the distinction between the last two is the point:

| State | What the user sees |
|---|---|
| `restoring` | A loading message. Every reload passes through here, so a blank screen would be the common case |
| `authenticated` | The console |
| `anonymous` | A sign-in prompt — the **ordinary** outcome of a first visit, not an error |
| `failed` | "Sign-in is unavailable", for something logging in again will not fix |

Collapsing `anonymous` and `failed` is what produces the blank screen `docs/UI-UX/14` is about, and telling somebody to sign in again when the issuer is misconfigured sends them round a loop that cannot succeed.

The renewal frame's `postMessage` is checked for **origin and for the `state` this renewal issued**, and the silent callback posts to its own origin rather than `*`. A page whose entire job is carrying an authorization code must not broadcast it to whatever happens to be embedding it.

**The verifier never enters the frame.** The frame reports a code; the parent exchanges it. So a frame that somehow ran somebody else's page could not complete an exchange with what it has.

## The guard is a user-experience feature, and says so three times

`docs/UI-UX/08` § Cross-Screen Requirements asks that a route the user's claims do not permit be **genuinely unreachable rather than hidden**, and `RequireAuth` wraps the route rather than the navigation item — so the test navigates directly to the URL, which is what a test that clicked a nav link would not prove.

`docs/PLAN/08` and `docs/SECURITY/02` §2–§3 are equally clear that the API is the only enforcement point. That is stated in the guard's own doc comment, in the routes file, and on the claims type — three places, because this is the belief that decays, and the decay looks like a screen someone protected with a role check and no server-side one.

The claims are read from the token **without verifying its signature**, deliberately. Verifying in the browser proves the token was not altered by something that already had the ability to alter it. The party the token must convince is the API, and it verifies.

## Small decisions worth keeping

- **`scope=openid` and nothing else.** `profile` and `email` would widen the token for no reader — the console asks the API for anything it needs. Adding a scope is a decision about what a stolen token reaches.
- **Logout clears the local token first**, then redirects. An interrupted logout must fail closed rather than leave a usable Management API token in a page that believes it signed out.
- **The callback navigates with `replace`**, so a spent code is not in the history and not in a referrer.
- **The callback's effect is guarded against React StrictMode's double invocation.** An authorization code is single-use, so the second run would fail against a code the first spent, and the user would see an error on a login that worked.
- **The 401 retry belongs to the caller, not the middleware.** The client renews on a 401 and the caller retries; replaying the request inside the middleware would replay a `POST` as readily as a `GET`, and silently repeating a mutation the server may already have applied is worse than the 401. `Idempotency-Key` exists because that decision belongs to whoever knows what the request was.
- **The session bar shows the subject, not a display name.** The console has one only by asking the API, and a name in the token would be a token carrying more than it needs for the sake of a nicer header.
- **No OIDC library.** The flow is `fetch`, `crypto.subtle` and an iframe. A dependency here is a supply-chain path to Management API tokens.

## Found while building it

**A test hook nearly went into production code.** The first E2E draft had the console read `window.__E2E_CLIENT_ID` so a test could retarget the running bundle at a freshly seeded application. It works, and it would have put a test-only path into the one file whose entire job is deciding where authorization codes are sent.

The console is built with its own `VITE_AUTH_CLIENT_ID` instead, pointing at an application seeded before the build. The fixture that creates applications stays — `P1-22` needs it — it is simply not how the console learns who it is.

**The shell's tests needed an auth context, and that is the wiring check working.** `useAuth` throws outside a provider, deliberately, and adding the session bar to the shell made twelve existing tests fail immediately. Making `useAuth` tolerant would have removed the failure and the check with it; the tests provide the context, which is what the real application does.

## Verification

- **Unit, 21 assertions across PKCE, the token store and claim reading**: the verifier's length and alphabet against RFC 7636; 200 distinct values, which catches the constant a mocked CSPRNG produces; the S256 derivation against **the RFC's own worked example**, so it is checked against the standard rather than against itself; no `+`, `/` or `=` in a challenge; the token absent from both `localStorage` and `sessionStorage`, asserted by reading them; the renewal boundary from **both sides**, because a test that only checks the true case passes against a function that always returns true; and claim reading for the nested shape `P2-04` will introduce, for non-string entries, and for a token naming no subject.
- **Component, 10 tests**: the guard reached **by URL** with and without the role; any-of semantics; the anonymous, restoring, failed and forbidden states; the absence of a password field on the sign-in screen; the two `/auth` routes outside the guard; a renewal failure degrading to a prompt; and a non-session failure saying something is wrong instead.
- **E2E, 5 tests** against a real service: sign-in through the hosted page landing on the deep link; the token absent from storage **in the built bundle in a real browser**; session recovery on reload without leaving the console; the page surviving a renewal; logout, asserted **across a reload** — which is what separates "the console forgot its token" from "the session ended"; and a role-gated route refused by URL.

## What this task did not build

- **Any screen's content.** `P1-22` through `P1-24` own those; the routes are still placeholders behind the guard.
- **Multi-tab renewal coordination.** Two tabs renew independently: wasteful, harmless, and a lock in shared storage is a second mechanism for a problem nobody has.
- **CI wiring for the E2E suite.** `P1-27` owns it. The fixtures fail loudly when their environment is absent rather than skipping, per `P0-15`'s rule — a suite that skips reports success having run nothing.

# P1-21 — Console: OIDC Login (Dogfooding)

**Task**: [`TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-21](../../TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md)
**Spec required**: Yes — authentication surface
**ADR**: [ADR-019](../DECISIONS.md) — token storage

---

## 1. Business Objective

The console authenticates as an ordinary `type: spa` OIDC client, through the same endpoints, the same grant and the same hosted login page any other client uses.

`docs/PLAN/02` § Constraints and `docs/PLAN/06` § Why the Console Must Log In Through the Same OIDC Flow both require it, and the reason is not symmetry for its own sake: **a console with a private authentication path is a console whose login is untested by anybody else's use.** Every bug in the flow would be found by the team that wrote it, in the surface they are least likely to look at.

## 2. Actors

| Actor | What they do |
|---|---|
| An administrator with a browser | Signs in through the hosted page, uses the console, signs out |
| The console itself | Holds an access token in memory and renews it silently |
| **Nobody else** | There is no service account, no bootstrap credential and no console-only endpoint |

## 3. Functional Requirements

| # | Requirement |
|---|---|
| FR-1 | Authorization Code with PKCE, `code_challenge_method=S256`, verifier and `state` from `crypto.getRandomValues` |
| FR-2 | `state` validated on return, against what **this tab** generated |
| FR-3 | The access token lives in memory only (ADR-019) |
| FR-4 | Silent renewal ahead of expiry, and on a `401` |
| FR-5 | A renewal failure degrades to interactive login, never to a blank screen |
| FR-6 | The token is attached to every Management API call through the generated client |
| FR-7 | Logout calls `P1-10`'s endpoint and clears local state first |
| FR-8 | A route the user's claims do not permit is unreachable by URL, not merely hidden |

## 4. Non-Functional Requirements

- No new dependency. The flow is `fetch`, `crypto.subtle` and an iframe; an OIDC library would be a large dependency in the place where a supply-chain compromise reads Management API tokens.
- The console remains a static bundle. Nothing here needs a server.
- Every configuration value is build-time (`VITE_*`), for the reason `client.ts` already gives about the API base URL.

## 5. Dependencies

- **`P1-06`** the authorize endpoint, including `prompt=none` — which already answers `login_required` when there is no session, and is what makes silent renewal viable rather than aspirational.
- **`P1-07`** the token endpoint and PKCE verification.
- **`P1-10`** logout.
- **`P0-16`** the generated client, which is what the token is attached to.
- **`P0-17`** the shell, the tokens and the routing this hangs from.

## 6. Database Changes

None. The console is registered as an ordinary application row through `P1-18`'s API — which is itself part of the point: registering it uses the same endpoint anybody else would.

## 7. API Contract

None added. The console consumes `/oauth/authorize`, `/oauth/token` and `/oidc/logout` exactly as documented, and the Management API through the generated client.

## 8. Frontend Changes

- `lib/auth/pkce.ts` — the verifier, the challenge, and `state`.
- `lib/auth/tokens.ts` — the in-memory token and the claims the console reads.
- `lib/auth/oidc.ts` — the flow: begin, complete, renew, log out.
- `lib/auth/AuthProvider.tsx` — the four-state machine and the renewal timer.
- `app/RequireAuth.tsx` — the route guard.
- `pages/CallbackPage.tsx`, `pages/SilentCallbackPage.tsx`, `pages/SignInPage.tsx`.
- `app/shell/SessionBar.tsx` — who is signed in, and the way out.

## 9. Backend Changes

None. **If this task had needed one, that would have been the signal that the dogfooding constraint was being broken** — the whole claim is that the console needs nothing the service does not already offer every client.

## 10. Authorization Rules

**The console enforces nothing.** `docs/UI-UX/08` § Cross-Screen Requirements asks that a route the user's claims do not permit be genuinely unreachable rather than hidden, and `RequireAuth` does that — but `docs/PLAN/08` and `docs/SECURITY/02` §2–§3 are equally clear that the API is the only enforcement point.

So the guard is a user-experience feature, stated as one in its own doc comment and in the routes file. Somebody who edits their token, or the guard, or simply calls the API directly is refused by the server. What they do not get is a console screen that renders half a page and then fails.

## 11. Token storage

ADR-019, in full. In memory, recovered by `prompt=none` against the SSO session cookie. Not `localStorage`: a Management API token there outlives the tab, the browser restart and the incident response, and any script on the origin reads it by a well-known key.

The one thing the console does persist is the **pending request** — a verifier and a `state`, in `sessionStorage`, for one in-flight login. They are single-use values rather than credentials, they must survive a full navigation, and `sessionStorage` is per tab so two tabs logging in at once do not overwrite each other's verifier.

## 12. Validation

- `state` is compared to what this tab generated, before the code is used.
- The pending request is read once and removed, like the code it guards.
- The silent-renewal `postMessage` is checked for origin **and** for the `state` this renewal issued.
- A token that does not decode, or names no subject, is treated as no token.

## 13. Abuse Cases

| # | Abuse case | Control | Test |
|---|---|---|---|
| A-1 | **Login CSRF** — a crafted callback signs the victim into the attacker's account | `state` compared to this tab's value, before the code is exchanged | A callback with a mismatched `state`, and one with no pending request at all |
| A-2 | **Token theft via XSS** (`docs/SECURITY/02` §6, §14) | In memory only (ADR-019) | The token appears in neither `localStorage` nor `sessionStorage`, asserted by reading both |
| A-3 | **Code interception** | PKCE S256; the verifier never leaves the parent window, so even the renewal frame cannot complete an exchange | The RFC's own worked example, so the derivation is checked against the standard |
| A-4 | **A hostile frame harvesting the renewal code** | The silent callback posts to its own origin, never `*`; the parent checks origin and `state` | Asserted in the code path; the E2E covers the happy path |
| A-5 | **Reaching a screen by URL that the navigation hides** | `RequireAuth` on the route, not on the nav item | Navigate directly to the URL with the wrong role |
| A-6 | **A predictable verifier or `state`** | `crypto.getRandomValues` | 200 values, all distinct — which catches the constant a mocked CSPRNG produces |

## 14. Logging / Audit

The console logs nothing of its own. The events that matter — `user.login.success`, `session.created`, `token.issued`, `user.logout` — are the service's, and they are written because the console uses the same endpoints as any other client. That is the dogfooding constraint paying for itself.

## 15. Security Controls

- No refresh token requested and none held. The durable credential is the `HttpOnly` session cookie, which an administrator can revoke (`P1-11`) and script cannot read.
- `scope=openid` only. A wider scope is a decision about what a stolen token reaches.
- Logout clears the local token **before** redirecting, so an interrupted logout fails closed.
- The callback navigates with `replace`, so a spent code is not in the history.

## 16. Testing Strategy

- **Unit**: PKCE against RFC 7636's worked example; verifier distinctness; the token store's absence from browser storage; the renewal boundary from both sides; claim reading including the shapes `P2-04` will introduce.
- **Component**: the guard, navigated to by URL; the anonymous, restoring, failed and forbidden states; the absence of a password field.
- **E2E** (`P1-27` runs it in CI): login, renewal, logout against a real service.

## 17. Acceptance Criteria

The card's six DoD items.

## 18. Implementation Sequence

1. ADR-019, because the storage decision shapes everything else.
2. PKCE and the token store, with their tests.
3. The flow, the provider, the callback pages.
4. The guard and the routes.
5. The API client middleware.
6. E2E.

## 19. Rollback Strategy

The console is a static bundle; rolling back is redeploying the previous one. Nothing here changes the service, so a console rollback needs no coordination with it.

## 20. Technical Risks

| Risk | Mitigation |
|---|---|
| Silent renewal breaks and nobody notices until a user complains | The E2E covers renewal specifically; a failure degrades visibly to an interactive login rather than silently |
| A future screen reads a role claim and treats it as a control | Said in three places — the guard, the routes file, and the claims type — and the API refuses regardless |
| Third-party cookie policy blocks the renewal frame | Same-origin here; the fallback is interactive login, which is exercised rather than assumed |

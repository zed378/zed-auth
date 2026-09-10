# P1-10 — `GET /oidc/logout` — End Session

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. "Spec required — session lifecycle".

---

## 1. Business Objective

Logout that actually ends the session, plus the "log out of all sessions" action `docs/PLAN/05` names as an MVP requirement.

The distinction that matters: **clearing the cookie is not logging out**. A cookie the user copied, or a proxy logged, or an attacker captured still works if the server-side row survives. The record has to go.

## 2. Actors

| Actor | Interaction |
|---|---|
| A user | Clicks "sign out" in an application |
| An application (RP) | Redirects the browser here with an `id_token_hint` |
| An attacker | Wants to log somebody else out with a crafted URL, or to use a captured cookie afterwards |
| An attacker who controls a page the user visits | Wants a bare `<img src="…/oidc/logout">` to end the user's session |

## 3. Functional Requirements

- **FR-1** RP-initiated logout, with `post_logout_redirect_uri` matched **exactly** against the client's registered list.
- **FR-2** `id_token_hint` validated where supplied, so an attacker cannot force-log-out an arbitrary user by URL.
- **FR-3** The session is terminated server-side and the cookie cleared.
- **FR-4** "Log out of all sessions" terminates every session for the user and revokes their refresh tokens.
- **FR-5** A confirmation interstitial when there is no valid `id_token_hint`, rather than acting on an unauthenticated GET.
- **FR-6** Every logout audited, with the scope of what was terminated.
- **FR-7** Back-channel logout left to a later phase, without precluding it.

## 4. Non-Functional Requirements

- **NFR-1** The interstitial works with JavaScript disabled and carries the same headers as the login page.
- **NFR-2** No token is logged.
- **NFR-3** Revocation is effective immediately, not at the cache's TTL.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-11` | The session manager: `Revoke`, `RevokeAllForUser`, and the post-commit cache invalidation |
| `P1-05` | The registered `post_logout_redirect_uris`, and the exact-match rule |
| `P1-03` | Verifying the `id_token_hint` |
| `P1-12` | The browser-page machinery: CSRF, the CSP, the rendered shell |
| `P0-12` | The audit writer |

## 6. Database Changes

None. `RefreshStore` gains `RevokeAllForUser`, which is a query rather than a schema change.

## 7. API Contract

```
GET  /oidc/logout    id_token_hint?, post_logout_redirect_uri?, state?, client_id?
POST /oidc/logout    the interstitial's confirmation
```

`GET` either acts (when the request proves itself, §10) or renders the interstitial. `POST` is the interstitial's submit and always acts.

The endpoint joins `openapi/openapi.yaml` — it is a protocol endpoint an RP codes against, unlike `/login` — and is excluded from code generation like the other OAuth endpoints.

## 8. Frontend Changes

None in `console/`. The interstitial is server-rendered by the auth service, for the reason `P1-12`'s FR-1 gives.

## 9. Backend Changes

**In `internal/login`, not a new package.** That package already owns this service's server-rendered browser surface — its CSRF machinery, its hashed-CSP page shell, its header discipline, its branding read. A separate package would have to export all of it across a boundary to render one more page.

The cost is a package named `login` that also serves logout, and that is worth stating rather than hiding: the package is "the browser pages this service serves", and its doc comment now says so.

`session.RefreshStore` gains `RevokeAllForUser`.

## 10. Authorization Rules — when a GET may act without asking

This is the security core of the task, and it is one rule:

> **A `GET` logs somebody out only when the request proves it was initiated by a party that already holds a token for that very session.**

Concretely: a valid `id_token_hint` — signature verified against the published key set, `typ` of `JWT`, `iss` this service — **whose `sid` matches the session the cookie resolves to**.

Anything else gets the interstitial:

- No `id_token_hint`.
- A hint that does not verify.
- A hint for a different session or a different user.
- A hint that verifies but the browser carries no session.

The reason is `docs/SECURITY/02` §5. A `GET` is triggerable by any page on the internet — an `<img>`, a prefetch, a link in an email. Acting on one is a cross-site request forgery whose payload is "log this person out", which is a nuisance attack on its own and a stepping stone when logout is a prerequisite for something else. The `id_token_hint` is what turns an unauthenticated GET into an authenticated one, because only a party that completed the flow has the token.

**A hint for another user's session never logs anybody out.** Not the hint's owner, and not the cookie's owner. It falls through to the interstitial, which acts on the cookie's session and only with the user's click.

## 11. Validation

| Input | Handling |
|---|---|
| `id_token_hint` | Verified: signature, `typ: JWT`, `iss`. An unverifiable hint is treated as absent, never trusted |
| `post_logout_redirect_uri` | **Exact string match** against the resolving client's registered list. No normalisation, no prefix, no wildcard |
| `client_id` | Used to resolve the client when there is no hint. Never used to bypass the URI match |
| `state` | Echoed back on the redirect, unexamined. Opaque to us |
| Duplicated parameters | Refused, the way `P1-06` refuses them |

**If `post_logout_redirect_uri` is supplied and does not match, the request is refused with a rendered page and no redirect.** The same rule and the same reason as `P1-06`'s phase 1: reporting an error by redirecting to an unvalidated URI *is* the open-redirect vulnerability.

## 12. Error Handling

| Condition | Behaviour |
|---|---|
| Unregistered `post_logout_redirect_uri` | Rendered error, **no redirect**, no logout |
| Unknown `client_id` | Same. Indistinguishable from an unregistered URI, so the endpoint does not say which client ids exist |
| Invalid `id_token_hint` | Treated as absent: the interstitial |
| No session at all | The interstitial still renders and the confirmation still redirects, so an RP's logout link works for a user who was already signed out. Logging out twice is not an error |
| Session revocation fails | A rendered error. The user must not be told they are signed out when they are not |

## 13. Edge Cases

| Case | Handling |
|---|---|
| A hint whose `sid` matches but whose signing key has been retired | Not verifiable, so treated as absent |
| A hint for a session that has already ended | Treated as not matching: the interstitial |
| "Log out everywhere" with one session | Terminates that one. The count in the audit entry is what differs |
| No `post_logout_redirect_uri` | A "you are signed out" page. There is nowhere to send them and inventing somewhere is the vulnerability |
| A second logout of the same session | Idempotent — `session.Manager.Revoke` already treats an already-revoked session as nothing to do |
| `client_id` present and `id_token_hint` for a different client | The hint wins for deciding whether to act; the URI is matched against the hint's client |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Forced logout via a crafted URL | card, `docs/SECURITY/02` §5 | §10's rule: a GET acts only on a hint that matches the current session |
| A-2 | Forced logout via `<img src>` on an attacker's page | `docs/SECURITY/02` §5 | Same. Such a request carries no hint |
| A-3 | The session still usable after logout | card, `docs/SECURITY/02` §4 | The row is revoked and the cache invalidated **after** the commit |
| A-4 | A replayed cookie captured before logout | card | Same control — the cookie is not what is checked, the row is |
| A-5 | Open redirect through `post_logout_redirect_uri` | `docs/SECURITY/02` §6 | Exact match, and no redirect at all when it fails |
| A-6 | Enumerating client ids through the error | `docs/SECURITY/02` §12 | One message for an unknown client and an unregistered URI |
| A-7 | CSRF on the interstitial's POST | `docs/SECURITY/02` §5 | `P1-12`'s double-submit token |
| A-8 | Clickjacking the interstitial | `docs/SECURITY/02` §6 | `frame-ancestors 'none'`, `X-Frame-Options: DENY` |

## 15. Logging / Audit

| Event | Payload |
|---|---|
| `user.logout` | user id, org id, session id, `scope` (`session` or `all`), and the count when it is `all` |

Never the cookie, the `id_token_hint`, or any token. `session_id` is safe to name — that is `PG-14`'s separation of credential from identifier paying off again.

## 16. Security Controls

- The server-side row is revoked; the cookie is cleared as well, never instead.
- The cache is invalidated **after** the commit, using the function `session.Manager.Revoke` returns — the ordering `P1-11` was built around, because invalidating first lets a concurrent reader repopulate from the pre-commit state.
- Exact matching on `post_logout_redirect_uri`, with no redirect when it fails.
- A GET acts only on proof; everything else is a click.
- The interstitial carries the login page's headers: strict CSP, unframable, `no-referrer`, `no-store`.

**On the parameters travelling through the interstitial**: they are re-validated on the `POST`, rather than carried by an opaque server-side reference the way `P1-12` carries a pending authorization request. The difference is deliberate. A pending authorization has seven parameters whose *validation order* is itself the security property, so re-deriving it is a second place to get the ordering wrong. Logout has one parameter that matters and one function that validates it, called unconditionally on both paths — so a hidden field the user can edit is a hidden field the same check rejects.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | A GET with no hint renders the interstitial and **does not** revoke |
| Unit | A GET with an invalid hint does not revoke |
| Unit | A GET with a hint for another session does not revoke |
| Unit | A GET with a matching hint revokes without asking |
| Unit | An unregistered `post_logout_redirect_uri` is refused with no `Location` header |
| Unit | An unknown client and an unregistered URI give the identical response |
| Unit | The interstitial's headers match the login page's |
| Unit | CSRF on the POST |
| Integration | **After logout, the same cookie no longer authorises `/oauth/authorize`** — full re-authentication |
| Integration | "Log out everywhere" ends every session and revokes the refresh tokens |
| Integration | Logout is audited with the scope, and no token appears |
| Integration | Logging out twice is not an error |

The `/oauth/authorize` test is the one that matters: it asserts the *consequence* rather than the row's `revoked_at` column, which is what the DoD actually asks about.

## 18. Acceptance Criteria

1. After logout, a subsequent `/oauth/authorize` requires full re-authentication.
2. A replayed session cookie captured before logout is rejected.
3. `post_logout_redirect_uri` matching is exact; an unregistered value is refused.
4. "Log out of all sessions" terminates every session and revokes refresh tokens.
5. Logout events are audited.

## 19. Implementation Sequence

1. Parameter parsing and the `id_token_hint` verification.
2. The decision rule of §10, with its tests.
3. Revocation and the cookie.
4. The interstitial and its CSRF.
5. "Log out everywhere", and `RefreshStore.RevokeAllForUser`.
6. Audit, metrics, OpenAPI.

## 20. Rollback Strategy

No schema change. Rolling back removes the endpoint; sessions still expire on their own and can still be revoked through `P1-09`'s refresh revocation and the sessions API when `P1-19` lands.

## 21. Gap raised

**`PG-20` — `docs/PLAN/05` treats RP-Initiated Logout and Back-Channel Logout as the same thing.** The § MFA, Passwordless, Social Login, Logout bullet says:

> "Back-channel logout (RP-Initiated Logout) is a later phase; MVP needs per-app logout + a 'log out of all sessions' button."

Those are two different OpenID Connect specifications. **RP-Initiated Logout 1.0** is the front-channel redirect this task implements — the browser goes to `/oidc/logout` and comes back. **Back-Channel Logout 1.0** is server-to-server: the provider POSTs a logout token to each relying party's registered `backchannel_logout_uri`, with no browser involved. Neither implies the other.

Read literally, the sentence defers the endpoint the same document lists in its own core-endpoint table (`GET /oidc/logout → end-session (single logout)`) and then asks for "per-app logout" in the MVP — so the document contradicts itself unless the two specs are separated.

`P1-10`'s card already has it right: step 1 implements RP-initiated logout, step 7 defers back-channel. `TASKS/BACKLOG.md`'s `DF-09` copied the plan's conflation and should be narrowed to back-channel only.

**Built per the card.** `docs/PLAN/05` should be amended to name the two specifications separately, through the deliberate plan-change process.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| A later edit lets a bare GET act | Medium | High | Three tests assert a GET without proof does not revoke |
| The exact match is loosened to a prefix | Low | High | `P1-05`'s rejection table plus this task's own test |
| The cache is invalidated before the commit | Low | High | The manager returns the invalidation function; there is no other way to call it |
| `state` is examined rather than echoed | Low | Low | It is opaque and stays opaque |

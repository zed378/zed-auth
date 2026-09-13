# P3-09 — Session Management API

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-09 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend (Management API) |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-5, `docs/PLAN/05-API-CONTRACT.md` § Endpoint Structure, `docs/UI-UX/04-USER-FLOWS.md` Flow 4, `docs/UI-UX/08` (Sessions tab, personal settings), `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3 |
| **Depends on** | `P1-11` (sessions), `P1-07` (refresh tokens), `P3-08` (device and location derivation) |
| **Written** | 2026-09-13 |

---

## 1. Business objective

FR-5: a user can see where they are signed in and end any of it. Today the only
tool is "sign out everywhere" from the hosted logout page, which is the right
answer to "I think someone has my password" and the wrong one to "I left myself
signed in on the library computer" — it signs you out of the laptop you are
holding too.

An administrator needs the same view of a member's sessions, because "is this
person still signed in somewhere" is the first question in every offboarding and
every incident.

## 2. Actors

| Actor | Interest |
|---|---|
| A signed-in user | See their own sessions; end one, or every one but this |
| `ORG_ADMIN` / `ORG_OWNER` | See and end a member's sessions |
| `INSTANCE_OWNER` | The same, across organizations, through the logged instance path |
| An attacker holding a user's token | Must not reach anybody else's sessions, and must not learn more from a session list than the user would want shown |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | `GET /v1/me/sessions` lists the caller's live sessions |
| F-2 | `DELETE /v1/me/sessions/{session_id}` ends one of the caller's sessions |
| F-3 | `POST /v1/me/sessions/revoke-others` ends every session but the one the caller's token came from |
| F-4 | `GET /v1/organizations/{org_id}/users/{user_id}/sessions` lists a member's live sessions |
| F-5 | `DELETE /v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}` ends one |
| F-6 | Each session shows a device summary, a coarse location when known, when it began, when it was last used, when it expires, and whether it is the caller's current one |
| F-7 | Ending a session also revokes every refresh token issued through it |
| F-8 | Every revocation is audited with actor and target |

## 4. Non-functional requirements

- **Immediate.** A revoked session is refused on the very next request, including
  through the session cache. `P1-11`'s invalidation-after-commit is the mechanism,
  and the tests assert the consequence (a request refused), not the column.
- Listing is one indexed query (`sessions_user_idx`).

## 5. Dependencies

`session.Manager` and its cache; `token.RefreshStore`; `management` (caller,
policy table, scoping, faults, pagination); `anomaly.ParseDevice` and
`anomaly.Locator` from `P3-08`, so the sessions screen and the anomaly notice
describe a device the same way.

## 6. Data model changes

**None.** `sessions` already has `user_id`, `ip`, `user_agent`, `created_at`,
`last_seen_at`, `expires_at`, `revoked_at`; `refresh_tokens` already has
`session_id`.

## 7. API contract

```
GET    /v1/me/sessions                                             Member, self
DELETE /v1/me/sessions/{session_id}                                Member, self
POST   /v1/me/sessions/revoke-others                               Member, self
GET    /v1/organizations/{org_id}/users/{user_id}/sessions         ORG_ADMIN
DELETE /v1/organizations/{org_id}/users/{user_id}/sessions/{id}    ORG_ADMIN
```

`docs/PLAN/05`'s summary names only the organization path. The self-service
path is `FR-5`'s requirement expressed the way `P2-13` already expressed
`/v1/me/organizations`: a route about the caller, needing no role, that cannot
describe anybody else. Not a plan contradiction — the summary is a summary — but
recorded here so the addition is visible.

```json
{
  "sessions": [{
    "id": "…",
    "device": { "browser": "Chrome", "os": "Windows" },
    "location": "Jakarta, ID",
    "auth_methods": ["pwd", "otp"],
    "created_at": "…", "last_active_at": "…", "expires_at": "…",
    "current": true
  }],
  "page_info": { "next_page_token": "…" }
}
```

`device.browser` / `device.os` are `null` when unrecognised; `location` is
`null` when unknown or when no geolocation database is configured (`PG-41`).

**No IP address and no raw user agent** — see § 13 A-3.

## 8. Frontend changes

None here. `P3-11` (Sessions tab) and `P3-12` (personal settings) consume this.

## 9. Authorization rules

| Route | Rule |
|---|---|
| `/v1/me/sessions*` | A valid token. The user id and organization come **from the token**; nothing in the request names a user |
| Organization routes | `ORG_ADMIN` over the organization, the same requirement as deactivating the member — ending one session is strictly less than ending all of them |

A session id in the path is resolved **together with** the user it must belong
to, in one query, inside the tenant. A session belonging to someone else, to
another organization, or to nobody is the same `404`.

## 10. Validation rules

| Input | Rule |
|---|---|
| `session_id`, `user_id`, `org_id` | UUIDs, enforced by the generated wrapper |
| `revoke-others` | Needs a token bound to a session (`sid`). A token without one — `client_credentials` — has no "this session" to keep, and is refused with `400` rather than interpreted as "revoke all" |

## 11. Error handling

| Case | Answer |
|---|---|
| Unknown member on the organization route | `404` |
| Session not found, not the target user's, other tenant | `404` |
| Session already ended, but the target user's | `204` — ending an ended session is not an error, and a double-click must not produce one |
| Cache invalidation fails after commit | `500`, as deactivation does: the table says revoked and the cache would say valid |

## 12. Edge cases

| Case | Behaviour |
|---|---|
| The caller ends their own current session via `DELETE` | Allowed. It is a sign-out; the token stops working on the next request |
| `revoke-others` with no other sessions | `200` with `revoked: 0` |
| A session past its idle timeout but not revoked | Not listed. It cannot be used, and listing it would offer a "revoke" on something already dead |
| Unrecognised user agent (API clients, scripts) | Listed with a null device |
| An access token already issued to a consumer application | Stays valid at that application until it expires (≤ 10 min), **if** the application validates JWTs locally. Everything this service mediates — userinfo, introspection, refresh, the Management API — refuses it immediately. Stated in the contract rather than implied away |

## 13. Abuse cases

| # | Scenario (`docs/SECURITY/02`) | Control | Test |
|---|---|---|---|
| A-1 | Revoke another user's session with a self-service token (§2) | The session is looked up with the token's user id; no request field names a user | Another user's session id → `404`, still live |
| A-2 | An `ORG_ADMIN` of org A reaches org B's member | `ScopeOrganization` + RLS | Cross-tenant list and delete → `404` |
| A-3 | The list leaks precise location or a full fingerprint | Browser family and OS only; city and country only; no IP, no raw user agent | Response body contains neither the IP nor the user agent string |
| A-4 | A revoked session still usable through a cached path | Invalidation after commit; asserted by resolving the session cookie through a warmed cache with `session.Manager.Lookup` — the call `/oauth/authorize` makes — and by the Management API's `sid` check on the revoked token's next request | Both refused on the next request |
| A-5 | A refresh token from a revoked session keeps minting access tokens | All refresh tokens with that `session_id` revoked in the same transaction | Row state and refresh grant |
| A-6 | An ordinary member lists a colleague's sessions via the organization route | `ORG_ADMIN` required | `403`/`404` |

## 14. Logging and audit

`session.revoked`, as today, with `actor_user_id` the caller, `user_id` the
target, `reason` one of `admin`, `self_service`, `logout_others`, and
`refresh_tokens_revoked`. `revoke-others` writes one event listing the session
ids, as "sign out everywhere" already does.

**Correction from building it: the handler writes these events, not
`session.Manager`.** The first version audited inside the manager, as `Revoke`
does. Through the Management API that is wrong twice: the event carries no
request id and no client address, and the API's `AuditGuard` — which only sees
events written through `management.Audit` — reports every successful
revocation as an unaudited mutation. The new manager methods return what they
ended and the handler records it, in the same transaction, so a revocation that
cannot be recorded does not happen (tested).

A request that changed nothing — ending an ended session, `revoke-others` with
no others — writes no event and declares so with `management.Unchanged`. That
declaration did not exist; see the record for the existing endpoints that were
tripping the guard on correct behaviour.

## 15. Security controls

Summarised from §§ 9, 13: token-derived identity on self routes; one query binding
session to user; immediate invalidation; refresh tokens revoked with the session;
minimal detail in responses.

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | Response shaping (no IP, no raw UA), current marker, revoke-others refusal without `sid` |
| Integration | Every route through the real `/v1` chain against Postgres and Redis; immediacy through the cache; refresh revocation; the cross-user and cross-tenant cases |
| Mutation | Each control in § 13 |

## 17. Acceptance criteria

The card's Definition of Done.

## 18. Implementation sequence

1. Store and manager: list live, revoke owned, revoke all except; refresh by session.
2. `Caller.SessionID` from the token's `sid`.
3. OpenAPI operations, policy table, generated code, shipped paths.
4. Handler package, wiring.
5. Tests, mutation run.

## 19. Rollback strategy

No migration. A rolled-back build no longer serves the routes; nothing is left
inconsistent.

## 20. Technical risks

- **The per-application access token window** (§ 12) is inherent to JWTs, not to
  this task, and is the same window deactivation already has.

# P1-08 — `GET /oauth/userinfo`

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. The card marks it "Spec required — data exposure", which is the right reason: this endpoint's whole job is to hand personal data to a caller, and every decision in it is about how much.

---

## 1. Business Objective

The endpoint a consumer application calls to find out who is holding an access token. It is the last piece of the OIDC core surface a conforming client expects, and `docs/PLAN/12` measures it at `< 30ms` p50 / `< 100ms` p95 because SPAs call it on every page load.

Its objective is stated most usefully as a negative: **return exactly the claims the granted scopes authorise, and nothing else**. Every extra field is a field that leaks through every consumer that stores the response.

## 2. Actors

| Actor | Interaction |
|---|---|
| A consumer application | Presents an access token, gets the user's claims |
| An attacker with a stolen access token | Gets whatever the token's scopes allow, for as long as it is valid |
| An attacker with a forged token | Must be refused before any database read |
| A user who just logged out | Must stop being described by this endpoint immediately, not in ten minutes |

## 3. Functional Requirements

- **FR-1** Authenticate by bearer access token: signature, `typ`, issuer, audience, expiry.
- **FR-2** Map scopes to claims. `openid` → `sub`; `profile` → name and related; `email` → email. A claim outside the granted scopes is never returned.
- **FR-3** `sub` is a stable, non-guessable identifier — never sequential, never the email.
- **FR-4** An expired, forged or revoked token gets `401` with the RFC 6750 challenge.
- **FR-5** No organization-internal identifier appears in the response.

## 4. Non-Functional Requirements

- **NFR-1** One database round trip. `docs/PLAN/12`'s budget is 100ms at p95 for the whole request, and the signature verification is already CPU work.
- **NFR-2** No token is logged, in any branch (`CLAUDE.md`).
- **NFR-3** `Cache-Control: no-store`. The response is personal data addressed to one caller.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-03` | The key set and `signing.Verifier` — which until now had no caller at all |
| `P1-07` | Issues the tokens this endpoint verifies, and defines their claims |
| `P1-11` | The session whose liveness is checked |
| `P1-04` | Advertises the endpoint once it exists |

Depended on by: `P1-21` (console login), `P1-26` (demo applications).

## 6. Database Changes

None. Two gaps surfaced instead, both raised rather than invented — see §21.

## 7. API Contract

```
GET  /oauth/userinfo      Authorization: Bearer <access token>
POST /oauth/userinfo      Authorization: Bearer <access token>
```

OIDC Core 5.3.1 requires both methods. Both take the token **only in the `Authorization` header**.

RFC 6750 §2.3 also defines a `access_token` URI query parameter and calls it NOT RECOMMENDED. It is not supported here, and that is a decision worth stating: a query parameter is written to the access log of every proxy in the path, to the browser's history, and to the `Referer` of anything the page loads next. This service already refuses to put a credential in a URL at the authorization endpoint (`P1-06` keeps the pending request opaque); accepting one here would undo that on the endpoint that returns the user's email.

Response `200`:

```json
{ "sub": "…", "name": "…", "preferred_username": "…", "updated_at": 1757404800, "email": "…" }
```

Every field but `sub` is present only when its scope was granted **and** the underlying column is not null. `sub` is always present.

Errors carry the RFC 6750 challenge in `WWW-Authenticate` and `docs/PLAN/05`'s envelope in the body — the header because that is what an OAuth client library reads, the body because every other endpoint in this service speaks the envelope and a client that reads bodies should not have to special-case this one.

## 8. Frontend Changes

None.

## 9. Backend Changes

New package `internal/oauth/userinfo`: a claims builder (pure) and a handler.

`signing.Verifier.Verify` gains a **mandatory** `wantType` parameter:

```go
func (v *Verifier) Verify(compact, wantType string) ([]byte, error)
```

Not an additional `VerifyTyped` method beside a permissive `Verify`. The `typ` header is what stops an ID token being presented as a bearer credential (abuse case A-5, and half of what `P1-07` built the distinction for), and a permissive function with the shorter name is the one that gets called. Making it a parameter means the check cannot be forgotten, only got wrong — and got wrong is visible in review.

The verifier has had no caller until now. That is why changing its signature is cheap, and it is also the thing to be suspicious about: a verifier nothing has ever verified with.

## 10. Authorization Rules

The token is the authorization. There is no permission check: a valid access token for a live session describes its own subject and nothing else. Notably it cannot ask about **another** user — there is no user id parameter, by design.

## 11. Validation

In order, and the order is the point — nothing touches the database until the token is proven ours:

| # | Check | Failure |
|---|---|---|
| 1 | `Authorization: Bearer <token>` present and well-formed | `401`, bare challenge |
| 2 | Signature verifies against a key still in the set, `typ` is `at+jwt` | `401` `invalid_token` |
| 3 | `iss` is this service | `401` `invalid_token` |
| 4 | `aud` contains this service | `401` `invalid_token` |
| 5 | `exp` in the future, `iat` not absurdly ahead | `401` `invalid_token` |
| 6 | `scope` contains `openid` | `403` `insufficient_scope` |
| 7 | `sub` and `sid` present | `401` `invalid_token` |
| 8 | The session is live and the user is active | `401` `invalid_token` |

Step 8 is the only database access.

## 12. Error Handling

**One error code for every unusable token.** Expired, forged, wrong audience, unknown key, revoked session and deactivated user all answer `401` with `error="invalid_token"` and the same description. The specific reason goes to the log, not to the caller.

The alternative — a helpful `error_description` per case — turns the endpoint into a probe. "Signature invalid" versus "session revoked" tells an attacker holding a captured token whether the user has logged out since, which is exactly the sort of thing worth knowing before using it.

The exception is `insufficient_scope`, which is `403` and names the scope required. That one is not a disclosure: the caller already knows what it asked for, and RFC 6750 defines the code precisely so a client can tell "your token is broken" from "your token is fine but does not cover this".

## 13. Edge Cases

| Case | Handling |
|---|---|
| A `client_credentials` token | Refused. There is no user behind it: `sub` is the client, and there is no `sid`. An endpoint that answered would be describing an application as if it were a person |
| An ID token presented as a bearer token | Refused at the `typ` check, before `aud` is even read |
| A token signed by a key that has since been retired | Refused — `KeySet.ByKID` only returns keys in a verifying state |
| The user's `display_name` or `username` is NULL | The claim is omitted, not returned as `""`. An empty string is a value somebody chose |
| The session was revoked one second ago | Refused. See §16 |
| `profile` granted but not `email` | No `email` claim, and the response does not hint that one exists |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | An ID token used as an access token | `docs/SECURITY/02` §1 | `typ` must be `at+jwt`, checked before anything else about the token |
| A-2 | `alg: none` or algorithm confusion | `docs/SECURITY/02` §1 | `P1-03`'s closed algorithm list, applied before a key is fetched |
| A-3 | A token minted by another issuer | `docs/SECURITY/02` §1 | `iss` and signature both checked |
| A-4 | A token for another audience replayed here | `docs/SECURITY/02` §1 | `aud` checked |
| A-5 | A stolen token used after logout | `docs/SECURITY/02` §4 | Session liveness, §16 |
| A-6 | Enumerating users by varying `sub` | `docs/SECURITY/02` §12 | There is no parameter. The subject comes from the token |
| A-7 | Learning whether a user exists from the error | `docs/SECURITY/02` §12 | One `invalid_token` for every failure |
| A-8 | Reading the response cross-origin from an attacker's page | `docs/SECURITY/02` §12 | No CORS headers are sent, so a browser will not expose the body. See §21 |
| A-9 | The token appearing in a proxy log | `docs/SECURITY/02` §12 | Header only; no query parameter accepted |

## 15. Logging / Audit

**Not audited.** A successful userinfo call is a read of one's own claims by the holder of a token that was already audited when it was issued, and `docs/PLAN/12` says SPAs call it on every page load — auditing it would add volume proportional to page views and no signal. `P1-09`'s spec makes the same call about introspection, for the same reason.

Failures are logged at INFO with the reason class and never the token. A metric counts outcomes.

## 16. Security Controls, and the one that costs something

**Session liveness is checked, and it is a deliberate departure in spirit from `docs/PLAN/04`.**

`docs/PLAN/04` § What Is Deliberately Not Stored Here says access tokens are not stored because "storing them would add a lookup to the hottest path in the system for no security gain". That reasoning is about the token endpoint and about storing tokens. Neither changes here: nothing is stored, and this is not the hottest path.

What would change without the check is real: a user clicks "log out", their session is revoked, and this endpoint keeps describing them for up to ten minutes — the access token's remaining life. For a page-load-time identity call that is the difference between logging out and appearing to.

It costs nothing extra, because this endpoint has to read the user's row anyway. The liveness test is a predicate in that same query, in the same tenant-scoped transaction, in one round trip. If it needed a second round trip the trade would be worth arguing about; it does not.

Other controls: `Cache-Control: no-store`, no token in any log line, no organization identifier in the response, and the ordering in §11 so that an unverified token never reaches the database.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Every scope combination, asserting both what is present and **what is absent** |
| Unit | An `email` claim never appears without the `email` scope, and so on for each |
| Unit | No `org_id`, `sid`, `client_id` or internal identifier in any response |
| Unit | Header parsing: missing, wrong scheme, empty token, extra whitespace |
| Unit | The challenge header on each failure class |
| Integration | A real token from `P1-07` verifies and returns the real user's claims |
| Integration | **Revoking the session makes the same token stop working** |
| Integration | A deactivated user's token stops working |
| Integration | A token for another organization's user cannot read across tenants |
| Security | An ID token presented as a bearer token is refused |
| Security | A `client_credentials` token is refused |
| Security | A token signed by a retired key is refused |
| Security | Every failure returns the identical body and code |

The scope tests must assert absence, not just presence. A handler that returns every claim regardless of scope passes every "is `email` there when `email` was granted" test ever written.

## 18. Acceptance Criteria

1. Claims are exactly what the granted scopes permit, across combinations.
2. An expired token yields `401` and a correct challenge.
3. No organization-internal identifier appears.
4. Revoking the session invalidates the token here immediately.
5. `docs/PLAN/12`'s p95 target is met.

## 19. Implementation Sequence

1. `signing.Verifier.Verify` takes the wanted `typ`.
2. Token validation, with its ordering tests.
3. The claims mapping, with the absence tests.
4. The store read with the liveness predicate.
5. Discovery advertises the endpoint.
6. Metrics.

## 20. Rollback Strategy

No schema change. Rolling back removes the endpoint; discovery stops advertising it in the same build, so a client configuring itself afterwards never sees it.

## 21. Gaps raised, not filled

**PG-17 — there is no CORS policy anywhere in the plan.** `docs/PLAN/12` says SPAs call this endpoint on every page load, which means a browser, which means cross-origin — and no plan document specifies an origin policy. `docs/SECURITY/02` §12 names "overly permissive CORS" as a token-leak path, which is a warning without a rule to follow. The console (`P1-21`) is the second consumer and it is on a different hostname from the service.

This endpoint sends **no CORS headers**, so a browser will not expose its body cross-origin. That is the safe default and it is also the one that makes a browser-based consumer not work, which is the honest state of affairs — the alternative is inventing an origin policy for an endpoint that returns email addresses. Doing it properly needs a per-application allowed-origins list, and the data model has nowhere to put one either.

**PG-18 — `email_verified` has nothing behind it.** OIDC's `email` scope defines `email` and `email_verified`, and `docs/PLAN/04` § `users` has no verification column. The claim is therefore **omitted** rather than returned as `false`: absent means "not asserted", which is true, while `false` would mean "we checked and it is not verified", which we did not. `P1-19.5` (invitation acceptance) is where verification would first be established.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| A claim is added later without a scope gate | Medium | High | The absence tests enumerate what may appear; a new claim fails them until it is deliberately allowed |
| `Verify`'s new parameter is passed the wrong constant | Low | High | The type check has its own test in both directions, and only two constants exist |
| The liveness predicate is dropped in a later query rewrite | Medium | Medium | The integration test revokes a session and re-presents the same token |
| CORS is added carelessly to unblock the console | Medium | High | `PG-17` states the shape of the answer, so the shortcut is visibly a shortcut |

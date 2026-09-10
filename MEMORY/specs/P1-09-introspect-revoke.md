# P1-09 — `POST /oauth/introspect` and `POST /oauth/revoke`

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. "Spec required — token lifecycle".

---

## 1. Business Objective

Two endpoints that let the rest of an estate reason about tokens it did not issue.

- **Introspection** (RFC 7662) lets a resource server ask whether a token is currently good. It matters most for the refresh token, which is opaque and cannot be inspected any other way.
- **Revocation** (RFC 7009) lets a client throw a token away deliberately — on logout, on a user disconnecting an integration, on a suspected leak — rather than waiting for expiry.

Both are small endpoints where the interesting decisions are all about what they refuse to say.

## 2. Actors

| Actor | Interaction |
|---|---|
| A resource server | Introspects a token before honouring it |
| A client | Revokes a token it no longer needs |
| An attacker holding a stolen client secret | Can introspect and revoke — but only that client's own tokens |
| An attacker probing | Wants introspection to tell it whether a guessed token ever existed |

## 3. Functional Requirements

- **FR-1** Introspection requires client authentication. An unauthenticated introspection endpoint is a token oracle.
- **FR-2** `{"active": false}` for anything invalid, expired, revoked or unknown — never distinguished.
- **FR-3** Revocation accepts access and refresh tokens; revoking a refresh token invalidates its lineage.
- **FR-4** Revocation returns `200` even for an unknown token (RFC 7009 §2.2).
- **FR-5** Revocations are audited; routine introspection is not.

## 4. Non-Functional Requirements

- **NFR-1** No token, in any form, reaches a log line.
- **NFR-2** `Cache-Control: no-store` on both.
- **NFR-3** Neither endpoint is a place to push bytes: the form body is bounded.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-07` | Client authentication, the refresh store, and the OAuth error vocabulary |
| `P1-03` | Verifying an access token's signature |
| `P1-11` | Session liveness, which is what makes an access token's `active` answer honest |
| `P0-12` | The audit writer |

## 6. Database Changes

None.

## 7. API Contract

```
POST /oauth/introspect   token, token_type_hint?    → {"active": …}
POST /oauth/revoke       token, token_type_hint?    → 200, empty
```

Both `application/x-www-form-urlencoded`, both authenticated the way `/oauth/token` is — `client_secret_basic` or `client_secret_post`.

Introspection response when active:

```json
{ "active": true, "scope": "openid profile", "client_id": "…", "token_type": "Bearer",
  "exp": 1757404800, "iat": 1757404200, "sub": "…", "aud": "…", "iss": "…" }
```

When not: `{"active": false}` and nothing else. Not `{"active": false, "reason": …}` — see §12.

**`username` is deliberately absent**, though RFC 7662 lists it. It is an email address, the resource server already has `sub`, and a resource server that wants the address can ask `/oauth/userinfo` with the user's own token. Adding it here would put a personal identifier into a response that resource servers routinely log.

Both endpoints join `openapi/openapi.yaml` — they are protocol endpoints an integrator codes against, unlike `/login` — and are excluded from code generation like the other OAuth endpoints, because client credentials live in the `Authorization` header.

## 8. Frontend Changes

None.

## 9. Backend Changes

`internal/oauth/token` gains `introspect.go` and `revoke.go`. **Not a new package**: both endpoints need `ParseCredentials`, `AuthenticateClient`, the `Error` type and `RefreshStore`, and a separate package would mean exporting more of this one's internals to no benefit. They are token-lifecycle endpoints and this is the token-lifecycle package.

## 10. Authorization Rules

**A client may only introspect or revoke tokens issued to itself.** This is the rule that does the most work in this spec.

- An access token carries `client_id`; it must equal the authenticated client's id.
- A refresh token's row carries `client_id`; same comparison.

A token belonging to another client is answered exactly as an unknown one: `{"active": false}` for introspection, `200` and no action for revocation. Not `403` — a "forbidden" would confirm the token exists and belongs to somebody, which is the disclosure the uniform answer exists to prevent, and it is the card's second abuse case.

**Introspection additionally requires a *confidential* client.** A public client cannot authenticate — it has no secret — so admitting one would make the endpoint reachable by anyone who reads a `client_id` out of a browser URL, which is the token oracle FR-1 forbids. This is stricter than RFC 7662, which leaves the authentication method open.

**Revocation admits public clients**, and RFC 7009 §2.1 explicitly contemplates it. The reasoning differs because the operation does: a caller can only destroy a token it already holds and that already belongs to its own client, so the worst a forged caller achieves is throwing away a credential it was already holding.

## 11. Validation

| Input | Handling |
|---|---|
| `token` | Required. Bounded. Never logged |
| `token_type_hint` | Optional. A *hint*: the other type is still tried if it fails, per RFC 7662 §2.1 |
| Client credentials | As `/oauth/token` — Basic or post, never both |
| Anything else | Ignored |

## 12. Error Handling

**`{"active": false}` is the answer to every question the caller is not entitled to a better one for**: an unknown token, an expired one, a revoked one, a malformed one, one belonging to another client, and one whose session has since ended.

The temptation is a `reason` field to help an integrator debug. It would also tell an attacker holding a captured token whether it ever existed, whether it has been revoked since, and whether it belongs to the client they are impersonating — three separate disclosures for one convenience. The reason goes to the log.

Genuine protocol errors keep the OAuth shape `/oauth/token` already uses: `invalid_client` (401, with `WWW-Authenticate: Basic` where Basic was attempted), `invalid_request` (400) for a missing `token`.

## 13. Edge Cases

| Case | Handling |
|---|---|
| An access token presented to revoke | RFC 7009 §2.1's SHOULD: the refresh tokens for the same session **and the same client** are revoked. The access token itself keeps working until it expires — see §16 |
| A refresh token presented to introspect | Looked up; `active` reflects the row's revocation and expiry |
| An ID token presented to either | `typ` is `JWT`, not `at+jwt`, so it is not an access token and not a refresh token: `{"active": false}` and no revocation |
| An already-revoked refresh token presented to revoke | `200`, nothing happens. Idempotent by construction |
| A token from another organization | Its client is not the authenticated client, so it is invisible by the §10 rule |
| `token_type_hint` naming the wrong type | The hint is wrong, not the token; the other type is tried |
| A well-formed JWT signed by another issuer | Signature fails: `{"active": false}` |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Enumerating valid tokens through response shape | card, `docs/SECURITY/02` §12 | One `{"active": false}` for every negative, asserted byte for byte |
| A-2 | Enumerating through timing | card | See §16 — the cost is determined by the token's shape, which the caller can already see |
| A-3 | One client revoking another client's token | card | The ownership check in §10, answered as "unknown" |
| A-4 | One client introspecting another client's token | §10's other half | Same |
| A-5 | An unauthenticated caller introspecting | `docs/SECURITY/02` §12 | Client authentication required, confidential only |
| A-6 | A public client used as an introspection oracle | — | Public clients refused from introspection entirely |
| A-7 | Revoking a refresh token and keeping its descendants | RFC 7009 §2.1 | The whole family is revoked, not the one row |
| A-8 | A token in a log line | `CLAUDE.md` | Neither endpoint logs the token; the reason is logged without it |

## 15. Logging / Audit

| Event | Recorded |
|---|---|
| `token.revoked` | client id, user id, org id, how many rows, what kind. **Never the token** |
| Introspection | **Nothing.** Deliberate |

Introspection is not audited because a resource server may call it on every request, so auditing it would add volume proportional to API traffic and no signal — and an audit log nobody can read is an audit log nobody reads. The same call `P1-08`'s spec makes about userinfo. A metric counts outcomes.

## 16. Security Controls, and two honest limitations

**An access token cannot actually be revoked.** It is a signed JWT that nothing looks up, by `docs/PLAN/04`'s deliberate choice. Presenting one to `/oauth/revoke` revokes the refresh tokens behind it — which stops new access tokens being minted — but the presented token itself stays valid for up to its remaining ten minutes. RFC 7009 §5 warns clients about exactly this, and the OpenAPI description says it plainly rather than letting an integrator assume otherwise. What shortens the window in practice is `P1-08`'s session-liveness check and `P1-10`'s logout, not this endpoint.

**Timing does not distinguish a real token from a fake one, but it does distinguish their shapes.** A JWT-shaped input costs a signature verification; an opaque-token-shaped one costs a hash and an indexed lookup. Those differ from each other — and a caller can already see which shape it sent, so nothing is disclosed. Within each shape the cost is the same whether or not the token exists: the lookup runs either way, and the signature is verified either way.

Also: `Cache-Control: no-store`; a bounded body; the token never logged; and the ownership check applied before any answer is composed.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | The `active: false` response is byte-identical across every negative case |
| Unit | An active response carries the documented fields and **not** `username` |
| Unit | `token_type_hint` is a hint: a wrong hint still resolves the token |
| Integration | An unauthenticated introspection is refused |
| Integration | A public client is refused from introspection and admitted to revocation |
| Integration | **One client cannot introspect or revoke another client's token**, and gets the unknown answer |
| Integration | Revoking a refresh token invalidates its family, and a refresh with a sibling then fails |
| Integration | Revoking is idempotent |
| Integration | A revocation appears in the audit log, and the token does not |
| Integration | Introspecting an access token whose session was revoked answers `active: false` |
| Security | An ID token is not introspectable as an access token |

The byte-identical test is the one to write carefully: comparing "both say inactive" would pass while a `Content-Length` differed.

## 18. Acceptance Criteria

1. Unauthenticated introspection is rejected.
2. An unknown, an expired and a revoked token produce byte-identical responses.
3. Revoking a refresh token invalidates tokens derived from it.
4. Revocation events appear in the audit log.

## 19. Implementation Sequence

1. Shared credential handling and the ownership rule.
2. Introspection, with the uniformity tests.
3. Revocation, with the family test.
4. Audit and metrics.
5. OpenAPI, and the codegen exclusion.

## 20. Rollback Strategy

No schema change. Rolling back removes both endpoints; discovery stops advertising them in the same build.

## 21. Gap raised, not filled

**`PG-19` — per-client rate limiting has a requirement and no owner.** Step 5 of this card says "rate-limit both endpoints per client (`docs/PLAN/05` § Rate Limiting)". That section of Part A says only "limit login attempts per account … and per IP"; the per-`client_id` sentence lives in **Part B**, about the Management API. `P1-13` builds login limiting — per account and per IP, against credential stuffing — and nothing in Phase 1 builds a general per-client limiter.

**Not implemented here**, and the deferral is smaller than it sounds because both endpoints require client authentication: abuse costs an attacker a valid client secret, not merely a network connection. What it does not bound is a *compromised* client, which is the case a limiter would help with.

`P1-15` is the natural owner — it is where `/v1` gets its middleware, and a per-client limiter belongs beside it rather than being invented twice.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| The ownership check is dropped in a later edit | Low | High | Two integration tests, one per endpoint, using two real clients |
| A `reason` field is added to help debugging | Medium | Medium | The byte-identical test fails the moment the negative response varies |
| An integrator assumes access-token revocation works | High | Medium | Said plainly in the OpenAPI description, which is what they read |
| Introspection is quietly opened to public clients | Low | High | Its own test, and the reasoning in §10 |

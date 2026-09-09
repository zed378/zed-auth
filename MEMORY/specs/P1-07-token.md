# P1-07 — `POST /oauth/token`

Feature specification, per `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. `CLAUDE.md` requires one for anything touching authentication; the task card calls this the authentication core.

---

## 1. Business Objective

Turn an authorization code into tokens a consumer application can actually use, and do it fast. `PLAN/12` calls this "the highest call volume, most latency-sensitive" endpoint in the system and gives it p50 < 50ms, p95 < 200ms — every consumer's login latency is this endpoint's latency, and so is every silent renewal.

It is also where the flow finally closes. `P1-06` issues codes that nothing can redeem; after this, a consumer can complete a login end to end.

## 2. Actors

| Actor | Interaction |
|---|---|
| A consumer application's backend | Exchanges a code, or refreshes, presenting its client credentials |
| A public client (SPA, native) | Exchanges a code with no secret, authenticated solely by the PKCE verifier |
| A machine client | Uses `client_credentials`, with no user involved |
| A resource server | Validates the issued access token by signature and `aud` |
| An attacker holding an intercepted code | Tries to redeem it without the verifier |
| An attacker holding a leaked refresh token | Replays it |

## 3. Functional Requirements

- **FR-1** Support exactly three grants: `authorization_code`, `refresh_token`, `client_credentials`. Refuse `password` and `implicit` explicitly.
- **FR-2** Authenticate confidential clients by `client_secret_basic` or `client_secret_post`; public clients present only `client_id` and are authenticated by the PKCE verifier.
- **FR-3** Verify `code_verifier` against the stored `code_challenge` with S256, in constant time.
- **FR-4** Verify the code is unredeemed, unexpired, and bound to this client and this `redirect_uri`. Redemption is atomic.
- **FR-5** Issue an ID token with `iss`, `sub`, `aud`, `exp`, `iat`, `auth_time`, `nonce` (echoed when supplied) and `amr`.
- **FR-6** Issue an access token with a 5–15 minute lifetime carrying `org_id`, reserving the role-claim namespace `P2-04` will populate.
- **FR-7** Issue a refresh token stored only as a hash.
- **FR-8** Return OAuth error codes with the correct HTTP status and no internal detail.
- **FR-9** `Cache-Control: no-store` on every response.
- **FR-10** Instrument latency against `PLAN/12`'s targets.

## 4. Non-Functional Requirements

- **NFR-1** p50 < 50ms, p95 < 200ms, p99 < 400ms. The budget is one Redis `GETDEL`, one client lookup, one signature, one insert.
- **NFR-2** Client secret verification must stay cheap. ADR-016 chose SHA-256 over Argon2id specifically because this endpoint verifies on every request — that decision is spent here.
- **NFR-3** No token, secret, verifier or hash reaches a log, metric label, error body or audit payload.
- **NFR-4** The response body is never cacheable.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-06` | Issues the codes this redeems, and binds what must be checked |
| `P1-03` | Signs the tokens; `kid` selection and the key lifecycle |
| `P1-05` | Client authentication and grant permissions |
| `P1-11` | The session a code came from, for `auth_time` and revocation linkage |
| `P0-12` | Token issuance is audited |

Depended on by: `P1-08` (userinfo validates the access token), `P1-09` (introspection, revocation), `P1-26` (the demo applications), `P2-04` (populates the role claim).

## 6. Database Changes

**None.** `P0-07` created `refresh_tokens` with `family_id`, `replaced_by` and `family_expires_at` already — columns Phase 1 does not use, added so that `P3-06` changes behaviour rather than storage.

Phase 1 writes `family_id` (each issuance starts its own family) and leaves `replaced_by` null.

## 7. API Contract

`POST /oauth/token`, `application/x-www-form-urlencoded`, joining `openapi/openapi.yaml`. Unlike `/oauth/authorize` this is a normal JSON endpoint and **is** generated from the spec — its parameters are form fields validated in one pass, with no ordering constraint, so the generated binding is a help rather than an obstacle.

Errors are OAuth's JSON shape, not `PLAN/05`'s envelope:

```json
{ "error": "invalid_grant", "error_description": "..." }
```

| Error | Status |
|---|---|
| `invalid_request`, `invalid_grant`, `unauthorized_client`, `unsupported_grant_type`, `invalid_scope` | 400 |
| `invalid_client` | 401, with `WWW-Authenticate: Basic` when Basic was attempted |

## 8. Frontend Changes

None.

## 9. Backend Changes

New package `internal/oauth/token`, and `internal/signing` gains claim assembly.

```go
type Grant string // authorization_code, refresh_token, client_credentials

type Tokens struct {
    AccessToken  string
    IDToken      string
    RefreshToken Secret   // opaque, redacting type
    ExpiresIn    int
    Scope        []string
}
```

### The three token types are deliberately different things

| Token | Form | Why |
|---|---|---|
| ID token | JWT, `typ: JWT`, `aud` = `client_id` | An assertion *about the user*, for the client that asked |
| Access token | JWT, `typ: at+jwt` (RFC 9068), `aud` = the resource | A capability *at a resource server*, verified statelessly (`PLAN/12`: "storing them would add a lookup to the hottest path for no security gain") |
| Refresh token | **Opaque random**, stored as a hash | Must be revocable, so it must be looked up — which a JWT would defeat |

The refresh token being opaque rather than a JWT also sidesteps `P1-03`'s finding directly. That record established that sixteen distinct base64url encodings of one RSA signature all verify, so **reuse detection keyed on a JWT string can be defeated by mutating one character**. An opaque random token has exactly one representation, and its SHA-256 is canonical. `P3-06` inherits a foundation where that trap cannot be walked into.

## 10. Authorization Rules

The client must hold the grant it is using (`P1-05`). The code must have been issued to *this* client. Everything after the code is resolved runs in the organization the code carries.

`client_credentials` has no user, so it issues an access token only — no ID token (there is no one to make an assertion about) and no refresh token (the client can always authenticate again, so a refresh token would be a second, weaker credential for no benefit).

## 11. Validation

### Client authentication

| Method | Accepted for |
|---|---|
| `client_secret_basic` (`Authorization: Basic`) | Confidential clients |
| `client_secret_post` (`client_secret` form field) | Confidential clients |
| Neither, `client_id` only | Public clients only |

Presenting **both** Basic and a form secret is rejected: RFC 6749 forbids it, and more usefully, a request that authenticates two ways is a request where something is confused about which credential it holds.

A confidential client presenting no secret is `invalid_client`, never "treated as public". That downgrade is the client-authentication bypass in the abuse list.

Comparison is constant-time and the client is looked up before the secret is checked, so a wrong `client_id` and a wrong secret cost the same.

### PKCE

`S256`: `base64url(sha256(verifier)) == challenge`, compared in constant time. Verifier length 43–128, base64url alphabet (RFC 7636).

Required whenever the code carries a challenge, which — after `P1-06` — is always.

### The code

Every field the code bound is checked:

| Bound | Checked against |
|---|---|
| `client_id` | The authenticated client. Cross-client redemption is the abuse case |
| `redirect_uri` | The `redirect_uri` form field, by exact string comparison |
| `code_challenge` | The presented verifier |

## 12. Error Handling

Every failure to redeem is `invalid_grant` with no detail about which check failed. An error saying "wrong redirect_uri" rather than "unknown code" tells an attacker holding a code that the code is real.

| Condition | Response |
|---|---|
| Unknown, expired or already-redeemed code | `invalid_grant`, 400 — one answer for all three |
| Wrong verifier | `invalid_grant`, 400 |
| Code belongs to another client | `invalid_grant`, 400 |
| `redirect_uri` mismatch | `invalid_grant`, 400 |
| No secret from a confidential client | `invalid_client`, 401 |
| Wrong secret | `invalid_client`, 401 |
| Grant not permitted for the client | `unauthorized_client`, 400 |
| `password` or `implicit` | `unsupported_grant_type`, 400 |
| Signing key unavailable | `server_error`, 500 — the one place internal failure is admitted, because a client must distinguish "retry" from "your request is wrong" |

**The code is consumed even when a later check fails.** `GETDEL` removes it before the client or verifier is checked, so a wrong guess burns the code. That is deliberate: allowing retries against a live code turns a single-use credential into an oracle you can brute-force a verifier against.

## 13. Edge Cases

| Case | Handling |
|---|---|
| Code redeemed twice concurrently | Exactly one success (`P1-06`'s `GETDEL`) |
| Refresh token presented after the session it came from was revoked | Rejected. `P3-09` makes this systematic; Phase 1 checks the session is still live |
| `scope` narrower than the code's on refresh | Allowed — narrowing is the client's right |
| `scope` wider on refresh | `invalid_scope`. Refreshing must not gain authority |
| `client_credentials` requesting `openid` | `invalid_scope`. There is no user to identify |
| Clock skew making `iat` future | `iat` is our clock; consumers allow skew. Nothing to do here |
| Two refresh tokens issued from one session | Both valid. Phase 1 does not rotate |
| A public client sending a secret | `invalid_client`. It holds a credential it should not have |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Code replay after successful redemption | `PLAN/10` | Atomic `GETDEL`; the second attempt finds nothing |
| A-2 | Code redeemed without the verifier | `PLAN/10` | PKCE required and constant-time checked |
| A-3 | Cross-client code redemption | Task card | The code binds `client_id`; checked against the authenticated client |
| A-4 | Client authentication bypass — a confidential client's code redeemed with no secret | Task card | A confidential client with no secret is `invalid_client`, never downgraded to public |
| A-5 | Token substitution: an access token used where an ID token is expected | Task card | Different `typ` (`at+jwt` vs `JWT`) and different `aud` |
| A-6 | A token with the wrong `aud` accepted | `PLAN/11` | `aud` is the client for ID tokens and the resource for access tokens; asserted in tests |
| A-7 | Refresh token recovered from the database | `PLAN/04` | Stored as SHA-256 of an opaque 256-bit value |
| A-8 | Reuse detection defeated by mutating the token string | `P1-03` | Opaque token, not a JWT — one representation, canonical hash |
| A-9 | Verifier brute-forced by retrying against a live code | — | The code is consumed before the verifier is checked |
| A-10 | Timing oracle on client secret or verifier | `SECURITY/02` §1 | Constant-time comparison throughout |
| A-11 | Token response cached by a proxy | `SECURITY/02` §16 | `Cache-Control: no-store`, asserted on the header |

## 15. Logging / Audit Requirements

| Event | Payload | Never |
|---|---|---|
| `token.issued` | client id, user id, org id, grant, scope, session id | any token, code, verifier, secret or hash |
| `token.denied` | client id, grant, OAuth error code | the same |

Metrics: `auth_tokens_issued_total{grant}`, `auth_token_errors_total{error}`, `auth_token_duration_seconds{grant}` — bucketed on `PLAN/12`'s targets so a quantile query answers "did we meet it" without interpolating, the way `P0-11` did for the request histogram.

## 16. Security Controls

- Three grants, two refused by name.
- Confidential clients never silently downgraded to public.
- Constant-time secret and verifier comparison.
- Code consumed before later checks, so it cannot be an oracle.
- Every binding on the code re-checked.
- Distinct `typ` and `aud` separating the token types.
- Refresh tokens opaque and hashed.
- One error for every redemption failure.
- `Cache-Control: no-store`.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | PKCE verification: correct, wrong, empty, wrong length, non-base64url, and the challenge computed independently rather than by the same function under test |
| Unit | Client authentication: Basic, post, both at once, neither, public with a secret, confidential without |
| Unit | Claim assembly: every required claim present, `aud` correct per token type, `amr` from the session, `nonce` echoed only when supplied |
| Unit | Grant refusal for `password` and `implicit`, and per-client grant permission |
| Integration | Each of the three grants end to end against a real database and Redis |
| Integration | The full flow: authorize → code → token, with a real session |
| Integration | Concurrent redemption yields exactly one token set |
| Integration | Refresh tokens are stored only as hashes, by direct row inspection |
| Integration | A wrong verifier, a wrong client, a wrong redirect URI each fail — and each burns the code |
| Integration | The issued ID token verifies against the published JWKS, which is the property a consumer actually depends on |
| Security | Every abuse case A-1…A-11 |

The JWKS test is the one worth naming: it closes the loop from `P1-03`'s signing through `P1-04`'s publication to this endpoint's issuance, and it is the first test in the project that exercises all three together.

## 18. Acceptance Criteria

1. A code issued by `/oauth/authorize` exchanges for tokens.
2. A wrong `code_verifier` is refused, and the code is gone afterwards.
3. Concurrent redemption yields exactly one token set.
4. The ID token verifies against `/.well-known/jwks.json` and carries the full claim set.
5. `password` and `implicit` are refused by name.
6. No refresh token appears in the database.
7. Every response carries `Cache-Control: no-store`.

## 19. Definition of Done

The task's seven items plus the global DoD. Item 7 (latency under load) belongs to `P1-27`; this task ships the histogram bucketed on the targets.

## 20. Implementation Sequence

1. PKCE verification and client authentication — pure, fully tested.
2. Claim assembly and signing, against `P1-03`'s signer.
3. The `authorization_code` grant end to end.
4. `refresh_token` and `client_credentials`.
5. The OpenAPI entry and generated router.
6. Discovery grows `token_endpoint` — and only then can `P1-04`'s first DoD item be ticked.
7. Metrics and audit.

## 21. Rollback Strategy

No schema change. Rolling back removes the endpoint; issued access tokens keep verifying until they expire, which is the point of stateless verification. Refresh tokens become unusable until it returns.

Discovery must lose `token_endpoint` in the same rollback, for the reason `P1-06` gave about `authorization_endpoint`.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Refresh tokens do not rotate in Phase 1 | Certain | Medium | The card assigns rotation to `P3-06` and the schema is already shaped for it. A stolen refresh token is replayable until it expires or its session is revoked — stated plainly rather than left implied |
| A future change makes the refresh token a JWT | Low | High | It would reopen `P1-03`'s malleability trap for `P3-06`'s reuse detection. Recorded here and in the package comment |
| Retrying against a live code becomes possible | Low | High | The code is consumed first; a test asserts the code is gone after a failed verifier |
| `aud` conflated between token types | Medium | High | Different `typ` and `aud`, asserted per type |

# P1-06 — `GET /oauth/authorize` — Authorization Code + PKCE

Feature specification, per `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. `CLAUDE.md` requires one for anything touching authentication; the task card calls this the authentication core.

---

## 1. Business Objective

The endpoint where single sign-on becomes visible. A user who already has a session reaches the second application without seeing anything at all; a user who does not is sent to log in and comes back exactly where they were.

It is also the endpoint with the most ways to be wrong. It takes seven parameters from an untrusted redirect, decides whether to authenticate somebody, and ends by sending a browser somewhere with a credential in the URL. Almost every OAuth vulnerability that has a name lives here.

## 2. Actors

| Actor | Interaction |
|---|---|
| A consumer application | Redirects the browser here with a `client_id`, a `redirect_uri` and a PKCE challenge |
| The end user's browser | Arrives by top-level navigation, carrying the SSO cookie if there is one |
| The hosted login page (`P1-12`) | Where a user without a session is sent, and from which they return |
| `/oauth/token` (`P1-07`) | Redeems the code this endpoint issues |
| An attacker who controls a page the user visits | Crafts an authorize URL, hoping for an open redirect or a leaked code |
| An attacker who can observe a redirect | Sees the code in a URL and tries to redeem it |

## 3. Functional Requirements

- **FR-1** Validate `client_id` and `redirect_uri` **first**, before anything else. See §12 — this ordering is the security property.
- **FR-2** Require `response_type=code`. Nothing else is supported.
- **FR-3** Require PKCE for **every** client type, public and confidential alike, with `code_challenge_method=S256`.
- **FR-4** Require `state` to be present, and echo it unchanged on every redirect back.
- **FR-5** Validate `scope` against what the service supports and what the client's grants permit.
- **FR-6** With a live session, issue a code without rendering anything — the silent-SSO path.
- **FR-7** Without a session, persist the pending request server-side under an opaque id and redirect to the login page; resume on return.
- **FR-8** Honour `prompt=none` (never render UI; return `login_required`) and `prompt=login` (re-authenticate even with a session).
- **FR-9** Issue a single-use code with a lifetime under 60 seconds, bound to client, redirect URI, session, user, scope and `code_challenge`.
- **FR-10** Store codes in Redis with a sub-60-second TTL; redemption must be atomic (`PLAN/04` § What Is Deliberately Not Stored Here).
- **FR-11** Distinguish the silent-SSO path from the login-required path in metrics.

## 4. Non-Functional Requirements

- **NFR-1** The silent-SSO path meets `PLAN/12`: p50 < 50ms, p95 < 150ms, p99 < 300ms. Its budget is one session lookup (a Redis round trip after `P1-11`), one application read, and one Redis write.
- **NFR-2** No code, `code_challenge`, or `code_verifier` reaches a log, a metric label, an error message or an audit payload.
- **NFR-3** The endpoint is a browser entry point and must be safe to reach unauthenticated, repeatedly, from anywhere.
- **NFR-4** An unredeemed code cannot linger: expiry is the storage's, not a cleanup job's.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P1-05` | `MatchesRedirectURI`, grant checks, the application record |
| `P1-11` | The session lookup that makes silent SSO silent |
| `P0-05` | Redis holds codes and pending requests |
| `P0-12` | Authorization decisions are audited |
| `P0-16` | The endpoint joins the OpenAPI spec, so the generated router serves it (ADR-013) |

Depended on by: `P1-07` (redeems the code), `P1-12` (renders the login page and resumes the pending request), `P1-26` (the demo applications).

## 6. Database Changes

**None.** Codes and pending requests live in Redis with short TTLs, per `PLAN/04` § What Is Deliberately Not Stored Here: single-use and very short-lived, so durability across a restart is not required, and an automatic expiry is stronger than a cleanup job that can fail silently.

## 7. API Contract

`GET /oauth/authorize` joins `openapi/openapi.yaml`, which is what makes the generated router serve it (ADR-013) and what lets `P1-04`'s discovery document advertise `authorization_endpoint` truthfully for the first time.

**This endpoint does not speak `PLAN/05`'s JSON error envelope**, and that is worth stating because every other endpoint does. OAuth 2.1 defines its own error vocabulary delivered as query parameters on a redirect — `invalid_request`, `unauthorized_client`, `access_denied`, `unsupported_response_type`, `invalid_scope`, `server_error`, `temporarily_unavailable`, `login_required`. A consumer library parses those, not our envelope. The two vocabularies coexist; the spec documents which applies where.

Parameters: `client_id`, `redirect_uri`, `response_type`, `scope`, `state`, `code_challenge`, `code_challenge_method`, `prompt`, `nonce`, `max_age`.

Responses: `302` to the redirect URI with `code` and `state`; `302` to the login page; `400` rendering an error page (never a redirect).

## 8. Frontend Changes

None here. `P1-12` builds the login page and calls back into the pending-request resume path this task defines.

## 9. Backend Changes

New package `internal/oauth/authorize`:

```go
type Request struct {  // the validated parameters
    ClientID            string
    RedirectURI         string
    Scope               []string
    State               string
    CodeChallenge       string
    Nonce               string
    Prompt              []string
    MaxAge              *time.Duration
}

func Parse(q url.Values) (Raw, error)                     // shape only
func (h *Handler) Authorize(w, r)                         // the endpoint

type Code struct { ... }                                  // what a code binds
type Store interface {
    IssueCode(ctx, Code, ttl) (string, error)
    RedeemCode(ctx, code string) (Code, error)            // atomic, single-use
    SavePending(ctx, Request, ttl) (string, error)
    LoadPending(ctx, id string) (Request, error)
}
```

The `Store` is Redis-backed. `RedeemCode` is a single `GETDEL`, which is atomic by construction — two concurrent redemptions yield exactly one success, as `PLAN/04` requires. Doing it as `GET` then `DEL` would be the classic replay window.

## 10. Authorization Rules

There is no permission check here: the endpoint's job is to *establish* who the user is. What it does enforce is that the client asking is registered, that the redirect it names is one of its own, and that the grants it wants are ones its type may use — all from `P1-05`.

The session it finds determines the organization. Everything read afterwards is scoped to that organization.

## 11. Validation

### Order is the security property

Parameters are validated in two phases, and the split is not stylistic.

**Phase 1 — before any redirect is possible.** `client_id` must resolve to a registered application. `redirect_uri` must match one of its registered URIs by exact string comparison (`P1-05`). If either fails, the response is a rendered error page with **no redirect at all**.

That is the whole point. An error reported by redirecting to an unvalidated `redirect_uri` *is* the open-redirect vulnerability; the attacker supplies a `redirect_uri` they control, supplies something else invalid, and the service obligingly sends the browser to them. So nothing can be reported by redirect until the redirect target is known to be legitimate.

**Phase 2 — reported by redirecting to the now-validated URI**, carrying `error`, `error_description` and the original `state`.

| Parameter | Rule | Error |
|---|---|---|
| `response_type` | Exactly `code` | `unsupported_response_type` |
| `code_challenge` | Present, 43–128 chars, base64url | `invalid_request` |
| `code_challenge_method` | Exactly `S256` | `invalid_request` |
| `state` | Present and non-empty | `invalid_request` |
| `scope` | Every value known; `openid` required; `offline_access` only if the client has the `refresh_token` grant | `invalid_scope` |
| `prompt` | Known values only; `none` not combined with others | `invalid_request` |
| `nonce` | Optional, bounded length | `invalid_request` if oversized |
| `max_age` | Optional non-negative integer seconds | `invalid_request` |

### PKCE is mandatory for every client type

`PLAN/05` says PKCE is "Mandatory for all clients (not just public clients)", which is stricter than OAuth 2.1's baseline. A confidential client omitting `code_challenge` is rejected exactly like a public one.

The reason it is worth being stricter: a confidential client's secret protects the *token* request, not the authorization code in transit. A code intercepted from a browser redirect — a referrer leak, a malicious app registered for the same custom scheme, shoulder-surfing a URL bar — is useless without the verifier regardless of what the client can prove later.

### On `state`

`state` is required and echoed unchanged. What this endpoint **cannot** do is validate it: `state` is opaque to us and only the client knows what it sent. The task card's abuse case says "a missing or mismatched `state`", and only the first half is ours — mismatch is detected by the consumer, which is the entire mechanism. Requiring presence is real hardening (a client that forgot `state` has no CSRF defence and should be told at integration time rather than in production), and claiming to verify it would be a false claim about who is protecting whom.

## 12. Error Handling

Beyond the two-phase split above:

| Condition | Behaviour |
|---|---|
| `prompt=none` with no usable session | Redirect with `error=login_required`. Never render UI — an SPA doing silent renewal in a hidden iframe must get a machine-readable answer, not a login form |
| `prompt=none` with a session that fails `max_age` | Redirect with `error=login_required` |
| `prompt=login` with a live session | Ignore the session and send to the login page. The session is not revoked — the user asked to re-authenticate, not to be logged out of everything |
| `prompt` contains `none` and another value | `invalid_request`. The combination is contradictory and guessing an intent is worse than refusing |
| Redis unavailable | `server_error` by redirect. A code that cannot be stored must not be issued, because it could never be redeemed |
| Session lookup fails | Treated as no session, which routes to login. Failing closed here is a login prompt, which is recoverable |
| The client is registered but lacks `authorization_code` | `unauthorized_client` |

An `error_description` is included and is deliberately terse: it says which parameter was wrong, never what the correct value would be. "redirect_uri does not match" is helpful to the integrator who owns the client; "redirect_uri does not match https://app.example/cb" hands a prober the registered URI.

## 13. Edge Cases

| Case | Handling |
|---|---|
| Duplicate query parameters (`?state=a&state=b`) | Rejected as `invalid_request`. Taking the first or the last is how a parameter-pollution bypass gets in |
| `redirect_uri` omitted entirely | Rejected in phase 1. Some servers default to the single registered URI; that is a convenience that makes the exact-match rule conditional |
| The user has a session in a different organization than the client | The client belongs to a project in one organization; a session for another is not a session for this client. Routed to login |
| A session that is live but older than `max_age` | Re-authentication required |
| Code issued, browser never returns | It expires. Nothing to clean up — that is why Redis holds it |
| Two tabs authorize simultaneously | Two independent codes; each redeems once |
| Code redeemed twice | Second attempt fails. `P1-07` additionally treats it as a reuse signal |
| `scope` contains duplicates | Deduplicated, not rejected — harmless and common |
| Very long parameters | Bounded before parsing; a URL is attacker-controlled and unbounded |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Open redirect via an unregistered `redirect_uri` | `SECURITY/02` §1 | Phase-1 validation; no redirect before the target is validated |
| A-2 | Open redirect via an error response | Same | The same ordering — this is the subtle half, and the reason the phases exist |
| A-3 | Authorization code interception without the verifier | `PLAN/10` | PKCE mandatory for all client types |
| A-4 | Code replay | `PLAN/10` | Atomic `GETDEL`; one success per code |
| A-5 | A code issued to client A redeemed by client B | Task card | The code binds `client_id`; `P1-07` checks it |
| A-6 | A code redeemed against a different `redirect_uri` | OAuth 2.1 | The code binds the redirect URI too |
| A-7 | Missing `state` leaving the client with no CSRF defence | `SECURITY/02` §5 | Required at the endpoint |
| A-8 | Parameter pollution to bypass validation | — | Duplicate parameters rejected |
| A-9 | `prompt=none` used to probe for a live session | — | Accepted: that is what it is *for*. It reveals only whether the browser has a session with us, which the browser already knows |
| A-10 | Codes accumulate and are guessable | `PLAN/04` | 256-bit random, sub-60s TTL, single use |
| A-11 | A pending-request id used to resume somebody else's flow | — | Opaque, random, short-lived, single-use, and it carries no authority of its own — resuming still requires authenticating |

## 15. Logging / Audit Requirements

| Event | Payload | Never |
|---|---|---|
| `oauth.authorize.granted` | client id, user id, org id, scope, silent or interactive | the code, the challenge, `state`, `nonce` |
| `oauth.authorize.denied` | client id, the OAuth error code, whether a redirect was possible | the code, the challenge |

Metrics: `auth_authorize_total{outcome,path}` where path ∈ {`silent`, `login_required`, `error`}, and `auth_authorize_duration_seconds{path}` — separate because `PLAN/12` sets a target for the silent path specifically and an average across both would hide it.

## 16. Security Controls

- Two-phase validation; no redirect until the target is validated.
- Exact redirect matching (`P1-05`), no normalisation.
- PKCE mandatory for every client type; `S256` only.
- `state` required.
- Codes: 256-bit, single-use, sub-60-second, bound to client, redirect URI, session, user, scope and challenge.
- Atomic redemption.
- Duplicate parameters rejected.
- Bounded parameter lengths.
- Terse error descriptions that name the parameter, never the expected value.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Every parameter rule, valid and invalid, including duplicates and oversized values |
| Unit | PKCE required for all five client types — the DoD calls out confidential clients specifically |
| Unit | `code_challenge_method=plain` rejected; absent rejected |
| Unit | Prompt handling: `none`, `login`, both together, unknown values |
| Unit | Scope: unknown, missing `openid`, `offline_access` without the grant |
| Integration | **No redirect on phase-1 failure** — asserted on the response: status, `Location` absent, body is an error page. This is the open-redirect test and it must check for the *absence* of a header |
| Integration | Phase-2 failures redirect to the registered URI with `error` and the original `state` |
| Integration | Silent SSO: with a session, a code is issued with no interaction |
| Integration | `prompt=none` without a session returns `login_required` by redirect, with no HTML rendered |
| Integration | Codes are single-use: the second redemption fails |
| Integration | Concurrent redemption yields exactly one success — driven with real goroutines against real Redis |
| Integration | A code expires within 60 seconds |
| Security | Every abuse case A-1…A-11 |

The concurrency test matters more than it looks. `PLAN/04` requires atomic redemption, and a `GET`-then-`DEL` implementation passes every sequential test.

## 18. Acceptance Criteria

1. A request with no `code_challenge` is rejected for a `web` client, not only a `spa`.
2. `code_challenge_method=plain` is rejected.
3. An unregistered `redirect_uri` produces a page with no `Location` header.
4. With a session, a code comes back with no user interaction.
5. `prompt=none` without a session yields `error=login_required` on the redirect.
6. A code redeems once; the second attempt fails; concurrent attempts produce exactly one success.

## 19. Definition of Done

The task's seven items plus the global DoD. Item 7 (the p95 target under load) belongs to `P1-27`'s load test; what this task delivers is the metric that measures it, labelled so the silent path is separable.

## 20. Implementation Sequence

1. Parameter parsing and validation, pure, with the full table.
2. The Redis store: codes and pending requests, with the atomic redemption.
3. The handler: phase-1, session lookup, prompt handling, code issuance.
4. The OpenAPI entry, so the generated router serves it.
5. Discovery grows `authorization_endpoint` and `code_challenge_methods_supported` — the first time `P1-04`'s document says more than four things, and only because it is now true.
6. Metrics and audit.

## 21. Rollback Strategy

No schema change. Rolling back removes the endpoint; codes in Redis expire on their own within a minute. Discovery must lose `authorization_endpoint` in the same rollback, or it advertises something that 404s — which is exactly the failure `P1-04` exists to prevent, and is the one coupling worth remembering here.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| A future change reports a phase-1 error by redirect | Medium | Critical | The test asserts the absence of a `Location` header, which is an unusual assertion and therefore a conspicuous one to break |
| `GETDEL` replaced by `GET`+`DEL` during a refactor | Low | High | The concurrency test fails; sequential tests would not |
| Discovery advertises `authorization_endpoint` while `token_endpoint` is still absent | Certain until `P1-07` | Low | Accurate: both are true statements about what exists. A conforming client will fail at configuration, which is the correct outcome and better than a client that configures and fails mid-login |
| The pending-request store becomes a place to park data | Low | Medium | Short TTL, bounded size, single use |

# 05 - Rate Limiting

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-13, P1-15, closes PG-19 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the per-client request quota applied to the `/v1` Management API: what is counted, the headers a client sees, and what happens when the counter's own store (Redis) is unavailable.

## Scope

Request-rate quotas on the Management API (`backend/internal/ratelimit/quota.go`, wired in `backend/internal/management/quota.go`). This is a **different mechanism** from the login brute-force cooldown (`backend/internal/ratelimit/ratelimit.go`, `Policy`/`Evaluate`/`Next`), which bounds failed password attempts per submitted address/IP on the hosted login page rather than requests per client on the API — that cooldown is covered in `docs/IDENTITY-PROTOCOL/` and `docs/SECURITY/`, not here. This document also does not cover the mail-flooding bound (`ratelimit.MailKey`/`ConsumeMail`), which guards invitation email volume rather than API requests.

## As Built

**Counts requests, not failures, per `client_id`.** `docs/PLAN/05-API-CONTRACT.md` Part B: "Rate limits applied per `client_id`/API key (not just IP)". Keying on the client id rather than the user or the IP is deliberate: a service account's credentials are long-lived, get copied into CI, and do not change when the person who created them leaves — and keying on the client id survives a secret rotation, since the id is exactly the part that does not change when a secret does.

**A fixed window, `600` requests per minute per client** (`ratelimit.PerClient`), with one exception: `POST /v1/authz/check` gets its own, much larger bound, `6000` requests per minute (`ratelimit.PerClientAuthz`), because it is called on every protected request a consumer application serves rather than once per administrative action. `RateLimit.HotRoutes` (`backend/internal/management/quota.go`) is the one-entry table naming which routes get the larger allowance — `P2-17`'s load test found the default bound refusing 19,658 of 40,515 requests at 20 concurrent workers against this endpoint before the higher limit was introduced.

**The window is fixed, not sliding**, and the tradeoff is stated rather than hidden: a client can send `Limit` requests just before a window boundary and `Limit` again just after, so the true worst case over a sliding minute is `2×Limit`. For a bound whose purpose is "stop one client from consuming the estate", not metering a paid API, that factor of two is accepted in exchange for a `X-RateLimit-Reset` that names a real, predictable moment (`ratelimit.Quota.WindowStart`).

**Counted before the handler runs, and regardless of outcome.** An enumeration run — which is mostly 404s — is bounded exactly like legitimate traffic; counting only successes would let it proceed unbounded (`ratelimit/quota.go` `Quotas.Consume`).

**The limiter fails open, loudly (ADR-017).** If Redis cannot be reached, `Quotas.Consume` returns the full allowance (`Quota.Unlimited`) rather than refusing every request — the Management API's own availability must not depend on Redis — and logs a warning naming the client id, plus an `Unavailable` metric. This mirrors the breached-password check's fail-open design (ADR-015) for the same reason: an outage in a supporting store must not become an outage in the primary function.

**Headers are sent on every response, not only on a refusal.** A client that learns its remaining allowance only once it has run out cannot pace itself (`backend/internal/management/quota.go` `writeQuotaHeaders`).

## Rules and Defaults

| Setting | Value | Enforced in |
|---|---|---|
| Default per-client quota | 600 requests / 60s, fixed window | `backend/internal/ratelimit/quota.go` `PerClient` |
| `/v1/authz/check` quota | 6000 requests / 60s | `backend/internal/ratelimit/quota.go` `PerClientAuthz`; selected via `management.HotRoutes` |
| Quota key | `ratelimit:client:<client_id>:<window_start_unix>` | `ratelimit.ClientKey` |
| Counting point | Before the handler runs, on every method and outcome | `backend/internal/management/quota.go` `RateLimit.Wrap` |
| Store outage behaviour | Fail open (full allowance), logged, metered | `backend/internal/ratelimit/quota.go` `Quotas.unavailable`; ADR-017 |
| Response headers | `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (Unix seconds), on every response | `backend/internal/management/quota.go` |
| `429` body | `Error` envelope, `code: RATE_LIMITED`, `Retry-After` header in seconds (rounded up) | `backend/internal/management/errors.go` |
| Middleware position | Inside `Require` (after authentication — the bound is per-client and there is no client before the token is read), outside `Idempotency` (a replay is still a request and must still be counted) | `backend/internal/management/chain.go` |

## Interfaces

No dedicated endpoint — this is cross-cutting middleware applied to every `/v1` route via `Chain.Handle`/`Chain.Guarded`. A `429` response:

```json
{
  "error": {
    "code": "RATE_LIMITED",
    "message": "Too many requests. Please retry later."
  }
}
```

with headers:

```
X-RateLimit-Limit: 600
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1789574460
Retry-After: 42
```

## Security Considerations

- Rate limiting `/v1` per client, not per IP, so one noisy tenant behind a shared NAT cannot exhaust another tenant's budget (`openapi/openapi.yaml` `components/responses/RateLimited` description).
- Fail-open is a deliberate availability-over-strictness trade (ADR-017); it means a sustained Redis outage removes this specific control. The `Unavailable` metric exists precisely so that removal is visible to an operator rather than silent — see `docs/PLAN/13-OBSERVABILITY.md`.
- `/v1/authz/check`'s larger bound does not materially increase enumeration value: the endpoint answers allow/deny for one action at a time within the caller's own project and is deliberately unhelpful as an oracle (a nonexistent subject and an unauthorized one are byte-identical) — see `ratelimit/quota.go` `PerClientAuthz` comment and `12-ROLE-AND-PERMISSION-API.md`.

## Verification

- `TestTheClientQuotaIsEnforcedAndReported`, `TestRotatingAClientSecretDoesNotResetTheBound`, `TestAReadIsCountedAgainstTheQuota` — `backend/internal/management/chain_integration_test.go`.
- `backend/internal/ratelimit/quota_test.go` (pure `Decide`/`WindowStart` arithmetic) and `quota_integration_test.go` (against real Redis).
- Load test evidence for the `/v1/authz/check` bound: `scripts/loadtest/`, referenced from `P2-17`.

## Not Yet Built / Open Questions

- The three OAuth protocol endpoints that take client credentials directly (`/oauth/token`, `/oauth/introspect`, `/oauth/revoke`) are not yet bound by per-client quota — `TASKS/BACKLOG.md` PG-19 notes this gap explicitly: "`P1-15` built the mechanism `PG-19` asked for and wired it to `/v1` only." This is a real, currently-open gap, not a design choice to leave undocumented.

## Related Documents

- `TASKS/BACKLOG.md` PG-19
- `MEMORY/DECISIONS.md` ADR-017 (fail open, loudly), ADR-015 (the analogous breach-check trade)
- `docs/PLAN/05-API-CONTRACT.md` Part B, `docs/PLAN/12-PERFORMANCE.md`
- `docs/API/04-ERROR-HANDLING.md`, `docs/API/07-IDEMPOTENCY.md`

# 07 - Idempotency

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-15 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the `Idempotency-Key` header: what it protects, how a claim/replay/release actually works against PostgreSQL, and the cases where a retry is deliberately refused rather than served from the record.

## Scope

The `Idempotency-Key` mechanism (`backend/internal/management/idempotency.go`, `idempotency_middleware.go`). Not in scope: the rate limiter (`05-RATE-LIMITING.md`), which runs before this and still counts a replayed request.

## As Built

**Backed by PostgreSQL, not Redis.** Unlike the rate limiter, idempotency records live in the `idempotency_records` table and are claimed with an `INSERT ... ON CONFLICT DO NOTHING` — the insert itself is the lock, avoiding a check-then-insert race where two concurrent requests with the same key both "see" no existing record and both run the handler.

**Opt-in, and only where a replay is dangerous.** The header is optional (`docs/PLAN/05-API-CONTRACT.md` Part B says POST endpoints "support" it); a request without one runs exactly as it would have. It is honoured only on `POST`, `PUT`, `PATCH`, `DELETE` — never `GET`/`HEAD`, which are already idempotent by method, and honouring a key there would mean caching a permission-dependent read under a caller-chosen name.

**Three, and only three, outcomes** (`IdempotencyStore.Begin`):
1. **The key is unclaimed** (or its prior claim has expired): the handler runs, and the middleware stores what it answered.
2. **The exact same request was already answered**: the original response is replayed verbatim, with `Idempotency-Replayed: true`, and the handler does **not** run again.
3. **A conflict**: refused with `409 CONFLICT`, in three distinguishable situations — the same key reused with a **different** request (different method, path, or body — all three are hashed together, so reusing a key across two different endpoints is treated as a different request even if the bodies happen to match); the first request with this key is **still in flight** (never queued — waiting would hold a connection for as long as the first request takes, and queuing would defeat the header's purpose entirely); or the record vanished between the claim and the read (an extremely rare expiry-sweep race), where the caller is told to retry.

**The key is scoped to `(org_id, client_id, key)`.** A caller without a `client_id` claim (i.e., a token that did not come from an OAuth client) cannot use idempotency keys at all — `400 VALIDATION_ERROR`, `"Idempotency-Key requires an access token issued to a client"` — because without a client there is no namespace to key the record on, and inventing one would collapse every caller in an organization into a shared idempotency namespace.

**Only a 2xx success is stored for replay.** A `4xx`/`5xx` response is answered normally and the claim is **released**, not stored — a `500` must stay retryable, and a `400` must be retryable with a corrected body; storing either would refuse the caller's own correction as a conflict for up to 24 hours. A success whose body is not valid JSON, or that exceeds the stored-response size bound, is also answered normally but not stored (the claim is released) rather than replayed in a broken form.

**A claim left behind by a crash is released, not stuck.** Every code path out of the middleware after a successful claim either stores an answer or releases the claim (`defer` in `idempotency_middleware.go` `Wrap`) — a claim that outlived a panic would otherwise refuse the caller's retries for a full day.

**The key itself is never logged.** `docs/PLAN/05-API-CONTRACT.md`'s NFR-2 forbids it: a caller-chosen string is exactly the kind of thing a provisioning script uses an email address, an employee number, or an internal record id for, none of which belongs in a log. Only a 12-character SHA-256 fingerprint of the key is logged, sufficient to correlate two log lines about the same key without disclosing what the key says.

**The request body is hashed, never stored.** Storing the body itself would put whatever a caller sent — including, for example, a password on a user-creation call — into a durable table; only its SHA-256 hash (combined with method and path) is kept for comparison.

## Rules and Defaults

| Setting | Value | Enforced in |
|---|---|---|
| Header | `Idempotency-Key`, optional | `openapi/openapi.yaml` `components/parameters/IdempotencyKey` |
| Key length | 1–255 characters, printable ASCII only (`0x21`–`0x7E`) | `backend/internal/management/idempotency.go` `ValidateKey` |
| Record scope | `(org_id, client_id, key)` | `idempotency.go` `Begin`/`Complete`/`Release` |
| Record TTL | 24 hours from claim | `idempotency.go` `IdempotencyTTL` |
| Applies to methods | `POST`, `PUT`, `PATCH`, `DELETE` — never `GET`/`HEAD` | `idempotency_middleware.go` `Wrap` |
| Request buffer bound (for hashing) | 1 MiB | `idempotency_middleware.go` `maxIdempotentBody` |
| Stored-response bound | 256 KiB; larger responses are answered but not stored | `idempotency_middleware.go` `maxStoredResponse` |
| What is stored for replay | 2xx responses with a valid-JSON (or empty) body only | `idempotency_middleware.go` `worthStoring` |
| Concurrent duplicate with the same key | Exactly one runs the handler; the other gets `409` (in-flight) or the replay (if the first has finished) | `idempotency.go` `Begin` |
| Different request, same key | `409 CONFLICT` | `idempotency.go` `Begin` |
| Missing `client_id` claim | `400 VALIDATION_ERROR` | `idempotency_middleware.go` `Wrap` |
| Middleware position | Inside `Require` and `RateLimit`, outside `AuditGuard` (a replay must not appear as an unaudited mutation) | `backend/internal/management/chain.go` |

## Interfaces

Request:

```
POST /v1/organizations/{org_id}/users
Idempotency-Key: 8f3e6b2a-invite-budi-2026-09-17
```

Replay response headers:

```
Content-Type: application/json
Cache-Control: no-store
Idempotency-Replayed: true
```

Conflict response (different body, same key):

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "This Idempotency-Key was already used for a different request."
  }
}
```

In-flight conflict:

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "A request with this Idempotency-Key is still in progress."
  }
}
```

## Security Considerations

- The idempotency key is never logged in full — only a fingerprint (`idempotency_middleware.go` `fingerprint`) — because it is caller-chosen and routinely carries a business identifier the operator has no legitimate need to see (`docs/PLAN/05` NFR-2).
- Scoping by `(org_id, client_id, key)` prevents one client from reading another client's stored response by guessing a key.
- A replayed response is written with the exact `Content-Type: application/json` it was stored with, and `worthStoring` refuses to store anything that is not valid JSON — closing a reflected-content path even though the bytes are the caller's own earlier output, not third-party input.

## Verification

- `TestAReplayReturnsTheOriginalAndCreatesNothing`, `TestTheSameKeyWithADifferentBodyIsAConflict`, `TestConcurrentDuplicatesRunTheHandlerOnce` — `backend/internal/management/chain_integration_test.go`.
- `backend/internal/management/idempotency_test.go` (pure `ValidateKey`/`HashRequest`/middleware behaviour without Postgres) and `idempotency_integration_test.go` (claim/replay/release/sweep against real Postgres).

## Not Yet Built / Open Questions

None — the mechanism is stable and applied uniformly to every mutating `/v1` route.

## Related Documents

- `docs/PLAN/05-API-CONTRACT.md` Part B, NFR-2
- `docs/API/00-API-OVERVIEW.md` (middleware chain ordering)
- `docs/API/05-RATE-LIMITING.md` (a replay is still counted against the client quota)

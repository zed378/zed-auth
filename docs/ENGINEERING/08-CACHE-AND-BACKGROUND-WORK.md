# 08 - Cache and Background Work

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P2-07, P4-04, P1-13, P3-14 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

The rules for caching in an authorization service — where a stale entry is a security
property, not a performance detail — and for work that runs outside a request.

## Scope

`backend/internal/authz/cache.go`, `backend/internal/ratelimit/`, and the scheduled units
in `deploy/`.

## As Built

### Cache the inputs to a decision, never the decision

```
grants:{org}:{user}:{project}  → the role keys the user holds there
roles:{org}:{project}          → every role in the project, with what it carries
```

`P2-07` fixes this and Phase 4b is the reason: once policies can read
`resource.attributes`, a decision depends on values that differ per request, and a cached
decision would be served for a resource it was never computed against. Caching what a user
holds and what a role carries stays correct whatever the decision function grows into.

### Two keys, not one

Grants and role definitions change independently and are invalidated by different events.
One combined entry would mean a single role edit invalidating every user who holds it — a
scan or a guess, neither of which belongs in a request path.

The role key is **per project**, not per role, because a decision looks up several roles at
once and N lookups to answer one question is how a cache makes things slower.

### Invalidation by generation, not by enumeration

Revoking a Project Grant makes every delegated role it carried stale. The obvious
implementation — delete one cache entry per holder — is a fan-out proportional to the
number of users, in the request path, at the exact moment an administrator is revoking
access because something is wrong.

`P4-04` uses a **generation counter** instead:

```
authz:grantgen:{grant_id}     INCR on revoke, TTL 24h
```

Every cached entry records the grant it came through and the generation it was written at.
A read whose generation does not match the current one is a miss. Revocation is therefore
**one `INCR`**, whatever the number of holders, and no entry has to be found to be retired.

This was a direct answer to threat review T4-3, which asked what happens when a revocation
must reach many sessions at once.

### The TTL is the backstop and is published

`DefaultTTL = 30s`. Correctness does not depend on invalidation arriving: an entry expires
regardless. The window is stated in the integrator documentation
(`public-site/docs/guides/authorization-checks.md`) and **pinned by a drift test**
(`backend/internal/docsdrift/`), so changing the constant without changing the published
sentence fails the build.

### A cache failure is never an authorization failure

A Redis error is logged and treated as a miss. The decision is then made from PostgreSQL.
A cache that can deny access when it is unavailable is a second, worse, availability
dependency in the authorization path.

The `generation()` helper distinguishes three outcomes: `redis.Nil` → 0 (no generation
recorded yet), an error → −1 (never matches, so everything misses), a value → the value.

### The invalidator is an interface the consumer declares

```go
type Invalidator interface{ InvalidateGrant(ctx context.Context, grantID string) }
```

Declared in `internal/projectgrant`, implemented by `internal/authz`, wired in
`backend/cmd/authservice/main.go`. It is nil-safe: nil means the TTL is the only
mechanism, which is slower to take effect and never wrong. That shape keeps the dependency
graph acyclic — `authz` depends on `role` and `grant`, and the reverse edge would close a
ring.

Invalidation is called **after the commit**, never inside the transaction: retiring cache
entries for a write that then rolls back would evict correct data and, worse, teach the
cache a value that never existed.

### Rate limiting is Redis, and fails open by design decision

`internal/ratelimit` counts in Redis with a quota per client. The behaviour when Redis is
unavailable is a deliberate, documented decision rather than an accident — see
[`../PERFORMANCE/`](../PERFORMANCE/).

### Background work runs as systemd units on the VM, not as goroutines

Backups and key rotation are scheduled outside the service. A goroutine in the service
process would mean a backup that depends on the service being up and a rotation that runs
once per replica.

The backup taught this project a specific lesson twice: **alert on the absence of a
success, not on a failure count.** A timer that stops firing produces no failures at all,
and a dashboard counting failures shows zero, which reads as healthy. See
`deploy/RUNBOOK-mfa-recovery.md` and [`../DEVOPS/`](../DEVOPS/).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Cache contents | inputs to a decision, never a decision | `backend/internal/authz/cache.go` |
| Entry TTL | 30 s | `DefaultTTL` |
| Grant generation TTL | 24 h | `generationTTL` |
| Revocation cost | one `INCR`, independent of holders | `InvalidateGrant` |
| Cache error | treated as a miss | `backend/internal/authz/cache.go` |
| Invalidation timing | after commit | `backend/internal/projectgrant/handler.go` |
| Published revocation window | pinned by a drift test | `backend/internal/docsdrift/` |
| Scheduled work | systemd units, not goroutines | `deploy/` |

## Verification

- `backend/internal/authz/cache_integration_test.go` — hits, misses, generation mismatch.
- `backend/internal/authz/delegated_integration_test.go` — a revoked grant stops granting
  access on the next check.
- `backend/internal/docsdrift/` — the published window matches the constant.

## Not Yet Built / Open Questions

- **No cache warming**, and none is wanted: a cold cache answers from PostgreSQL.
- **Tracing is wired but emits no spans** (`internal/observability/tracing.go`), so a slow
  authorization check cannot be attributed to the cache or the database from telemetry
  alone.

## Related Documents

- [`07-DATABASE-ACCESS-STANDARDS.md`](./07-DATABASE-ACCESS-STANDARDS.md)
- [`../AUTHORIZATION/`](../AUTHORIZATION/)
- [`../PERFORMANCE/`](../PERFORMANCE/)

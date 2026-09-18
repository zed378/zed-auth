# 01 - Connection Pooling and Cache Budgets

> Category: **Performance** (`docs/PERFORMANCE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-08, P1-11, P1-15, P2-07, P1-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the PostgreSQL connection pool settings, the Redis-backed caches in front of PostgreSQL, and the reasoning behind each cache's TTL — the actual numbers in the code, not aspirational ones.

## Scope

Covers `backend/internal/storage/postgres` (pool), and every Redis-backed cache: the session lookup cache (`backend/internal/session/cache.go`), the authorization decision-input cache (`backend/internal/authz/cache.go`), and the signing key set cache (`backend/internal/signing/keyset.go`, in-process rather than Redis-backed). Also covers the Postgres-backed idempotency store, which is a durability mechanism rather than a latency cache but shares this document because it has its own TTL. Rate-limiter quotas are covered here only insofar as they bound request volume against the pool; the authorization implications of the quotas are in `docs/AUTHORIZATION/`.

## As Built

### PostgreSQL connection pool

`backend/internal/storage/postgres/postgres.go`'s `Open` applies `cfg.Postgres.MaxOpenConns`, `MaxIdleConns`, and `ConnMaxLifetime` directly to the `database/sql` pool via `SetMaxOpenConns`/`SetMaxIdleConns`/`SetConnMaxLifetime`. Defaults, from `backend/internal/config/config.go`:

| Setting | Default | Env var |
|---|---|---|
| `MaxOpenConns` | 25 | `AUTH_POSTGRES_MAX_OPEN_CONNS` |
| `MaxIdleConns` | 5 | `AUTH_POSTGRES_MAX_IDLE_CONNS` |
| `ConnMaxLifetime` | 30 minutes | `AUTH_POSTGRES_CONN_MAX_LIFETIME` |

Config validation refuses `MaxOpenConns <= 0`, a negative `MaxIdleConns`, or `MaxIdleConns > MaxOpenConns` (`config.go`). Pool statistics are exposed to Prometheus via `sql.DBStats` (`Metrics.RegisterDBStats`, wired in `backend/cmd/authservice/main.go`) — see `docs/OBSERVABILITY/03-METRICS-AND-SLO-TRACKING.md`. `docs/PLAN/13-OBSERVABILITY.md` names pool utilization explicitly because exhaustion presents as latency at every endpoint simultaneously, which is why `DatabasePoolSaturated` exists in `deploy/observability/alerts.yml` at 90% of `postgres_max_open_connections`.

Every measured load test to date attributes its throughput ceiling to **PostgreSQL CPU**, not the connection pool itself, on a shared 4-core staging VM — see `00-PERFORMANCE-TARGETS.md`. The pool has not been identified as the bottleneck in any recorded measurement.

### Redis: cache, not store

`ADR-003` establishes PostgreSQL as authoritative for sessions; Redis is a cache in front of it, so a Redis outage costs latency, not correctness. `backend/cmd/authservice/main.go` constructs a single `redis.NewClient` with `Addr`, `Password`, and `DB` from `config.RedisConfig` — **no explicit `PoolSize` or `MinIdleConns` is configured**, so the client runs on `go-redis`'s own defaults rather than a value tuned for this service.

### The three Redis/in-process caches, and their TTLs

| Cache | TTL | Why this number | Source |
|---|---|---|---|
| Session lookup cache | 1 minute (`DefaultCacheTTL`) | Backstop only — revocation is explicit (commit to Postgres, then delete the Redis key), with a Lua-scripted tombstone closing the one race a plain delete-then-repopulate would leave open | `backend/internal/session/cache.go` |
| Authorization decision-input cache (`grants:`, `roles:`) | 30 seconds (`DefaultTTL`) | Backstop for exactly two cases: Redis was unreachable when a grant changed, or a grant changed outside the API (migration, direct SQL). Invalidation is proactive on every grant/role write, so this is the ceiling on staleness, not the mechanism. `P1-28` measured the database as able to serve this load directly, so the cache protects latency rather than making the system possible — a longer TTL buys little and costs a ten-times-larger worst-case window | `backend/internal/authz/cache.go` |
| Signing key set cache (in-process, not Redis) | 5 minutes (`DefaultCacheTTL`) | A new key must reach every instance without a deploy | `backend/internal/signing/keyset.go` |
| Idempotency claims (PostgreSQL, not Redis) | 24 hours (`IdempotencyTTL`) | How long a replayed `Idempotency-Key` is honored | `backend/internal/management/idempotency.go` |

**Cache operation timeout.** Every authz cache operation gets its own 50ms deadline (`authz.OperationTimeout`), separate from and shorter than the request's own deadline — without it, an unreachable Redis makes every check wait out the client's own dial/retry budget (seconds), turning a cache outage into a request timeout instead of a database read. 50ms is generous for a local Redis: `P1-28` measured the whole token endpoint at a p50 of 9.9ms in isolation.

**Failure mode, both caches.** A cache read failure (not a miss — an actual error) is treated as a miss and falls through to PostgreSQL, never as a reason to deny or to allow; a cache write failure is silently ignored, since a cache that cannot be written just becomes a cache that misses more, which is slower but never wrong. An invalidation (delete) failure is the one case that is *not* silent: it is counted (`authz_cache_invalidation_failures_total`, `auth_session_cache_invalidation_failures_total`) and logged at `ERROR`, because it is the one failure that makes the TTL load-bearing rather than a pure backstop.

### Rate-limit quotas that bound load reaching the pool

| Quota | Limit | Note |
|---|---|---|
| `PerClient` (Management API, general) | 600/minute | Sized for console sessions and provisioning scripts |
| `PerClientAuthz` (`/v1/authz/check`) | 6,000/minute | Raised from sharing `PerClient`'s bound after `P2-17`'s load test found the shared 600/minute limit refusing roughly half of a realistic authz-check load (19,658 of 40,515 requests in the first run) |

`backend/internal/ratelimit/quota.go` defines both as named constants read directly by the load-test harness (`scripts/loadtest/run-authz.sh` greps `PerClientAuthz` out of the source rather than hard-coding the number), so the test and the enforced limit cannot silently diverge.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Postgres `MaxOpenConns` | 25 default | `backend/internal/config/config.go` |
| Postgres `MaxIdleConns` | 5 default, must not exceed `MaxOpenConns` | `backend/internal/config/config.go` (validation) |
| Postgres `ConnMaxLifetime` | 30 minutes default | `backend/internal/config/config.go` |
| Redis client pool size | Not explicitly configured (library default) | `backend/cmd/authservice/main.go` |
| Session cache TTL | 1 minute | `backend/internal/session/cache.go` `DefaultCacheTTL` |
| Authz cache TTL | 30 seconds | `backend/internal/authz/cache.go` `DefaultTTL` |
| Authz cache operation timeout | 50 milliseconds | `backend/internal/authz/cache.go` `OperationTimeout` |
| Signing key cache TTL | 5 minutes | `backend/internal/signing/keyset.go` `DefaultCacheTTL` |
| Idempotency record TTL | 24 hours | `backend/internal/management/idempotency.go` `IdempotencyTTL` |
| Management API quota | 600/minute per client | `backend/internal/ratelimit/quota.go` `PerClient` |
| `/v1/authz/check` quota | 6,000/minute per client | `backend/internal/ratelimit/quota.go` `PerClientAuthz` |

## Interfaces

Not an API surface; these are internal configuration values and Go constants listed above, plus the Prometheus pool-saturation and cache metrics documented in `docs/OBSERVABILITY/03-METRICS-AND-SLO-TRACKING.md`.

## Security Considerations

- The authorization cache holds **inputs to a decision, never a decision itself** (`ADR-022`) — this is what keeps caching compatible with a future ABAC evaluator whose result can depend on per-request resource attributes, which must never be cached as if they were static.
- The two-key cache split (`grants:{org}:{user}:{project}` vs. `roles:{org}:{project}`) exists so that a single role definition edit invalidates precisely, rather than requiring a scan across every user who might hold it or a guess at who they are.
- A cache-invalidation failure is a genuine, if bounded, authorization exposure window (a revoked permission may still be served for up to the TTL) — this is why it is counted and alerted on (`SessionCacheInvalidationFailing` in `deploy/observability/alerts.yml`) rather than only logged.
- `docs/PLAN/08-AUTHORIZATION.md` Part B's requirement that Project Grant subset validation happen server-side on every request (not just at grant time) depends on the grants cache being invalidated correctly on every grant write — see `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md` for the authorization-side treatment.

## Verification

- `backend/internal/authz/cache.go`-adjacent tests (not enumerated in this pass; see the `authz` package test files for cache-hit/miss/unavailable/invalidation-failure coverage).
- `MEMORY/records/2026-09-12-P2-07-decision-caching.md` — the design record for the 30-second TTL and the 50ms operation timeout, including the reasoning that a longer TTL buys little given `P1-28`'s database-capacity measurement.
- `MEMORY/records/2026-09-12-P2-17-acceptance.md` — the load test that found the shared-quota bug and the fix (`PerClientAuthz`), including a unit test asserting ordinary Management API routes keep the tighter 600/minute bound.

## Not Yet Built / Open Questions

- **Redis client pool sizing has never been tuned or load-tested explicitly** — it runs on `go-redis` defaults. No record in `MEMORY/` measures Redis connection exhaustion under load.
- **No read replica exists.** `docs/PLAN/12-PERFORMANCE.md`'s stated remedy for the mixed-workload latency misses recorded in `00-PERFORMANCE-TARGETS.md` is a Phase 5 item (`TASKS/PHASE-5-HARDENING.md`), not yet built.
- **Degraded-Redis testing** (what happens to the caches, and to overall latency, when Redis is slow rather than simply down) has not been performed as a dedicated exercise — see `02-LOAD-TESTING-STRATEGY.md`.

## Related Documents

- `docs/PLAN/12-PERFORMANCE.md` § Design Decisions Made for Performance
- `docs/OBSERVABILITY/03-METRICS-AND-SLO-TRACKING.md`
- `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`
- `MEMORY/DECISIONS.md` ADR-003, ADR-022

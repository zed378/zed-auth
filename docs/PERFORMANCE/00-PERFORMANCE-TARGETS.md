# 00 - Performance Targets

> Category: **Performance** (`docs/PERFORMANCE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-28, P2-06, P2-17, P3-15, P5-04 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State which of `docs/PLAN/12-PERFORMANCE.md`'s latency targets have actually been measured, against what, and what the measurement found — as opposed to restating the target table as if meeting it were established fact.

## Scope

The targets themselves are design intent and are not restated in full here beyond what is needed to read the measurements below — see `docs/PLAN/12-PERFORMANCE.md` § Latency Targets for the authoritative table. This document is the measurement record. Connection pool and cache sizing that these numbers depend on are in `01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md`. The harness that produced these numbers is documented in `02-LOAD-TESTING-STRATEGY.md`.

## As Built

**Every measurement to date was taken by `scripts/loadtest/` against a real deployment** (staging VM or a local Docker stack) — never assumed. Three load-test-bearing acceptance tasks exist in `MEMORY/records/`: `P1-28` (Phase 1, staging), `P2-17` (Phase 2, `/v1/authz/check`, local stack), `P3-15` (Phase 3, refresh-token latency, staging).

### Targets vs. measured

| Endpoint | Target (p50 / p95 / p99) | Measured | Source |
|---|---|---|---|
| `/oauth/token` (refresh), isolated | 50ms / 200ms / 400ms | p50 **9.9ms** (isolated single-endpoint run) | `MEMORY/records/2026-09-11-P1-28-threat-model-review.md`, cited in `backend/internal/authz/cache.go` and `MEMORY/records/2026-09-12-P2-07-decision-caching.md` |
| `/oauth/token` (refresh), **mixed workload** | 50ms / 200ms / 400ms | p50 **72.7ms**, p95 **209ms** — misses both | `MEMORY/records/2026-09-12-P1-phase-1-summary.md` |
| `/oauth/token` refresh, MFA on (Phase 3, staging, 3 runs × 20 workers) | 50ms / 200ms / 400ms | p50 57.7–63.0ms, p95 141–152ms, p99 192–220ms — meets target | `MEMORY/records/2026-09-15-P3-15-acceptance.md` |
| `/oauth/token` refresh, MFA off (same run) | 50ms / 200ms / 400ms | p50 58.6–63.1ms, p95 149–161ms, p99 221–234ms — meets target | same |
| `/oauth/token` refresh, mixed load (Phase 3) | 50ms / 200ms | p50 **109ms**, p95 **264ms** — misses both | same |
| `/oauth/authorize` (silent SSO), isolated | 50ms / 150ms / 300ms | p50 **14.7ms**, p95 **107ms** — meets target | `MEMORY/records/2026-09-15-P3-15-acceptance.md` |
| `/v1/authz/check` (RBAC) | 20ms / 80ms / 150ms | p50 **6.3ms**, p95 **22.0ms**, p99 **30.4ms**, max 74.5ms, over 2,880 requests at 96 rps, zero failures | `MEMORY/records/2026-09-12-P2-17-acceptance.md` |
| `/v1/authz/check` (ABAC) | 40ms / 150ms / 300ms | Not measured — Phase 4b (ABAC) is not built | — |
| `/oauth/userinfo` | 30ms / 100ms / 200ms | Reported as "within target" in Phase 3 acceptance; exact figures not recorded | `MEMORY/records/2026-09-15-P3-15-acceptance.md` |
| Management API (CRUD) | 100ms / 300ms / 600ms | Reported as "within target" in Phase 3 acceptance; exact figures not recorded | same |
| Interactive sign-in, password step (not a `docs/PLAN/12` target — reported, not judged) | none set | median **106ms** (argon2id + challenge page), 6 samples | same |
| Interactive sign-in, TOTP step (not a `docs/PLAN/12` target) | none set | median **13.6ms**, 6 samples | same |

### What limited throughput, when measured

`scripts/loadtest/sweep.sh` walks concurrency up to find the ceiling rather than pass/fail at one level. Phase 1's sweep (`MEMORY/records/2026-09-12-P1-phase-1-summary.md`) found `/oauth/token` peaking at **385 rps** (4 workers) and flattening thereafter (384 at 8, 378 at 16, 329 at 24), and `/oauth/authorize` peaking at **1,774 rps**, holding every target to 32 workers. In both cases the bottleneck was **PostgreSQL CPU** (~190% of 4 cores) rather than the Go service (~100%), on a VM shared with other containers (27 in the Phase 1 measurement, 39 by Phase 3 — see `02-LOAD-TESTING-STRATEGY.md`). Phase 3's refresh-latency regression (p50 46ms → ~60ms across the phase) was attributed by an isolated-vs-mixed A/B run to roughly 22 database round trips per refresh, from token rotation and reuse detection (`P3-06`), not to the MFA mandate check added in the same phase (measured as adding no detectable cost).

### Accepted misses

Both mixed-workload misses above are recorded as **accepted, not fixed** — each phase's summary states this explicitly, citing a shared 4-core VM with one replica and no read replica as the cause, and pointing at `docs/PLAN/12`'s own remedies (read replicas, horizontal scaling) as Phase 5 work.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Latency targets | See `docs/PLAN/12-PERFORMANCE.md` § Latency Targets (not restated here in full) | Design intent |
| Metric buckets aligned to targets | Yes, for every latency histogram | `backend/internal/observability/metrics.go` (`TestBucketsCoverThePerformanceTargets`) |
| Alert thresholds match targets exactly | Yes, for `/oauth/token`, `/oauth/authorize`, `/v1/authz/check` (RBAC) | `deploy/observability/alerts.yml` |
| "Performance acceptable for go-live" definition | All targets met under load test; degraded-dependency tests show graceful degradation; horizontal scaling verified in practice | `docs/PLAN/12-PERFORMANCE.md` § Definition of "Performance Acceptable" for Go-Live — **not yet satisfied**, see Not Yet Built |

## Interfaces

Not applicable — this document records measurements rather than defining an API surface. See `docs/PLAN/12-PERFORMANCE.md` for the endpoints the targets apply to.

## Security Considerations

- The `/v1/authz/check` RBAC measurement (`P2-17`) directly informed `ratelimit.PerClientAuthz` (6,000/minute), replacing a shared Management API quota (600/minute) that the first load-test run found refusing 19,658 of 40,515 requests — a genuine availability bug the measurement exercise found, not merely a performance number. See `01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md` and `docs/AUTHORIZATION/` for the quota's authorization implications.
- Measuring past a rate limiter's own bound measures the limiter, not the endpoint — `MEMORY/records/2026-09-12-P2-17-acceptance.md` documents this mistake and its fix (`run-authz.sh` reads the pacing rate out of the same source-of-truth constant the service enforces, rather than a hand-typed number).

## Verification

- `MEMORY/records/2026-09-11-P1-28-threat-model-review.md`, `MEMORY/records/2026-09-12-P1-phase-1-summary.md` — Phase 1, staging.
- `MEMORY/records/2026-09-12-P2-17-acceptance.md` — Phase 2, `/v1/authz/check`, local stack (staging unreachable at the time).
- `MEMORY/records/2026-09-15-P3-15-acceptance.md` — Phase 3, staging, including the refresh-latency A/B and the interactive sign-in timings.
- `backend/internal/observability/metrics_test.go` `TestBucketsCoverThePerformanceTargets` — confirms histogram buckets straddle the targets rather than merely existing.

## Not Yet Built / Open Questions

- **`/v1/authz/check` with ABAC** cannot be measured until Phase 4b (ABAC/OPA) ships.
- **Exact `/oauth/userinfo` and Management API CRUD figures** were not recorded in Phase 3 acceptance beyond "within target" — no p50/p95/p99 numbers exist in `MEMORY/` for these two rows.
- **A full, current-scale load test against every target with realistic data volumes and mixed workload** is `TASKS/PHASE-5-HARDENING.md` § P5-04's explicit goal and has not run; every measurement above predates Phase 4's delegation features.
- **Horizontal scale-out has never been tested** — every measurement to date ran against one replica. Owned by `TASKS/PHASE-5-HARDENING.md` § P5-06.
- **Degraded-dependency testing** (Redis slow, DB replica lagging, OPA slowed) has not been performed as a load-testing exercise — see `02-LOAD-TESTING-STRATEGY.md` and `TASKS/PHASE-5-HARDENING.md` § P5-05.

## Related Documents

- `docs/PLAN/12-PERFORMANCE.md`
- [`01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md`](./01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md), [`02-LOAD-TESTING-STRATEGY.md`](./02-LOAD-TESTING-STRATEGY.md)
- `docs/OBSERVABILITY/03-METRICS-AND-SLO-TRACKING.md`

# 12 — Performance

Auth Service sits on the **critical path** of every consumer application — its latency and availability directly become part of every other service's latency and availability budget. This document sets explicit targets and the plan to meet them.

## Latency Targets

| Endpoint | p50 target | p95 target | p99 target | Notes |
|---|---|---|---|---|
| `/oauth/token` | < 50ms | < 200ms | < 400ms | Highest call volume, most latency-sensitive |
| `/oauth/authorize` (session already exists) | < 50ms | < 150ms | < 300ms | The "silent SSO" path — must feel instantaneous to the end user |
| `/oauth/userinfo` | < 30ms | < 100ms | < 200ms | Often called on every page load by consumer SPAs |
| `/v1/authz/check` (RBAC only) | < 20ms | < 80ms | < 150ms | Called frequently at runtime by resource servers |
| `/v1/authz/check` (with ABAC policy evaluation) | < 40ms | < 150ms | < 300ms | Slightly higher due to policy evaluation, still embedded in-process |
| Management API (CRUD) | < 100ms | < 300ms | < 600ms | Lower traffic, less latency-sensitive than the auth path |

> These are starting targets — adjust once real usage data and SLAs from consumer teams are available. Track actual numbers against these in `13-OBSERVABILITY.md` dashboards.

## Design Decisions Made for Performance

- **Stateless service**: no in-memory session state, so any instance can serve any request — enables simple horizontal scaling (`07-BACKEND-ARCHITECTURE.md`).
- **Embedded OPA** (in-process policy evaluation) instead of a network call to a separate policy service, to keep `/v1/authz/check` fast even with ABAC enabled (`08-AUTHORIZATION.md`).
- **Short-TTL caching** for Project Grant validity and user attributes, avoiding a DB round-trip on every permission check while still catching revocations quickly (`08-AUTHORIZATION.md`).
- **Asymmetric JWT verification** requires no round-trip to Auth Service at all for consumer apps that only need to validate a token locally against the JWKS — this is the single biggest latency win available, since most authorization decisions never need to touch Auth Service after token issuance.
- **Read replicas** for read-heavy queries (`userinfo`, listing users) so they don't compete with write traffic on the primary.

## Load Testing Plan

- Load-test `/oauth/token` and `/oauth/authorize` under realistic concurrent-user simulation before each major release, not just before the initial launch.
- Include a **mixed workload** test: simultaneous token issuance + management API traffic + authz checks, since production will never see just one traffic type in isolation.
- Test **degraded-dependency** scenarios explicitly: what happens to p95/p99 latency when Redis is slow, when the DB read replica lags, when OPA policy evaluation is artificially slowed — the service should degrade gracefully (see `13-OBSERVABILITY.md` for circuit-breaker/fallback behavior), not fail catastrophically.

## Capacity Planning

- Baseline capacity planning on: expected concurrent active users, expected token issuance rate (logins + refreshes), expected authz-check rate from consumer apps (this is usually the largest volume, since it happens on every protected request across every consumer app).
- Revisit capacity assumptions at the start of each roadmap phase (`16-IMPLEMENTATION-ROADMAP.md`) that adds a new consumer application or a new organization at meaningful scale.

## Definition of "Performance Acceptable" for Go-Live

- [ ] All latency targets in the table above are met under the load test in §"Load Testing Plan."
- [ ] Degraded-dependency tests show graceful degradation (bounded latency increase, no cascading failure) rather than outright failure.
- [ ] Horizontal scaling has been verified in practice (not just assumed from "the service is stateless") by actually running a scale-out test.

Continue to [13 — Observability](./13-OBSERVABILITY.md).

# 03 - Metrics and SLO Tracking

> Category: **Observability** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-11, P1-06..P1-13, P2-06, P2-07, P3-06, P3-08, P4-01, P5-08 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the Prometheus instruments the service exposes, the alert rules built on top of them, and what "SLO tracking" means here today: metrics and alerting exist; dashboards and formal error-budget tracking do not.

## Scope

Covers `backend/internal/observability/metrics.go`, the admin listener that serves `/metrics`, and `deploy/observability/alerts.yml`. Latency targets these metrics are judged against are `docs/PERFORMANCE/00-PERFORMANCE-TARGETS.md` (which links `docs/PLAN/12-PERFORMANCE.md`, the source of the numbers). Does not cover tracing (`04-DISTRIBUTED-TRACING.md`) or the audit log's own event stream (`01-AUDIT-LOGGING-SPECIFICATION.md`), though several metrics observe the audit writer's health.

## As Built

**A dedicated registry**, not the Prometheus global default (`observability.NewMetrics`) — the default registry is package-global mutable state any dependency can register into, which would make the exposed metric set depend on the import graph rather than on this one file. Every instrument is named and documented in `metrics.go` up front, including ones for phases not yet built (`authz_policy_eval_duration_seconds`, reserved for Phase 4b/ABAC and correctly at zero until then).

**Latency buckets are chosen around `docs/PLAN/12`'s targets**, not left at client-library defaults, so a `histogram_quantile` query can answer "did we meet the target" without interpolating across a bucket that straddles it. `RequestDuration`'s buckets run from 5ms to 5s; `AuthzCheckDuration` reuses the same set (tightest target in the system: p50 < 20ms RBAC); `UserInfoDuration` and `TokenDuration` have their own tighter buckets sized to their specific targets.

**Cardinality is deliberately bounded in two ways**, both load-bearing rather than stylistic:
- The `route` label on every HTTP metric is the chi **route pattern** (`/v1/organizations/{org_id}/users`), never the concrete path — a concrete path would be one time series per organization, unbounded cardinality that an application change (not an attacker) could use to take down Prometheus.
- An unmatched request (a scan, a typo — entirely attacker-controlled) collapses to the single label `unmatched` rather than one series per probed path.
- Status codes are reduced to their class (`2xx`/`4xx`/`5xx`) on HTTP metrics for the same reason: alerts care about the 5xx rate, not 502 vs. 503.

**What is instrumented today** (non-exhaustive — see `metrics.go` for the full list with rationale on each field): HTTP request duration/count/in-flight; login attempts and lockouts; token issuance and token-endpoint errors; refresh-token reuse detection (unlabelled, deliberately — the correct alert threshold is any increase at all); login anomaly findings by signal; rate-limit refusals and rate-limiter unavailability (the metric that makes the fail-open rate limiter, `ADR-017`, safe to ship); outbound mail send failures (the metric that makes best-effort mail, `ADR-018`, safe to ship); unaudited-mutation count (should be permanently zero — see `01-AUDIT-LOGGING-SPECIFICATION.md`); logout, token lifecycle (introspect/revoke), userinfo, and silent-authorize outcomes and latency; session creation/revocation and session-cache lookup latency by source; session cache invalidation failures (the only signal that a revoked session might still be served until its cache entry expires); password policy rejections and breach-check outcomes (the metric that makes the fail-open breach check, `ADR-015`, safe to ship); authorization decisions and latency by mode; authorization cache lookups, entry age, and invalidation failures; Project Grant creation/revocation counts; audit write outcomes and partition runway/errors; Redis operation latency; and cross-tenant (`instance-scoped`) database access counts.

**Where metrics are served.** `Metrics.Handler()` is mounted at `GET /metrics` on the **admin listener** (`backend/internal/httpserver/admin.go`), a separate `http.Server` bound to `127.0.0.1:9090` by default — never a route on the public listener. `Metrics.RegisterDBStats` additionally exposes PostgreSQL connection pool statistics through Go's `sql.DBStats` collector.

**Alerting.** `deploy/observability/alerts.yml` defines 22 Prometheus rules across five groups (availability, latency, security, audit, dependencies), validated with `promtool check rules`. Every rule's annotation states the reason it exists — an alert whose purpose nobody remembers is an alert that gets silenced. The rules are built around one explicit principle: **absence is not health** — a counter with no observations produces no time series at all, so `rate(x) == 0` never fires when the service is down (it fires when the service is up and idle). Rules that must catch "this stopped happening" use `absent()` (`AuditPartitionRunwayUnknown`) or compare a gauge (`AuditPartitionRunwayLow`) rather than a rate.

Selected rules and what they cover: `AuthServiceDown` (scrape absence); `TokenEndpointErrorRate` (>1% 5xx on `/oauth/token`); `HighErrorRate` (>5% 5xx per route); `TokenEndpointSlow`/`AuthorizeEndpointSlow`/`AuthzCheckSlow` (p95 against the exact `docs/PLAN/12` targets); `LoginFailureSpike` (>50% failure rate); `SessionCacheInvalidationFailing` (any occurrence, fires immediately); `SessionLookupsBypassingTheCache`; `PasswordBreachCheckFailingOpen` / `PasswordBreachCheckDisabled`; `RefreshTokenReuseDetected` (any occurrence, critical); `LoginNewDeviceShareHigh` / `ImpossibleTravelLogins` (thresholds explicitly marked **provisional**, not yet tuned against real traffic — see Not Yet Built); `LockoutSpike`; `UnusualProjectGrantActivity`; `UnexpectedInstanceScopedAccess`; `AuditPartitionRunwayLow` / `AuditPartitionRunwayUnknown` / `AuditWriteFailures`; `DatabasePoolSaturated`; `RedisSlow`.

**Backup-success alerting does not exist.** The nightly database backup (`deploy/vm/zed-auth-backup.service`/`.timer`) has no metric and no Prometheus rule: `alerts.yml` contains no backup-related rule, and `deploy/vm/backup.sh` pushes no metric anywhere. The unit was hardened after a real incident — `MEMORY/records/2026-09-15-P3-14-test-suite.md` records that the timer failed silently every night from 2026-09-11 to 2026-09-15 with `226/NAMESPACE`, invisible because `systemctl list-timers` reports the *timer* as healthy even when every run of the *service* fails — but the fix made the failure loud in systemd (a real `mkdir` failure now, not a silent namespace error), not alerted anywhere outside the host. Detecting a silently-stopped backup still requires someone to check `systemctl status zed-auth-backup.service` or `journalctl` on the VM.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Metrics registry | Dedicated `prometheus.Registry`, not the global default | `backend/internal/observability/metrics.go` `NewMetrics` |
| Route label | chi route pattern only, never a concrete path | `Metrics.Instrument`, `routePattern` in `management/audit.go` |
| Unmatched-route label | `unmatched` (also applied to chi's `/*` catch-all) | `Metrics.Instrument` |
| Status label granularity | Class (`1xx`..`5xx`), not exact code | `statusClass` |
| `UnauditedMutations` alert threshold | Any non-zero value is a defect | `metrics.go`, `alerts.yml` (none currently defined for it — see Not Yet Built) |
| `RefreshReuse` alert threshold | Any increase | `alerts.yml` `RefreshTokenReuseDetected` |
| Alert rule count | 22, across 5 groups | `deploy/observability/alerts.yml` |
| Backup success alert | None exists | absence confirmed in `alerts.yml` and `deploy/vm/backup.sh` |

## Interfaces

- `GET /metrics` on the admin listener (`127.0.0.1:9090` default, `AUTH_ADMIN_ADDR`), bearer-token gated when not loopback.
- Key metric names: `http_request_duration_seconds`, `http_requests_total`, `http_requests_in_flight`, `auth_login_attempts_total{outcome}`, `auth_tokens_issued_total{grant_type}`, `auth_lockouts_total`, `authz_check_duration_seconds{mode}`, `authz_decisions_total{decision,mode}`, `authz_project_grant_changes_total{action}`, `audit_events_written_total{outcome}`, `audit_partition_runway_months`, `db_instance_scoped_access_total{reason}`, `redis_operation_duration_seconds{operation}`, `postgres_*` (pool stats). Full list with rationale: `backend/internal/observability/metrics.go`.

## Security Considerations

- The metrics endpoint discloses a reconnaissance summary (login failure rate, pool saturation, service version) and is a reliable oracle for whether an attack in progress is succeeding — see `00-OBSERVABILITY-OVERVIEW.md` and `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12. This is why it is gated and non-public rather than a metrics-specific concern.
- `LoginFailureSpike`, `RefreshTokenReuseDetected`, `LoginNewDeviceShareHigh`, `ImpossibleTravelLogins`, `LockoutSpike`, and `UnusualProjectGrantActivity` are the alert-side half of `docs/SECURITY/02`'s brute-force, token-theft, anomaly, and misuse-indicator scenarios.
- `PasswordBreachCheckFailingOpen` and `PasswordBreachCheckDisabled` are the alert-side half of `ADR-015`'s fail-open decision — the decision is only defensible because these fire.

## Verification

- `backend/internal/observability/metrics_test.go` — `TestEveryMetricNamedInThePlanIsRegistered`, `TestBucketsCoverThePerformanceTargets`, `TestInstrumentRecordsDurationAndCount`, `TestRouteLabelUsesThePatternNotThePath`, `TestUnmatchedRoutesCollapseToOneLabel`, `TestStatusIsReducedToItsClass`, `TestInFlightReturnsToZero`, `TestInFlightIsReleasedOnPanic`, `TestServiceAndVersionAreConstLabels`.
- Alert rule syntax: `docker run --rm -v "$PWD:/rules" --entrypoint promtool prom/prometheus:v3.1.0 check rules /rules/alerts.yml` (`deploy/observability/README.md`).
- No record in `MEMORY/records/` documents an alert rule fired under an *induced* condition on staging or in CI — this verification step is the explicit goal of `TASKS/PHASE-5-HARDENING.md` § P5-08 ("verify each alert fires by inducing its condition"), not yet done.

## Not Yet Built / Open Questions

- **No dashboards.** No Grafana (or equivalent) configuration exists anywhere in the repository. Metrics and alert rules exist; a visual SLO/error-budget view does not. Owned by `TASKS/PHASE-5-HARDENING.md` § P5-08 ("build a security dashboard covering authentication failure rates, grant changes, policy changes, and anomaly detections").
- **No formal SLO/error-budget tracking.** The alert thresholds in `alerts.yml` are the closest thing to an SLO today; there is no burn-rate alerting or a recorded error budget.
- **`LoginNewDeviceShareHigh` and `ImpossibleTravelLogins` thresholds are provisional**, by the alert file's own annotation — they have not been tuned against real traffic because staging was unreachable during the relevant window (`MEMORY/specs/P3-08-login-anomaly-detection.md` § 21, cited in `alerts.yml`).
- **No alert rule exists for the audit guard's own `UnauditedMutations` metric**, despite the metric's help text stating "alert on any non-zero value."
- **Alert routing and escalation are not defined anywhere** (who receives what, at what hour) — owned by `TASKS/PHASE-5-HARDENING.md` § P5-08.
- **Backup-success alerting does not exist** — see As Built, above. Owned by `TASKS/PHASE-5-HARDENING.md` § P5-08, alongside SIEM forwarding and dashboard work.

## Related Documents

- `docs/PLAN/13-OBSERVABILITY.md` § Metrics, § Alerting
- `docs/PLAN/12-PERFORMANCE.md` § Latency Targets
- `docs/PERFORMANCE/00-PERFORMANCE-TARGETS.md`
- `deploy/observability/README.md`, `deploy/observability/alerts.yml`
- `MEMORY/DECISIONS.md` ADR-015, ADR-017, ADR-018

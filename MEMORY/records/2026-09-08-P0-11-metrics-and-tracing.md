# P0-11 — Metrics, Tracing, and Alerting

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Task** | `TASKS/PHASE-0-FOUNDATION.md` P0-11 |
| **Phase** | Phase 0 — Foundation |
| **Surface** | backend, infra |
| **Author** | Claude Code |
| **Branch** | `feat/P0-11-metrics-and-tracing` |
| **Status** | Completed |

---

## What Changed

Sixteen Prometheus instruments on a separate internal listener, fifteen validated alert rules, an OpenTelemetry tracing skeleton, and the two follow-ups `P0-12` left open.

## Why

`docs/PLAN/13` lists what must be observable; `docs/PLAN/12` sets the targets those observations are judged against. The connection is the point: a latency target nobody measures is an aspiration, and a metric with no target is a number nobody knows how to read. `P0-11` sits in Phase 0 rather than later because the measurement has to exist before the thing being measured, or the first deployment's behaviour is simply unknown.

## Decisions

### A separate listener, not a route

`docs/PLAN/13` requires the metrics endpoint not to be reachable from the public ingress. A separate binding makes that a property of the socket rather than something an ingress rule has to remember — it keeps holding when someone reconfigures the ingress without knowing the rule exists.

Worth stating plainly, because "metrics are harmless" is a common and wrong assumption: the endpoint discloses request rates per route, error rates, login success and failure counts, in-flight concurrency, database pool saturation, and the service version. That is a reconnaissance summary, and during an attack it is a reliable oracle for whether the attack is working (`docs/SECURITY/02` §12).

So: the config loader refuses an all-interfaces bind in production, compose publishes it to loopback only, and `pprof` is deliberately not mounted — it is genuinely useful and it exposes heap contents, which on this service means tokens and passwords in flight.

### Two cardinality rules

**The route label is the chi route pattern, never the concrete path.** `/v1/organizations/{org_id}/users` is one time series; the concrete path would be one per organization. Unbounded cardinality is the usual way an application change takes down a Prometheus server without anyone attacking anything.

**Unmatched requests collapse to one label.** A 404's path is entirely attacker-controlled, so a scanner walking a wordlist would otherwise create a series per probe — turning the metrics endpoint into the denial-of-service vector. chi reports these as `/*`, which the middleware normalizes.

Both are tested, including with ten distinct concrete paths asserting they produce one series.

### Buckets chosen against the targets

The client library's default buckets top out at 10s, which wastes resolution on a service whose interesting range is 5ms to 500ms. A histogram whose buckets straddle a target badly cannot answer whether the target was met — which is the only question these metrics exist to answer.

Bucket edges now sit exactly on `docs/PLAN/12`'s numbers: 20ms, 80ms, 100ms, 150ms, 200ms, 400ms, 600ms. A test asserts every target has a boundary, so a future edit that "tidies" the bucket list fails rather than silently degrading the quantiles.

### Every metric named now, including for phases not yet built

A metric renamed after a dashboard is built produces an empty panel rather than an error. Naming them once means the code, the dashboard, and the alerts agree from the start. `authz_policy_eval_duration_seconds` sits at zero until Phase 4b, which is correct and visible rather than missing.

### Absence is not health

The principle that shaped the alert rules, and the thing most likely to be got wrong by someone adding one later.

A counter with no observations produces **no time series at all**. So `rate(...) == 0` never fires when the service is down — it fires when the service is up and idle, which is the opposite of the intent. Rules that must catch "this stopped happening" use `absent()` or a gauge.

`AuditPartitionRunwayUnknown` is the clearest case: if the maintenance goroutine dies, the gauge stops being reported, and a rule comparing its value would never fire. The absence *is* the signal.

## Two Follow-Ups From `P0-12`, Closed

**The unsupervised maintenance goroutine is now visible.** `audit_partition_runway_months` drifts toward zero weeks before the month boundary where it becomes an outage — the difference between a ticket and an incident. Two alerts cover it: the value dropping, and the metric disappearing.

**Cross-tenant database access is counted**, which `docs/PLAN/08` Part B asks for. The import cycle — `audit` already imports `postgres` — is broken with a callback set at startup rather than a direct dependency. It runs *outside* the scoped transaction deliberately: failing to record the access should not roll back the access, which is the opposite trade from business events (ADR-012).

## Files Touched

| Path | Change |
|---|---|
| `backend/internal/observability/metrics.go` + test | 16 instruments, HTTP middleware, pool collector |
| `backend/internal/observability/tracing.go` | OTLP export, W3C propagation, no-op when disabled |
| `backend/internal/httpserver/admin.go` | Internal listener |
| `backend/internal/storage/postgres/postgres.go` | Instance-scope hook, `SQL()` for the pool collector |
| `backend/internal/audit/audit.go` | `Observer` for maintenance outcomes |
| `deploy/observability/alerts.yml` + README | 15 rules, promtool-validated |
| `deploy/vm/*.yml`, `.env.example` | Metrics on loopback; tracing knobs |

## Tests Added

Nine unit tests: duration and count recording, pattern-not-path labelling, unmatched collapsing, status-class reduction, bucket coverage of every `docs/PLAN/12` target, in-flight returning to zero including after a panic, every plan-named metric being registered, and const labels.

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Metrics endpoint as reconnaissance | `docs/SECURITY/02` §12 | Production all-interfaces bind refused at startup; verified unreachable from the LAN on the VM |
| Cardinality explosion via crafted paths | `docs/SECURITY/02` §10 | `TestUnmatchedRoutesCollapseToOneLabel` |
| Cardinality explosion via normal use | — | `TestRouteLabelUsesThePatternNotThePath` |
| Heap disclosure via pprof | `docs/SECURITY/02` §12 | Not mounted; reasoning recorded |
| Secrets in span attributes | `docs/PLAN/13` | Documented on `StartSpan`; no automatic redaction reaches spans |

## Definition of Done Verification

- [x] Latency histograms for every registered route
- [x] Metrics endpoint not reachable from the public ingress — verified on the VM
- [x] Every metric `docs/PLAN/13` names exists, tested
- [x] Tracing exporter configurable and off by default
- [x] Alert rules map onto `docs/PLAN/12`'s target table, promtool-validated
- [ ] **A trace showing handler and database spans with the same trace ID as its log lines** — the provider, propagation, and helpers are in place, but no handler or query is instrumented yet, because there are no handlers beyond health checks. The spans arrive with the endpoints in Phase 1.

The last item is stated rather than ticked. `P0-11`'s DoD asks for a trace to be observable end to end, and what exists is the machinery, not the trace.

## What Did Not Work

**A test asserted metrics that could not exist.** `TestEveryMetricNamedInThePlanIsRegistered` failed on the two HTTP metrics because it never made an HTTP request, and an unobserved `HistogramVec` produces no output at all.

The test was wrong, but the underlying behaviour is worth having learned: a dashboard panel for `http_requests_total{status_class="5xx"}` shows "no data" rather than zero until the first 5xx occurs. That is ambiguous between "healthy" and "not reporting", and it is why the alert rules use `absent()` where they do.

**Route labels initially showed `/*` for everything.** Investigating found only unmatched requests were affected — `/healthz` and `/readyz` resolved correctly — but the check was worth doing rather than assuming, given how quietly a single-label metric would have defeated the per-endpoint targets it exists to serve.

**Two OTel API mismatches** (`resource` construction, `semconv` import path) and one unused import. Ordinary, and caught by the compiler.

## Follow-Ups

- Handler and query spans arrive with the endpoints in Phase 1. The tracing skeleton is unexercised until then.
- No Prometheus is running against the VM yet. The listener binds loopback (ADR-011), so scraping needs either a container on the compose network or an explicit private-interface bind — a decision, not a default.
- A Grafana dashboard was scoped for this task and not built: with only health endpoints emitting, every panel would be empty, and a dashboard built against no data is a dashboard that gets rebuilt. The alert rules carry the same thresholds, so nothing is unmeasured meanwhile.

## What to Watch

**Cardinality is one careless route away from a problem.** The pattern-not-path rule is enforced by how the middleware reads chi's context, not by anything that would stop a future handler adding a label of its own. A `WithLabelValues(userID)` anywhere would be unbounded, and it would look reasonable in review.

**The metrics listener will be tempting to expose.** The moment someone wants to scrape from another host, `0.0.0.0` is the obvious edit. The production guard catches it there; staging has no such guard, and staging is where the habit would form.

**Alert thresholds have never fired.** They are derived from `docs/PLAN/12`'s targets rather than from observed behaviour, so the first real traffic will show whether they are tuned or merely plausible. `P5-08` is where they get validated against reality; until then a quiet alert manager means nothing has been tested, not that nothing is wrong.

# Observability

Metrics, alerts, and tracing for the Auth Service.

**Governing documents**: `PLAN/13-OBSERVABILITY.md` (what must be observable), `PLAN/12-PERFORMANCE.md` (the targets those observations are judged against).

---

## Where the Metrics Are

A **separate listener** from the public one, default `127.0.0.1:9090`, path `/metrics`.

Separate rather than a route on the public server, because `PLAN/13` requires the endpoint not to be reachable from the public ingress and a separate binding makes that a property of the socket rather than something an ingress rule has to remember. It keeps holding when someone reconfigures the ingress without knowing the rule exists.

**What the endpoint discloses**, since "metrics are harmless" is a common and wrong assumption: request rates per route, error rates, login success and failure counts, in-flight concurrency, database pool saturation, and the service version. That is a reconnaissance summary, and a reliable oracle for whether an attack is working (`SECURITY/02` §12). The config loader refuses `0.0.0.0` for this listener in production for that reason.

`pprof` is deliberately not mounted. It is genuinely useful and it exposes heap contents, which on this service means tokens and passwords in flight.

---

## Metric Names

Named once, up front, including for phases not yet built. A metric renamed after a dashboard is built produces an empty panel rather than an error, so the dashboard, the alerts, and the code have to agree from the start.

| Metric | Phase | Why it exists |
|---|---|---|
| `http_request_duration_seconds` | now | The source of every p50/p95/p99 in `PLAN/12`'s table |
| `http_requests_total` | now | Error rate per endpoint |
| `http_requests_in_flight` | now | Saturation, visible before latency shows it |
| `audit_partition_runway_months` | now | The goroutine has no supervisor; this is how its failure becomes visible |
| `audit_partition_maintenance_errors_total` | now | |
| `audit_events_written_total` | now | Writes are in-transaction, so failures here mean actions are failing |
| `db_instance_scoped_access_total` | now | `PLAN/08` Part B wants the cross-tenant path auditable |
| `auth_login_attempts_total` | P1-12 | `PLAN/13`'s brute-force alert keys on the failure ratio |
| `auth_tokens_issued_total` | P1-07 | Named in `PLAN/13` |
| `auth_lockouts_total` | P1-13 | |
| `authz_check_duration_seconds` | P2-06 | `PLAN/12`: p95 < 80ms RBAC, < 150ms ABAC |
| `authz_decisions_total` | P2-06 | A shift in the allow ratio is worth investigating |
| `authz_policy_eval_duration_seconds` | P4B-02 | Named in `PLAN/13`; zero until Phase 4b, correctly |
| `authz_project_grant_changes_total` | P4-01 | `PLAN/13` names an unusual spike as a possible misuse indicator |
| `redis_operation_duration_seconds` | P1-11 | Named in `PLAN/13` |
| `postgres_*` | now | Pool statistics; saturation reads as latency everywhere at once |

### Two cardinality rules that are not optional

**The route label is the chi route *pattern*, never the concrete path.** `/v1/organizations/{org_id}/users` is one time series; the concrete path would be one per organization. That is unbounded cardinality, and it is the usual way an application change takes down a Prometheus server without anyone attacking anything.

**Unmatched requests collapse to a single `unmatched` label.** A 404's path is entirely attacker-controlled, so a scanner walking a wordlist would otherwise create a time series per probe — turning the metrics endpoint into the denial-of-service vector.

Status codes are reduced to their class for the same reason. The exact code is in the logs; here it would multiply every series by the number of distinct codes, and alerts care about the 5xx rate rather than 502 versus 503.

---

## Alerts

`alerts.yml`, validated with `promtool check rules`. Fifteen rules, each carrying the reason it exists in its annotation — an alert whose purpose nobody remembers is an alert that gets silenced.

```bash
docker run --rm -v "$PWD:/rules" --entrypoint promtool prom/prometheus:v3.1.0 \
  check rules /rules/alerts.yml
```

### Absence is not health

The rule that shapes the others. A counter with no observations produces **no time series at all**, so `rate(...) == 0` never fires when the service is down — it fires when the service is up and idle, which is the opposite of what was wanted.

Rules that must catch "this stopped happening" use `absent()` or a gauge. `AuditPartitionRunwayUnknown` is the clearest case: if the maintenance goroutine dies, the gauge stops being reported, and a rule comparing its value would never fire. The absence *is* the signal.

The same caveat applies to dashboards: a panel for `http_requests_total{status_class="5xx"}` shows "no data" rather than zero until the first 5xx occurs, which is ambiguous between "healthy" and "not reporting".

---

## Tracing

Off by default. Set `AUTH_OTLP_ENDPOINT` to enable; empty means a no-op tracer provider rather than an unset global, so instrumented code works unchanged either way and there is no `if tracingEnabled` branch scattered through the codebase.

`AUTH_TRACE_SAMPLE_RATIO` defaults to 0.05. Sampling matters more here than usual: `/oauth/token` and `/v1/authz/check` are the highest-volume endpoints in the system (`PLAN/12`), and tracing every request would cost more than serving them.

W3C trace context propagation is enabled, so a trace started by a consumer application continues through this service rather than starting again. That continuity is most of the value — a consumer team debugging a slow login needs to see this service's spans inside their own trace.

**Span attributes must never carry a token, a password, or a raw `/v1/authz/check` resource attribute.** Spans go to a collector that is usually a different trust boundary from the logs, and the logger's redaction (`P0-09`) does not reach them. This is a rule the caller has to keep.

---

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `AUTH_ADMIN_ADDR` | `127.0.0.1:9090` | Refused if it binds all interfaces in production |
| `AUTH_ADMIN_ENABLED` | `true` | |
| `AUTH_OTLP_ENDPOINT` | *(empty)* | Empty disables tracing |
| `AUTH_OTLP_INSECURE` | `false` | Plain HTTP; only for a collector on the same host |
| `AUTH_TRACE_SAMPLE_RATIO` | `0.05` | 0.0–1.0 |

---

## Scraping on the VM

The admin listener binds loopback, so Prometheus must run on the same host or reach it over a private network. On the current single-VM deployment (ADR-011) that means either a Prometheus container joined to the compose network, or `AUTH_ADMIN_ADDR` set to a private interface — the second is a deliberate decision to make, not a default to fall into.

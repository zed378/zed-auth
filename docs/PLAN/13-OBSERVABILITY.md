# 13 — Observability

## Logging

- **Structured JSON logs**, including a `request_id`/`trace_id` for correlation across services.
- Clear log levels: `ERROR` for system failures, `WARN` for "normal" auth failures (wrong password) — don't treat a failed login as an `ERROR`, since it's expected, not a bug.
- Never log tokens, passwords, or raw resource attributes sent to `/v1/authz/check` — see `10-THREAT-MODEL.md` (Information Disclosure).

## Metrics (Prometheus)

Minimum metrics to monitor, tied to the targets in `12-PERFORMANCE.md`:
- Latency per endpoint (p50/p95/p99), especially `/oauth/token`, `/oauth/authorize`, `/v1/authz/check`.
- Failed vs. successful login rate (brute-force indicator).
- Number of tokens issued per second.
- Error rate per endpoint.
- DB connection pool usage, Redis latency, OPA policy evaluation duration.
- Project Grant creation/revocation rate (unusual spikes may indicate misuse).

## Tracing (OpenTelemetry)

End-to-end tracing from incoming request → validation → DB query → policy evaluation → response, to make debugging latency reports straightforward.

## Alerting

Example critical alerts:
- Spike in failed logins from one IP/account (possible attack — feeds into `10-THREAT-MODEL.md` monitoring).
- `/oauth/token` error rate above threshold (every consumer application is affected).
- DB replica lag too far behind primary.
- `/v1/authz/check` latency exceeding the `12-PERFORMANCE.md` targets sustained over a window.

## Graceful Degradation Behavior

Tied to the degraded-dependency testing in `12-PERFORMANCE.md`:
- Circuit breakers around Redis/DB calls where a slow dependency shouldn't block the whole request indefinitely.
- Fail-safe defaults for authorization: an authz check that cannot complete (dependency down) should **deny by default**, not allow by default — availability of a decision is never a reason to weaken security posture.

## Environments

| Environment | Purpose |
|---|---|
| `local` | Development, docker-compose |
| `staging` | Integration testing with consumer applications before production |
| `production` | Live |

Every environment has **separate signing keys & databases** — never share production keys/data with staging.

Continue to [14 — Deployment](./14-DEPLOYMENT.md).

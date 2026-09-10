# 03 — Detection & Monitoring

Consolidates the "Detection" column from `02-ATTACK-SURFACE-AND-SCENARIOS.md` into an actionable monitoring plan, tied to the metrics/alerting infrastructure in `PLAN/13-OBSERVABILITY.md`.

## Detection Layers

| Layer | Examples | Ties to |
|---|---|---|
| Network/edge | WAF rules, rate-limit triggers, unexpected outbound connections (SSRF indicator) | `PLAN/13-OBSERVABILITY.md` |
| Application | Failed login spikes, authorization-denial spikes, unusual API usage patterns | `PLAN/13-OBSERVABILITY.md` metrics |
| Data layer | Row-level security violations attempted (even if blocked, the attempt itself is a signal), unexpected audit-log write patterns | `02-ATTACK-SURFACE-AND-SCENARIOS.md` §19 |
| CI/CD | Secret-scanning hits, workflow file changes, dependency scan failures | `PLAN/11-TESTING.md`, `02-ATTACK-SURFACE-AND-SCENARIOS.md` §15/§18 |
| Runtime/infra | Container runtime anomalies, unexpected privilege escalation attempts within a pod | `02-ATTACK-SURFACE-AND-SCENARIOS.md` §17 |

## Priority Alerts (Consolidated from `02-ATTACK-SURFACE-AND-SCENARIOS.md`)

| Alert | Source scenario | Severity |
|---|---|---|
| Spike in failed logins across many distinct accounts from distributed IPs | §1 Credential stuffing | High |
| Repeated authorization-denial responses from a single account/IP | §2/§3 Authorization bypass / privilege escalation attempts | High |
| A `manager_roles` change outside expected admin workflow patterns | §3 Privilege escalation | Critical |
| `project_grants` created/revoked at unusual frequency | §11 Business-logic abuse | Medium |
| Attempted write to `events` outside the application's normal insert path | §19 Audit integrity | Critical |
| Secret-scanning hit in a commit or CI log | §16 Secret exposure | Critical |
| Dependency scan surfaces a new critical/high CVE | §15 Supply-chain | High |
| Outbound connection from Auth Service to an unexpected internal/private address | §7 SSRF | High |
| Sustained `/oauth/token` or `/v1/authz/check` latency breach (`PLAN/12-PERFORMANCE.md` targets) | §10 API abuse / general degradation | Medium–High depending on duration |

## Audit Log as a Detection Source

Since `events` (`PLAN/04-DATA-MODEL.md`) already captures every sensitive action, detection rules should be built as queries/alerts against this stream (forwarded to the SIEM, `PLAN/09-SECURITY.md`) rather than requiring separate instrumentation for every new feature — this is why `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` §15 requires every feature to specify its audit logging up front.

## Alert Routing & Ownership

- **Critical** alerts page the on-call engineer immediately, per the incident runbook in `PLAN/15-DISASTER-RECOVERY.md` and `04-INCIDENT-RESPONSE-PLAYBOOKS.md`.
- **High** alerts notify the security/backend on-call channel for same-business-day triage.
- **Medium** alerts are reviewed in a regular (e.g. weekly) security review, feeding `PLAN/18-RISK-REGISTER.md` if a pattern emerges.

## Review Cadence

Revisit this detection plan whenever a new entry is added to `02-ATTACK-SURFACE-AND-SCENARIOS.md` (e.g. when SAML or ABAC ships, per `PLAN/16-IMPLEMENTATION-ROADMAP.md`) — new attack surface without matching detection coverage is itself a gap worth tracking in `PLAN/18-RISK-REGISTER.md`.

Continue to [04 — Incident Response Playbooks](./04-INCIDENT-RESPONSE-PLAYBOOKS.md).

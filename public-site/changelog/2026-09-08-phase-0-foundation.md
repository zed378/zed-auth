---
slug: phase-0-foundation
title: Phase 0 — foundation
authors: [zed]
tags: [release]
date: 2026-09-08
---

The foundation is in place: the service builds, deploys, and runs against real
PostgreSQL and Redis, with cross-tenant isolation enforced by the database rather than
by application code.

Nothing user-facing has shipped. This entry records what exists so the
[roadmap](https://github.com/zed378/zed-auth/blob/main/docs/PLAN/16-IMPLEMENTATION-ROADMAP.md)
can be read against something real.

<!-- truncate -->

### Added

- **The service, deployed.** Structured logging with redaction by key name, Prometheus
  metrics on a listener separate from the public one, and OpenTelemetry tracing.
  Liveness and readiness are distinct probes — a liveness check that consulted the
  database would restart every instance during a database outage.
- **Row-level security on every tenant-scoped table.** The application connects as a
  role that cannot bypass it, and refuses to start if it can. A query that forgets its
  organization filter returns nothing rather than another tenant's rows.
- **An append-only audit log**, partitioned by month, with the write inside the
  transaction of the action that caused it. If the audit write fails, the action fails.
- **The API contract as the source of truth.** The server's Go interfaces are generated
  from `openapi/openapi.yaml`, so a handler that stops matching the contract fails to
  compile. The [API reference](/docs/api-reference) on this site is generated from the
  same file.
- **The management console shell** — design tokens, navigation, and accessibility
  foundations. No screens yet.
- **This site.**

### Fixed

- Six Go standard-library vulnerabilities, one of them a request-header timeout that
  was not applied during the unencrypted HTTP/2 check.
- A configuration bug that handed the service container database credentials capable of
  bypassing row-level security. Found by a CI check that parses the rendered
  configuration rather than reading the file.

### Next

Phase 1 delivers the OIDC provider, token issuance, user management and the first
console screens. The [quickstart](/docs/quickstart) becomes real then.

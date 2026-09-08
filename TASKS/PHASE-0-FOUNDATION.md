# Phase 0 — Foundation

**Goal**: turn a documentation-only repository into a working engineering environment — a running service skeleton, a real database schema, a CI pipeline that can reject bad code, and a public site stakeholders can look at — without implementing a single authentication feature yet.

**Why this phase exists separately**: every Phase 1 task assumes it can run migrations, write structured logs, emit an audit event, and be tested against a real Postgres. Building those foundations *while* building the OIDC provider is how security-critical code ends up untested. This phase buys the ability to move fast later.

**Exit criteria**: `docker compose up` produces a service that answers `/healthz`, connects to Postgres and Redis, has the full schema from `PLAN/04-DATA-MODEL.md` applied by migration, emits structured JSON logs with a request ID, and exposes Prometheus metrics — and a push to `main` runs build + test + SAST + dependency scan in CI. The public landing page, About page, and docs skeleton are live at a real URL.

**Roadmap reference**: `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 0.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P0-01 | Confirm and freeze the tech stack (ADRs) | docs | S | — |
| P0-02 | Initialize repository structure | infra | S | P0-01 |
| P0-03 | Git conventions, branch strategy, PR template | infra | S | P0-02 |
| P0-04 | Go service skeleton (config, router, graceful shutdown) | backend | M | P0-02 |
| P0-05 | Local environment via Docker Compose | infra | M | P0-04 |
| P0-06 | Migration tooling and baseline migration | backend | S | P0-05 |
| P0-07 | Core schema implementation | backend | L | P0-06 |
| P0-08 | Row-level security scaffolding | backend | M | P0-07 |
| P0-09 | Structured logging with redaction | backend | M | P0-04 |
| P0-10 | Health and readiness endpoints | backend | S | P0-04 |
| P0-11 | Metrics and tracing baseline | backend | M | P0-09 |
| P0-12 | Audit event writer (`events` table) | backend | M | P0-07, P0-09 |
| P0-13 | CI pipeline: build, test, lint, SAST, dependency scan | infra | M | P0-04 |
| P0-14 | Secrets and configuration conventions | infra | S | P0-04 |
| P0-15 | Test harness: testcontainers + Playwright skeleton | backend, console | M | P0-05, P0-16 |
| P0-16 | OpenAPI spec skeleton and client generation pipeline | backend, console | M | P0-13 |
| P0-17 | Console skeleton with design tokens | console | L | P0-02, P0-16 |
| P0-18 | Public site skeleton (marketing + docs) | public-site | L | P0-02 |
| P0-19 | Landing page, About page, docs skeleton content | public-site, docs | M | P0-18 |
| P0-20 | Staging environment provisioning | infra | L | P0-13 |
| P0-21 | Adopt the TASKS/MEMORY working discipline | docs | S | — |

---

## P0-01 — Confirm and Freeze the Tech Stack

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | — |
| **Plan refs** | `PLAN/07-BACKEND-ARCHITECTURE.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md`, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` |
| **Spec required** | No |
| **Surface** | docs |

**Goal** — Convert `PLAN/07`'s explicitly-labelled "initial recommendations" into decisions the rest of the project can build on without re-litigating.

**Steps**
1. Confirm Go as the backend language, or record the alternative — `PLAN/07` names TypeScript/NestJS and Java/Spring Authorization Server as acceptable substitutes if the team's existing skills point elsewhere.
2. Choose the OAuth2/OIDC provider library: `ory/fosite` or `zitadel/oidc`. Evaluate against four questions that later phases depend on: does it support Authorization Code + PKCE with PKCE mandatory for confidential clients too (`PLAN/05-API-CONTRACT.md`), pluggable storage, JWKS key rotation with an overlap window, and refresh token rotation with reuse detection (needed in Phase 3)?
3. Choose the HTTP router: `chi` or plain `net/http` + middleware. Constraint from `AGENTS.md` Code Style: no framework "magic" in security-critical paths.
4. Choose the migration tool (`golang-migrate`, `goose`, or `atlas`) — it must support running migrations as a **separate step before rollout**, never on service startup (`PLAN/14-DEPLOYMENT.md`).
5. Choose the public-site stack: a static generator for marketing (Astro or Next static export) plus a docs framework with native versioning (`PLAN/20` recommends Docusaurus).
6. Write one ADR per decision in `MEMORY/DECISIONS.md`.

**Definition of Done**
- [ ] An ADR exists for: backend language, OIDC library, HTTP router, migration tool, console build tool, public-site generator, docs framework.
- [ ] Each ADR names at least one rejected alternative and why it was rejected.
- [ ] The OIDC library ADR explicitly confirms support for key rotation with overlap and refresh-token reuse detection, since Phase 3 and Phase 5 depend on both.
- [ ] `AGENTS.md`'s placeholder "Setup Commands" section is replaced with the real toolchain and versions.

**Notes** — This is the one task in the project where choosing differently from the plan is cheap. Every later phase makes it more expensive.

---

## P0-02 — Initialize Repository Structure

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-01 |
| **Plan refs** | `CLAUDE.md` § Working Conventions |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — Create the directory layout `CLAUDE.md` already prescribes, so nobody has to invent one.

**Steps**
1. Create `backend/`, `console/`, `public-site/`.
2. Add `deploy/` for Docker Compose, Kubernetes manifests, and environment configuration — `PLAN/14-DEPLOYMENT.md` needs a home.
3. Add `openapi/` for the specification that both the console client and the public API reference are generated from (`PLAN/05`, `PLAN/20`).
4. Add a root `.gitignore` covering Go build output, `node_modules`, `.env` files, and build artifacts.
5. Add per-surface `README.md` files stating what lives there and which plan document governs it.
6. Leave `PLAN/`, `UI-UX/`, and `SECURITY/` untouched — they are reference material (`AGENTS.md` rule 9).

**Definition of Done**
- [ ] The layout matches `CLAUDE.md` exactly, plus `deploy/` and `openapi/`.
- [ ] `.gitignore` prevents any `.env` file from ever being committed (`SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §16 Secret Exposure).
- [ ] The root `README.md` mentions `TASKS/` and `MEMORY/` alongside the three plan folders.

---

## P0-03 — Git Conventions, Branch Strategy, PR Template

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-02 |
| **Plan refs** | `AGENTS.md` § PR / Change Instructions, `TASKS/00-TASK-CONVENTIONS.md` |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — Make the traceability rule (PRs reference the plan sections they implement) structurally enforced rather than merely remembered.

**Steps**
1. Initialize the git repository if it isn't one yet; make the first commit the existing documentation.
2. Define branch naming: `feat/<task-id>-<slug>`, `fix/`, `chore/`, `docs/`.
3. Add `.github/pull_request_template.md` with required fields: task ID, plan documents implemented, test layers added and run, deviation statement (with ADR link, or "none"), abuse cases covered.
4. Add `CODEOWNERS` marking `PLAN/`, `UI-UX/`, and `SECURITY/` as requiring explicit review — they should change only deliberately.
5. Add a `commit-msg` hook (or CI check) rejecting commit subjects that don't start with a valid task ID.

**Definition of Done**
- [ ] A PR cannot be opened without filling in the task ID and plan references.
- [ ] A commit without a task ID prefix is rejected before it reaches `main`.
- [ ] Editing anything under `PLAN/`, `UI-UX/`, or `SECURITY/` requires a code-owner review.

---

## P0-04 — Go Service Skeleton

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-02 |
| **Plan refs** | `PLAN/07-BACKEND-ARCHITECTURE.md` § Service Layout, `PLAN/14-DEPLOYMENT.md` § Deployment Model |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — A single Go binary that starts, reads configuration, serves HTTP, and shuts down gracefully — with the modular-monolith package boundaries already in place, so Phase 1 code lands in the right package instead of one enormous `main`.

**Steps**
1. `go mod init`, pin the Go version, commit `go.sum`.
2. Create the package layout reflecting `PLAN/07`'s modular monolith:
   ```
   backend/
     cmd/authservice/main.go
     internal/config/          → env + file config, validated at startup
     internal/httpserver/      → router, middleware chain, graceful shutdown
     internal/authn/           → login, session, MFA (Phase 1/3)
     internal/authz/           → RBAC, ABAC, decision logic (Phase 2/4b)
     internal/oidc/            → OIDC/OAuth2 provider (Phase 1)
     internal/saml/            → SAML IdP (Phase 4)
     internal/management/      → REST Management API (Phase 1+)
     internal/storage/postgres/
     internal/storage/redis/
     internal/audit/           → events writer
     internal/observability/   → logging, metrics, tracing
     migrations/
   ```
3. Configuration loader that fails fast and loudly on a missing required value. A service that boots with a missing signing-key configuration is worse than one that refuses to boot.
4. Middleware chain skeleton: request ID → structured logging → panic recovery → metrics → timeout. Security middleware (rate limiting, authn, authz) is added by later tasks at defined positions in this chain.
5. Graceful shutdown on `SIGTERM`: stop accepting connections, drain in-flight requests within a bounded timeout, then exit — required because this service sits on every consumer app's critical path (`PLAN/14`).

**Definition of Done**
- [ ] `go build ./...` and `go test ./...` pass.
- [ ] The binary starts, serves an HTTP port, and exits cleanly on `SIGTERM` with in-flight requests completed.
- [ ] Starting with a missing required config value fails at startup with a message naming the variable — never with a nil dereference at first use.
- [ ] Package boundaries exist as empty-but-real packages, so no Phase 1 task has to decide where its code goes.

---

## P0-05 — Local Environment via Docker Compose

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-04 |
| **Plan refs** | `PLAN/14-DEPLOYMENT.md` § Environment Strategy, `PLAN/13-OBSERVABILITY.md` § Environments |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — One command brings up the entire local stack, so "works on my machine" never becomes a debugging variable in a security-critical codebase.

**Steps**
1. `deploy/docker-compose.yml` with: the auth service, PostgreSQL, Redis, a mail catcher (invite and magic-link emails appear from Phase 1), and optionally Prometheus + Grafana for `P0-11`.
2. A multi-stage `Dockerfile` for the backend producing a minimal non-root runtime image (`SECURITY/02` §17 Container / Runtime Security).
3. Pin image versions — never `:latest` (supply-chain risk, `SECURITY/02` §15).
4. A `.env.example` documenting every variable with safe placeholder values; the real `.env` stays git-ignored.
5. Health-check-gated startup ordering, so the service waits for Postgres and Redis to be genuinely ready rather than merely started.

**Definition of Done**
- [ ] `docker compose up` from a clean checkout yields a service that answers `/healthz`.
- [ ] The runtime container runs as a non-root user, with a read-only root filesystem where feasible.
- [ ] No secret value is present in any committed file — only placeholders.
- [ ] `AGENTS.md`'s Setup Commands section documents the real command.

---

## P0-06 — Migration Tooling and Baseline Migration

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-05 |
| **Plan refs** | `PLAN/14-DEPLOYMENT.md` § Release Process, § Rollback Strategy |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — Schema changes are versioned, reviewable, and run as a deliberate step — never implicitly on service startup in production.

**Steps**
1. Wire in the migration tool chosen in `P0-01`, with `up`/`down` files under `backend/migrations/`.
2. Add a `make migrate-up` / `make migrate-down` entry point that is a **separate command from starting the service**.
3. Document the expand/contract discipline in `backend/migrations/README.md`: every migration must leave the previous application version able to run against the new schema, so an app rollback never requires a DB rollback (`PLAN/14`).
4. Add a CI check rejecting a migration that touches an existing column destructively (drop, rename, or type-narrow) without an accompanying expand/contract justification.
5. Baseline migration: required extensions (`pgcrypto`), plus the `instances` table.

**Definition of Done**
- [ ] Migrations run as an explicit command; the service does not run them at startup.
- [ ] A destructive migration is caught by CI and requires explicit justification to merge.
- [ ] `down` migrations exist and have been exercised locally.

---

## P0-07 — Core Schema Implementation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-06 |
| **Plan refs** | `PLAN/04-DATA-MODEL.md` (all of it), `PLAN/08-AUTHORIZATION.md` Part B |
| **Spec required** | Yes — data model change |
| **Surface** | backend |

**Goal** — The complete entity model from `PLAN/04` exists in Postgres, multi-tenant-ready from day one even though the MVP runs with a single default organization.

**Steps**
1. Create tables in dependency order: `instances` → `organizations` → `users`, `projects` → `applications`, `roles` → `user_grants`, `project_grants`, `manager_roles`, `sessions`, `refresh_tokens`, `signing_keys`, `user_tokens`, `events`.
2. Later-phase tables may be created now or deferred to the phase that uses them — `user_mfa_factors` and `user_recovery_codes` (Phase 3), `user_identities`, `webhook_endpoints`, `webhook_deliveries` (Phase 4), `user_attributes` and `policies` (Phase 4b). Defer by default, per `PLAN/00`'s principle of not over-engineering ahead of need; create early only where doing so costs nothing. Record the choice in the MEMORY record either way.
3. `signing_keys` and `user_tokens` are **not** deferrable — `P1-03` and `P1-19` both need them inside Phase 1.
4. Include the columns Phase 3 depends on but Phase 1 does not use: `refresh_tokens.family_id` and `replaced_by` (`PLAN/04`). The storage shape must not need changing when rotation arrives.
5. Partition `events` by month on `created_at` from the start (`PLAN/04` § Retention and Growth) — retrofitting partitioning onto a large table is far more disruptive than starting with it.
6. Enforce the constraints the plan states rather than implies:
   - `users.email` unique **per org**, not globally (`PLAN/04`) — a composite unique index on `(org_id, lower(email))`.
   - `users.username` unique per org where present.
   - `users.password_hash` nullable, because social and passwordless users legitimately have none.
   - `applications.client_secret_hash` nullable for public clients (SPA/native), which must use PKCE.
   - `project_grants.status` and `users.status` as real enums or check constraints, never free text.
   - `user_identities` unique on `(provider, provider_subject)` — matching is never on email (`PLAN/04`).
   - `roles` unique on `(project_id, key)`.
7. Put `org_id` on every tenant-scoped table, even where it is derivable through a join — RLS in `P0-08` needs it directly, and a misscoped query is exactly the failure mode `PLAN/08` Part B guards against. This now includes `sessions.org_id` (`PLAN/04`).
8. Index for the access patterns the plan already anticipates: login lookup by `(org_id, email)`, session lookup by id, `user_grants` by `(user_id, project_id)`, `events` by `(org_id, created_at DESC)` and by `event_type`, `signing_keys` by `kid` and by `status`, `user_tokens` by `token_hash`.
9. Make `events` append-only at the database level: revoke `UPDATE` and `DELETE` from the application role (`SECURITY/02` §19 Logging / Audit Integrity).
10. Store only hashes where the plan says so: `refresh_tokens.token_hash`, `user_tokens.token_hash`, `user_recovery_codes.code_hash`, `applications.client_secret_hash`, `webhook_endpoints.secret_hash`. `signing_keys.private_key_ref` holds a secret-manager reference, never key material.

**Definition of Done**
- [ ] Every Phase 1 table and column in `PLAN/04-DATA-MODEL.md` exists with the stated nullability and types; every deferred table is named in the MEMORY record with the phase that will create it.
- [ ] The application database role cannot `UPDATE` or `DELETE` rows in `events`, verified by an integration test that attempts it and expects failure.
- [ ] `events` is month-partitioned and a partition can be dropped without touching the rest of the table.
- [ ] Two users with the same email in *different* organizations can both be created; two in the same organization cannot.
- [ ] No table storing a credential stores it in a reversible form, verified by inspecting every column listed in step 10.
- [ ] An ERD generated from the live schema matches the diagram in `PLAN/04`.

**Abuse cases to test**
- Cross-organization data access through a query that omits its `org_id` filter — stopping this is `P0-08`'s job, but the test belongs to the schema too.
- Attempted tampering with an `events` row (`SECURITY/02` §19).

---

## P0-08 — Row-Level Security Scaffolding

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-07 |
| **Plan refs** | `PLAN/08-AUTHORIZATION.md` Part B, `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2 |
| **Spec required** | Yes — authorization boundary |
| **Surface** | backend |

**Goal** — Cross-tenant isolation that holds even when the application layer forgets to filter. `PLAN/08` explicitly wants "a misscoped query can't leak data across organizations" to be a database property, not a code-review outcome.

**Steps**
1. Enable `ROW LEVEL SECURITY` on every tenant-scoped table.
2. Define policies against a session-local setting (e.g. `app.current_org_id`) set per connection or transaction by the application.
3. Ensure the application's database role is **not** the table owner and does not have `BYPASSRLS` — a superuser connection silently disables the entire protection.
4. Make the connection-scoped tenant context part of the storage layer's transaction helper, so no query path can accidentally run without it.
5. Decide and document the behavior for genuinely instance-level operations (an `INSTANCE_OWNER` listing all organizations): a separate role, an explicit elevated context, or a scoped bypass function. Whichever it is, it must be an explicit, auditable path — not "the normal path with the filter omitted."

**Definition of Done**
- [ ] An integration test issues a deliberately unfiltered `SELECT * FROM users` under org A's context and receives only org A's rows.
- [ ] An automated check confirms the application role is non-owner and non-`BYPASSRLS`.
- [ ] The instance-level access path is explicit, documented, and writes an audit event when used.
- [ ] A query with no tenant context set fails closed — returning nothing or erroring, never returning everything.

**Abuse cases to test**
- `SECURITY/02` §2 Authorization Bypass / IDOR: request a resource ID belonging to another organization and confirm a response that does not confirm the resource's existence (see also §12 Enumeration).

---

## P0-09 — Structured Logging with Redaction

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-04 |
| **Plan refs** | `PLAN/13-OBSERVABILITY.md` § Logging, `SECURITY/02` §16, `PLAN/10-THREAT-MODEL.md` § Information Disclosure |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — JSON logs correlated by request ID, with the "never log secrets" rule enforced by the logger itself rather than by developer discipline.

**Steps**
1. Structured JSON logging (Go's `log/slog` or equivalent) with `request_id`/`trace_id` on every line.
2. Middleware generates a request ID, or adopts an inbound one from a trusted proxy only, and puts it in the request context.
3. Implement a redaction layer: a deny-list of field names (`password`, `token`, `access_token`, `refresh_token`, `code_verifier`, `client_secret`, `authorization`, `cookie`, `attributes`) replaced before serialization.
4. Set log levels per `PLAN/13`: a failed login is `WARN`, not `ERROR` — it is expected behavior, and treating it as an error trains everyone to ignore errors.
5. Add a CI lint rule failing the build if a raw request body, an `Authorization` header, or a `/v1/authz/check` payload is passed to a log call.

**Definition of Done**
- [ ] Every log line is valid JSON and carries a request ID.
- [ ] A unit test asserts that logging a struct containing a token field emits a redacted placeholder.
- [ ] Failed logins log at `WARN` with no credential material.
- [ ] The CI rule demonstrably fails on a deliberately-introduced violation.

---

## P0-10 — Health and Readiness Endpoints

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-04 |
| **Plan refs** | `PLAN/14-DEPLOYMENT.md` § Deployment Model |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — The orchestrator can tell "the process is alive" apart from "ready to serve traffic," so a rolling update never routes traffic to an instance whose database pool isn't up.

**Steps**
1. `/healthz` — liveness. Cheap, no dependency checks; answers as long as the process isn't wedged.
2. `/readyz` — readiness. Verifies the Postgres connection pool and Redis reachability with a short timeout.
3. Neither endpoint discloses versions, hostnames, dependency addresses, or error detail to an unauthenticated caller (`SECURITY/02` §12 Enumeration).
4. Exclude both from request logging and rate limiting, so probe traffic doesn't drown the logs or trip limits.

**Definition of Done**
- [ ] `/readyz` returns non-200 when Postgres or Redis is down; `/healthz` still returns 200.
- [ ] Neither response body contains infrastructure detail.
- [ ] Docker Compose and the Kubernetes manifests both use them.

---

## P0-11 — Metrics and Tracing Baseline

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-09 |
| **Plan refs** | `PLAN/13-OBSERVABILITY.md` § Metrics, § Tracing, `PLAN/12-PERFORMANCE.md` |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — The measurement infrastructure exists before there is anything to measure, so Phase 1's latency targets are observed from the first deployment rather than retrofitted after the first incident.

**Steps**
1. A Prometheus metrics endpoint, not exposed on the public ingress.
2. Baseline instruments matching what `PLAN/13` requires: request latency histograms per endpoint (p50/p95/p99 derivable), error rate per endpoint, DB pool utilization, Redis operation latency.
3. Reserve and document the metric names later phases will populate: login success/failure counters, tokens issued per second, `/v1/authz/check` latency, OPA evaluation duration, Project Grant create/revoke rate.
4. OpenTelemetry tracing skeleton: incoming request → handler → DB query spans, with the exporter configurable and off by default locally.
5. A starter Grafana dashboard in `deploy/` whose panels map one-to-one onto `PLAN/12`'s latency target table, so "are we meeting target" is answerable at a glance rather than by ad-hoc querying.

**Definition of Done**
- [ ] Latency histograms exist for every registered route.
- [ ] The metrics endpoint is not reachable from the public ingress.
- [ ] A single request's trace shows handler and database spans carrying the same trace ID as its log lines.
- [ ] The dashboard's panels correspond one-to-one with `PLAN/12`'s target table rows.

---

## P0-12 — Audit Event Writer

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-07, P0-09 |
| **Plan refs** | `PLAN/04-DATA-MODEL.md` § `events`, `PLAN/09-SECURITY.md` § Audit, `SECURITY/02` §19 |
| **Spec required** | Yes — security control |
| **Surface** | backend |

**Goal** — A single, uniform way to record "something identity- or permission-changing happened," available before Phase 1 needs it, so no feature invents its own audit format.

**Steps**
1. An `audit` package with one entry point taking: org, actor (nullable — a failed login has no authenticated actor), event type, and a structured payload.
2. Define the event-type taxonomy as constants, following the plan's `noun.verb.outcome` shape: `user.login.success`, `user.login.failed`, `user.created`, `role.assigned`, `role.revoked`, `project_grant.created`, `project_grant.revoked`, `policy.activated`, `session.revoked`.
3. Apply the same redaction rules as `P0-09` to the payload — an audit log that records a password attempt is a credential store.
4. Decide and document write semantics: is the audit write inside the business transaction (consistent, but a failed audit write rolls back the action) or after it (the action succeeds even if auditing fails)? For a security-critical action, prefer failing the action over losing the record. State the choice in the MEMORY record.
5. Provide a query helper with pagination for the Audit Log console screen (`UI-UX/08-PAGE-SPECIFICATIONS.md`) and for Phase 5's export.
6. Include a SIEM forwarding hook as a no-op implementation now, so `PLAN/09`'s "ideally forwarded to an external SIEM" has somewhere to plug in later.

**Definition of Done**
- [ ] Every event-type constant is documented with its payload shape.
- [ ] A payload containing a token or password field is redacted before storage, verified by test.
- [ ] Writes are append-only in practice — `P0-07`'s integration test covers the database-level guarantee.
- [ ] The write-semantics decision is recorded in `MEMORY/DECISIONS.md`.

---

## P0-13 — CI Pipeline

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-04 |
| **Plan refs** | `PLAN/14-DEPLOYMENT.md` § Release Process, `PLAN/09-SECURITY.md` § Secure Development Practices, `PLAN/11-TESTING.md` |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — A pipeline that can actually reject unsafe code, in place before there is unsafe code to reject.

**Steps**
1. A GitHub Actions workflow on pull request and on `main`: build → unit tests → integration tests (with service containers for Postgres and Redis) → lint → `gosec` SAST → dependency CVE scan.
2. Frontend jobs: console build plus component tests; public-site build.
3. Fail the build on critical or high findings from SAST or dependency scanning — `PLAN/11`'s "Production-Ready" criteria require no unaddressed critical/high.
4. Pin action versions by commit SHA rather than by tag: CI is itself an attack surface (`SECURITY/02` §18 CI/CD Attack Surface).
5. Scope workflow permissions to the minimum (`contents: read` by default), and never expose repository secrets to workflows triggered by forked pull requests.
6. Add a secret-scanning step over the diff, so a committed credential is caught at PR time (`SECURITY/02` §16).
7. Cache dependencies for speed, but never cache anything derived from a secret.

**Definition of Done**
- [ ] A PR introducing a `gosec`-flagged high finding cannot merge.
- [ ] A PR introducing a dependency with a known critical CVE cannot merge.
- [ ] A PR containing a credential-shaped string is flagged by secret scanning.
- [ ] All third-party actions are SHA-pinned.
- [ ] Workflow token permissions are explicitly declared and minimal.

---

## P0-14 — Secrets and Configuration Conventions

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-04 |
| **Plan refs** | `PLAN/07-BACKEND-ARCHITECTURE.md` § Infrastructure, `PLAN/09-SECURITY.md`, `SECURITY/02` §16 |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — Establish, before the first signing key exists, where secrets live and how they reach the service — because `PLAN/02`'s constraint is that no third party ever holds the private signing key.

**Steps**
1. Document config precedence: environment variables for deployment-specific values, a secret manager (Vault or the cloud provider's) for credentials, files only for local development.
2. Enumerate every secret the system will hold, with owner and rotation expectation: JWT signing private keys, database credentials, Redis credentials, OIDC client secrets (hashed at rest per `PLAN/04`), SMTP credentials, and later social-IdP client secrets.
3. Define separate secrets per environment — `PLAN/13` and `PLAN/14` both state that staging and production never share signing keys or databases.
4. Write the rotation runbook skeleton for each secret class; the JWT key rotation procedure itself is implemented in `P1-03`.
5. Add a pre-commit hook for local secret scanning, complementing the CI check in `P0-13`.

**Definition of Done**
- [ ] Every secret is inventoried with owner, storage location, and rotation cadence.
- [ ] Local, staging, and production use distinct values for every secret, verified by an environment-diff check.
- [ ] No secret is readable from the container image or from the repository.
- [ ] The pre-commit hook is documented in the contributor setup instructions.

---

## P0-15 — Test Harness

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-05, P0-16 |
| **Plan refs** | `PLAN/11-TESTING.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md` § Testing |
| **Spec required** | No |
| **Surface** | backend, console |

**Goal** — All four layers of `PLAN/11`'s pyramid have a working example test before Phase 1 starts, so no Phase 1 task can plead "there's no harness for that yet."

**Steps**
1. Unit test conventions: table-driven, no database, fast enough to run on every save.
2. An integration harness using testcontainers to spin up real Postgres and Redis, applying migrations per suite, with per-test isolation (transaction rollback or schema-per-test).
3. A Playwright skeleton for the console with a placeholder test, plus the fixtures Phase 1 will need: seed an org, a project, an application, and a user.
4. A security-test package kept separate from feature tests, so `PLAN/11` § Security Testing scenarios stay visible as a group rather than scattered.
5. A test-data factory, so each test states only what it actually cares about.
6. CI wiring with coverage reporting. Set a coverage floor for `internal/authn`, `internal/authz`, and `internal/oidc` specifically, rather than a meaningless repo-wide average.

**Definition of Done**
- [ ] One passing example test exists at each pyramid layer.
- [ ] The integration suite runs from a clean machine with only Docker installed.
- [ ] Tests are isolated: the suite passes when run repeatedly and in random order.
- [ ] The security-test package exists with at least one real assertion — `P0-08`'s RLS test is its natural first inhabitant.

---

## P0-16 — OpenAPI Spec Skeleton and Client Generation

| | |
|---|---|
| **Status** | WIP — steps 1, 2 and 5 done ([ADR-013](../MEMORY/DECISIONS.md)). Steps 3 and 4 need `console/` and `public-site/` to exist and are carried into `P0-17` and `P0-18` |
| **Depends on** | P0-13 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Documentation, `PLAN/06-FRONTEND-ARCHITECTURE.md` § Tech Stack, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § API Reference Generation |
| **Spec required** | No |
| **Surface** | backend, console |

**Goal** — Establish the single API contract artifact that both the console client and the public API reference are generated from, satisfying `CLAUDE.md`'s rule that API documentation is never hand-written.

**Steps**
1. Create `openapi/openapi.yaml` with info, servers, security schemes (OAuth2 bearer), and the shared component schemas the whole API depends on: the standard error object from `PLAN/05` (`code`, `message`, `details[]`), the pagination envelope (`next_page_token`), and common parameters (`page_size`, `page_token`).
2. Decide spec-first versus code-first generation and record it as an ADR. Spec-first is easier to review; code-first is harder to let drift. `PLAN/05` accepts either as long as CI validates the result.
3. Wire the console's typed API client generation, so a spec change regenerates client types.
4. Wire the public site's API reference rendering from the same file (`PLAN/20`).
5. CI: lint the spec, and fail if the committed generated client is stale relative to it.

**Definition of Done**
- [x] The spec validates in CI. `redocly lint`, in the `api-contract` job and in `scripts/check.sh`.
- [x] The error and pagination schemas match `PLAN/05-API-CONTRACT.md` exactly.
- [x] Changing the spec without regenerating the client fails CI. Verified by deliberately renaming an `operationId` and watching the gate fail with the diff.
- [ ] The public site renders the (currently near-empty) API reference from this file, proving the pipeline works before there is content in it. — carried to `P0-18`.

**Beyond the stated steps**

The generated Go server interface was not asked for and is the reason this task is worth more than a validated YAML file. `PLAN/05` accepts a spec that CI merely validates; handlers implementing a generated interface make a contract mismatch a compile error instead. `scripts/openapi-shipped-paths.py` was also added, because `/docs/api-reference` renders from the spec and a documented endpoint is therefore a public claim that it exists.

---

## P0-17 — Console Skeleton with Design Tokens

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-08-P0-17-console-skeleton.md) |
| **Depends on** | P0-02, P0-16 |
| **Plan refs** | `PLAN/06-FRONTEND-ARCHITECTURE.md`, `UI-UX/05-DESIGN-SYSTEM.md`, `UI-UX/06-VISUAL-LANGUAGE.md`, `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `UI-UX/13-ACCESSIBILITY.md` |
| **Spec required** | No |
| **Surface** | console |

**Goal** — A React + TypeScript application shell whose design tokens are implemented *as tokens*, so no screen ever hard-codes a color and the rebrand-safety property `UI-UX/05` requires actually holds.

**Steps**
1. Scaffold React + TypeScript with the chosen build tool; static SPA output, deployable to a CDN independently of the backend (`PLAN/06`).
2. Implement every token from `UI-UX/05-DESIGN-SYSTEM.md` as CSS custom properties mapped into the Tailwind theme: the nine color tokens, the constrained type scale, the 4px/8px spacing scale, and the three elevation levels.
3. Enforce token-only usage with a lint rule rejecting raw hex colors and arbitrary spacing values in application code.
4. Encode the branding constraint structurally: `color-accent` and the logo are overridable per organization; `color-danger` and `color-warning` are not (`UI-UX/05` § Color).
5. Application shell: routing, the navigation structure from `PLAN/06`'s IA, the 12/8/4-column responsive grid from `UI-UX/12`, and an error boundary.
6. TanStack Query for server state, with no global store for cached API data (`PLAN/06`).
7. Wire the generated API client from `P0-16`.
8. Accessibility baseline from the start: visible focus rings using `color-accent`, a skip-to-content link, and correct landmark regions (`UI-UX/13`). Retrofitting these in Phase 5 is how accessibility audits fail.

**Definition of Done**
- [x] Every token in `UI-UX/05` exists in code, by name. Asserted by `tokens.test.ts`, which also fails if a tenth colour token appears — that would be a design-system change and `UI-UX/05` § Governance requires it to happen there first.
- [x] A raw hex color in a component fails lint. Verified against a deliberately violating component; all three rules fire.
- [x] `color-danger` cannot be overridden by organization branding, verified by test — through the type and through the runtime filter behind it, since branding arrives as untyped JSON.
- [x] The shell renders correctly at 1440px, 1024px, and 768px per `UI-UX/12`'s grid. Measured in a real browser against `console.zedth.my.id`, plus 600px for `UI-UX/12` § Testing's unsupported-width message.
- [x] Keyboard navigation reaches every interactive element in the shell with a visible focus indicator. Verified with real `Tab` presses: every link matches `:focus-visible` with a 2px accent outline and a 44px target.

**Beyond the stated steps**

`utilities.test.ts` compiles Tailwind and asserts the classes the components use actually produce CSS. It exists because every type token was defined, correctly named, and generated nothing at all — and the source-reading tests passed throughout. `deploy/console/` carries the nginx config and compose file for the staging preview, including the history-mode fallback that a static host needs and a healthcheck that probes a route with no file behind it.

---

## P0-18 — Public Site Skeleton

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-08-P0-18-public-site-skeleton.md), [ADR-014](../MEMORY/DECISIONS.md) |
| **Depends on** | P0-02 |
| **Plan refs** | `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` |
| **Spec required** | No |
| **Surface** | public-site |

**Goal** — A separately-built, separately-deployed static site with a docs framework that supports versioning — deliberately sharing neither codebase nor pipeline with the console.

**Steps**
1. Scaffold the marketing site with the static generator chosen in `P0-01`.
2. Scaffold the docs section with the docs framework, versioning enabled from the start. Retrofitting versioning once v1 docs exist is painful, and `PLAN/20` requires old-version docs to stay reachable through their deprecation window.
3. Build the route structure from `PLAN/20`: `/`, `/about`, `/docs` (with `quickstart`, `concepts`, `guides`, `api-reference`, `console`), `/changelog`, `/security`, `/contact`. Omit `/pricing` entirely unless a commercial tier exists.
4. Share only the visual language with the console (`UI-UX/06-VISUAL-LANGUAGE.md`) — brand-level tokens, not components, not code.
5. Set up an independent deploy pipeline to static hosting/CDN. A docs typo fix must not require a backend deploy.
6. Wire up docs search.
7. Add privacy-respecting analytics.
8. Keep content as Markdown/MDX in the repo — docs-as-code, reviewed through the same PR process as code.

**Definition of Done**
- [x] The site builds and deploys through a pipeline entirely separate from the console's — its own CI job, install, cache and `deploy/public-site/`.
- [x] Docs versioning is enabled and demonstrated with a placeholder second version, labelled as a placeholder rather than presented as a release.
- [x] Search returns results across docs pages. Local index, 123 documents, covering docs, changelog and pages.
- [x] No code is shared with `console/`; only design tokens are duplicated, deliberately — and both halves are enforced by script rather than left to memory.
- [ ] **Lighthouse performance and SEO scores meet the bar set in `UI-UX/20` § Cross-Page Requirements.** That section sets an accessibility bar and no numeric performance target, so there is no bar to meet. Raised as `OQ-10`; settle the number or drop the item.

**Beyond the stated steps**

`check-contrast.mjs` caught a dark palette that would have shipped unreadable — the console's colours measure 2.4:1 to 3.3:1 on a dark surface, all below AA. `write-robots.mjs` generates the sitemap pointer from the same `SITE_URL` Docusaurus uses for canonical URLs, after the first deploy published a pointer to the wrong host.

---

## P0-19 — Landing, About, and Docs Skeleton Content

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-18 |
| **Plan refs** | `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § Deployment & Roadmap Placement |
| **Spec required** | No |
| **Surface** | public-site, docs |

**Goal** — Real content on the landing, About, and docs-concepts pages, under the hard constraint that nothing may describe a capability that isn't shipped.

**Steps**
1. Build the landing page to `UI-UX/20`'s detailed spec, using the copy blueprints in `UI-UX/21`.
2. Build the About page.
3. Write `/docs/concepts` — organizations, projects, applications, users, roles, grants — at a product-explainer level derived from `PLAN/03` and `PLAN/08`. Concepts are safe to document now because they describe the model, not shipped endpoints.
4. Add `/docs/quickstart` as an explicit **placeholder** stating that the MVP flow is not yet available. `P1-24` replaces it with the real guide.
5. Publish `/contact` with the responsible-disclosure channel (`security@` or a form), per `PLAN/20`'s requirement that external researchers have a documented path.
6. Apply the governance rule as a review gate: no marketing or docs sentence claims a capability beyond the current phase (`UI-UX/21`, `CLAUDE.md`).
7. Do **not** publish `/security` content yet — `PLAN/20` places the trust page alongside Phase 5, and it must never leak `SECURITY/` internals.

**Definition of Done**
- [ ] Landing and About pages match `UI-UX/20`'s spec and `UI-UX/21`'s copy blueprints.
- [ ] A capability audit confirms every claim on every published page maps to something either shipped or explicitly labelled as planned.
- [ ] The quickstart placeholder is unambiguous about what does not exist yet.
- [ ] A responsible-disclosure contact path is live.
- [ ] No content derived from `SECURITY/02`, `PLAN/14` topology, or `PLAN/18` appears anywhere on the site.

---

## P0-20 — Staging Environment Provisioning

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-13 |
| **Plan refs** | `PLAN/14-DEPLOYMENT.md`, `PLAN/13-OBSERVABILITY.md` § Environments, `PLAN/15-DISASTER-RECOVERY.md` |
| **Spec required** | No |
| **Surface** | infra |

**Goal** — A staging environment mirroring production topology at smaller scale, so consumer teams have somewhere real to integrate against during Phase 1.

**Steps**
1. Provision managed Postgres and Redis for staging, with credentials distinct from every other environment.
2. Write Kubernetes manifests (or Compose, if staging is small) in `deploy/`, with liveness and readiness probes wired to `P0-10`.
3. Terminate TLS at the ingress; no HTTP fallback anywhere (`PLAN/09`).
4. Generate staging-only signing keys. Never copy production keys into staging, and never copy production data into staging (`PLAN/13`, `PLAN/14`).
5. Continuous deployment: deploy to staging on merge to `main`, run smoke tests, and require a manual promotion gate for production.
6. Run migrations as a separate pipeline step before the application rollout (`PLAN/14`).
7. Configure automated Postgres backups with point-in-time recovery, and verify a restore actually works — an unverified backup is a hypothesis (`PLAN/15`).

**Definition of Done**
- [ ] Staging is reachable over TLS only.
- [ ] Staging signing keys and database are provably distinct from production.
- [ ] A merge to `main` deploys to staging automatically and runs a smoke test.
- [ ] A backup restore has been executed successfully at least once, with the result recorded in MEMORY.
- [ ] Production promotion requires an explicit human action.

---

## P0-21 — Adopt the TASKS/MEMORY Working Discipline

| | |
|---|---|
| **Status** | DONE |
| **Depends on** | — |
| **Plan refs** | `TASKS/00-TASK-CONVENTIONS.md`, `MEMORY/README.md` |
| **Spec required** | No |
| **Surface** | docs |

**Goal** — Establish, before implementation starts, that every change produces a record — so the audit trail is complete from the first commit rather than reconstructed later.

**Definition of Done**
- [x] `TASKS/` exists with conventions, phase files, a progress board, and a backlog.
- [x] `MEMORY/` exists with a README, index, changelog, decision log, and record template.
- [x] The first change record documents the creation of these two folders.
- [ ] `CLAUDE.md` and `AGENTS.md` reference `TASKS/` and `MEMORY/` in their documentation maps — pending user approval, since both files govern agent behavior and editing them is a deliberate act (`BACKLOG.md` OQ-01).

---

## Phase 0 Exit Checklist

- [ ] `docker compose up` yields a healthy service against real Postgres and Redis.
- [ ] The full `PLAN/04-DATA-MODEL.md` schema is applied by migration, with RLS active and verified.
- [ ] Structured logs, metrics, and traces are all emitted and correlated by request ID.
- [ ] CI rejects: build failure, test failure, high SAST finding, critical CVE, committed secret, stale generated client.
- [ ] Staging is live, isolated from production, with a verified backup restore.
- [ ] The console shell renders with real design tokens and full keyboard accessibility.
- [ ] The public landing page, About page, and docs concepts are live and claim nothing unshipped.
- [ ] Every completed task has a MEMORY record.

# Documentation — Centralized Auth Service

An identity and access platform: centralized authentication (OIDC / OAuth 2.1), a complete REST Management API, multi-tenant RBAC with cross-organization delegation, a management console, and a public site.

```
Consumer application / SPA / service
            |  OIDC, OAuth 2.1, REST
            v
   +-------------------------------+
   |      Auth Service (Go)        |
   |  hosted login · OIDC provider |
   |  Management API · authz check |
   +---------------+---------------+
                   |
        +----------+----------+
        v                     v
   PostgreSQL              Redis
  (RLS, audit)      (cache, rate limits)
```

## Two kinds of document, deliberately separated

| | |
|---|---|
| **`PLAN/`, `UI-UX/`, `SECURITY/`** | **Design intent.** Written before the code, amended only through the deliberate plan-change process (`AGENTS.md` rule 9; `.github/CODEOWNERS` requires a review that a code change does not). They say what should be true. |
| **Every other category** | **The system as built.** Each document states a status — `Implemented`, `Partially implemented` or `Draft specification` — and cites the code, migration, contract or test that backs each claim. |

Where the implementation deliberately differs from the plan, the difference is recorded as an ADR in [`../MEMORY/DECISIONS.md`](../MEMORY/DECISIONS.md) or as a plan gap (`PG-xx`) in [`../TASKS/BACKLOG.md`](../TASKS/BACKLOG.md) — never resolved silently, and never by quietly editing the plan.

## Categories

| Folder | Covers |
|---|---|
| [`PLAN/`](./PLAN/) | Product scope, requirements, architecture, data model, API contract, authorization, security baseline, testing, performance, deployment, roadmap, acceptance criteria |
| [`UI-UX/`](./UI-UX/) | Design direction and system, page and component specifications, accessibility, the public site |
| [`SECURITY/`](./SECURITY/) | Assets and trust boundaries, threat actors, attack scenarios, detection, incident response, red-team verification |
| [`ARCHITECTURE/`](./ARCHITECTURE/) | The deployable units, their boundaries, and the backend's internal structure |
| [`ENGINEERING/`](./ENGINEERING/) | How code is written, reviewed and landed: coding standards, layer templates, error and logging conventions, the gate, the review checklist |
| [`API/`](./API/) | The `/v1` REST contract: conventions, errors, pagination, idempotency, and every resource family |
| [`IDENTITY-PROTOCOL/`](./IDENTITY-PROTOCOL/) | OIDC discovery, the OAuth 2.1 authorization server, MFA, passkeys, and (unbuilt) SAML |
| [`SESSION-MANAGEMENT/`](./SESSION-MANAGEMENT/) | Sessions, token issuance and structure, refresh rotation, key rotation, revocation |
| [`AUTHORIZATION/`](./AUTHORIZATION/) | RBAC, the manager-role hierarchy, Project Grants, the permission table, the live check |
| [`MULTI-TENANCY/`](./MULTI-TENANCY/) | Tenant model, row-level security, isolation testing |
| [`DATABASE/`](./DATABASE/) | Schema, policies, triggers, indexes, migrations, audit storage |
| [`OBSERVABILITY/`](./OBSERVABILITY/) | Logging, the audit log, metrics, tracing, SLOs |
| [`PERFORMANCE/`](./PERFORMANCE/) | Targets, measured results, load-test method |
| [`TESTING/`](./TESTING/) | The test layers, abuse-case testing, and the CI gates |
| [`DEVOPS/`](./DEVOPS/) | Environments, containers, CI/CD, secrets, backup and recovery |
| [`DEVELOPER/`](./DEVELOPER/) | Getting started, local development, integrating an application |
| [`FRONTEND/`](./FRONTEND/) | The management console as engineering: structure, tokens, components, data layer, routing, forms, testing, build |
| [`WEBSITE/`](./WEBSITE/) | The public site: purpose, information architecture, content governance, the generated API reference, launch |
| [`SDK/`](./SDK/) | Client libraries — what exists today and what is only specified |
| [`WEBHOOK/`](./WEBHOOK/) | Outbound event delivery (not built; specified for `P4-12`) |

Each folder has a `README.md` listing its documents with their status.

## Reading order

```
1.  PLAN/00-PROJECT-CONTEXT.md        → the problem and the product
2.  PLAN/01-PRODUCT-SCOPE.md          → and what is deliberately out of scope
3.  ARCHITECTURE/00-SYSTEM-ARCHITECTURE.md
4.  DATABASE/00-DATABASE-ARCHITECTURE.md and 01-SCHEMA-DEFINITIONS.md
5.  AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md   → RBAC, delegation and the live check
6.  API/00-API-OVERVIEW.md
7.  SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md      → mandatory before touching auth
8.  UI-UX/00-DESIGN-DIRECTION.md                     → before any console work
9.  FRONTEND/00-FRONTEND-CONTEXT.md                  → and before writing any of it
10. ENGINEERING/00-ENGINEERING-CONTEXT.md            → before writing any code at all
11. TASKS/PROGRESS.md                                → what is in scope right now
```

## Principles that bind every document

1. **Every console capability is also in the REST API** (`PLAN/02` FR-14). A console-only shortcut is never acceptable.
2. **Authorization is enforced server-side on every request.** A hidden button is not a security control.
3. **Delegated role assignment is validated as a subset of `granted_role_keys` on every request**, not only at grant creation (`PLAN/08` Part C).
4. **Tenant isolation is the first security property.** Row-level security is the mechanism, not a predicate somebody remembers to write.
5. **Nothing published describes a capability that has not shipped.** The public site's capability audit enforces it in CI; these documents carry a status line for the same reason.
6. **A document cites its evidence.** A claim about behaviour names the code, migration, contract or test that makes it true.

## Working with this repository

`../CLAUDE.md` carries the documentation map for AI agents: which document to open for a given task. Execution lives in [`../TASKS/`](../TASKS/) — one branch per task card, a feature specification in `../MEMORY/specs/` for anything touching authentication, authorization or the data model, and a record in `../MEMORY/records/` before a card is marked done.

**If a question is not answered here, that is a real gap.** Raise it in `../TASKS/BACKLOG.md` rather than deciding it silently.

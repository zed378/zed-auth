# Product Documentation — Centralized Auth Service

Welcome to the technical specification and reference documentation for the **Centralized Auth Service**: an enterprise-grade identity, SSO, access control, and organization management platform.

```
Consumer Application / SDK / SPA
             |
             v (HTTP / OIDC / OAuth 2.1)
  +------------------------------------+
  |     Centralized Auth Service       |
  |  +------------------------------+  |
  |  | OIDC / SAML / OAuth 2.1 Engine |  |
  |  +------------------------------+  |
  |  | Multi-Tenant RBAC & Grants   |  |
  |  +------------------------------+  |
  |  | REST Management API          |  |
  |  +------------------------------+  |
  +-----------------+------------------+
                    |
          +---------+---------+
          v                   v
     PostgreSQL             Redis
  (RLS / Audit / DB)    (Sessions / Cache)
```

## Documentation Architecture

This repository uses a **domain-category modular specification structure** inspired by high-reliability infrastructure specs (such as `zed378/image-management`). Every category contains a `README.md` index and numbered specification documents following strict quality & testability mandates.

## Categories Overview

| Folder | Category | Scope & Mandate |
|---|---|---|
| [`PLAN/`](file:///c:/Users/Zed/Documents/Project/auth/docs/PLAN/README.md) | Product Intent & Scope | Core product requirements, scope boundaries, roadmap, risk register, acceptance criteria. |
| [`ARCHITECTURE/`](file:///c:/Users/Zed/Documents/Project/auth/docs/ARCHITECTURE/README.md) | System Architecture | System topology, service boundaries, Go/Chi backend design, React frontend design, public site design. |
| [`API/`](file:///c:/Users/Zed/Documents/Project/auth/docs/API/README.md) | REST Management API | Public & internal HTTP endpoints, standards, error codes, rate limiting, pagination, idempotency. |
| [`IDENTITY-PROTOCOL/`](file:///c:/Users/Zed/Documents/Project/auth/docs/IDENTITY-PROTOCOL/README.md) | OIDC & OAuth 2.1 Protocol | OpenID Connect, OAuth 2.1 Server, SAML 2.0 Federation, WebAuthn/Passkeys, MFA. |
| [`AUTHORIZATION/`](file:///c:/Users/Zed/Documents/Project/auth/docs/AUTHORIZATION/README.md) | RBAC & Delegation Engine | Multi-tenant RBAC, Project Grants (cross-org delegation), ABAC evaluation, permission engine. |
| [`SESSION-MANAGEMENT/`](file:///c:/Users/Zed/Documents/Project/auth/docs/SESSION-MANAGEMENT/README.md) | Token & Session Lifecycle | JWT issuance, JWKS key rotation, refresh token families, token revocation & blacklisting. |
| [`MULTI-TENANCY/`](file:///c:/Users/Zed/Documents/Project/auth/docs/MULTI-TENANCY/README.md) | Tenant & Org Isolation | Organization & Project scoping, row-level security isolation, custom domain mapping. |
| [`DATABASE/`](file:///c:/Users/Zed/Documents/Project/auth/docs/DATABASE/README.md) | Relational Storage | PostgreSQL schema definitions, RLS policies, indexing, migrations, append-only audit trail storage. |
| [`SECURITY/`](file:///c:/Users/Zed/Documents/Project/auth/docs/SECURITY/README.md) | Threat Model & Controls | Asset & trust boundaries, threat actors, attack surface & scenarios, security baselines, incident playbooks. |
| [`OBSERVABILITY/`](file:///c:/Users/Zed/Documents/Project/auth/docs/OBSERVABILITY/README.md) | Audit & Monitoring | Structured logging, RFC 5424 audit trail, privacy sanitization, Prometheus metrics, tracing. |
| [`PERFORMANCE/`](file:///c:/Users/Zed/Documents/Project/auth/docs/PERFORMANCE/README.md) | SLA & Benchmarking | Latency budgets (<5ms token verification), DB query budgets, load testing strategy with k6. |
| [`DEVOPS/`](file:///c:/Users/Zed/Documents/Project/auth/docs/DEVOPS/README.md) | Infrastructure & CI/CD | Docker/K8s, GitHub Actions CI/CD pipelines, secret management (Vault), backup & disaster recovery. |
| [`TESTING/`](file:///c:/Users/Zed/Documents/Project/auth/docs/TESTING/README.md) | Quality Assurance | Test pyramid, Go unit/integration tests, React component tests, E2E Playwright, security abuse tests. |
| [`SDK/`](file:///c:/Users/Zed/Documents/Project/auth/docs/SDK/README.md) | Client Integration SDKs | Go SDK, TypeScript SDK, React Auth Provider & Hooks, HTTP gateway middleware. |
| [`WEBHOOK/`](file:///c:/Users/Zed/Documents/Project/auth/docs/WEBHOOK/README.md) | Event Delivery System | Outbound webhook events (User registered, Role assigned), HMAC-SHA256 signatures, retry queues. |
| [`UI-UX/`](file:///c:/Users/Zed/Documents/Project/auth/docs/UI-UX/README.md) | Console & Public Site UI | Design system, visual language, component specs, management console pages, public docs site. |
| [`DEVELOPER/`](file:///c:/Users/Zed/Documents/Project/auth/docs/DEVELOPER/README.md) | Developer Experience | Getting started, local development setup, task conventions, API integration guide. |

## Core Architectural Principles

1. **API Parity**: Every capability exposed in the Management Console is backed by a public, versioned REST API endpoint.
2. **Server-Side Enforcement**: Authorization, tenant boundaries, and project grant limits are enforced on every request on the server side — UI component hiding is pure UX.
3. **Cross-Organization Project Grants**: Roles delegated to another organization are strictly validated server-side as a subset of `granted_role_keys`.
4. **Tenant Isolation by Default**: PostgreSQL Row-Level Security (RLS) is enforced at the DB session level.
5. **Zero Token Leaks**: Passwords, tokens, credentials, and raw attribute evaluations are never logged to stdout or tracing systems.

## Recommended Reading Path

1. [`PLAN/00-PROJECT-CONTEXT.md`](file:///c:/Users/Zed/Documents/Project/auth/docs/PLAN/00-PROJECT-CONTEXT.md) — Big picture vision & product scope.
2. [`ARCHITECTURE/00-SYSTEM-ARCHITECTURE.md`](file:///c:/Users/Zed/Documents/Project/auth/docs/ARCHITECTURE/00-SYSTEM-ARCHITECTURE.md) — Core services and system boundaries.
3. [`DATABASE/01-SCHEMA-DEFINITIONS.md`](file:///c:/Users/Zed/Documents/Project/auth/docs/DATABASE/01-SCHEMA-DEFINITIONS.md) — Relational domain data model.
4. [`AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md`](file:///c:/Users/Zed/Documents/Project/auth/docs/AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md) — RBAC, Project Grants, and ABAC engine.
5. [`API/00-API-OVERVIEW.md`](file:///c:/Users/Zed/Documents/Project/auth/docs/API/00-API-OVERVIEW.md) — REST Management API contract.
6. [`SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`](file:///c:/Users/Zed/Documents/Project/auth/docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md) — Mandatory threat & abuse model.

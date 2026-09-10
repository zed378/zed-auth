# 00 — Project Context

## Background

Currently (assumed) every service/application has its own login mechanism and user management. This causes:

- Duplicated auth logic across many places → hard to maintain and prone to security bugs.
- Users having to log in repeatedly across different applications (no SSO).
- No single place to audit "who has access to what."
- Difficulty adding new security features (MFA, passwordless, etc.) consistently, since it would need to change in many services.

## What We're Building

An **Auth Service** — a centralized Identity & Access Management (IAM) platform, following the philosophy of [Zitadel](https://zitadel.com): SSO via OIDC/SAML, a full REST Management API, multi-tenant RBAC (with cross-organization delegation, i.e. Project Grants), optional ABAC for fine-grained policy needs, and a management console (admin UI) on top of it.

## Target Users

| Role | Needs |
|---|---|
| **End user** | Log in once (SSO), reset password, MFA, manage active sessions |
| **Internal developer** | Integrate a new service with Auth Service via OIDC client / REST API |
| **Organization admin** | Manage users, roles, projects, and applications within their organization |
| **Super admin (platform)** | Manage instance, organizations, and global policies |

## Design Principles

- **API-first**: anything possible through the UI must also be possible through the REST API.
- **Standard-compliant**: follow official OIDC/OAuth specifications; don't build custom flows that deviate unless truly necessary.
- **Stateless service, separate stateful store**: the auth service itself is stateless for easy horizontal scaling; state (session, token) is kept in a separate store (DB/cache).
- **Secure by default**: security features (rate limiting, lockout, MFA-readiness) are active from the start, not optional add-ons that are easy to forget.
- **Incremental rollout**: MVP first (single-tenant, basic OIDC), then add multi-tenancy, SAML, ABAC, and enterprise features — see `16-IMPLEMENTATION-ROADMAP.md`.
- **Don't over-engineer ahead of need**: multi-organization delegation (Project Grants) and ABAC are only built once a concrete use case demands them, not speculatively.

## Initial Assumptions (subject to change)

- **Backend language:** Go — I/O-bound workload, high concurrency needs, fast startup, and it's the same stack Zitadel itself uses.
- **Database:** PostgreSQL as the source of truth, with an append-only audit log; event sourcing is not adopted wholesale at the start.
- **SSO protocols:** OIDC/OAuth 2.1 as the primary priority, SAML 2.0 as additional support for enterprise/legacy needs.
- **Deployment:** Container-based (Docker/Kubernetes), stateless service + cache layer (Redis) for sessions.
- **Frontend:** React + TypeScript for the management console.

If these assumptions don't fit your needs, `07-BACKEND-ARCHITECTURE.md` and `06-FRONTEND-ARCHITECTURE.md` are the two documents to revise — most other documents don't depend on a specific programming language or framework.

## Document Map

This plan is split into two top-level folders:

- **`PLAN/`** — product, architecture, and engineering planning (this folder).
- **`UI-UX/`** — design planning for the management console, kept separate since UI/UX work has a different cadence and different owners than backend/architecture work.

Continue to [01 — Product Scope](./01-PRODUCT-SCOPE.md).

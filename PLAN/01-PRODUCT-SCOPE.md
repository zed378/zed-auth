# 01 — Product Scope

## In Scope (Eventually — Phased, see `16-IMPLEMENTATION-ROADMAP.md`)

- Centralized authentication: password login, MFA (TOTP, WebAuthn), passwordless, social login federation.
- SSO across all registered applications via OIDC (primary) and SAML 2.0 (secondary, enterprise-focused).
- Full REST Management API: organizations, projects, applications, users, roles, grants.
- Multi-tenant model: Instance → Organization → Project → Application, supporting many organizations in one deployment.
- RBAC scoped per project, including cross-organization delegation (Project Grants) for B2B scenarios.
- Optional ABAC (policy-based, OPA/Rego) for authorization needs RBAC can't express cleanly.
- A management console (web UI) covering everything exposed by the REST API.
- A public-facing website (landing page, product/about pages, documentation site, changelog) — see `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` and `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`. This is a separate frontend surface from the console (different audience, different tech, different deploy cadence).
- Audit logging, session management, and standard security controls (rate limiting, key rotation, etc.).

## Out of Scope (Not Planned, or Explicitly Deferred)

| Item | Status | Reason |
|---|---|---|
| Full Identity Governance (periodic access certification, automated access review) | Deferred indefinitely | Only relevant at a scale/compliance need this project doesn't have yet |
| Fully white-labeled, per-tenant-branded UI | Deferred | Basic org-level branding (logo, primary color) is enough initially |
| No-code visual policy/workflow builder | Deferred | Plain forms + the ABAC Rego editor are enough; a visual builder is a large project on its own |
| Big-bang migration of all existing auth systems | Explicitly rejected | Migration is gradual, app by app, not a single cutover |
| Full SCIM provisioning | Deferred to Phase 4 (optional) | Only build once an external IdP integration actually needs it |
| Database-per-tenant isolation | Deferred indefinitely | Shared schema + `org_id` + row-level security is sufficient unless a specific enterprise client contractually requires stronger isolation |
| One user belonging to multiple organizations simultaneously (workspace switching) | Deferred | Meaningful added complexity (context-switching UI, per-context tokens); revisit only if a real need appears |

## MVP Definition of Done

The MVP (Phase 1 in `16-IMPLEMENTATION-ROADMAP.md`) is considered complete when:

1. At least two internal applications successfully authenticate users through this Auth Service.
2. A user can log into Application A, then open Application B, and **not** be asked to log in again (SSO working).
3. Organizations, projects, applications, and users can all be created and managed via the REST API **and** the management console.
4. Basic audit logging captures login success/failure and administrative changes.
5. Login is protected by rate limiting.

Full phase-by-phase scope is detailed in `16-IMPLEMENTATION-ROADMAP.md`; detailed UI scope is in `UI-UX/08-PAGE-SPECIFICATIONS.md`.

Continue to [02 — Requirements](./02-REQUIREMENTS.md).

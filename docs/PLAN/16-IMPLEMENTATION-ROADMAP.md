# 16 — Implementation Roadmap

Approach: **incremental**, starting with core needs (login + basic SSO) before adding enterprise features (SAML, complex multi-org, ABAC, etc.). The management console is built in lockstep with these phases, not as a separate track — see `UI-UX/08-PAGE-SPECIFICATIONS.md` for exactly which screens get added in each phase below.

## Phase 0 — Foundation (Preparation)
- [ ] Finalize tech stack (`07-BACKEND-ARCHITECTURE.md`, `06-FRONTEND-ARCHITECTURE.md`)
- [ ] Set up repo, basic CI/CD, `local` & `staging` environments
- [ ] Set up PostgreSQL + initial schema (`04-DATA-MODEL.md`)
- [ ] Set up basic observability (structured logging, health checks) (`13-OBSERVABILITY.md`)
- [ ] Public site: landing page, About page, docs skeleton live (`20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`) — this ships ahead of Phase 1 since it's how early adopters and stakeholders evaluate the project

## Phase 1 — MVP: Core Auth + Basic SSO
- [ ] Basic OIDC provider implementation: `/authorize`, `/token`, `/userinfo`, `/.well-known/openid-configuration`, `/jwks.json`
- [ ] Authorization Code + PKCE flow
- [ ] Login with email/password + Argon2id hashing
- [ ] Session management + SSO across applications (single organization first)
- [ ] Minimal centralized login page (hosted login UI)
- [ ] Basic Management REST API: CRUD for `users`, `applications`, `projects`
- [ ] Basic audit logging (`events` table)
- [ ] Login rate limiting
- [ ] Management console MVP: login via OIDC (dogfooding), Organization overview, Projects, Applications, Users (list/create/deactivate), basic Audit Log (`UI-UX/08-PAGE-SPECIFICATIONS.md`)
- [ ] Public docs: real Quickstart guide reflecting the actual MVP flow, generated API reference for the endpoints that exist (`20-PUBLIC-SITE-ARCHITECTURE.md`)

**Definition of done for Phase 1**: at least 2 internal applications successfully use this Auth Service for login, and a user can SSO between them without logging in again. Full acceptance criteria in `17-ACCEPTANCE-CRITERIA.md`.

## Phase 2 — Full RBAC & Multi-Tenancy
- [ ] Roles & permissions per project (`08-AUTHORIZATION.md` Part A)
- [ ] Role claims embedded in the access token
- [ ] `/v1/authz/check` endpoint for real-time authorization checks
- [ ] Activate multi-organization support (if more than one tenant is needed) (`08-AUTHORIZATION.md` Part B)
- [ ] Per-organization policies (password policy, mandatory MFA, etc.)
- [ ] Console: Roles tab, Authorizations tab, Policies screen, org switcher

## Phase 3 — Advanced Security
- [ ] MFA: TOTP
- [ ] MFA: WebAuthn/Passkey
- [ ] Refresh token rotation & revocation
- [ ] Login anomaly detection (new location/device)
- [ ] "Manage active sessions" page for end users
- [ ] Console: MFA tab, Sessions tab (admin + self-service), personal account settings

## Phase 4 — Enterprise Interoperability
- [ ] SAML 2.0 Identity Provider
- [ ] Social login (Google, Microsoft, GitHub)
- [ ] SCIM provisioning (optional, based on client integration needs)
- [ ] Webhooks for important events
- [ ] Project Grants: cross-organization delegation (`08-AUTHORIZATION.md` Part C)
- [ ] Console: Project Grants tab, Granted Projects list, SAML application type in the Applications tab

### Phase 4b — ABAC (optional, only if RBAC + Project Grants isn't enough)
- [ ] Embed OPA policy engine, extended `/v1/authz/check` with subject/resource/context attributes (`08-AUTHORIZATION.md` Part D)
- [ ] `user_attributes` and `policies` tables, dry-run evaluation before activating a policy

## Phase 5 — Hardening & Production Scale
- [ ] External security audit / pentest (`10-THREAT-MODEL.md`, `11-TESTING.md`)
- [ ] Load testing & scalability tuning against `12-PERFORMANCE.md` targets
- [ ] Disaster recovery drill (`15-DISASTER-RECOVERY.md`)
- [ ] Complete documentation for internal/external developers who will integrate
- [ ] Console: audit log filtering/export polish, accessibility audit (`UI-UX/13-ACCESSIBILITY.md`), UI-side load/perf pass
- [ ] Console: Policies (ABAC) screen — Rego editor, dry-run simulation, activation/rollback (only if Phase 4b was undertaken)

## Short Priority Sequence

```
Phase 0 → Phase 1 (MVP) → Phase 2 (RBAC) → Phase 3 (Advanced Security) → Phase 4 (Enterprise) → Phase 4b (ABAC, optional) → Phase 5 (Hardening)
```

> Note: Phase 3 and Phase 4 can be swapped depending on business needs — if an enterprise client needs SAML before advanced MFA, the order can be adjusted.

Continue to [17 — Acceptance Criteria](./17-ACCEPTANCE-CRITERIA.md).

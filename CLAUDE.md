# CLAUDE.md

Guidance for Claude Code when working in this repository. This project builds a centralized **Auth Service** (SSO + REST API + multi-tenant RBAC/ABAC) plus its management console and public site, following the plan in `PLAN/`, `UI-UX/`, and `SECURITY/`.

**Read this file in full before writing any code.** It is shorter than the plan itself and tells you which plan document to open for any given task — don't skip straight to coding from assumptions.

## What This Repository Is

An identity and access management platform (Zitadel-style): centralized authentication (OIDC/OAuth 2.1, SAML later), a full REST Management API, multi-tenant RBAC with cross-organization delegation (Project Grants), optional ABAC, a management console, and a public marketing/docs site. Nothing has been implemented yet unless a `backend/`, `console/`, or `public-site/` directory already exists with code in it — check before assuming.

## Documentation Map — Read Before You Build

Every technical decision in this project has already been made and written down. **Do not re-derive architecture, data models, or API shapes from scratch — find the answer in the plan first.**

| If you're working on... | Read first |
|---|---|
| Anything at all, first time in this repo | `PLAN/00-PROJECT-CONTEXT.md`, `PLAN/01-PRODUCT-SCOPE.md` |
| Overall system design | `PLAN/03-ARCHITECTURE.md` |
| Database schema / migrations | `PLAN/04-DATA-MODEL.md` |
| Any API endpoint (new or modified) | `PLAN/05-API-CONTRACT.md` |
| The management console (frontend) | `PLAN/06-FRONTEND-ARCHITECTURE.md` + the entire `UI-UX/` folder, starting at `UI-UX/00-DESIGN-DIRECTION.md` |
| The public site (landing/docs/about) | `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` + `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` + `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| Backend service internals | `PLAN/07-BACKEND-ARCHITECTURE.md` |
| Roles, permissions, RBAC, Project Grants, ABAC | `PLAN/08-AUTHORIZATION.md` (this is the single source of truth for all authorization logic — RBAC, delegation, and ABAC are documented together because they're layered, not separate systems) |
| Anything security-related | `PLAN/09-SECURITY.md` for baseline controls, then the full `SECURITY/` folder starting at `SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md` for the detailed threat model |
| Writing or reviewing tests | `PLAN/11-TESTING.md` |
| Anything touching latency/scale | `PLAN/12-PERFORMANCE.md` |
| Deployment, environments, CI/CD | `PLAN/14-DEPLOYMENT.md` |
| What phase we're in / what's in scope right now | `PLAN/16-IMPLEMENTATION-ROADMAP.md` |
| Whether a feature is "done" | `PLAN/17-ACCEPTANCE-CRITERIA.md` and, for console UI work, `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md` |

If a question isn't answered in these documents, that's a real gap — flag it to the user rather than guessing and silently deciding.

## Mandatory Workflow for Any New Feature

Before writing implementation code for a non-trivial feature, work through **`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`** — business objective, actors, functional/non-functional requirements, dependencies, DB changes, API contract, frontend/backend changes, authorization rules, validation, error handling, edge cases, **abuse cases** (cross-reference `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`), logging/audit requirements, security controls, testing strategy, acceptance criteria, implementation sequence, rollback strategy, and technical risks.

For small, clearly-scoped changes (a bug fix, a copy tweak, a config change), this full template is overkill — use judgment, but err toward writing it out for anything that touches authentication, authorization, or data model changes, since those are exactly the categories where skipped analysis becomes a security incident later.

## Non-Negotiable Constraints

These come directly from the plan and must not be silently violated for convenience:

- **API-first**: every capability exposed in the console must also be exposed via the REST API (`PLAN/02-REQUIREMENTS.md` FR-14). Never build a console-only shortcut that bypasses the documented API contract.
- **Authorization checks are server-side, always.** The console may hide UI elements based on role claims for UX purposes, but the API must independently enforce every permission check — a hidden button is not a security control (`PLAN/08-AUTHORIZATION.md`, `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2/§3).
- **Project Grant role assignment must be validated server-side as a subset of `granted_role_keys`** on every single request, not just at grant-creation time (`PLAN/08-AUTHORIZATION.md` Part C, `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`'s worked example).
- **Never build a Phase N+1 feature while Phase N is incomplete**, per `PLAN/16-IMPLEMENTATION-ROADMAP.md`'s incremental approach — check the roadmap before starting work on multi-org delegation, ABAC, or SAML if the earlier phases aren't done.
- **Never hand-write API reference documentation.** The public site's `/docs/api-reference` is generated from the OpenAPI spec (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`) — if you change an endpoint, update the OpenAPI spec, and the docs follow automatically.
- **Never log tokens, passwords, or raw resource attributes** sent to `/v1/authz/check` (`PLAN/13-OBSERVABILITY.md`, `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` Information Disclosure).
- **Never mark a security-sensitive feature (auth, authz, sessions, grants) as done without a corresponding abuse-case test** from `PLAN/11-TESTING.md` and, where relevant, `SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`.
- **Marketing/docs copy never describes a capability that isn't actually shipped** in the current roadmap phase (`UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`'s governance rule).
- **`color-danger` in the console design system is reserved exclusively for destructive/irreversible actions** — don't reuse it for anything else, since its meaning must stay reliable (`UI-UX/06-VISUAL-LANGUAGE.md`).

## Assumed Tech Stack (Per the Plan — Confirm Against `PLAN/07` and `PLAN/06` Before Deviating)

| Layer | Choice |
|---|---|
| Backend language | Go |
| Backend HTTP | `chi` or `net/http` + middleware |
| OAuth2/OIDC | `ory/fosite` or `zitadel/oidc` |
| Policy engine | OPA (embedded, Go library) for ABAC |
| Database | PostgreSQL |
| Cache/session | Redis |
| Console frontend | React + TypeScript, Tailwind-based design tokens, TanStack Query |
| Public site | Static-generated marketing pages + Docusaurus-style docs, deployed separately from the console |
| Containerization | Docker, Kubernetes for production |

## Working Conventions

- **Directory layout** (propose this if the repo is empty; keep consistent if it already exists):
  ```
  backend/         → Go service (auth, authz, management API, OIDC provider)
  console/         → React management console
  public-site/     → Landing, docs, about, changelog
  PLAN/            → Engineering plan (read-only reference, don't edit as part of feature work)
  UI-UX/           → Design plan (read-only reference)
  SECURITY/        → Threat model & security plan (read-only reference)
  ```
- **Migrations**: additive/backward-compatible by default (`PLAN/14-DEPLOYMENT.md` expand/contract pattern) — never a migration that breaks a running previous-version instance mid-rollout.
- **Commits/PRs**: keep scoped to one feature-spec unit where possible; reference the relevant `PLAN/`/`UI-UX/`/`SECURITY/` document(s) in the PR description so reviewers can check the implementation against the actual spec, not just against the diff.
- **When the plan and a practical constraint conflict** (e.g. a chosen library doesn't support something `PLAN/07-BACKEND-ARCHITECTURE.md` assumed), stop and surface the conflict rather than quietly picking a different approach — the plan documents are supposed to stay the source of truth, so a deviation should be a visible decision, not a silent one.

## Testing Expectations

Follow the pyramid in `PLAN/11-TESTING.md`: unit tests for pure logic (hashing, JWT, RBAC/ABAC decisions), integration tests against real Postgres/Redis (testcontainers), E2E tests for full login/console flows (Playwright), and dedicated security tests for every abuse scenario listed against the feature (`PLAN/10-THREAT-MODEL.md`, `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`). Run the full test suite before considering any feature-spec item complete.

## When You're Unsure

Prefer asking the user or leaving a clearly marked `TODO`/open question over inventing an architectural decision that contradicts or extends the plan. The plan is large and detailed specifically so implementation doesn't require guessing — if you find yourself guessing, that's a sign to re-check `PLAN/`, `UI-UX/`, or `SECURITY/` more carefully first.

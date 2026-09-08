# AGENTS.md

Instructions for any AI coding agent (Claude Code, Codex, or otherwise) working in this repository. This file follows the [agents.md](https://agents.md) convention and should stay consistent with `CLAUDE.md` — if you update one, update the other.

## Project Summary

This repository implements a centralized **Auth Service**: SSO (OIDC/OAuth 2.1, SAML later), a full REST Management API, multi-tenant RBAC with cross-organization delegation, optional ABAC, a management console, and a public marketing/docs site. The complete engineering, design, and security plan already exists in three folders at the repo root — **`PLAN/`**, **`UI-UX/`**, **`SECURITY/`** — and is the authoritative source for architecture, data model, API shape, authorization rules, and UI behavior. Do not re-derive these from scratch; look them up.

## Setup Commands

> Fill in once the actual toolchain is initialized (`PLAN/07-BACKEND-ARCHITECTURE.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md`). Placeholder conventions below — update this section as soon as the repo has real build tooling, and keep it accurate; a stale setup section is worse than none.

```bash
# Backend (Go)
cd backend && go mod download

# Console (React + TypeScript)
cd console && npm install

# Public site
cd public-site && npm install
```

## Build & Test Commands

```bash
# Backend
cd backend && go build ./...
cd backend && go test ./...
cd backend && gosec ./...          # SAST, per PLAN/09-SECURITY.md / PLAN/11-TESTING.md

# Console
cd console && npm run build
cd console && npm run test         # component tests
cd console && npm run test:e2e     # Playwright, per PLAN/11-TESTING.md / PLAN/06-FRONTEND-ARCHITECTURE.md

# Public site
cd public-site && npm run build
```

Run the relevant test command(s) before considering any change complete. A change touching authentication, authorization, or the data model requires the full backend test suite, not just the package you edited — these areas have cross-cutting invariants (`PLAN/08-AUTHORIZATION.md`) that a narrow test run can miss.

## Where to Find the Spec for Any Task

| Task | Primary document(s) |
|---|---|
| First time in the repo | `PLAN/00-PROJECT-CONTEXT.md`, `PLAN/01-PRODUCT-SCOPE.md` |
| System architecture | `PLAN/03-ARCHITECTURE.md` |
| Database/migrations | `PLAN/04-DATA-MODEL.md` |
| API endpoints | `PLAN/05-API-CONTRACT.md` |
| Console (frontend) | `PLAN/06-FRONTEND-ARCHITECTURE.md`, `UI-UX/00` through `UI-UX/19` |
| Public site (landing/docs/about) | `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| Backend internals | `PLAN/07-BACKEND-ARCHITECTURE.md` |
| Roles/RBAC/Project Grants/ABAC | `PLAN/08-AUTHORIZATION.md` |
| Security controls & threat model | `PLAN/09-SECURITY.md`, then `SECURITY/00` through `SECURITY/05` |
| Testing | `PLAN/11-TESTING.md` |
| Performance targets | `PLAN/12-PERFORMANCE.md` |
| Deployment/CI/CD | `PLAN/14-DEPLOYMENT.md` |
| Current phase / what's actually in scope | `PLAN/16-IMPLEMENTATION-ROADMAP.md` |
| Definition of done | `PLAN/17-ACCEPTANCE-CRITERIA.md`, `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md` |

## Feature Workflow

For any non-trivial feature, produce (or follow, if one already exists) a spec using **`PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`** — covering business objective, actors, requirements, dependencies, DB/API/frontend/backend changes, authorization rules, validation, error handling, edge cases, **abuse cases** (cross-check `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`), audit logging, security controls, tests, acceptance criteria, implementation sequence, rollback strategy, and risks. Skip the full template only for trivial, clearly-scoped changes (typo fixes, config tweaks) — anything touching auth, authorization, or the data model should go through it.

## Code Style

- **Go**: idiomatic Go, standard project layout, no framework "magic" in security-critical paths (`PLAN/07-BACKEND-ARCHITECTURE.md`'s stated preference for `chi`/`net/http` over heavier frameworks). Parameterized queries only — never string-concatenated SQL (`SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §8).
- **TypeScript/React**: functional components, TanStack Query for server state rather than a global store for cached API data (`PLAN/06-FRONTEND-ARCHITECTURE.md`), Tailwind utility classes mapped to the design tokens in `UI-UX/05-DESIGN-SYSTEM.md` — no ad hoc colors/spacing outside the token set.
- **Commits**: scoped to one logical change; reference the relevant plan document(s) in the message/PR body.

## Hard Rules (Do Not Violate)

1. Every capability in the console must also exist in the REST API (`PLAN/02-REQUIREMENTS.md` FR-14) — no console-only shortcuts.
2. Authorization is enforced server-side on every request, independent of any UI-level hiding (`PLAN/08-AUTHORIZATION.md`, `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2/§3).
3. Project Grant role assignments are validated server-side as a subset of `granted_role_keys` on every request, not just at creation (`PLAN/08-AUTHORIZATION.md` Part C).
4. Do not implement a later roadmap phase's feature while an earlier phase is incomplete (`PLAN/16-IMPLEMENTATION-ROADMAP.md`).
5. The public API reference is generated from the OpenAPI spec, never hand-maintained separately (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`).
6. Never log tokens, passwords, or raw `/v1/authz/check` resource attributes (`PLAN/13-OBSERVABILITY.md`).
7. Every security-sensitive feature ships with a corresponding abuse-case test (`PLAN/11-TESTING.md`, `SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`).
8. Marketing/docs copy never claims a capability beyond what's actually shipped in the current phase (`UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`).
9. `PLAN/`, `UI-UX/`, and `SECURITY/` are reference documentation, not implementation output — don't edit them as a side effect of feature work; if the plan itself needs to change, that's a deliberate, separate action the user should be aware of.

## PR / Change Instructions

- Link the PR description to the specific `PLAN/`/`UI-UX/`/`SECURITY/` sections the change implements, so reviewers can check against spec rather than only against the diff.
- Include which tests were added/run, referencing `PLAN/11-TESTING.md`'s pyramid layer(s).
- If the change deviates from the plan (a library limitation, a discovered ambiguity), state the deviation explicitly in the PR rather than silently diverging — the plan should stay accurate, or the deviation should be flagged for the plan to be updated.

## When Uncertain

Ask rather than assume. The plan is detailed specifically so implementation doesn't require guessing; if an answer isn't in `PLAN/`, `UI-UX/`, or `SECURITY/`, that's a genuine gap to raise, not a decision to make silently.

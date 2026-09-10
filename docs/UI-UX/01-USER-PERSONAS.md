# 01 — User Personas

These personas correspond directly to the roles defined in `PLAN/08-AUTHORIZATION.md` — every persona here maps to a real `manager_role` or end-user state, not an invented archetype.

## Persona 1: Instance Owner ("Dian")

- **Role in system**: `INSTANCE_OWNER`.
- **Context**: Platform/infra engineer responsible for the Auth Service deployment itself, not any single business team.
- **Goals**: Onboard new organizations, set instance-wide defaults, monitor overall health and audit activity across all orgs.
- **Frustrations to design against**: Being forced to click into individual organizations just to get a platform-wide view; unclear which settings are instance-level defaults vs. org-level overrides.
- **Primary screens**: Organization list, instance-wide policies, instance audit log (`UI-UX/08-PAGE-SPECIFICATIONS.md`).

## Persona 2: Organization Admin ("Budi")

- **Role in system**: `ORG_OWNER` / `ORG_ADMIN`.
- **Context**: IT/ops lead for a single business unit or client organization. Manages their own team's access day-to-day.
- **Goals**: Invite new employees and get them productive fast (correct role, correct project, minimal steps); periodically review who has access to what; respond quickly when someone leaves the team.
- **Frustrations to design against**: A multi-step invite flow that requires remembering to visit three separate screens; unclear whether a role a user has was assigned directly or inherited via a delegation.
- **Primary screens**: Users list/detail, Projects, Authorizations, Policies (`UI-UX/08-PAGE-SPECIFICATIONS.md`), guided invite flow (`UI-UX/04-USER-FLOWS.md`).

## Persona 3: Project Owner ("Sari")

- **Role in system**: `PROJECT_OWNER`.
- **Context**: Tech lead for a specific application/product (e.g. the POS system), cares about roles and applications within their project, not the whole organization.
- **Goals**: Register new client applications (web/mobile) under their project, define and adjust roles as the application's feature set evolves, occasionally delegate the project to a partner organization.
- **Frustrations to design against**: Losing track of which applications share which roles; accidentally exposing a sensitive role to a delegated organization.
- **Primary screens**: Project detail (Applications, Roles, Authorizations, Project Grants tabs) (`UI-UX/08-PAGE-SPECIFICATIONS.md`).

## Persona 4: Vendor/Partner Admin ("Reza") — Receiving a Project Grant

- **Role in system**: `PROJECT_GRANT_OWNER`.
- **Context**: Admin at an external organization that has received delegated access to part of a client's project.
- **Goals**: Self-manage which of their own team members get the (limited) roles they've been granted, without needing to contact the owning organization for routine staffing changes.
- **Frustrations to design against**: Confusion about which roles they're actually allowed to assign (vs. roles that exist in the project but aren't delegated to them); a UI that doesn't make clear this access can be revoked at any time by the owning org.
- **Primary screens**: Granted Projects list, restricted user-grant assignment (`UI-UX/08-PAGE-SPECIFICATIONS.md`).

## Persona 5: End User ("Ayu") — Self-Service

- **Role in system**: No manager role; an ordinary authenticated user.
- **Context**: An employee or customer who only interacts with the console (if at all) through their own personal account settings, reached from any consumer application via SSO.
- **Goals**: Set up MFA once and not think about it again; occasionally check or revoke a session (e.g. after losing a phone); reset a forgotten password.
- **Frustrations to design against**: Being shown admin-oriented UI or terminology that doesn't apply to them; a session-revocation action that isn't clearly effective immediately.
- **Primary screens**: Personal account settings (own MFA, own sessions) (`UI-UX/08-PAGE-SPECIFICATIONS.md`).

## Persona 6: Policy Author ("Fajar") — ABAC (Phase 4b, optional)

- **Role in system**: `ORG_ADMIN` or `PROJECT_OWNER` with policy-authoring responsibility.
- **Context**: A technically-inclined admin (not necessarily a developer) who needs to express a conditional access rule RBAC can't cleanly express (e.g., approval-by-department-and-amount-limit, `PLAN/08-AUTHORIZATION.md` Part D).
- **Goals**: Write and test a policy safely before it affects real users; understand quickly why a policy did or didn't grant access when something goes wrong.
- **Frustrations to design against**: A policy editor with no safety net (no dry-run) that risks locking out real users; opaque allow/deny results with no explanation.
- **Primary screens**: Policies (ABAC) editor with dry-run simulation (`UI-UX/08-PAGE-SPECIFICATIONS.md`).

## Persona 7: Prospective Evaluator ("Nadia") — Public Site Visitor

- **Role in system**: none yet — an anonymous visitor to the public site (`UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`), not an authenticated console user.
- **Context**: a developer or technical decision-maker researching whether this Auth Service fits their organization's needs, arriving via search, a colleague's link, or direct navigation.
- **Goals**: quickly understand what the product does and whether it fits their use case (SSO? multi-tenant? self-hosted?); find concrete technical documentation to validate feasibility before committing time to a deeper evaluation; find a clear next step (quickstart, contact, sign up).
- **Frustrations to design against**: marketing copy so vague it doesn't actually answer "does this do X"; documentation that's out of sync with the real API; no clear path from "interested" to "trying it."
- **Primary screens**: Landing page, Docs (Quickstart, Concepts), About page (`UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`).

Continue to [02 — User Journeys](./02-USER-JOURNEYS.md).

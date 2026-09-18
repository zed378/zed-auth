# 00 - Authorization Architecture

> Category: **Authorization** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P2-03, P2-05, P2-06, P2-07, P4-01, P4-02, P4-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the two authorization systems this service runs, why they are separate, and where each decision is made. The design intent is `docs/PLAN/08-AUTHORIZATION.md`; this document describes the code.

## Scope

Covers the shape of the system and where each question is answered. The details live in the sibling documents: the RBAC model (`01`), delegation (`02`), ABAC (`03`, not built) and the decision path of `/v1/authz/check` (`04`).

## As Built

Two distinct systems, deliberately not sharing a table or a code path:

1. **Manager roles — who may administer this service.**
   `manager_roles` rows (`INSTANCE_OWNER`, `ORG_OWNER`, `ORG_ADMIN`, `PROJECT_OWNER`, `PROJECT_GRANT_OWNER`) are read on every `/v1` request by `backend/internal/management/store.go` and evaluated by `Authorize` in `backend/internal/management/roles.go` against a per-route requirement from `backend/internal/management/policy.go`.

2. **Application roles — who may do what inside a consumer's product.**
   `user_grants` rows carry role keys per user per project; `roles` rows carry the permission keys each role stands for. Consumers read them from token claims (`backend/internal/grant/claims.go`) or ask `/v1/authz/check` (`backend/internal/authz/`).

Conflating the two would let an administrator of one application administer the platform, which is why they share nothing but the user id.

**Every request is authorized twice, in different senses.** `Middleware.Require` decides whether the caller may reach the route at all; the handler then runs inside a tenant transaction where row-level security bounds what the query can see (`docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`). Neither substitutes for the other: RLS cannot express "this role may not create applications", and a role check cannot stop a query from reading another tenant's row.

**Visibility is not authority.** `project_grants` is visible to both parties by policy, so every handler filters on the side it acts for — `granting_org_id` for the granting routes, `granted_org_id` for the receiving ones. A mutation test in `P4-01` showed this filter was the only thing stopping a receiving organization revoking a grant it could see (`MEMORY/records/2026-09-15-P4-01-project-grants.md`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| A route with no entry in the policy table | Unreachable (zero `Requirement` satisfies nobody) | `backend/internal/management/policy.go`, `TestEveryV1RouteHasADeclaredPermission` |
| Refusal when the caller holds nothing over the target | `404`, not `403` | `Decision.Invisible`, `backend/internal/management/roles.go` |
| Refusal when the caller holds something but not enough | `403` | same |
| Instance-wide access | `INSTANCE_OWNER` only, and audited as `instance.scoped_access` | `backend/internal/storage/postgres`, `backend/internal/audit/audit.go` |
| Authorization cache TTL | 30 seconds (`DefaultTTL`), with proactive invalidation on change | `backend/internal/authz/cache.go` |
| A user with no grant | No access at all; there is no default role | absence of any code that writes one; `TestAUserWithNoGrantHasNoRoles` |

## Interfaces

- Route requirements: `backend/internal/management/policy.go` (see `05-...` in this category for the rendered table — the file itself is the source of truth).
- Live decisions: `POST /v1/authz/check`, requirement `Member` at organization scope.
- Role and grant administration: the APIs described in `docs/API/12-ROLE-AND-PERMISSION-API.md` and `docs/API/13-PROJECT-GRANT-API.md`.

## Security Considerations

- **Vertical escalation** (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §3): an `ORG_ADMIN` administers every user in the organization and is one of them, so no scope can express "not yourself". Self-granting is refused explicitly in `backend/internal/grant/handler.go` and `backend/internal/projectgrant/delegated.go`.
- **Cross-tenant reach**: manager-role scope ids are compared against the target, never merely matched by role name.
- **Enumeration**: the 403/404 split is decided by what the caller holds over the target, so a refusal cannot be used to test whether an id exists.

## Verification

- `backend/internal/management/hierarchy_exhaustive_test.go` — 23,520 combinations of held role, scope id, required role, required scope and target, with the expectation written from the plan rather than from the implementation. It found a live defect when `PROJECT_GRANT_OWNER` was switched on (`MEMORY/records/2026-09-17-P4-03-project-grant-owner.md`).
- `backend/tests/security/` — tenant isolation and RLS plan tests, plus the coverage map that keeps abuse-case tests named.
- Per-resource integration tests under `backend/internal/*/`.

## Not Yet Built / Open Questions

- Delegated roles do not appear in tokens or `/v1/authz/check` yet, and no reader joins `project_grants` (`P4-04`; threat review T4-2).
- ABAC is Phase 4b and conditional (`03-ATTRIBUTE-BASED-ACCESS-CONTROL.md`).
- Only `PROJECT_GRANT_OWNER` has a write API; the other four manager roles are still assigned by SQL (`TASKS/BACKLOG.md` PG-31).

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md` — the design intent.
- `01-MULTI-TENANT-RBAC.md`, `02-PROJECT-GRANTS-DELEGATION.md`, `04-PERMISSION-EVALUATION-ENGINE.md`.
- `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`.
- `MEMORY/DECISIONS.md` ADR-025; `TASKS/BACKLOG.md` PG-31, PG-32, PG-44.

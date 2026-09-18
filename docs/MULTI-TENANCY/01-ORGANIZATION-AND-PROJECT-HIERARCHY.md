# 01 - Organization and Project Hierarchy

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-07, P1-16, P1-17, P1-18, P2-05, P4-01, P4-02, P4-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the entity hierarchy (instance, organization, project, application, role), the manager-role hierarchy that administers it, and the cross-organization delegation model (Project Grants) that is the one deliberate exception to "an organization's resources belong only to that organization."

## Scope

Column/constraint/index detail for every table named here is in `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`; RLS policies and triggers are in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`. This document is about the *relationships* between entities and *who may administer what*, not the SQL. Design intent is `docs/PLAN/08-AUTHORIZATION.md` Parts A–C.

## As Built

### Entity hierarchy

```
instances (1 today)
  └── organizations (the tenant boundary)
        ├── users (org-scoped; email unique per organization, not globally)
        ├── projects
        │     ├── applications (OIDC/SAML clients; org_id denormalized for RLS)
        │     └── roles (project-scoped: "admin" in Project A ≠ "admin" in Project B)
        └── user_grants (user × project × role_keys; direct, or delegated via a project_grant)
```

`manager_roles` is layered on top, independent of this tree's parent/child edges — it names who **administers** an instance, organization, project, or (Phase 4) project grant, as opposed to who holds application-level permissions inside a consumer product.

### The manager-role hierarchy

Five roles exist in the schema (`backend/internal/management/roles.go`); a sixth, `MEMBER`, is a computed requirement ("holds a valid token for this organization") rather than a `manager_roles` row:

| Role | Scope | Administers |
|---|---|---|
| `INSTANCE_OWNER` | the instance | every organization |
| `ORG_OWNER` | one organization | that organization entirely, including deleting it or changing its owner |
| `ORG_ADMIN` | one organization | that organization, **except** deleting it or changing its owner |
| `PROJECT_OWNER` | one project | that project's roles, applications, and (Phase 4) grants |
| `PROJECT_GRANT_OWNER` | one Project Grant | the delegated roles reachable through that one grant, in the organization it was granted to |

`Role.Satisfies` (`backend/internal/management/roles.go`) encodes what each role also counts as:

```go
var satisfies = map[Role][]Role{
    InstanceOwner:     {InstanceOwner, OrgOwner, OrgAdmin, ProjectOwner, ProjectGrantOwner, Member},
    OrgOwner:          {OrgOwner, OrgAdmin, ProjectOwner, ProjectGrantOwner, Member},
    OrgAdmin:          {OrgAdmin, ProjectOwner, ProjectGrantOwner, Member},
    ProjectOwner:      {ProjectOwner, Member},
    ProjectGrantOwner: {ProjectGrantOwner, Member},
    Member:            {Member},
}
```

Two points in this table are corrections to a literal reading of `docs/PLAN/08` Part C's hierarchy diagram, each recorded as a backlog entry rather than applied silently:

- **`ORG_ADMIN` satisfies `PROJECT_OWNER` within its own organization.** The plan's diagram draws them as siblings under `ORG_OWNER` with "permissions flow downward only," which read strictly would mean an `ORG_ADMIN` cannot manage a project's roles — contradicting the diagram's own label for `ORG_ADMIN` ("org access, except deleting org/changing owner") and making every project/application endpoint shipped in Phase 1 wrong since the day it was written. `PG-32` (`TASKS/BACKLOG.md`) records this; `roles.go`'s own comment states the resolution: "an organization-scoped role covers every project in its organization... downward-only still holds in the direction that matters: `PROJECT_OWNER` gains nothing organization-wide."
- **`PROJECT_OWNER` does *not* satisfy `PROJECT_GRANT_OWNER`.** The plan draws `PROJECT_GRANT_OWNER` beneath `PROJECT_OWNER`, which — read literally across a delegation, where the project owner belongs to the *granting* organization and the delegated roles are administered inside the *receiving* one — would let a vendor's project owner administer a partner's own staff without the partner's consent (a confused deputy; threat review T4-4). `PG-44` (`TASKS/BACKLOG.md`) records this; the resolution (`P4-03`) is that **scope decides across organizations**: a `PROJECT_GRANT_OWNER` requirement is satisfied by a `PROJECT_GRANT_OWNER` row scoped to that grant, or by the *receiving* organization's `ORG_ADMIN`/`ORG_OWNER`/`INSTANCE_OWNER` — never by the granting organization's `PROJECT_OWNER`. The full case table is in `MEMORY/specs/P4-03-project-grant-owner.md` §7 and exercised by `internal/management/hierarchy_exhaustive_test.go`.

A representative slice of the endpoint-to-role-and-scope mapping (`backend/internal/management/policy.go`), showing the pattern:

| Method & path | Role required | Scope |
|---|---|---|
| `GET /v1/organizations` | `InstanceOwner` | `ScopeInstance` |
| `PATCH /v1/organizations/{org_id}` | `OrgOwner` | `ScopeOrganization` |
| `POST /v1/organizations/{org_id}/projects` | `OrgAdmin` | `ScopeOrganization` |
| `POST /v1/organizations/{org_id}/projects/{project_id}/roles` | `ProjectOwner` | `ScopeProject` |
| `POST /v1/organizations/{org_id}/projects/{project_id}/grants` | `ProjectOwner` | `ScopeProject` |
| `POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `ProjectGrantOwner` | `ScopeProjectGrant` |
| `POST /v1/organizations/{org_id}/project-grants/{grant_id}/owners` | `OrgAdmin` | `ScopeOrganization` (the **receiving** organization) |

**No API grants or revokes `INSTANCE_OWNER`, `ORG_OWNER` or `ORG_ADMIN`** (`TASKS/BACKLOG.md` PG-31). Every such row in `manager_roles` today exists only through a direct `INSERT` — a bootstrap or seed script. `PROJECT_GRANT_OWNER` is the sole exception: `POST/DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/owners` (`backend/internal/projectgrant/owners.go`) is the first, and so far only, manager-role write API (`P4-03`).

### Cross-organization delegation: Project Grants

A **Project Grant** (`project_grants` table) lets a project's owning organization (the *granting* organization) delegate a subset of that project's roles to another organization's own administrators (the *receiving* organization), who then assign those roles to their own users without the granting organization ever touching the receiving organization's people. This is the "Procurement Portal" scenario in `docs/PLAN/08` Part C: a vendor's project, used by several customer organizations' own staff.

**What is built (Phase 4, `P4-01`–`P4-03`):**

- **Grant lifecycle** (`P4-01`): create, list, read, revoke. A grant can only ever narrow — its project, both organizations, and `granted_role_keys` are immutable after creation, and a revoked grant can never be reactivated (`project_grants_only_narrow` trigger, enforced against every writer including the schema owner). There is deliberately no `UPDATE` endpoint; narrowing is "revoke and re-grant."
- **Delegated user grants** (`P4-02`): `POST/GET/PATCH/DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants[/{user_id}]` — the receiving organization assigns, replaces, lists and removes delegated roles for its own users. Every write is validated, in the same transaction and under a row lock that serializes against revocation, against: the grant is visible, `active`, granted to the path organization, for the path's project, and every requested role key is inside `granted_role_keys`; the target user belongs to the receiving organization. See `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` for the exact trigger.
- **`PROJECT_GRANT_OWNER` enforcement** (`P4-03`): `POST/DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/owners[/{user_id}]`, `OrgAdmin`-gated on the **receiving** organization only — the granting organization appoints nobody.
- **Holder-count visibility for the granting side** (`P4-05`): `project_grant_holder_counts()` gives the granting organization a number ("this will remove access for N users") without ever exposing the receiving organization's user identities.

**What is explicitly not built yet (`P4-04`):**

- **Delegated roles do not yet reach tokens or `/v1/authz/check`.** `grant.TokenClaims.ForToken` and the authz decision path read `user_grants` under one tenant's RLS, which hides every delegated row both from the granting tenant (`org_id` is the receiving organization) and from the receiving tenant's own resource servers (the project is not theirs). A delegated grant, once assigned, confers no runtime access yet — this is stated explicitly in `MEMORY/specs/P4-02-delegated-user-grants.md` §0 as the scope boundary of what `P4-02` shipped.
- **Revocation does not yet propagate to active tokens.** Because the reader join above does not exist, there is nothing yet to invalidate.
- **The receiving organization has no dedicated console screen** for its granted projects (`P4-06`, not built).
- **Cross-organization policy reconciliation** (ADR-025: stricter of both organizations' MFA/session/sign-in-method policies) is a decided rule with no enforcement code — it depends on `P4-04`'s issuing-path changes.

### Cross-organization data agreement, enforced by triggers

Four tables carry both an `org_id` (or `granting_org_id`) and a `project_id`: `roles`, `user_grants`, `applications`, `project_grants`. A shared trigger function, `org_must_match_project()`, refuses any row whose stated organization does not actually own the referenced project — except a delegated `user_grants` row, which is checked by the stricter delegation trigger instead. Full detail, including the incident that motivated generalizing this check (`applications` had no such trigger from Phase 0 until `P2-08`), is in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Email uniqueness | Per organization, not global | `users_org_email_key` |
| Role key uniqueness | Per project, not global | `roles_project_key_key` |
| `ORG_ADMIN` reaches project administration | Only within its own organization | `backend/internal/management/roles.go` (`satisfies`), `PG-32` |
| `PROJECT_OWNER` reaches `PROJECT_GRANT_OWNER` | Never — scope decides across organizations instead | `PG-44`, `P4-03`, ADR from `MEMORY/specs/P4-03-project-grant-owner.md` |
| A Project Grant can only narrow | Immutable project/parties/role-keys; no reactivation after revoke | `project_grants_only_narrow` trigger |
| Delegated role assignment validated | On every write, against the grant's current `granted_role_keys`, active status, and receiving organization | `user_grants_delegation_closed` trigger (`P4-02`) |
| Manager-role write API coverage | `PROJECT_GRANT_OWNER` only; `INSTANCE_OWNER`/`ORG_OWNER`/`ORG_ADMIN` have none | `TASKS/BACKLOG.md` PG-31 |

## Interfaces

| Method & path | operationId | Role / Scope |
|---|---|---|
| `GET/POST /v1/organizations/{org_id}/projects/{project_id}/grants` | (list/create project grant) | `ProjectOwner` / `ScopeProject` |
| `GET /v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}` | `getProjectGrant` | `ProjectOwner` / `ScopeProject` |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}` | `revokeProjectGrant` | `ProjectOwner` / `ScopeProject` |
| `GET /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `listDelegatedUserGrants` | `ProjectGrantOwner` / `ScopeProjectGrant` |
| `POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `assignDelegatedRoles` | `ProjectGrantOwner` / `ScopeProjectGrant` |
| `PATCH /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}` | `replaceDelegatedRoles` | `ProjectGrantOwner` / `ScopeProjectGrant` |
| `DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}` | `removeDelegatedRoles` | `ProjectGrantOwner` / `ScopeProjectGrant` |
| `GET/POST/DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/owners[/{user_id}]` | `listProjectGrantOwners`, `assignProjectGrantOwner`, `removeProjectGrantOwner` | `OrgAdmin` / `ScopeOrganization` (receiving org) |

(Every path above is nested under `/v1/organizations/{org_id}/...` — there is no standalone `/v1/project-grants` path; see `openapi/openapi.yaml`.)

## Security Considerations

- **Confused deputy across organizations** (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`, threat review T4-4) is the abuse case the `PROJECT_OWNER` / `PROJECT_GRANT_OWNER` scope separation and the receiving-organization-only owner-appointment rule both close.
- **Widening a delegation after the fact** (`docs/PLAN/09` § Delegation abuse, R-04) is closed by `project_grants_only_narrow` and the subset check in the delegation trigger, both enforced against every writer, not only the API handler.
- **A revoked grant continuing to serve access** — the abuse case `P4-04`'s not-yet-built reader join and token-invalidation work is meant to close. Recorded here as an open risk rather than implied closed.

## Verification

- `TestATokenForAnotherAudienceCannotAdminister` and the exhaustive hierarchy table — `backend/internal/management/hierarchy_exhaustive_test.go`.
- Project Grant lifecycle and delegation abuse cases (`A-1`…`A-9`) — `backend/internal/projectgrant/*_test.go`, catalogued by name in `backend/tests/security/isolation_test.go`'s coverage map under "Phase 4".
- `TestAGrantOwnerReachesNothingBeyondItsGrant`, `TestOnlyTheReceivingOrganizationAppointsOwnersFromItsOwnMembers`, `TestAGrantOwnerRowHeldOutsideTheReceivingOrganizationIsInert` — `backend/internal/projectgrant`.

## Not Yet Built / Open Questions

- `P4-04`: delegated roles in tokens and `/v1/authz/check`; revocation propagation; the granting-side RLS read policy on delegated rows; enforcement of ADR-025's cross-organization policy reconciliation.
- `P4-06`: the receiving organization's "Granted Projects" console screen.
- `PG-31`: no write API for `INSTANCE_OWNER`/`ORG_OWNER`/`ORG_ADMIN` — every such row today is created by direct database access outside any API.
- `docs/PLAN/08` Part C's hierarchy diagram has not itself been amended for `PG-32`/`PG-44`'s readings — both are recorded as backlog items awaiting the deliberate plan-change process, not yet reflected in the plan document.

## Related Documents

- [`00-MULTI-TENANCY-ARCHITECTURE.md`](./00-MULTI-TENANCY-ARCHITECTURE.md), [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md)
- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`, `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`
- `docs/PLAN/08-AUTHORIZATION.md` Part C
- `TASKS/BACKLOG.md` PG-31, PG-32, PG-44; `MEMORY/specs/P4-01-project-grants.md`, `P4-02-delegated-user-grants.md`, `P4-03-project-grant-owner.md`; `MEMORY/DECISIONS.md` ADR-025

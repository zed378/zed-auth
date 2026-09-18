# 01 - Multi-Tenant RBAC

> Category: **Authorization** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P2-01, P2-02, P2-03, P2-05 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Specify the role-based access control model: what a role is, what a grant is, and how the manager-role hierarchy resolves a request.

## Scope

Application roles and manager roles. Delegation across organizations is `02-PROJECT-GRANTS-DELEGATION.md`; the live check is `04-PERMISSION-EVALUATION-ENGINE.md`.

## As Built

### Application roles

- A **role** belongs to one project (`roles`, `backend/internal/role/`). Its `key` is immutable — grants reference it by name and nothing cascades, so re-keying would silently revoke access. Its `permission_keys` are a set of `resource:action` strings, replaced wholesale on update because a partial array update is ambiguous.
- A **grant** (`user_grants`) gives one user the role keys they hold in one project. One row per user per project (`user_grants_user_project_key`), never empty (`user_grants_role_keys_not_empty`).
- A grant naming a role that does not exist is refused by a trigger that names the offending key (`backend/migrations/20260912000023_user_grant_rules.up.sql`).

### Manager roles

`manager_roles` rows are `(user_id, role, scope_id)`, where `scope_id` means different things per role: the instance, an organization, a project, or — since `P4-03` — a Project Grant.

The hierarchy (`satisfies` in `backend/internal/management/roles.go`):

| Held | Also satisfies |
|---|---|
| `INSTANCE_OWNER` | `ORG_OWNER`, `ORG_ADMIN`, `PROJECT_OWNER`, `PROJECT_GRANT_OWNER`, `MEMBER` |
| `ORG_OWNER` | `ORG_ADMIN`, `PROJECT_OWNER`, `PROJECT_GRANT_OWNER`, `MEMBER` |
| `ORG_ADMIN` | `PROJECT_OWNER`, `PROJECT_GRANT_OWNER`, `MEMBER` |
| `PROJECT_OWNER` | `MEMBER` |
| `PROJECT_GRANT_OWNER` | `MEMBER` |

Two entries in that table are decisions rather than transcriptions of the plan's diagram:

- **`ORG_ADMIN` satisfies `PROJECT_OWNER`** (`TASKS/BACKLOG.md` PG-32): an organization-scoped role covers every project inside it. Reading the diagram strictly would produce an administrator who can create a project but not manage the roles in it.
- **`PROJECT_OWNER` does not satisfy `PROJECT_GRANT_OWNER`** (`PG-44`, threat review T4-4): with delegation the project owner is in the *granting* organization, and the delegated roles are administered in the *receiving* one. Inheriting across that boundary would be a confused deputy.

### Resolution

`Authorize(caller, requirement, target)` decides in this order: unset scope refuses; an unknown required role refuses; `INSTANCE_OWNER` passes anything; self-scoped routes need only a valid token; instance-scoped routes need `INSTANCE_OWNER`; `MEMBER` is satisfied by belonging to the target organization; then a grant must **name the target** — the grant for a grant-scoped route, the project or its organization for a project-scoped route, the organization otherwise. A `PROJECT_GRANT_OWNER` row is skipped in the organization and project loops, because its `scope_id` is a grant id and a uuid collision must buy nothing.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Role key format and length | Generated pattern, max 63 characters | `openapi/openapi.yaml` § `RoleKey`, `backend/internal/role/pattern.gen.go`, `console/src/lib/api/patterns.gen.ts` |
| Permission key format | `resource:action`, max 128 characters | same sources |
| Roles per grant | At most 64 (`MaxRoleKeys`) | `backend/internal/grant/store.go` |
| Built-in roles | Cannot be deleted or re-keyed; none are seeded today | `backend/internal/role/`, `TASKS/BACKLOG.md` PG-30, PG-34 |
| Deleting a role that grants reference | Refused, with the count | `backend/internal/role/store.go` |
| Granting yourself a role | Refused (`403`) | `refuseSelfService`, `backend/internal/grant/handler.go` |

## Interfaces

- Roles: `GET/POST /v1/organizations/{org_id}/projects/{project_id}/roles`, `GET/PATCH/DELETE …/roles/{role_id}` — `PROJECT_OWNER` at project scope.
- User grants: `GET/POST /v1/organizations/{org_id}/users/{user_id}/grants`, `PATCH/DELETE …/grants/{project_id}` — `ORG_ADMIN` at organization scope.
- Audit events: `role.created`, `role.updated`, `role.deleted`, `role.assigned`, `role.revoked` (`backend/internal/audit/audit.go`).

## Security Considerations

- Least privilege is asserted as an **absence**: nothing writes a grant except these endpoints, so a user with no row has no access. `TestAUserWithNoGrantHasNoRoles` pins it, because a future "default role for new users" convenience would break it silently.
- A grant is always written inside the tenant transaction, so RLS refuses a row filed under the wrong organization even if a handler forgot to check.

## Verification

- `backend/internal/management/roles_test.go` — the hierarchy table, restated from the plan.
- `backend/internal/management/hierarchy_exhaustive_test.go` — every combination, including rows the database should never hold.
- `backend/internal/grant/grant_integration_test.go`, `backend/internal/role/*_integration_test.go`.

## Not Yet Built / Open Questions

- No API assigns `ORG_OWNER`, `ORG_ADMIN` or `PROJECT_OWNER` (`PG-31`); they are inserted by SQL during bootstrap.
- No built-in roles are seeded (`PG-30`).

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md` Part A and Part C.
- `02-PROJECT-GRANTS-DELEGATION.md`, `04-PERMISSION-EVALUATION-ENGINE.md`.
- `docs/API/12-ROLE-AND-PERMISSION-API.md`.

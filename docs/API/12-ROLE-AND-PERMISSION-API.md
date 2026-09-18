# 12 - Role & Permission API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P2-02, P2-03, P2-05, P2-06 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document project roles (named bundles of permission keys), direct user grants (the row that actually gives a user access to a project), and the real-time authorization decision endpoint `/v1/authz/check`.

## Scope

`/v1/organizations/{org_id}/projects/{project_id}/roles*`, `/v1/organizations/{org_id}/users/{user_id}/grants*`, and `/v1/authz/check`. Delegated user grants and grant owners — the receiving side of a Project Grant — are `13-PROJECT-GRANT-API.md`, not this document, even though they share the `Grant` response schema. The authorization *model* (role hierarchy, what a permission key means) is `docs/PLAN/08-AUTHORIZATION.md` Part A and `docs/AUTHORIZATION/`.

## As Built

### Roles

**A role's `key` is unique within its project and may repeat freely across projects.** `admin` in one project is unrelated to `admin` in another (`docs/PLAN/08` Part A) — roles are namespaced per project specifically so this is true.

**A role's `key` cannot be changed by anyone, ever, including on update.** A grant references a role by `key`, with no foreign key to cascade a rename — re-keying would silently orphan every grant holding the old key, which is a rename that removes access without anyone asking for that. A different key is a different role; `display_name` and `permission_keys` remain editable.

**Roles are listed ordered by `key`, not by creation time** — the one list endpoint in this API with name-ordering rather than time-ordering (`06-PAGINATION.md`), because a role list is read as a reference table.

**Delete refuses while any grant still references the role, and reports the count**, rather than cascading — a cascade would remove access from every user holding the role, across every application reading it, in response to a request that looks like tidying up. A built-in role cannot be deleted or re-keyed at all (no built-in roles are seeded today — `TASKS/BACKLOG.md` PG-30).

**Five role names are reserved** at role-key validation, because a project role sharing a name with a manager role (`org_admin`, etc.) would arrive in a token beside a manager role of the same name meaning something entirely different.

### Direct user grants

**One grant per user per project.** `POST .../grants` on a user who already has a grant in that project is `409`; the operation for changing an existing grant's roles is `PATCH`, which **replaces the role set entirely** — a partial update of an array (add/remove/replace?) is ambiguous, and the audit event records a complete before-and-after, which needs the whole set on both sides.

**An empty `role_keys` on a replace is refused.** A grant with no roles grants nothing and should not exist (`docs/PLAN/08` § Least Privilege); to remove access, delete the grant.

**Every role key must name a role that exists in that specific project.** A grant referencing a role that does not exist would look like access, carry a key nothing defines, and be silently denied by every consumer with nothing explaining why.

**A caller cannot grant to themselves.** No scope expresses this (an `ORG_ADMIN` administers every user in the organization, including themselves), so it is refused explicitly in the handler rather than by a role check.

**Revocation is immediate and physical** — the row is deleted, not flagged, so nothing can read it afterward. An access token already issued keeps whatever claims it was minted with until it expires; a consumer needing certainty calls `/v1/authz/check`.

### `/v1/authz/check`

**Reads live grant data, never token claims** — the entire reason this endpoint exists. An access token's manager-role claim (and, once `P4-04` ships, its delegated-role claim) is a snapshot taken at issuance; this endpoint reflects a revocation on the very next call, which matters for anything destructive, anything involving money, and anything an auditor will ask about.

**The permission checked is `resource.type + ":" + action`** — `approve` on `purchase_request` asks whether the subject holds a role carrying `purchase_request:approve`. `resource.id`, `resource.attributes`, and `context` are accepted today and are **not used** by the role-based decision; they exist so a consumer sending them today does not have to change its integration when attribute-based policies (Phase 4B) start reading them.

**Scoped to the caller's own organization and project, from the access token — never from the request body.** Requiring only `Member` (a valid token for the organization, nothing more) rather than an administrative role is deliberate: this endpoint is called by a consumer application about its own users on every protected request it serves, and requiring an administrative role there would mean every service checking a permission holds one — the inversion of least privilege the endpoint exists to avoid.

**Anything that is not `200` must be treated as denied**, and the distinction between `200 {allowed: false}` and `503` is load-bearing, not incidental: `allowed: false` says "checked, and no"; `503` says "no decision was reached." Nothing is ever allowed by a failure, and a `503` cached as a denial would make an outage look like a policy change on any dashboard watching the allow/deny ratio.

**A denial reads identically whichever way it happened.** A subject that does not exist and a subject that holds no matching role produce the same response — otherwise the endpoint would answer "does this user exist?" for anybody holding a valid token, an enumeration oracle inside an authorization check.

**Not audited, deliberately.** This is a read, called on every protected request across the entire consumer fleet; an audit row per decision would multiply the audit log by that traffic and turn a record meant for investigation into a firehose. Decisions are counted as metrics instead (`docs/PLAN/13-OBSERVABILITY.md`).

## Rules and Defaults

| Field / rule | Value | Enforced in |
|---|---|---|
| `RoleKey` pattern | `^[a-z0-9][a-z0-9_-]{0,62}$`, 1–63 chars | `openapi/openapi.yaml` `RoleKey` |
| `PermissionKey` pattern | `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$`, 3–128 chars — no wildcard | `openapi/openapi.yaml` `PermissionKey`; generated identically into `backend/internal/role/pattern.gen.go` and `console/src/lib/api/patterns.gen.ts` |
| `permission_keys` | Optional array on role create/update, max 256 items, duplicates refused (not folded) | `openapi/openapi.yaml` `RoleCreate`/`RoleUpdate` |
| `role.key` mutability | Immutable after creation, for every role including built-ins | `openapi/openapi.yaml` `RoleUpdate` |
| Role delete | Refused (`409`, with `grant_count`) while any grant references it; built-in roles cannot be deleted | `openapi/openapi.yaml` `deleteRole` |
| Role list order | By `key`, ascending | `06-PAGINATION.md` |
| `GrantCreate`/`GrantUpdate.role_keys` | 1–64 items | `openapi/openapi.yaml` |
| Direct grant uniqueness | One grant per `(user_id, project_id)` | `openapi/openapi.yaml` `grantRolesToUser` description |
| `/v1/authz/check` scope | Caller's own `org_id`/project from the token; not settable in the request body | `openapi/openapi.yaml` `AuthorizationCheck` |
| `/v1/authz/check` on dependency failure | `503 UNAVAILABLE`, treat as denied — never `200 {allowed: false}` | `openapi/openapi.yaml`; `backend/internal/management/errors.go` |
| Required role — roles | `PROJECT_OWNER` over the project, or any organization-scoped role over its organization | `backend/internal/management/policy.go` |
| Required role — direct user grants | `ORG_ADMIN` over the organization (not `PROJECT_OWNER` — see below) | `policy.go` |
| Required role — `/v1/authz/check` | `Member` | `policy.go` |

**Why direct user grants require `ORG_ADMIN`, not the `PROJECT_OWNER` scope roles use**: the grants path is addressed by user, not by project, so a project-scoped caller reaching it could enumerate the organization's users one grant request at a time — a narrower role buying a wider read. See `backend/internal/management/policy.go` inline commentary.

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET/POST /v1/organizations/{org_id}/projects/{project_id}/roles` | `listRoles` / `createRole` | `PROJECT_OWNER` or org role |
| `GET/PATCH/DELETE .../roles/{role_id}` | `getRole` / `updateRole` / `deleteRole` | `PROJECT_OWNER` or org role |
| `GET/POST /v1/organizations/{org_id}/users/{user_id}/grants` | `listUserGrants` / `grantRolesToUser` | `ORG_ADMIN` |
| `PATCH/DELETE .../grants/{project_id}` | `replaceUserGrant` / `revokeUserGrant` | `ORG_ADMIN` |
| `POST /v1/authz/check` | `checkAuthorization` | `Member` |

Full schemas: `public-site/docs/api-reference/list-roles.api.mdx`, `create-role.api.mdx`, `get-role.api.mdx`, `update-role.api.mdx`, `delete-role.api.mdx`, `list-user-grants.api.mdx`, `grant-roles-to-user.api.mdx`, `replace-user-grant.api.mdx`, `revoke-user-grant.api.mdx`, `check-authorization.api.mdx`.

Idempotency: `Idempotency-Key` accepted on role/grant-creating `POST`s; `/v1/authz/check` has no idempotency parameter (it is a read).

Audit events: `role.created`, `role.updated`, `role.deleted`, `role.assigned`, `role.revoked`. `/v1/authz/check` writes **no** audit event by design (see above); its volume is exposed as metrics instead.

## Security Considerations

- `/v1/authz/check` never logs `resource.attributes` or `context` — both are the consumer's own business data (`openapi/openapi.yaml` field descriptions; `CLAUDE.md`'s non-negotiable constraint on this exact point; `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` § Information Disclosure).
- The identical denial for "no such subject" and "subject lacks the role" is the enumeration defence for this endpoint specifically — see `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12.
- No wildcard in `PermissionKey` is a deliberate authorization-model decision, not a formatting rule: a `*` in a stored key would be a decision hiding inside a string that every consumer would have to reimplement, inconsistently.

## Verification

- `backend/internal/role/role_integration_test.go`, `handler_integration_test.go`, `api_fixture_integration_test.go`.
- `backend/internal/grant/grant_integration_test.go`.
- `TestAReadIsCountedAgainstTheQuota` (the `/v1/authz/check` hot-route quota) — `backend/internal/management/chain_integration_test.go`.

## Not Yet Built / Open Questions

- `/v1/authz/check` does not yet evaluate delegated (Project Grant) roles — only direct grants. See `13-PROJECT-GRANT-API.md` and `P4-04`.
- Attribute-based policy evaluation (Phase 4B) is not built; `resource.attributes` and `context` are accepted and ignored today.

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md` Part A, `docs/AUTHORIZATION/01-MULTI-TENANT-RBAC.md`, `04-PERMISSION-EVALUATION-ENGINE.md`
- `docs/API/13-PROJECT-GRANT-API.md` (delegated grants, same `Grant` schema)
- `docs/API/02-AUTHENTICATION-AND-AUTHORIZATION.md`
- `TASKS/BACKLOG.md` PG-30

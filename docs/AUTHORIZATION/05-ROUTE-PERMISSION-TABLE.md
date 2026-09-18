# 05 - Route Permission Table

> Category: **Authorization** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-16, P2-05, P2-06, P4-01, P4-02, P4-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Render, in one place, what every `/v1` route demands. The table below is generated from `backend/internal/management/policy.go`, which is the source of truth — if the two disagree, the code is right and this document is stale.

## Scope

Administrative permission only. Row-level security bounds what a handler's queries can then see (`docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`), and delegation adds rules the handler enforces beyond the route requirement (`02-PROJECT-GRANTS-DELEGATION.md`).

## As Built

A route absent from the table gets the zero `Requirement`, which no caller satisfies: forgetting to declare a permission makes an endpoint unreachable rather than open. `TestEveryV1RouteHasADeclaredPermission` walks the routes the router actually registers and fails on any that is missing, so "unreachable but broken" is caught on the first test run.

Scopes:

- **Self** — a valid token; the endpoint describes the caller.
- **Organization** — the role over the organization in the path (or `INSTANCE_OWNER`).
- **Instance** — `INSTANCE_OWNER` only.
- **Project** — the role over the project in the path, or over the organization containing it.
- **Project Grant** — `PROJECT_GRANT_OWNER` of the grant in the path, held by a member of the path organization, or an organization role over that organization.

Roles satisfy weaker ones per the hierarchy in `01-MULTI-TENANT-RBAC.md`, so the column below is the **minimum**.

## Interfaces

| Route | Minimum role | Scope |
|---|---|---|
| `GET /v1/me/organizations` | `MEMBER` | Self |
| `GET /v1/me/sessions` | `MEMBER` | Self |
| `DELETE /v1/me/sessions/{session_id}` | `MEMBER` | Self |
| `POST /v1/me/sessions/revoke-others` | `MEMBER` | Self |
| `GET /v1/me` | `MEMBER` | Self |
| `POST /v1/me/password` | `MEMBER` | Self |
| `GET /v1/me/mfa` | `MEMBER` | Self |
| `POST /v1/me/mfa/totp` | `MEMBER` | Self |
| `POST /v1/me/mfa/totp/{factor_id}/confirm` | `MEMBER` | Self |
| `DELETE /v1/me/mfa/factors/{factor_id}` | `MEMBER` | Self |
| `POST /v1/me/mfa/recovery-codes` | `MEMBER` | Self |
| `GET /v1/organizations/{org_id}/users/{user_id}/mfa` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/users/{user_id}/sessions` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations` | `INSTANCE_OWNER` | Instance |
| `POST /v1/organizations` | `INSTANCE_OWNER` | Instance |
| `GET /v1/organizations/{org_id}` | `ORG_ADMIN` | Organization |
| `PATCH /v1/organizations/{org_id}` | `ORG_OWNER` | Organization |
| `GET /v1/organizations/{org_id}/mfa-impact` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}` | `INSTANCE_OWNER` | Instance |
| `GET /v1/organizations/{org_id}/projects` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/projects` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/projects/{project_id}` | `ORG_ADMIN` | Organization |
| `PATCH /v1/organizations/{org_id}/projects/{project_id}` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}` | `ORG_OWNER` | Organization |
| `GET /v1/organizations/{org_id}/projects/{project_id}/applications` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/projects/{project_id}/applications` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}` | `ORG_ADMIN` | Organization |
| `PATCH /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}/rotate-secret` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}/applications/{application_id}` | `ORG_OWNER` | Organization |
| `GET /v1/organizations/{org_id}/projects/{project_id}/roles` | `PROJECT_OWNER` | Project |
| `POST /v1/organizations/{org_id}/projects/{project_id}/roles` | `PROJECT_OWNER` | Project |
| `GET /v1/organizations/{org_id}/projects/{project_id}/roles/{role_id}` | `PROJECT_OWNER` | Project |
| `PATCH /v1/organizations/{org_id}/projects/{project_id}/roles/{role_id}` | `PROJECT_OWNER` | Project |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}/roles/{role_id}` | `PROJECT_OWNER` | Project |
| `GET /v1/organizations/{org_id}/projects/{project_id}/grants` | `PROJECT_OWNER` | Project |
| `POST /v1/organizations/{org_id}/projects/{project_id}/grants` | `PROJECT_OWNER` | Project |
| `GET /v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}` | `PROJECT_OWNER` | Project |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}` | `PROJECT_OWNER` | Project |
| `GET /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `PROJECT_GRANT_OWNER` | Project Grant |
| `POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `PROJECT_GRANT_OWNER` | Project Grant |
| `PATCH /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}` | `PROJECT_GRANT_OWNER` | Project Grant |
| `DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}` | `PROJECT_GRANT_OWNER` | Project Grant |
| `GET /v1/organizations/{org_id}/project-grants/{grant_id}/owners` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/project-grants/{grant_id}/owners` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/owners/{user_id}` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/users` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/users` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/users/{user_id}` | `ORG_ADMIN` | Organization |
| `PATCH /v1/organizations/{org_id}/users/{user_id}` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/users/{user_id}/deactivate` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/users/{user_id}/reactivate` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/users/{user_id}/password-reset` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/users/{user_id}/grants` | `ORG_ADMIN` | Organization |
| `POST /v1/organizations/{org_id}/users/{user_id}/grants` | `ORG_ADMIN` | Organization |
| `PATCH /v1/organizations/{org_id}/users/{user_id}/grants/{project_id}` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}/users/{user_id}/grants/{project_id}` | `ORG_ADMIN` | Organization |
| `POST /v1/authz/check` | `MEMBER` | Organization |
| `GET /v1/organizations/{org_id}/events` | `ORG_ADMIN` | Organization |

61 routes.

## Security Considerations

- A refusal is `404` when the caller holds nothing over the target and `403` when they hold something but not enough, so a status code cannot be used to test whether an organization or project exists.
- `POST /v1/authz/check` is deliberately `MEMBER`: it is called by consumer applications on every protected request they serve, and requiring an administrative role there would invert least privilege.
- The three `/owners` routes are `ORG_ADMIN` rather than grant-scoped: a grant owner must not be able to appoint another (`MEMORY/specs/P4-03-project-grant-owner.md` A-6).

## Verification

- `TestEveryV1RouteHasADeclaredPermission` — every registered route has an entry.
- `backend/internal/management/hierarchy_exhaustive_test.go` — the resolution rules behind the table.
- Per-package integration tests assert the refusals for callers who hold the wrong role.

## Not Yet Built / Open Questions

- Routes for SAML, social login, webhooks and SCIM do not exist yet; they will need entries here when they do.

## Related Documents

- `backend/internal/management/policy.go` (source of truth), `docs/API/02-AUTHENTICATION-AND-AUTHORIZATION.md`, `docs/PLAN/05-API-CONTRACT.md`.

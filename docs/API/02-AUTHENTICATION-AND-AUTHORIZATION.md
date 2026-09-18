# 02 - API Authentication & Authorization

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-15, P2-05, P2-06, P4-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how every `/v1` request is authenticated and authorized, and give the complete, current table of which role and scope each route demands. This is the API-surface view of the model `docs/PLAN/08-AUTHORIZATION.md` and `docs/AUTHORIZATION/` define; it does not re-derive that model, only shows how it is wired to HTTP.

## Scope

Bearer token verification and the per-route permission check for the `/v1` Management API. The OAuth/OIDC protocol endpoints (`/oauth/*`) authenticate differently (client credentials, PKCE) and are covered in `08-AUTH-API.md`. The authorization *model* — role hierarchy, Project Grants, delegation — is `docs/PLAN/08-AUTHORIZATION.md` and `docs/AUTHORIZATION/01-MULTI-TENANT-RBAC.md` / `02-PROJECT-GRANTS-DELEGATION.md`; this document does not restate it.

## As Built

**Every `/v1` route runs `authenticate → authorize → scope → handle`**, in `backend/internal/management/middleware.go`'s `Middleware.Require`. There is no other place a permission decision is made: `TestNothingOutsideThisPackageDecidesPermissions` (`backend/internal/management/architecture_test.go`) statically scans every non-test, non-`management` source file for a role comparison or grant iteration and fails the build if one exists outside this package.

**Authentication** (`Middleware.authenticate`):

1. The `Authorization: Bearer <token>` header is required; anything else is `401 UNAUTHENTICATED`.
2. The token must verify as an access token specifically — `typ: at+jwt` per RFC 9068 (`signing.TypeAccessToken`). An ID token presented here is refused before its claims are even read, the same substitution defence `/oauth/userinfo` uses.
3. Claims checked: `iss` must equal this service's issuer; `aud` must equal the issuer too (a token minted for a consumer's own resource server must not administer the platform); `exp` must be in the future; `iat` must not be more than 30 seconds in the future (clock skew tolerance, `iat` only — never `exp`); `sub` and `org_id` must both be present.
4. If the token carries a session (`sid` claim — absent for `client_credentials` tokens), the session must still be live (`Sessions.IsLive`). This is what makes an administrator's forced logout, or a deactivated account, take effect on the very next Management API request rather than waiting up to the token's ten-minute lifetime.
5. Every failure to authenticate answers identically: `401 UNAUTHENTICATED`, `WWW-Authenticate: Bearer realm="<issuer>", error="invalid_token"`. Expired, forged, wrong audience, wrong issuer, and "the session behind this token has ended" are not distinguished in the response — only in the server log's `reason` field — so a caller holding a captured token cannot use this endpoint to ask whether its owner has logged out.

**Authorization** (`Authorize` in `backend/internal/management/roles.go`, invoked by `Require`):

- The caller's manager roles are read from the database **on every request**, never from a token claim (`backend/internal/management/store.go` `RoleStore.GrantsFor`) — a role revoked a minute ago must not still work because the token that asserted it has ten minutes left to live.
- `Requirement{Role, Scope}` is looked up per route from the `Policy` table (below) and checked against the caller's grants and the request's `Target` (organization, project and/or grant id taken from the path — never from the request body, which is what stops a project-scoped endpoint being reached with a project id from the body while the path names another).
- **A route absent from `Policy` gets the zero `Requirement`, which is unsatisfiable by any caller — unreachable rather than open.** `TestEveryV1RouteHasADeclaredPermission` (`backend/internal/httpserver/v1_routes_test.go`) fails the build if the generated router registers a route `Policy` does not cover.
- **Refusal is 404, not 403, when the caller cannot already see the target.** A caller holding nothing over an organization (or, on a project-scoped route, over neither the project nor its organization) is told `404 NOT_FOUND` — the same answer a nonexistent id gets — so a 403 cannot be used to enumerate which organization or project ids are real (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2, §3, §12; `Decision.Invisible` in `roles.go`). A caller who already holds *something* over the target (e.g. an `ORG_ADMIN` asked for `ORG_OWNER`) is told `403 PERMISSION_DENIED`, because hiding an organization they already know exists buys nothing.
- **`INSTANCE_OWNER` satisfies every requirement, over any target**, and the decision records whether the action crossed into another organization (`Decision.InstanceScoped`) so the database work runs under a logged, audited cross-tenant scope (`Middleware.InScope`) rather than silently.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Credential | OAuth 2.0 Bearer access token, `typ: at+jwt` (RFC 9068) | `backend/internal/management/middleware.go` |
| Clock skew tolerance | 30s, `iat` only, never `exp` | `backend/internal/management/middleware.go` `clockSkew` |
| Manager roles source | Read from `manager_roles` on every request, never from a token claim | `backend/internal/management/store.go` |
| Unannotated route | Refused (zero `Requirement`, unsatisfiable) | `backend/internal/management/roles.go`, `policy.go` |
| Refusal for an invisible target | `404 NOT_FOUND` | `backend/internal/management/roles.go` `Decision.Invisible` |
| Refusal for a visible-but-insufficient target | `403 PERMISSION_DENIED` | `backend/internal/management/roles.go` |
| Cross-tenant `INSTANCE_OWNER` action | Runs under `WithTenant(target)`, logged (`an INSTANCE_OWNER is acting on another organization`) | `backend/internal/management/middleware.go` `InScope` |

## Interfaces

### Manager roles

| Role | Meaning | Where it applies |
|---|---|---|
| `INSTANCE_OWNER` | Administers every organization | Instance-scoped; satisfies every other role over any target |
| `ORG_OWNER` | Administers one organization entirely | One organization |
| `ORG_ADMIN` | Administers one organization except deleting it or changing its status | One organization; also satisfies `PROJECT_OWNER` and `PROJECT_GRANT_OWNER` requirements within it (`PG-32`) |
| `PROJECT_OWNER` | Administers one project's roles, applications, and granting side of Project Grants | One project |
| `PROJECT_GRANT_OWNER` | Administers the delegated roles of exactly one received Project Grant | One Project Grant, in the receiving organization |
| `Member` | Holds a valid token for an organization; not a stored role | Declared by an endpoint that needs "authenticated and in this tenant", nothing more (e.g. `/v1/authz/check`) |

### The complete route policy (`backend/internal/management/policy.go`)

| Method & path | Required role | Scope |
|---|---|---|
| `GET /v1/me/organizations` | none | Self |
| `GET /v1/me` | none | Self |
| `POST /v1/me/password` | none | Self |
| `GET /v1/me/mfa` | none | Self |
| `POST /v1/me/mfa/totp` | none | Self (+ recent authentication) |
| `POST /v1/me/mfa/totp/{factor_id}/confirm` | none | Self |
| `DELETE /v1/me/mfa/factors/{factor_id}` | none | Self (+ recent authentication) |
| `POST /v1/me/mfa/recovery-codes` | none | Self (+ recent authentication) |
| `GET /v1/me/sessions` | none | Self |
| `DELETE /v1/me/sessions/{session_id}` | none | Self |
| `POST /v1/me/sessions/revoke-others` | none | Self |
| `GET /v1/organizations` | `INSTANCE_OWNER` | Instance |
| `POST /v1/organizations` | `INSTANCE_OWNER` | Instance |
| `GET /v1/organizations/{org_id}` | `ORG_ADMIN` | Organization |
| `PATCH /v1/organizations/{org_id}` | `ORG_OWNER` (`INSTANCE_OWNER` for `status`) | Organization |
| `DELETE /v1/organizations/{org_id}` | `INSTANCE_OWNER` | Instance |
| `GET /v1/organizations/{org_id}/mfa-impact` | `ORG_ADMIN` | Organization |
| `GET/POST /v1/organizations/{org_id}/projects` | `ORG_ADMIN` | Organization |
| `GET/PATCH /v1/organizations/{org_id}/projects/{project_id}` | `ORG_ADMIN` | Organization |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}` | `ORG_OWNER` | Organization |
| `GET/POST /v1/organizations/{org_id}/projects/{project_id}/applications` | `ORG_ADMIN` | Organization |
| `GET/PATCH .../applications/{application_id}` | `ORG_ADMIN` | Organization |
| `POST .../applications/{application_id}/rotate-secret` | `ORG_ADMIN` | Organization |
| `DELETE .../applications/{application_id}` | `ORG_OWNER` | Organization |
| `GET/POST /v1/organizations/{org_id}/projects/{project_id}/roles` | `PROJECT_OWNER` | Project |
| `GET/PATCH/DELETE .../roles/{role_id}` | `PROJECT_OWNER` | Project |
| `GET/POST /v1/organizations/{org_id}/projects/{project_id}/grants` | `PROJECT_OWNER` | Project |
| `GET/DELETE .../grants/{grant_id}` | `PROJECT_OWNER` | Project |
| `GET/POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `PROJECT_GRANT_OWNER` | Project Grant |
| `PATCH/DELETE .../user-grants/{user_id}` | `PROJECT_GRANT_OWNER` | Project Grant |
| `GET/POST /v1/organizations/{org_id}/project-grants/{grant_id}/owners` | `ORG_ADMIN` | Organization |
| `DELETE .../owners/{user_id}` | `ORG_ADMIN` | Organization |
| `GET/POST /v1/organizations/{org_id}/users` | `ORG_ADMIN` | Organization |
| `GET/PATCH /v1/organizations/{org_id}/users/{user_id}` | `ORG_ADMIN` | Organization |
| `POST .../deactivate`, `/reactivate`, `/password-reset`, `/mfa-reset` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/users/{user_id}/mfa` | `ORG_ADMIN` | Organization |
| `GET /v1/organizations/{org_id}/users/{user_id}/sessions` | `ORG_ADMIN` | Organization |
| `DELETE .../sessions/{session_id}` | `ORG_ADMIN` | Organization |
| `GET/POST /v1/organizations/{org_id}/users/{user_id}/grants` | `ORG_ADMIN` | Organization |
| `PATCH/DELETE .../grants/{project_id}` | `ORG_ADMIN` | Organization |
| `POST /v1/authz/check` | `Member` | Organization (caller's own) |
| `GET /v1/organizations/{org_id}/events` | `ORG_ADMIN` | Organization |

Note: an organization-scoped role (`ORG_ADMIN`/`ORG_OWNER`) satisfies every `PROJECT_OWNER` requirement inside that organization, and `PROJECT_OWNER` does **not** satisfy `PROJECT_GRANT_OWNER` — delegation crosses an organization boundary, so scope decides rather than the role hierarchy (`backend/internal/management/roles.go`, recorded as `TASKS/BACKLOG.md` PG-32 and threat review `T4-4`).

## Security Considerations

- Every unusable token answers identically (`unusable()` in `middleware.go`) — expired, forged, wrong audience/issuer, and "session ended" are indistinguishable to the caller, closing the oracle a distinguishing error would open.
- 404-vs-403 (`Decision.Invisible`) is the primary defence against organization/project id enumeration across a tenant boundary — see `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §3, §12.
- `/v1/authz/check` deliberately requires only `Member`, not an administrative role, because it is called by consumer applications about their own users on every protected request they serve; requiring an administrative role there would be the inverse of least privilege. Its scope is bound to the caller's own organization and project from the token, not from the request body — see `12-ROLE-AND-PERMISSION-API.md`.
- Project Grant role assignment is validated as a subset of `granted_role_keys` on every request, not just at grant creation — `CLAUDE.md`'s non-negotiable constraint, implemented in `backend/internal/projectgrant/delegated.go` and covered in `13-PROJECT-GRANT-API.md`.

## Verification

- `TestEveryV1RouteHasADeclaredPermission` — `backend/internal/httpserver/v1_routes_test.go`.
- `TestNothingOutsideThisPackageDecidesPermissions` — `backend/internal/management/architecture_test.go`.
- `TestAnUnusableTokenIsRejected`, `TestAValidTokenReachesTheHandler`, `TestACallerWithoutTheRoleIsRejected`, `TestARoleRevokedBetweenTwoRequestsTakesEffectOnTheSecond`, `TestAnotherOrganizationIsNotFound`, `TestAnInstanceOwnerReachesAnotherOrganization`, `TestAnInstanceOwnerCanReadAnotherOrganizationsRows`, `TestActingOnAnotherOrganizationSeesOnlyThatOrganization`, `TestAnUnannotatedRouteRefusesEvenAnInstanceOwner` — `backend/internal/management/chain_integration_test.go`.
- The role hierarchy (`satisfies`) is exercised exhaustively in `backend/internal/management/hierarchy_exhaustive_test.go` and `roles_test.go`.

## Not Yet Built / Open Questions

- Delegated roles are not yet evaluated by `Authorize` or carried in access tokens — `/v1/authz/check` and the token issuance path both ignore Project Grant delegation until `P4-04` ships. See `13-PROJECT-GRANT-API.md`.
- No API key / service-account credential type exists beyond the OAuth `client_credentials` grant; there is no separate "API key" header or format anywhere in this service.

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md`, `docs/AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md`, `docs/AUTHORIZATION/01-MULTI-TENANT-RBAC.md`, `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
- `docs/API/13-PROJECT-GRANT-API.md` (subset-validation rule in practice)
- `TASKS/BACKLOG.md` PG-32; threat review `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` T4-4

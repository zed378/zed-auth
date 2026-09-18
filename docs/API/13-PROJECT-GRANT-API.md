# 13 - Project Grant API (Cross-Organization Delegation)

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P4-01, P4-02, P4-03, P4-05 (built); P4-04, P4-06 (not built) &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the cross-organization delegation surface: a Project Grant (the granting side, P4-01), delegated user grants against a received grant (the receiving side, P4-02), and grant owners — the one role scoped to a single grant (P4-03). State plainly what a Project Grant does and does not confer today, since that boundary has moved once already as the feature was built in slices.

## Scope

`/v1/organizations/{org_id}/projects/{project_id}/grants*` (granting side) and `/v1/organizations/{org_id}/project-grants/{grant_id}/*` (receiving side: `user-grants`, `owners`). Direct (non-delegated) user grants are `12-ROLE-AND-PERMISSION-API.md`. The authorization model behind delegation is `docs/PLAN/08-AUTHORIZATION.md` Part C and `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`.

## As Built

**A Project Grant lends a project's roles to another organization; it does not, by itself, give anyone access.** `POST .../projects/{project_id}/grants` requires `PROJECT_OWNER` over the project (or any organization-scoped role over the organization that owns it — the **granting** side) and creates a grant naming `granted_org_id` and the subset of the project's roles (`role_keys`) it delegates. As built today, a grant is the recorded contract; the roles it delegates start conferring access to the receiving organization's users through `P4-02` (delegated user grants, below), and — not yet built — through tokens and `/v1/authz/check` via `P4-04`.

**A grant cannot be changed after creation — there is no update.** Widening a delegation in place is exactly the privilege-escalation path this feature has to rule out; a different set of roles means revoking the grant and creating a new one. `role_keys` must all name roles that exist in that project; an organization cannot grant to itself; one project may have at most one **active** grant per receiving organization (a second attempt is `409`).

**Revocation is a status transition, not a deletion.** `DELETE .../grants/{grant_id}` marks the grant `revoked` with a `revoked_at` timestamp; the row and its history survive (`docs/PLAN/08` Part C: a revoked grant is the record that a delegation existed and ended). A revoked grant cannot be reactivated; revoking an already-revoked grant succeeds and changes nothing.

**Grant identity is by ID, never by search (ADR-026).** `granted_org_id` is supplied directly by the caller — there is no lookup-by-name endpoint for choosing a receiving organization. This is a deliberate console/API decision, not an oversight: it avoids building a cross-tenant organization search surface for a feature whose entire point is to bound cross-tenant visibility.

**The receiving organization administers delegated roles through its own routes, never through the granting side's.** `GET/POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` requires `ORG_ADMIN` over `{org_id}` — which must be the organization the grant was made **to** — or `PROJECT_GRANT_OWNER` of that specific grant while it is active. The **granting** organization's administrators hold nothing on these routes at all; the handler finds the grant only by `granted_org_id`, so they cannot reach it even through their own organization's path (threat review T4-4).

**Every delegated role assignment is validated as a subset of `granted_role_keys`, on every single request — not only at grant-creation time.** This is `CLAUDE.md`'s non-negotiable constraint, and it is checked against the grant **as it stands at the moment of the request**: if a grant is narrowed (revoke and re-grant with fewer roles, since a grant cannot be edited in place), a role key the old grant delegated but the new one does not is refused on the next assignment, and `TestNarrowingByRegrantRefusesTheDroppedRoleThroughEitherGrant` (`backend/internal/projectgrant/delegated_integration_test.go`) verifies exactly this. A revoked grant refuses every assignment with `409`.

**A stale assignment is re-pointed, not orphaned.** If a user's delegated roles for a project came through a grant that has since been revoked, a fresh `assignDelegatedRoles` call re-assigns them through the current grant; the audit event records the replacement.

**`PROJECT_GRANT_OWNER` is the one role scoped to a single grant, not a project or an organization.** It lets a member of the receiving organization administer that grant's delegated user grants without holding `ORG_ADMIN` generally. Grant owners are assigned by `ORG_ADMIN` of the receiving organization only (`POST .../owners`) — a grant owner cannot appoint another, and the granting organization cannot appoint anyone. The role works only while the grant is active; a grant revoked and re-granted has a new id, and owners of the old grant are not owners of the new one.

**No caller can assign to or make an owner of themselves**, on either the user-grants or owners routes.

**`holder_count` on a `ProjectGrant` counts users, never lists them** — how many of the receiving organization's users hold a role through this grant, which is the number a revocation would take access away from. It was `0` for every grant until `P4-02` shipped delegated assignment; `P4-05` (the console's Project Grants tab) uses it to show the affected count before a typed-confirmation revocation.

## Rules and Defaults

| Field / rule | Value | Enforced in |
|---|---|---|
| `ProjectGrantCreate.granted_org_id` | Required, `ResourceId` (UUID); supplied directly, never searched (ADR-026) | `openapi/openapi.yaml` |
| `ProjectGrantCreate.role_keys` | Required, 1–50 items, unique, each must exist in this project | `openapi/openapi.yaml` |
| Grant mutability | Immutable after creation; no update operation exists | `openapi/openapi.yaml` (`ProjectGrant` has no `PATCH`) |
| One active grant per `(project_id, granted_org_id)` | A second is `409` | `backend/internal/projectgrant/store.go` |
| Grant revocation | Status transition (`active` → `revoked`), not deletion; irreversible; idempotent | `openapi/openapi.yaml` `revokeProjectGrant` |
| `DelegatedGrantCreate.role_keys` | Required, 1–64 items; must be a subset of `granted_role_keys`, checked live on every request | `openapi/openapi.yaml`; `backend/internal/projectgrant/delegated.go` |
| Delegated assignment target | Must be a member of the receiving organization; not the caller themselves | `backend/internal/projectgrant/delegated.go` |
| Delegated assignment against a revoked grant | Refused, `409` | `openapi/openapi.yaml` `assignDelegatedRoles` |
| `ProjectGrantOwnerCreate.user_id` | Required; must be a member of the receiving organization; not the caller themselves | `openapi/openapi.yaml` |
| Grant owner scope | Tied to one grant id; does not survive revoke-and-regrant | `backend/internal/projectgrant/owners.go` |
| `ProjectGrant.holder_count` | Count only, never a list of holders | `openapi/openapi.yaml` |
| Required role — granting side (`.../projects/{project_id}/grants*`) | `PROJECT_OWNER` over the project, or an organization-scoped role over the granting organization | `backend/internal/management/policy.go` |
| Required role — receiving side (`user-grants*`) | `ORG_ADMIN` over the receiving organization, or `PROJECT_GRANT_OWNER` of this grant | `policy.go` |
| Required role — grant owners (`owners*`) | `ORG_ADMIN` over the receiving organization | `policy.go` |

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET/POST /v1/organizations/{org_id}/projects/{project_id}/grants` | `listProjectGrants` / `createProjectGrant` | `PROJECT_OWNER` or org role (granting side) |
| `GET/DELETE .../grants/{grant_id}` | `getProjectGrant` / `revokeProjectGrant` | `PROJECT_OWNER` or org role (granting side) |
| `GET/POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` | `listDelegatedUserGrants` / `assignDelegatedRoles` | `ORG_ADMIN` (receiving org) or `PROJECT_GRANT_OWNER` |
| `PATCH/DELETE .../user-grants/{user_id}` | `replaceDelegatedRoles` / `removeDelegatedRoles` | `ORG_ADMIN` (receiving org) or `PROJECT_GRANT_OWNER` |
| `GET/POST /v1/organizations/{org_id}/project-grants/{grant_id}/owners` | `listProjectGrantOwners` / `assignProjectGrantOwner` | `ORG_ADMIN` (receiving org) |
| `DELETE .../owners/{user_id}` | `removeProjectGrantOwner` | `ORG_ADMIN` (receiving org) |

Full schemas: `public-site/docs/api-reference/create-project-grant.api.mdx`, `get-project-grant.api.mdx`, `revoke-project-grant.api.mdx`, `list-project-grants.api.mdx`, `assign-delegated-roles.api.mdx`, `list-delegated-user-grants.api.mdx`, `replace-delegated-roles.api.mdx`, `remove-delegated-roles.api.mdx`, `assign-project-grant-owner.api.mdx`, `list-project-grant-owners.api.mdx`, `remove-project-grant-owner.api.mdx`.

Idempotency: `Idempotency-Key` accepted on every `POST`/`DELETE` here.

Audit events: `project_grant.created`, `project_grant.revoked`, `delegated_role.assigned`, `delegated_role.replaced`, `delegated_role.removed` — each written in the **receiving** organization's log and naming both organizations, the grant, and the exact role keys, because "who could give that partner user manager, and through what" is the question an investigation of delegated access starts with.

## Security Considerations

- Subset validation on every request (not only at assignment time) is the control against a grant narrowed after roles were already delegated — `docs/PLAN/08-AUTHORIZATION.md` Part C, `CLAUDE.md`'s non-negotiable constraint, verified by `TestNarrowingByRegrantRefusesTheDroppedRoleThroughEitherGrant`.
- The granting organization holds nothing on the receiving side's routes (found and closed via threat review `T4-4`) — this is the control against one organization administering another's people through a delegation it merely offered.
- `holder_count` as a count rather than a list is the same "counts, not names" pattern `10-ORGANIZATION-API.md`'s `mfa-impact` uses, for the same reason: the list of exactly who a revocation would affect is more than a caller deciding whether to revoke needs to see.
- ADR-025: until cross-organization delegated access is fully wired (`P4-04`), the sign-in-policy question ("whose MFA mandate and login methods apply to a delegated user") is decided as *the stricter of both organizations' policies* — recorded, but not yet exercised by a live code path, since no delegated user currently authenticates with delegated authority.

## Verification

- `backend/internal/projectgrant/projectgrant_integration_test.go`, `delegated_integration_test.go`, `owners_integration_test.go`.
- `TestNarrowingByRegrantRefusesTheDroppedRoleThroughEitherGrant` — `backend/internal/projectgrant/delegated_integration_test.go`.
- Staging records: `MEMORY/records/2026-09-15-P4-01-project-grants.md`, `2026-09-17-P4-02-delegated-user-grants.md`, `2026-09-17-P4-03-project-grant-owner.md`.

## Not Yet Built / Open Questions

- **`P4-04` — Delegated role claims and revocation propagation (`TODO`).** Delegated roles do not yet appear in access tokens, and `/v1/authz/check` does not yet evaluate delegated grants at all — it is direct-grant-only today (`12-ROLE-AND-PERMISSION-API.md`). Until this ships, "a Project Grant confers access" is true only in the sense that a delegated user grant row exists in `user_grants`; nothing downstream of token issuance or `/v1/authz/check` currently honours it. `docs/PLAN/08-AUTHORIZATION.md` Part C's guidance to prefer `/v1/authz/check` for sensitive decisions is not yet actionable for delegated access.
- **No endpoint lets the receiving organization list the Project Grants made *to* it.** Every route under `/v1/organizations/{org_id}/project-grants/{grant_id}/*` requires already knowing the `grant_id`. The console feature this would back — "Granted Projects list" — is `P4-06` (`TODO`, depends on `P4-02`), and as of this writing there is no corresponding backend card either; a receiving organization's administrator currently has no API-level way to discover which grants exist without being told the id out of band. This is a real gap, not a deferred nicety, and should be raised before `P4-06` starts.
- Cross-organization sign-in policy (whose MFA mandate, whose login methods apply to a delegated user) is decided in principle (ADR-025) but has no live code path exercising it yet — see threat review `T4-1`.

## Related Documents

- `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`, `docs/PLAN/08-AUTHORIZATION.md` Part C
- `MEMORY/DECISIONS.md` ADR-025, ADR-026
- `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` P4-01 through P4-06
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` (T4-1, T4-2, T4-4)
- `docs/API/12-ROLE-AND-PERMISSION-API.md`

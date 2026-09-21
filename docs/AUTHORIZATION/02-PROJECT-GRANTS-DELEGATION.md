# 02 - Project Grants and Cross-Organization Delegation

> Category: **Authorization** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P4-01, P4-02, P4-03, P4-04, P4-05 built; P4-06 open &nbsp;|&nbsp; Verified against: `2b84577`

## Purpose

Describe delegation: how one organization lends a project to another with a subset of its roles, who may act on each side, and what a delegated grant does — and does not — currently confer.

## Scope

The delegation contract, the receiving side's assignments, and the grant-scoped manager role. Token and live-check behaviour is `04-PERMISSION-EVALUATION-ENGINE.md`; the schema is `docs/DATABASE/`; the console is `docs/UI-UX/18` and `console/docs/implementation-chain-P4-05.md`.

## As Built

### The contract (P4-01)

`project_grants` records: which project, from which organization, to which organization, with which `granted_role_keys`, and a status of `active` or `revoked`.

- Only the granting organization writes it (`project_grants_tenant_isolation` WITH CHECK), though both sides can see it.
- A grant **only ever narrows**: a trigger refuses any change to project, parties or `granted_role_keys`, and refuses moving `revoked` back to `active` (`backend/migrations/20260915000035_project_grant_lifecycle.up.sql`). There is no update endpoint. Widening means revoke and re-grant.
- At most one active grant per (project, receiving organization); a second is `409`.
- Revocation is a status change with a timestamp, audited once; revoking an already revoked grant is `204` and writes nothing.
- The receiving organization's name is resolved through `granted_organization_names()`, which returns a name only for an organization that already holds a grant from the caller's organization — so the endpoint cannot be used to enumerate organizations.
- Deleting a role an **active** grant carries is refused with a `409` naming the grant count.

### The assignments (P4-02)

The receiving organization assigns the delegated roles to its own users through
`/v1/organizations/{org_id}/project-grants/{grant_id}/user-grants`. A delegated row is a `user_grants` row whose `project_grant_id` is set; its `org_id` is the **receiving** organization and its `project_id` is the granting organization's project.

Every create and replace, in one transaction, reads the grant `FOR SHARE` and checks four things: the grant was made to the path organization, it is `active`, every requested key is in `granted_role_keys`, and the subject is a member of that organization. `backend/migrations/20260917000037_delegated_user_grants.up.sql` repeats all of it in the trigger `user_grants_delegation_closed`, which also makes `project_grant_id` immutable and fires on every column the rules read.

The `FOR SHARE` read pairs with the `FOR UPDATE` in `projectgrant.Store.Revoke`: an assignment that waits behind a revocation sees it and fails.

### The grant-scoped role (P4-03)

`PROJECT_GRANT_OWNER` is scoped to one grant (`ScopeProjectGrant`). It is satisfied by a `manager_roles` row whose `scope_id` is the grant in the path, held by a member of the path organization — or by an organization role over that organization. The receiving organization's administrators appoint and remove owners through `…/project-grants/{grant_id}/owners`; the granting organization cannot, and neither can another grant owner. A grant owner loses standing the moment the grant is revoked, and a re-grant has a new id, so an old appointment never revives.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Roles per grant | 1–50 (`MaxRoleKeys`) | `backend/internal/projectgrant/store.go`, `openapi/openapi.yaml` |
| Roles per delegated assignment | 1–64 | `backend/internal/projectgrant/delegated.go` |
| Self-grant / self-appointment | Refused `403` | `refuseSelf`, `backend/internal/projectgrant/delegated.go` |
| Assignment through a revoked grant | `409` | handler + trigger |
| Removal or listing under a revoked grant | Allowed for an organization administrator; `404` for a grant owner | `requireStanding`, `backend/internal/projectgrant/owners.go` |
| Re-assignment across a re-grant | Replaces the stale row, recording `superseded_grant_id` | `Store.AssignDelegated` |
| Holder count | Counts distinct users per grant, for grants from the caller's organization only | `project_grant_holder_counts()`, migration `20260915000036` |

## Interfaces

Granting side (`PROJECT_OWNER` at project scope):
`GET/POST /v1/organizations/{org_id}/projects/{project_id}/grants`, `GET/DELETE …/grants/{grant_id}`.

Receiving side (`PROJECT_GRANT_OWNER` at grant scope, satisfied by organization roles):
`GET/POST …/project-grants/{grant_id}/user-grants`, `PATCH/DELETE …/user-grants/{user_id}`.

Owners (`ORG_ADMIN` at organization scope):
`GET/POST …/project-grants/{grant_id}/owners`, `DELETE …/owners/{user_id}`.

Audit events: `project_grant.created`, `project_grant.revoked` (granting organization's log); `delegated_role.assigned`, `.replaced`, `.removed`, `manager_role.assigned`, `manager_role.revoked` (receiving organization's log, each naming both organizations and the grant).

## Security Considerations

- **A delegated grant confers access through `/v1/authz/check`** since `P4-04`. Both readers resolve a delegated row against its grant on every read — `grantsql.EffectiveRoleKeys` — so the effective set is `role_keys ∩ granted_role_keys` and nothing at all once the grant is revoked. The granting organization gained a read-only view of its own grants' rows in the same change, which is the ordering T4-2 demanded.
- **Widening** is refused on every path that can write the row, including the direct-grant `PATCH` — the trigger fires on `role_keys`, which it did not before `P4-02` (`MEMORY/records/2026-09-17-P4-02-delegated-user-grants.md`).
- **Cross-organization administration** is refused by scope, not by hierarchy (T4-4, `PG-44`).
- **Enumeration** of organizations through `granted_org_id` is answered with one refusal for unknown, deleted, suspended and self (`P4-01` A-5).

## Verification

- `backend/internal/projectgrant/projectgrant_integration_test.go`, `delegated_integration_test.go`, `owners_integration_test.go` — lifecycle, subset, revocation, standing, isolation and the revoke/assign race.
- Owner-connection tests prove each trigger rule holds for writers that are not the handler.
- Mutation runs: 4 (P4-01), 8 (P4-02), 6 (P4-03) — each control broken in turn, each turning a test red.
- Staging smoke runs are recorded in the P4-02 and P4-03 records.

## Not Yet Built / Open Questions

- **Cross-organization sign-in** (ADR-025): a partner's user still cannot sign in to the granting organization's applications, so no live path issues a token carrying a delegated role. The claim code is built and tested; the sign-in is not.
- `P4-06`: the receiving organization's console screen.

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md` Part C; `MEMORY/specs/P4-01-project-grants.md`, `P4-02-delegated-user-grants.md`, `P4-03-project-grant-owner.md`.
- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`, `docs/MULTI-TENANCY/`, `docs/API/13-PROJECT-GRANT-API.md`.

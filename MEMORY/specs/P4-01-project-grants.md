# P4-01 — Project Grants: Data and Lifecycle

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-01
**Plan refs**: `docs/PLAN/08` Part C, `docs/PLAN/04` § `project_grants`, `docs/PLAN/19` § Worked Example (this feature), `docs/PLAN/05` § Endpoint Structure
**Threat review**: `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` T4-1, T4-2, T4-3, T4-4

---

## 0. Scope, stated first because the threat review changed it

This card creates, lists, reads and revokes the **delegation contract**. It does **not**
let the receiving organization assign anything (`P4-02`), put delegated roles in tokens or
`/v1/authz/check` (`P4-04`), or let a partner's user sign in to the granting organization's
applications.

That last item is **T4-1**, and it is not decided. `docs/PLAN/08` Part C does not say
whose MFA mandate or permitted sign-in methods govern a partner's user, and today's code
refuses a session from another organization outright. The decision is surfaced to the
project owner. It must be made before `P4-02` and `P4-04`, which is why this card stops
at the contract.

A grant created by this card therefore **confers no access by itself**. It is safe to ship
alone: the worst a wrong grant does before `P4-02` is list a project to an organization
that cannot yet act on it.

## 1. Business Objective

A project owner lends a project to a partner organization with a restricted subset of its
roles, so the partner can later manage its own people's access without the owner
administering them.

## 2. Actors

- **Granting organization's `PROJECT_OWNER` (or above).** Creates, lists and revokes.
- **Receiving organization's administrators.** Can see that a grant exists and which role
  keys it carries: read-only, through the receiving side's own route in `P4-06`. Not
  through the granting project's route.

## 3. Functional Requirements

| # | Requirement |
|---|---|
| F-1 | `POST /v1/organizations/{org_id}/projects/{project_id}/grants` with `granted_org_id` and `role_keys` creates an active grant |
| F-2 | `GET …/grants` lists the project's grants, active and revoked, newest first, paginated |
| F-3 | `GET …/grants/{grant_id}` reads one |
| F-4 | `DELETE …/grants/{grant_id}` revokes: status `revoked`, `revoked_at` set, row kept |
| F-5 | Every key in `role_keys` must be a role that exists in **this** project |
| F-6 | A self-grant (granted org = this org) is refused |
| F-7 | At most one **active** grant per (project, receiving organization). A second is `409`. Widening means revoking and re-granting (F-9) |
| F-8 | Revoking an already revoked grant is `204` and writes nothing. An idempotent success, audited once |
| F-9 | **No update endpoint.** `granted_role_keys` cannot be changed in place, so a delegation cannot be widened silently (abuse case) |
| F-10 | Deleting a role an **active** grant carries is refused with `409` naming the grant count. A revoked grant does not block it |
| F-11 | The receiving organization must exist and not be soft-deleted |

## 4. Non-Functional Requirements

- **Revocation propagation is `P4-04`'s**, but the lifecycle has to allow it:
  - revocation writes one row;
  - it records `revoked_at`;
  - it records the revoking actor in the audit event.

  `P4-04` needs "by grant, not by user" invalidation (T4-3). This card contributes the
  `id` that key needs, and does no per-user scan.
- **List latency** is within the Management API targets (`docs/PLAN/12`: p95 under 300ms).

## 5. Dependencies

`P2-02` (roles), `P2-05` (manager hierarchy, `PROJECT_OWNER`), `P2-08` (multi-organization).
The table and its RLS policy exist since `P0-07`.

## 6. Database Changes

The table already exists (`20260908000004`, `20260908000007`), with:

- `project_grants_no_self_grant`;
- `project_grants_revoked_has_timestamp`;
- a unique active grant per (project, granted org);
- the two-sided RLS policy: both sides may see a row, and only the granting side may
  write one.

Changes:

1. **`org_must_match_project` for `project_grants`.** A trigger ensures `granting_org_id`
   owns `project_id`, so a row cannot name another organization's project under the
   caller's own tenant. The generic function from `20260912000024` is reused if its
   signature fits; otherwise a sibling is added.
2. **Grants stay mutable only in the revoke direction.** A trigger refuses any `UPDATE`
   that changes `project_id`, `granting_org_id`, `granted_org_id` or `granted_role_keys`,
   or that moves `status` from `revoked` back to `active`. The API has no update path
   (F-9). The trigger makes that a property of the data rather than of the handler, so a
   future handler cannot add one by accident.
3. **Role deletion** (F-10) is a query in `role.Store.Delete`, run in the granting tenant,
   where RLS shows the row.

All additive. A previous-version instance never writes `project_grants`.

## 7. API Contract

In `openapi/openapi.yaml`, tag `Project Grants`. The request field is `role_keys`, as in
`docs/PLAN/08`; the response carries `granted_role_keys`, the column's name:

```
POST   /v1/organizations/{org_id}/projects/{project_id}/grants      201 ProjectGrant
GET    /v1/organizations/{org_id}/projects/{project_id}/grants      200 ProjectGrantList
GET    /v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}  200 ProjectGrant
DELETE /v1/organizations/{org_id}/projects/{project_id}/grants/{grant_id}  204
```

`ProjectGrant`: `id`, `project_id`, `granting_org_id`, `granted_org_id`, `granted_org_name`,
`granted_role_keys`, `status`, `created_at`, `revoked_at`.

`granted_org_name` is shown because a UUID is not something a person can check before
revoking. The receiving organization row is not visible under the granting tenant's RLS,
so the name is resolved through an organization-name lookup that reveals the name of an
organization only once a grant to it exists. A caller cannot enumerate organizations by
guessing ids, because F-11's refusal is identical for "does not exist" and "not permitted".

## 8. Frontend Changes

None in this card. The Project Grants tab is `P4-05`.

## 9. Backend Changes

A new `internal/projectgrant` package with a store and a handler implementing the generated
strict-server interface, the same shape as `internal/role`. `role.Store.Delete` gains the
active-grant check. Policy table rows are added. Audit event types are added.

## 10. Authorization Rules

All four routes require `PROJECT_OWNER` at `ScopeProject`, satisfied by `ORG_OWNER`,
`ORG_ADMIN` and `INSTANCE_OWNER` through the existing inheritance (`P2-05`). A
`PROJECT_GRANT_OWNER` of the receiving side gets **nothing** on these routes: they are the
granting side's.

Because RLS lets a receiving organization *see* a grant row, the handler must never infer
authority from visibility. Every query filters on
`granting_org_id = <path org> AND project_id = <path project>`. A receiving organization
administrator who calls the granting project's route with their own organization in the
path is refused by project scoping, since the project is not theirs, before any grant
query runs.

## 11. Validation

| Field | Rule | Error |
|---|---|---|
| `granted_org_id` | UUID; not this organization; an existing, non-deleted organization | `400 VALIDATION_ERROR` (field `granted_org_id`), one message for missing and self |
| `role_keys` | 1–50 keys, no duplicates, each matching the role-key pattern and existing in this project | `400`, naming the unknown keys |
| Body | No unknown fields (`additionalProperties: false`) | `400` |

## 12. Error Handling

- `404` when the project or grant is not visible under the caller's scope.
- `409 CONFLICT` for a second active grant to the same organization, and for deleting a
  role an active grant carries.
- Revocation of an unknown grant id is `404`. Revoking an already revoked grant is `204`.

## 13. Edge Cases

- **A role renamed:** keys are immutable (`P2-02`), so none.
- **A role deleted after a grant was revoked:** allowed. The revoked row keeps the key as
  history.
- **The receiving organization soft-deleted after the grant:** the grant stays and is
  listed as it was. `P4-04`'s readers must treat it as inactive, and that is recorded for
  `P4-04`.
- **Two concurrent creates to the same organization:** the partial unique index decides.
  The loser gets `409`, not `500`.
- **Revoke racing a create:** independent rows. No shared state.

## 14. Abuse Cases (to test)

| # | Scenario | Control |
|---|---|---|
| A-1 | Create a grant on a project the caller does not own (`docs/SECURITY/02` §3) | Project scope in the policy table, plus the ownership trigger |
| A-2 | Grant a role that does not exist, or one from a different project | F-5, validated against this project's roles in the same transaction |
| A-3 | Widen `granted_role_keys` after creation | No update route (F-9), and the immutability trigger refuses it even from the owner connection |
| A-4 | The receiving organization writes or revokes a grant row it can see | RLS `WITH CHECK` on the granting side, plus the handler's path filter. Tested with a receiving administrator's token |
| A-5 | Enumerate organizations through `granted_org_id` | One refusal for a missing and a forbidden organization, and no name for an organization without a grant |
| A-6 | Reactivate a revoked grant | The trigger refuses `revoked` to `active` |
| A-7 | Delete a delegated role to break the partner's access unnoticed, or to leave a dangling key | F-10: refused while active, with the count |
| A-8 | Revocation not propagating to already-issued access | **`P4-04`.** Recorded here so it is not believed done |

## 15. Logging / Audit

`project_grant.created` records the grant id, project, receiving organization and role
keys. `project_grant.revoked` records the grant id, project, receiving organization, the
role keys that were delegated, and `revoked_at`. Both carry the actor, and both sit in the
**granting** organization's audit log. The receiving organization's audit visibility is
decided with `P4-06`.

## 16. Security Controls

- Server-side authorization at `ScopeProject`.
- RLS two-sided read and granting-side write.
- Immutability and ownership triggers.
- No update path.
- Idempotent revocation.
- No `SECURITY DEFINER` except the organization-name lookup, which is bounded to
  organizations with a grant from the caller's organization.

## 17. Testing Strategy

- **Integration, through the real `/v1` chain:** every F and A row, with three
  organizations. The third must see nothing, per T4-3.
- **Migration:** the triggers are tested from the owner connection. A trigger that only the
  application role hits proves less than one that holds for every writer.
- **Security map:** add rows to `tests/security/isolation_test.go`. The coverage-map test
  keeps their names honest.

## 18. What this task cannot finish

- Delegated user grants and subset validation on every request (`P4-02`).
- Revocation propagation to tokens and checks (`P4-04`).
- Cross-organization sign-in (**T4-1**, undecided).
- The console (`P4-05`, `P4-06`).

## 19. Rollback Strategy

The routes are additive and the triggers only refuse writes no path makes. Rolling back
the application leaves grant rows as inert data, which is exactly what they are until
`P4-02`.

## 20. Technical Risks

- **R-04** (privilege escalation via delegation). Mitigated here by immutability and
  by granting-side-only writes. The larger part of R-04 lives in `P4-02` and `P4-04`.

# P4-02 — Delegated User Grants with Subset Validation

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-02
**Plan refs**: `docs/PLAN/08` Part C (API for managing delegation), `docs/PLAN/04` § `user_grants`, `docs/PLAN/09` § Delegation abuse, `CLAUDE.md` non-negotiable 3
**Threat review**: `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` T4-2, T4-3, T4-4
**Decisions**: ADR-025 (stricter of both policies, for `P4-04`), ADR-026

---

## 0. Scope

This card is the **write path**. The receiving organization assigns, replaces, lists and
removes delegated roles for its own users, through a grant it received. Every write is
validated against the grant as it stands at that moment, in the same transaction, under a
lock that serializes it with revocation.

It does **not** make a delegated role grant access:

- the token claims (`grant.TokenClaims.ForToken`) and `/v1/authz/check`
  (`authz.readRoleKeys`) read under one tenant, where RLS hides every delegated row: from
  the granting tenant because `org_id` is the receiving organization, and from the
  receiving tenant's own applications because the project is not theirs;
- cross-organization sign-in is refused outright today.

Both change in `P4-04`, which must add the reader-side join that T4-2 requires
(`role_keys ∩ granted_role_keys`, and nothing unless the grant is active) **in the same
change that makes the rows reachable**. Recorded in §18 so it is not believed done here.

## 1. Business objective

A partner organization's administrator gives their own staff the roles a vendor delegated
to them, without the vendor administering the partner's people (`docs/PLAN/08` Part C,
the Procurement Portal scenario).

## 2. Actors

- **Receiving organization's `ORG_ADMIN` / `ORG_OWNER`** (and `INSTANCE_OWNER`): assign,
  replace, list and remove. `P4-03` adds `PROJECT_GRANT_OWNER` at grant scope.
- **Granting organization's administrators**: **nothing** on these routes. They lend the
  project; they do not act on another organization's people (T4-4's confused deputy).
- **A third organization**: nothing, and cannot see that the rows exist.

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | `POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants` `{user_id, role_keys}` creates a delegated grant: `org_id` = the receiving organization, `project_id` = the grant's project, `project_grant_id` = the grant |
| F-2 | `GET …/user-grants` lists the delegated grants made through this grant, by the receiving organization |
| F-3 | `PATCH …/user-grants/{user_id}` `{role_keys}` replaces that user's delegated roles |
| F-4 | `DELETE …/user-grants/{user_id}` removes them |
| F-5 | On **every** create and replace: the grant exists, was granted **to the path organization**, is `active`, and every requested key is in its `granted_role_keys` |
| F-6 | The user belongs to the receiving organization |
| F-7 | A refused key is named, and the refusal lists what the grant does delegate |
| F-8 | A user who holds a delegated grant under a **revoked** grant for the same project may be assigned through a new active grant: the stale row is replaced, and the replacement is audited as such |
| F-9 | A caller cannot assign delegated roles to themselves (as `P2-03`) |
| F-10 | Removal is allowed under a revoked grant (cleanup); list is allowed under a revoked grant (history) |

## 4. Non-functional

- One transaction per request; the grant row is read `FOR SHARE`, so a concurrent
  revocation (`FOR UPDATE` in `projectgrant.Store.Revoke`) either commits first and is seen,
  or waits for the assignment to commit.
- Management API latency targets (`docs/PLAN/12`).

## 5. Dependencies

`P4-01` (grants, lifecycle, two-sided RLS), `P2-03` (user grants and the designed slot).

## 6. Database changes (migration 037)

**Which `org_id` a delegated row carries** (T4-3's first ask): the **receiving**
organization. The row is that organization's statement about its own user. It lives under
its tenant and is administered there. The existing `user_grants_tenant_isolation` policy
already bounds it to that tenant, and a third organization sees nothing.

1. **`user_grants_delegation_is_not_yet_implemented()`'s body is replaced**, as `P2-03`
   designed. The name stays, because renaming it would mean dropping it. It now:
   - refuses any `UPDATE` that changes `project_grant_id`, in either direction, so a direct
     grant cannot borrow a delegation's scope and a delegated one cannot shed its grant;
   - passes a direct row (`project_grant_id IS NULL`) through untouched;
   - for a delegated row, reads the grant `FOR SHARE` under the caller's RLS and refuses
     each case with its own message:
     - the grant is not visible;
     - it is not `active`;
     - `granted_org_id` is not `NEW.org_id`;
     - `project_id` is not `NEW.project_id`;
     - a key is not in `granted_role_keys` (named);
     - the user is not in `NEW.org_id`.
   The trigger is recreated to fire on `INSERT` and on
   `UPDATE OF project_grant_id, role_keys, org_id, project_id, user_id`. Previously it
   fired on `project_grant_id` only, and a delegated row's `role_keys` could have been
   widened by an `UPDATE`.
2. **`user_grants_roles_must_exist()` skips delegated rows.** The granting project's roles
   are invisible under the receiving tenant. The grant's keys were validated against those
   roles when it was created, and `P4-01`'s F-10 refuses deleting a role an active grant
   carries.
3. **`org_must_match_project()` skips `user_grants` rows with a `project_grant_id`.** The
   delegation trigger checks the grant's project and organization instead, which is the
   stricter check.
4. **No new RLS policy, and no `SECURITY DEFINER`.** T4-3 asks for a granting-side read
   policy. Adding it here would make delegated rows visible to the granting tenant's
   readers before those readers join the grant, so a revoked grant would keep serving
   roles. It moves to `P4-04`, together with the join. The granting side learns the blast
   radius through `holder_count` (`P4-05`).

Additive: function bodies are replaced and one trigger is recreated inside the migration's
transaction. A previous-version instance never writes a non-null `project_grant_id`; it
could not, because the old body refused it.

## 7. API contract

Tag `Project Grants`. Paths as `docs/PLAN/08` Part C writes them:

```
GET    /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants              200 GrantList
POST   /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants              201 Grant
PATCH  /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}    200 Grant
DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}    204
```

`Grant` gains `project_grant_id` (nullable), which is what the console's role-source badge
reads.

## 8. Frontend

None here. `P4-06` builds the receiving side's screen. The User Grants tab (`P2-12`)
already renders a delegated badge when `project_grant_id` is set, and it now arrives.

## 9. Backend

The four operations live on `projectgrant.Handler`, beside the grant they act through. The
store gains `Received` (lock and read a grant as the receiving side) and the delegated
create, replace, list and remove. `grant.Store`'s direct routes stay unchanged. A
receiving administrator's direct `PATCH` of a delegated row is still validated by the
trigger.

## 10. Authorization

Policy table: `OrgAdmin` at `ScopeOrganization` for all four; the organization in the path
is the receiving one. Every query filters on `granted_org_id = <path org>`: the two-sided
RLS would show the grant to its granting organization too, and visibility is not authority
(`P4-01`'s lesson). A granting administrator calling with their own organization in the
path finds no grant: 404.

## 11. Validation

| Field | Rule | Error |
|---|---|---|
| `user_id` | UUID; a user of this organization | `404` (one answer for "not here" and "does not exist") |
| `role_keys` | 1–64, no duplicates, pattern; **each in the grant's `granted_role_keys`** | `400`, naming the keys not delegated and listing those that are |
| grant | granted to this organization, `active` | `404` / `409` "revoked" |

## 12. Error handling

- `404`: no such grant for this organization, or no such user.
- `409`: the grant is revoked (create and replace only); or the user already holds a
  delegated grant under this same active grant (use `PATCH`).
- `403`: self-assignment.

## 13. Edge cases

- **Re-granting after a revoke** (F-8): the new grant has a new id; the user's stale row
  under the revoked grant is deleted and a new one inserted in the same transaction.
- **The user is deactivated**: the assignment is still allowed, matching direct grants.
  Deactivation ends access at sign-in.
- **A direct grant for the same (user, project)**: impossible. A direct row's `org_id` must
  own the project, and the user belongs to another organization.
- **The receiving organization is suspended**: the Management API refuses its tenant
  already.

## 14. Abuse cases (to test)

| # | Scenario | Control |
|---|---|---|
| A-1 | Assign a role outside `granted_role_keys` | Handler subset check, and the trigger for any writer |
| A-2 | Assign through a revoked grant | `status` read `FOR SHARE` on every write |
| A-3 | Assign to a user in a third organization, or the granting org's own user | `requireUser` under the receiving tenant, and the trigger's user check |
| A-4 | Revocation racing an assignment | `FOR SHARE` against `FOR UPDATE`: an assignment that waits sees the revocation |
| A-5 | Set `project_grant_id` on a direct grant to borrow a delegation | Trigger: `project_grant_id` is immutable, and `NEW.org_id` must be the grantee |
| A-6 | Widen a delegated row's `role_keys` by `UPDATE`, through the direct `PATCH` or by SQL | The trigger now fires on `role_keys` |
| A-7 | Granting org administrator acting on the partner's people | Path organization scope and the `granted_org_id` filter: 404 |
| A-8 | "Narrow a grant, then assign under the old set" | Grants cannot change (`P4-01`). Narrowing is revoke and re-grant: the old grant refuses as revoked, the new one refuses the dropped key |
| A-9 | A third organization reads delegated rows | Tenant RLS; `tests/security/isolation_test.go` |

The task card's Definition of Done asks for a test that "narrows `granted_role_keys` after
a grant exists". No path can do that since `P4-01`'s trigger. A-8 is the same property,
stated as the operations that exist.

## 15. Logging / audit

`delegated_role.assigned`, `delegated_role.replaced` and `delegated_role.removed`, written
in the receiving organization's log. Each payload carries `grant_id`, `project_id`,
`granting_org_id`, `granted_org_id`, `subject_user_id` and the exact roles: added and
removed, or the full set on removal. F-8 records `superseded_grant_id`.

## 16. Security controls

- Subset, active and grantee checks, repeated on every write in the handler and in the
  database for any writer.
- Row lock against revocation.
- `project_grant_id` immutable.
- No `SECURITY DEFINER`, no tenant switch, no new RLS policy.

## 17. Testing

- Integration tests through the real `/v1` chain with three organizations: every F and A
  row. Every trigger rule is also tested from the owner connection.
- `P2-03`'s guard test is **inverted**, not deleted.
- `P4-05`'s holder-count test drops its trigger bypass and uses a real delegated
  assignment.
- `tests/security/isolation_test.go` gains the delegated rows.
- Mutations: remove the subset check (handler and trigger separately), the active check,
  the grantee filter and the row lock. Each must turn a test red.

## 18. What this card cannot finish

- Delegated roles in tokens and `/v1/authz/check`, and the reader join (T4-2): `P4-04`.
- The granting-side read policy on delegated rows (T4-3): `P4-04`, with the join.
- Invalidation by grant: `P4-04`.
- `PROJECT_GRANT_OWNER`: `P4-03`.
- The receiving side's screen: `P4-06`.

## 19. Rollback

The previous application does not call these routes. Rolling back the migration restores
the refusal. Delegated rows written in between would then be refused on any `UPDATE`, and
remain as inert data, as they are to every reader.

## 20. Risks

**R-04, privilege escalation via delegation.** The controls here are defence in depth on
the write path. The largest remaining piece is the reader join in `P4-04`, and nothing in
this card may make delegated rows readable before it.

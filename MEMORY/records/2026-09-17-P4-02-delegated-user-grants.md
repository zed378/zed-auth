# P4-02 — Delegated User Grants with Subset Validation

| | |
|---|---|
| **Date** | 2026-09-17 |
| **Task** | `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-02 |
| **Phase** | Phase 4 — Enterprise Interop |
| **Surface** | backend + Management API |
| **Branch** | `feat/P4-02-delegated-user-grants` |
| **Status** | Complete — the write path, by design |

**Spec**: [`MEMORY/specs/P4-02-delegated-user-grants.md`](../specs/P4-02-delegated-user-grants.md)

---

## What shipped

The receiving organization assigns, replaces, lists and removes the roles a Project Grant
delegates, for its own users:

```
GET/POST      /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants
PATCH/DELETE  /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants/{user_id}
```

- **Every create and replace** reads the grant `FOR SHARE`, in the same transaction, and
  checks four things: the grant was made **to** the path organization, it is `active`,
  every key is in `granted_role_keys`, and the user is that organization's own.
- **A refusal names** the keys the grant does not delegate and lists what it does.
- **`Grant.project_grant_id`** is now rendered, so the receiving organization's view of a
  user shows which roles are delegated.
- **Audit:** `delegated_role.assigned` / `.replaced` / `.removed` are written in the
  receiving organization's log, with both organizations, the grant, the user and the exact
  roles.

## The database holds the rule for every writer

`P2-03` closed `user_grants.project_grant_id` with a trigger whose body Phase 4 was to
replace. Migration 037 replaces it. The delegation check now refuses:

- a grant that is not visible;
- a revoked grant;
- a row filed under an organization other than the grantee;
- a project other than the grant's;
- a key the grant does not delegate;
- a user outside the organization;
- any change to `project_grant_id` itself, in either direction.

The trigger fired only on `UPDATE OF project_grant_id`. A delegated row's `role_keys` could
therefore have been widened by any `UPDATE`, including the existing direct-grant `PATCH`.
The trigger now fires on every column the rule reads. A test widens a delegated row through
the direct `PATCH` and from the owner connection, and both are refused.

The two older triggers skip delegated rows: role existence and org/project agreement. The
granting project and its roles are invisible under the receiving tenant, and the delegation
check is the stricter statement.

## Decisions made here

- **A delegated row's `org_id` is the receiving organization** (T4-3's first ask). The
  existing tenant policy bounds it, and a third organization sees nothing.
- **No granting-side read policy yet.** T4-3 asks for one. Adding it now would make
  delegated rows visible to readers that do not yet join the grant, so a revoked grant
  would keep serving roles (T4-2). It moves to `P4-04`, together with the join. A test
  pins the current state: A and C see zero rows.
- **"Narrow a grant, then assign" is revoke and re-grant.** The task card's Definition of
  Done asks for a test that narrows `granted_role_keys` in place. No path can do that since
  `P4-01`. The test checks the same property with the operations that exist: the dropped
  role is refused through the new grant, and the old grant refuses as revoked.
- **Re-assignment across a re-grant replaces the stale row.** A user who held roles under a
  revoked grant is assigned through the new one. The stale row is deleted in the same
  transaction, and the event records `superseded_grant_id`.
- **The list is paginated.** Found the same week that the console's unpaginated lists hid
  users past 100.

## Verification

| Check | Result |
|---|---|
| `internal/projectgrant` integration, 9 new tests | pass |
| `internal/grant`: P2-03's guard test **inverted**, not deleted | pass |
| `P4-05`'s holder count now uses a real delegated assignment, with no trigger bypass | pass |
| Mutations (8) | all red: handler subset, trigger subset, trigger active, `granted_org_id` filter, both row locks, trigger events, `project_grant_id` immutability, member check |
| `tests/security/isolation_test.go` | P4-02 row moved from "not yet testable" to eight named tests |

The handler's own active check has no mutation of its own: with it removed, the trigger
still refuses and the API still answers 409. That is the defence in depth working. The
trigger's active check is mutated and caught.

## On staging

Deployed 2026-09-17. Backup `staging-20260917T081635Z.dump` was taken first, migration 037
was applied as the owner role, and image `zed-auth:p4-02` came up healthy.

A smoke run against the live service created a throwaway vendor administrator and a
throwaway partner organization, with its own administrator, application and staff user.
Both administrators signed in through the hosted page. 8 of 8 checks passed:

- the vendor delegated one role;
- the partner assigned it to its own user;
- widening to an undelegated role was refused, naming the role and what is delegated;
- the vendor was refused on its own path;
- `holder_count` read 1;
- after revocation, the partner's change was refused with 409;
- cleanup under the revoked grant succeeded.

Everything was removed afterwards: 0 partner organizations and 0 smoke roles left.

The first rollout attempt stopped silently after the migration. `docker compose run` read
the rest of the deploy script from stdin, so the old image kept serving the new schema
until the rollout was re-run. That is harmless for an additive migration, and it is
recorded in the deploy notes.

## Not done here

- Delegated roles in tokens and `/v1/authz/check`, the reader-side join, the granting-side
  read policy, and invalidation by grant: `P4-04`. **Nothing here makes a delegated row
  readable by a reader.**
- `PROJECT_GRANT_OWNER`: `P4-03`.
- The receiving side's screen, and the granting organization's name on that side: `P4-06`.
- The stale trigger message "until P4-01" is gone with the body that carried it.

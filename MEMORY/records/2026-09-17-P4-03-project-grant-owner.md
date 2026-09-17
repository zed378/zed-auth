# P4-03 — `PROJECT_GRANT_OWNER` Enforcement

| | |
|---|---|
| **Date** | 2026-09-17 |
| **Task** | `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-03 |
| **Phase** | Phase 4 — Enterprise Interop |
| **Surface** | backend + Management API |
| **Branch** | `feat/P4-03-project-grant-owner` |
| **Status** | Complete |

**Spec**: [`MEMORY/specs/P4-03-project-grant-owner.md`](../specs/P4-03-project-grant-owner.md)

---

## What shipped

- **The fifth manager role is live.** `PROJECT_GRANT_OWNER` is scoped to one Project
  Grant. It reaches the four delegated-role routes of that grant, for the users of the
  organization the grant was made to, while the grant is active. It reaches nothing else.
- **`ScopeProjectGrant`**, a new requirement scope. It is satisfied by a
  `PROJECT_GRANT_OWNER` row whose `scope_id` is the grant in the path, held by a member of
  the path organization, or by an organization role over the path organization.
- **`GET/POST …/project-grants/{grant_id}/owners`** and **`DELETE …/owners/{user_id}`**.
  These are the first API in the service that writes a manager role. They require
  `ORG_ADMIN` of the receiving organization, an active grant and a member, and refuse
  self-appointment. They are audited as `manager_role.assigned` / `.revoked`, with both
  organizations and the grant.

## T4-4, answered by scope

The plan's diagram puts the role under `PROJECT_OWNER`. Across organizations that would let
a vendor administer a partner's people. The answer, recorded as `PG-44`:

- `PROJECT_OWNER` does **not** satisfy `PROJECT_GRANT_OWNER`;
- the receiving organization's administrators do;
- only they appoint.

## A bug the exhaustive table found

When the role went live, `TestTheHierarchyExhaustively` disagreed on four rows. The
project-scope branch of `Authorize` matched `g.ScopeID == target.ProjectID` without naming
the role. A `PROJECT_GRANT_OWNER` row whose `scope_id` equalled a project id therefore
passed that project's requirements. One of the four rows was `MEMBER` over a project in
**another** organization.

A grant id and a project id are both random UUIDs, so a collision is not a realistic
exploit. But `manager_roles.scope_id` has no foreign key and no RLS, and a row written
wrongly by SQL would have been honoured. Both scope loops now skip a
`PROJECT_GRANT_OWNER` row. Its only path is the grant-scope check, which names the role.

## Tests inverted, not deleted

`P2-05` pinned the role as reserved in three tests:

- "satisfies nothing yet";
- "a requirement for it refuses";
- the hierarchy-matches-the-plan table.

Each now states the live rule. The exhaustive table gained the role, the scope and grant
targets. Its expectation is written from the spec, not from `satisfies`. It checks 23,520
combinations.

## Verification

| Check | Result |
|---|---|
| `internal/management` unit tests, including the exhaustive table | pass |
| `internal/projectgrant` integration, 5 new owner tests plus all of P4-01, P4-02 and P4-05 | pass |
| Mutations (6) | all red: grant `scope_id` equality, token organization check, revoked grant refuses a grant owner, `PROJECT_OWNER` satisfying the role, the project-loop skip, owners routes requiring `ORG_ADMIN` |
| `tests/security` coverage map | four named tests added under Phase 4 |

## Gaps recorded, not built

- **Owner notification.** `docs/SECURITY/02` §3 expects manager-role writes to be rare
  and alerted on. The events are distinct, but the service has no channel for notifying an
  organization's owners. The only notifier is the anomaly one, and it is off by default.
  This belongs to Phase 5's alerting work.
- **`PG-31` remains open for the other four roles.** This card writes
  `PROJECT_GRANT_OWNER` only; `ORG_OWNER`, `ORG_ADMIN` and `PROJECT_OWNER` are still
  assigned by SQL.
- **The console** for appointing owners, and a grant owner's own view: `P4-06`.

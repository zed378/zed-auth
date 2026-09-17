# P4-03 — `PROJECT_GRANT_OWNER` Enforcement

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-03
**Plan refs**: `docs/PLAN/08` Part C § Manager Role Hierarchy, `docs/PLAN/04` § `manager_roles`, `docs/SECURITY/02` §3
**Threat review**: T4-4 (manager-role inheritance crosses TB-4)

---

## 1. Business objective

The receiving organization gives one person — typically the partner's team lead — the
right to assign the delegated roles of **one** grant, and nothing else. `docs/PLAN/08`'s
Procurement Portal scenario: "let the vendor's own admin (`PROJECT_GRANT_OWNER`) assign it
to their own staff".

## 2. Actors

- **Receiving organization's `ORG_ADMIN` / `ORG_OWNER`** (and `INSTANCE_OWNER`): assign and
  remove a grant's owners, and do everything an owner can.
- **`PROJECT_GRANT_OWNER`** of grant G: list, assign, replace and remove delegated roles
  under G, for users of their own organization. Nothing else.
- **The granting organization's roles** (`ORG_OWNER`, `ORG_ADMIN`, `PROJECT_OWNER`):
  **nothing** on the receiving side, including assigning a `PROJECT_GRANT_OWNER`.

## 3. Decision on T4-4's inheritance question

`docs/PLAN/08` draws `PROJECT_GRANT_OWNER` beneath `PROJECT_OWNER`. Read literally, the
granting organization inherits the receiving organization's delegated administration. That
is a confused deputy. It is answered by **scope**, not by the hierarchy diagram:

- A `PROJECT_GRANT_OWNER` requirement is scoped to **the grant and the organization it was
  granted to** (`ScopeProjectGrant`). It is satisfied by a `PROJECT_GRANT_OWNER` row whose
  `scope_id` is that grant, or by an organization role whose `scope_id` is the
  **receiving** organization.
- `PROJECT_OWNER` does **not** satisfy `PROJECT_GRANT_OWNER`. A project owner's
  `scope_id` is the granting organization's project, which is never the grant or the
  receiving organization. Leaving it out of the table as well means a future scope mistake
  cannot turn it on.
- The granting organization's `ORG_*` roles are scoped to the granting organization. That
  is not the path organization, so they are refused. This is the same answer `P4-02`
  already gives.

This is a reading of the plan, not a change to it. "Permissions flow downward" within one
organization; across organizations, scope decides. Recorded in `TASKS/BACKLOG.md` as a
one-sentence clarification for `docs/PLAN/08` Part C.

## 4. Functional requirements

| # | Requirement |
|---|---|
| F-1 | `ScopeProjectGrant`: the route's `grant_id` and `org_id` form the target. A `PROJECT_GRANT_OWNER` row satisfies it only when its `scope_id` equals the grant id **and the caller's token belongs to the path organization** |
| F-2 | The four `P4-02` routes require `PROJECT_GRANT_OWNER` at `ScopeProjectGrant`. Organization roles over the path organization satisfy it |
| F-3 | A `PROJECT_GRANT_OWNER` acts only while its grant is **active and granted to the path organization**. The handler refuses such a caller otherwise, as not found. A revoked grant, re-granted under a new id, does not revive the old row, because the id differs (T4-4) |
| F-4 | `GET/POST /v1/organizations/{org_id}/project-grants/{grant_id}/owners` and `DELETE …/owners/{user_id}` list, assign and remove the grant's owners. They require `ORG_ADMIN` over the path (receiving) organization |
| F-5 | Assignment requires an active grant made to the path organization, and a user of that organization. The caller cannot assign the role to themselves |
| F-6 | Assigning and removing are audited as `manager_role.assigned` / `manager_role.revoked`, naming the role, the grant, both organizations and the subject |
| F-7 | A `PROJECT_GRANT_OWNER` reaches no other route: not the project, its roles, its applications, the grant itself, users, sessions, policies or the audit log |

## 5. Database

No migration. `manager_roles` already accepts `PROJECT_GRANT_OWNER`, and `scope_id` has no
foreign key by design: it names different kinds of thing per role. The handler validates the
grant under the receiving tenant's RLS before writing. `manager_roles` has no RLS (recorded
since `20260908000007`), so every read and write filters on role **and** `scope_id` in the
same predicate.

## 6. API

```
GET    /v1/organizations/{org_id}/project-grants/{grant_id}/owners            200 ProjectGrantOwnerList
POST   /v1/organizations/{org_id}/project-grants/{grant_id}/owners            201 ProjectGrantOwner  { user_id }
DELETE /v1/organizations/{org_id}/project-grants/{grant_id}/owners/{user_id}  204
```

`ProjectGrantOwner`: `user_id`, `grant_id`, `created_at`.

## 7. Authorization truth table (added to `P2-05`'s exhaustive test)

Rows are callers. Columns are the requirement `PROJECT_GRANT_OWNER` at `ScopeProjectGrant`,
under grant G (A→B), with the path organization as noted.

| Caller | path B, grant G | path B, grant G2 (another grant to B) | path A, grant G |
|---|---|---|---|
| A `ORG_OWNER` | refused | refused | allowed by scope match on A* |
| A `ORG_ADMIN` | refused | refused | allowed by scope match on A* |
| A `PROJECT_OWNER` (G's project) | refused | refused | refused |
| B `ORG_OWNER` | allowed | allowed | refused |
| B `ORG_ADMIN` | allowed | allowed | refused |
| B `PROJECT_GRANT_OWNER` on G | allowed | refused | refused |
| B `PROJECT_GRANT_OWNER` on G, token of org C | refused | refused | refused |
| `INSTANCE_OWNER` | allowed | allowed | allowed |

\* The path-A column is allowed by `Authorize` and then refused by the handler, which finds
the grant only by `granted_org_id = path org` (`P4-02`). Both layers are tested. The
table covers `Authorize`; the endpoint tests cover the handler.

## 8. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | A PGO acts on the granting organization's project, roles or grant (`docs/SECURITY/02` §3) | Those routes require `ProjectOwner` at `ScopeProject`; PGO satisfies nothing there |
| A-2 | A PGO widens its own grant | No route edits a grant; the granting side's routes refuse PGO |
| A-3 | A PGO acts on a different grant in the same organization | `scope_id` must equal the path grant |
| A-4 | A PGO keeps working after revocation, or after re-grant under a new id | Handler refuses a PGO-only caller on a revoked grant; a new id never matches the old `scope_id` |
| A-5 | The granting organization assigns a PGO in the receiving organization | Owner routes require `ORG_ADMIN` over the receiving organization |
| A-6 | A PGO assigns PGO (self-propagation) | Owner routes require `ORG_ADMIN`; PGO does not satisfy it |
| A-7 | A PGO row naming a grant, held by a user of a third organization | F-1: the caller's token organization must be the path organization |

## 9. Audit and alerting

`manager_role.assigned` / `.revoked` at elevated visibility. `docs/SECURITY/02` §3 expects a
manager-role write to be rare and alerted on. There is **no owner-notification channel** in
the service today; the only notifier is the anomaly one, and it is off by default. The
events are distinct and alertable. The notification itself is recorded as a gap for the
Phase 5 alerting work (`P5`), not built here.

## 10. Tests

- `Authorize` truth table, exhaustive, in `hierarchy_exhaustive_test.go`.
- Endpoint tests through `/v1`:
  - a PGO assigns under their grant;
  - a PGO is refused under another grant, on a revoked grant, on every other route family,
    and on the owners routes;
  - a granting administrator is refused on the owners routes;
  - owners can be assigned and removed, and both are audited.
- Mutations:
  - the `scope_id` equality;
  - the token-organization check;
  - the revoked-grant refusal for a PGO-only caller;
  - `ProjectOwner` satisfying PGO, which must be red when it is added.

## 11. Not here

- Delegated roles in tokens and checks: `P4-04`.
- The console for owners: `P4-06`.

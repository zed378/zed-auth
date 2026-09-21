# P4-06 — Granted Projects: the Receiving Side

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-06 (`PF-37`)
**Plan refs**: `docs/UI-UX/04` Flow 3, `docs/UI-UX/08` § Granted Projects list, `docs/PLAN/08` Part C
**Depends on**: P4-01 (the contract), P4-02 (delegated assignment), P4-03 (grant owners), P4-04 (delegated access)

---

## 0. The gap this closes first

The receiving organization can already assign delegated roles — but only if it somehow
**knows the grant id**. Nothing lists the grants made *to* an organization. The granting
side has `GET …/projects/{project_id}/grants`; the receiving side has no equivalent, which
the `docs/API` review flagged independently.

So this card is a small API addition and then the screen Flow 3 describes.

## 1. Functional requirements

| # | Requirement |
|---|---|
| F-1 | `GET /v1/organizations/{org_id}/project-grants` lists the grants made **to** this organization, newest first, paginated |
| F-2 | Each row names the project and the granting organization — neither of which is visible under the receiving tenant's RLS — plus the delegated role keys, status and dates |
| F-3 | A revoked grant stays listed, marked, and offers no assignment |
| F-4 | The console screen lists those grants, and for each active one lets an administrator assign its roles to the organization's own users |
| F-5 | **A role the grant does not delegate is never rendered** (`docs/UI-UX/08`: not shown-disabled, not shown at all) |
| F-6 | Roles assigned this way carry the delegated role-source badge (`docs/UI-UX/06`), naming the granting organization |
| F-7 | A revocation that happened elsewhere is reflected as a clear state, never as an opaque failure on the next action |
| F-8 | The empty state explains what a granted project is |

## 2. Database (migration 039)

One `SECURITY DEFINER` function, the mirror of `P4-01`'s `granted_organization_names`, and
bounded the same way:

```sql
received_grant_context(grant_ids uuid[])
  RETURNS TABLE (grant_id uuid, project_name text, granting_org_name text)
```

It returns a row only for a grant whose `granted_org_id = current_org_id()`. It reveals a
project's name and an organization's name to the organization that was *given* that
project — which is precisely the delegation it already holds — and nothing else. No other
column, no listing, no lookup by name.

## 3. API

```
GET /v1/organizations/{org_id}/project-grants   200 ReceivedGrantList
```

`ORG_ADMIN` at organization scope. `ReceivedGrant`: `id`, `project_id`, `project_name`,
`granting_org_id`, `granting_org_name`, `granted_role_keys`, `status`, `created_at`,
`revoked_at`, `holder_count`.

Filtered on `granted_org_id = <path org>`: the two-sided RLS policy would also show the
grants this organization *made*, and a receiving-side route must not list those (the
"visibility is not authority" lesson from `P4-01`).

## 4. Console

Route `/granted-projects` (today a placeholder), reachable by `ORG_ADMIN` and above.

- **List**: granting organization, project, delegated roles as badges, status, created.
- **Assign**: a side panel per grant — pick one of the organization's own users, tick roles
  **from the grant's `granted_role_keys` only**, confirm. The panel states plainly that
  these roles come from another organization and what that means.
- **Existing assignments** per grant, with remove.
- **Revoked grants**: listed, marked "Ended", no assignment control, with a sentence saying
  the granting organization ended it and the access is already gone.
- **Empty state**: what a granted project is, and that only another organization can create
  one.

## 5. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | The screen offers a role the grant does not delegate | Roles rendered from `granted_role_keys`; server refuses independently (`P4-02`) |
| A-2 | An administrator assigns to a user outside the organization | The user picker lists only this organization's users; the server refuses (`P4-02` F-6) |
| A-3 | The receiving side lists grants it *made* through this route | `granted_org_id` filter |
| A-4 | A third organization reads either side | Two-sided RLS; isolation tests |
| A-5 | Assignment through a grant revoked a moment ago | Server answers `409`; the screen shows the state rather than a raw error |

## 6. Tests

- Integration: the new route (list, filter, pagination, names resolved, third organization
  sees nothing), and that the granting side's own grants are absent from it.
- Console: rendering, that a non-delegated role is absent from the DOM, the delegated
  badge, the revoked state, the empty state, axe.
- E2E: Flow 3 end to end — a grant exists, an administrator assigns a delegated role, and
  the API shows the assignment.
- Mutations: the `granted_org_id` filter, the role list source, the badge.

## 7. Not in this card

Cross-organization sign-in (ADR-025) — the partner's users still cannot sign in to the
granting organization's applications, so the roles assigned here are visible to the
granting organization's `/v1/authz/check` and nowhere else yet.

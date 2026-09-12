# P2-03 — User Grants (User × Project × Roles)

**Task**: `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-03
**Depends on**: `P2-01`, `P1-19`
**Plan refs**: `docs/PLAN/04-DATA-MODEL.md` § `user_grants`, `docs/PLAN/08-AUTHORIZATION.md` Part A and § Least Privilege

---

## 1. Business Objective

`P2-01` defined roles. This is the row that gives one to somebody, and it is the first thing in the system that actually grants access to anything.

It is also the shape Phase 4's delegation has to fit into. `user_grants.project_grant_id` already exists, nullable, and `docs/PLAN/08` Part C is explicit that a delegated grant's `role_keys` must be a **subset** of the delegation's `granted_role_keys`, revalidated on every request. Building the direct case now without leaving that slot open is how the most security-critical check in the system ends up retrofitted into code that was not shaped for it.

## 2. Actors

| Actor | What they do |
|---|---|
| `ORG_ADMIN`, `ORG_OWNER` | Grant and revoke roles for any user in their organization |
| `PROJECT_OWNER` | The same, for their own project (`P2-05`'s scope) |
| A user | Holds grants. Never edits their own — see §14 |
| Phase 4 | Fills the `project_grant_id` slot this task leaves closed |

## 3. Functional Requirements

- **FR-1** One grant row per (user, project), carrying a non-empty set of role keys.
- **FR-2** A new user has **no access at all** until a grant exists. No implicit role, no default.
- **FR-3** Every role key on every write must name a role that exists **in that project**.
- **FR-4** `GET`/`POST`/`PATCH`/`DELETE` under `/v1/organizations/{org_id}/users/{user_id}/grants`.
- **FR-5** A non-null `project_grant_id` is **refused** in this phase, at the database.
- **FR-6** Every assignment and revocation is audited with actor, subject, project and the exact keys.
- **FR-7** Revocation takes effect immediately — the row is gone, not marked.

## 4. Non-Functional Requirements

- **NFR-1** Reading a user's grants is one query, not one per project.
- **NFR-2** The role-key check is a database trigger as well as an application check, because a grant referencing a role that does not exist is a permission that cannot be reasoned about, and the application is not the only writer.

## 5. Dependencies

| Needs | Provided by |
|---|---|
| `roles`, and its rules | `P2-01` |
| Users, and their tenancy | `P1-19` |
| `PROJECT_OWNER` and the project scope | `P2-05` |
| Audit writer and guard | `P0-12`, `P1-15` |

## 6. Database Changes

The table is from `P0-07`: `(user_id, project_id)` unique, `role_keys` non-empty, `project_grant_id` nullable with a foreign key. One additive migration adds the rules.

**Every role key must exist in the project** — a trigger, because it spans two tables. Without it, a grant can name `billing-admin` in a project that has no such role: the row looks like access, the token carries a key nothing defines, and every consumer that checks for it silently denies. A permission hole that reads as a working grant.

**`org_id` must match the project's**, the same invariant and the same reasoning as `P2-01`'s roles trigger: a mismatch files the grant under a tenant that row-level security then hides it from.

**`project_grant_id` must be NULL** — the designed slot. A trigger that refuses any non-null value with a message naming Phase 4, rather than a `CHECK (project_grant_id IS NULL)` that a later migration would have to drop. The difference matters: Phase 4 replaces the trigger's *body* with subset validation, and the call site, the error path and the tests are already where they need to be.

## 7. API Contract

```
GET    /v1/organizations/{org_id}/users/{user_id}/grants
POST   /v1/organizations/{org_id}/users/{user_id}/grants
PATCH  /v1/organizations/{org_id}/users/{user_id}/grants/{project_id}
DELETE /v1/organizations/{org_id}/users/{user_id}/grants/{project_id}
```

The last two are keyed by **project**, not by a grant id. There is exactly one grant per (user, project) — the unique index says so — so the project is the natural identifier, and a separate id would be a second way to name the same row.

`ORG_ADMIN` over the organization. **Not** `PROJECT_OWNER` scoped to the project, despite `P2-02` setting that precedent: the path is `/users/{user_id}/...`, so the route is about a user, and a `PROJECT_OWNER` who could reach it would be able to enumerate the organization's users by asking for each one's grants. `P2-12`'s console screen reaches the same data from the project side; when that needs a project-scoped route it gets one, listing the grants of a project rather than of a user.

## 8. Frontend Changes

None. `P2-12` builds the Authorizations tab.

## 9. Backend Changes

`internal/grant`: a store with no `org_id` parameter, and a handler that owns the audit events.

Validation is deliberately **not** duplicated from `P2-01`: a role key is checked by asking whether the role exists, not by re-running the pattern. Two places that decide what a valid key looks like would drift; one place that asks the database what exists cannot.

## 10. Authorization Rules

`ORG_ADMIN` over the organization in the path, enforced by the policy table.

**A caller may not edit their own grants.** Refused in the handler, because no scope expresses it: an `ORG_ADMIN` legitimately administers every user in the organization, and they are one of those users. Self-service escalation is `docs/SECURITY/02` §3's vertical escalation, and the check is cheap.

## 11. Validation

| Field | Rule |
|---|---|
| `project_id` | Exists in this organization, else 404 |
| `role_keys` | Non-empty; every key names a role in that project; no duplicates |
| `project_grant_id` | Must be absent. Present and non-null is refused |
| `user_id` | Exists in this organization, else 404 |

## 12. Error Handling

A role key that does not exist names **which** key, because "one of your role keys is invalid" sends an operator to guess. A project in another organization is 404, never 403.

## 13. Edge Cases

- **Granting twice for the same project** — the unique index refuses; the caller wanted `PATCH`. Answered as a conflict naming the project.
- **An empty `role_keys` on PATCH** — refused. `docs/PLAN/08` § Least Privilege: a grant with no roles grants nothing and should not exist, so the operation is `DELETE`.
- **Deleting a role that a grant references** — already refused by `P2-01`.
- **A user deleted while grants exist** — cascades, by the existing foreign key. A grant for a user who does not exist is not a thing.

## 14. Abuse Cases

| # | Scenario | Control | Test |
|---|---|---|---|
| A-1 | §3 Self-granting: an `ORG_ADMIN` giving themselves a role | The handler refuses editing your own grants | `TestACallerCannotGrantToThemselves` |
| A-2 | §2 A role from a project the caller does not administer | Tenancy: the project is not visible, so its roles are not either | `TestARoleFromAnotherOrganizationCannotBeGranted` |
| A-3 | §11 Mass assignment through another endpoint's body | `additionalProperties: false`, and no other endpoint accepts `role_keys` | `TestRoleKeysAreNotAcceptedByTheUserEndpoint` |
| A-4 | §3 A grant naming a role that does not exist | Trigger, and an application check with a named key | `TestAGrantCannotNameARoleThatDoesNotExist` |
| A-5 | §3 Phase-4 delegation smuggled in early | The `project_grant_id` trigger | `TestADelegatedGrantIsRefusedUntilPhaseFour` |
| A-6 | Least privilege | A user with no grant has nothing | `TestAUserWithNoGrantHasNoRoles` |

## 15. Logging / Audit Requirements

`role.assigned` and `role.revoked` — both already defined in `internal/audit` and unused until now. Payload: subject user, project, and the exact keys added or removed. On a `PATCH`, the difference, for the same reason `P2-02`'s role update records one.

## 16. Security Controls

The trigger pair (role existence, org/project agreement), RLS, the self-edit refusal, and the closed delegation slot.

## 17. Testing Strategy

Unit for the difference calculation; integration for every trigger, as the **runtime** role; endpoint tests through the whole chain for authorization and audit; and a mutation run reverting each control.

## 18. What this task cannot finish

The card's DoD says "revocation is reflected by `/v1/authz/check` immediately". **That endpoint is `P2-06`.** What is deliverable here is the property underneath — the row is deleted rather than marked, so nothing can read it afterwards — and the end-to-end assertion lands with `P2-06`. Named here rather than ticked.

Step 6 asks whether re-authentication for sensitive grant changes belongs to Phase 2 or Phase 3. **Phase 3**, recorded as a decision: MFA itself is `P3-01`, and a re-authentication prompt with no second factor behind it is a password re-entry, which adds friction without adding assurance. The `amr` claim `P1-11` already emits is what Phase 3 will read.

## 19. Rollback Strategy

The down migration drops the three triggers. Grants written while they were in force remain valid afterwards.

## 20. Technical Risks

**The closed slot is only closed at the database.** If Phase 4 removes the trigger before implementing subset validation, there is a window where a delegated grant can name any role. The trigger's message says so, and the test that asserts refusal is written to be **inverted** rather than deleted, so the Phase 4 diff shows what changed.

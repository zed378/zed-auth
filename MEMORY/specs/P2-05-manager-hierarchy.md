# P2-05 — Manager Role Enforcement, Full Hierarchy

**Task**: `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-05
**Depends on**: `P1-15`
**Plan refs**: `docs/PLAN/08-AUTHORIZATION.md` Part C § Manager Role Hierarchy & Inheritance, `docs/PLAN/04-DATA-MODEL.md` § `manager_roles`

> **On sequence.** `CLAUDE.md` asks for this document before the implementation code. The analysis in §1, §6 and §12 — the reading of Part C, and the two gaps that became `PG-31` and `PG-32` — was done first and is what made the task tractable; the prose was written alongside the code rather than strictly before it. Recording that rather than back-dating it.

---

## 1. Business Objective

`P1-15` shipped three of the five manager roles. `PROJECT_OWNER` and `PROJECT_GRANT_OWNER` existed as constants that deliberately satisfied nothing, so an endpoint requiring either was unreachable.

That becomes a blocker at `P2-02`, whose card requires `PROJECT_OWNER` or higher for the roles API. Implementing it there would mean a slice of the hierarchy living inside a feature task, which is exactly what step 5 of this card exists to prevent.

**So this is done before `P2-02`, not after.** The board lists `P2-05` as depending only on `P1-15`, so the ordering is available; taking it is a deliberate choice to respect the real dependency rather than ship an endpoint with the wrong authorization and retrofit it.

## 2. Actors

| Actor | Reach |
|---|---|
| `INSTANCE_OWNER` | Every organization, every project |
| `ORG_OWNER` | One organization entirely, including every project in it |
| `ORG_ADMIN` | One organization except deleting it or changing its owner — and every project in it (see §6) |
| `PROJECT_OWNER` | One project, and nothing outside it |
| `PROJECT_GRANT_OWNER` | Reserved. Means nothing until `P4-01` gives it a grant to be scoped by |

## 3. Functional Requirements

- **FR-1** All five roles exist as constants; four resolve, and the fifth is explicitly reserved.
- **FR-2** Inheritance flows downward only, and never upward.
- **FR-3** A role is matched against the **scope it is held over**, not only by name.
- **FR-4** A project-scoped endpoint is satisfied by a grant over the project, or by one over the organization containing it.
- **FR-5** One resolution function serves every endpoint.
- **FR-6** A caller who can see neither the organization nor the project gets a 404, not a 403.

## 4. Non-Functional Requirements

- **NFR-1** The hierarchy stays readable as a table. A traversal that is correct for reasons a reader must reconstruct is worse than five lines they can check.
- **NFR-2** No database access: this is a pure function over the caller's grants, which `P1-15` already loads once per request.

## 5. Dependencies

| Needs | Provided by |
|---|---|
| `manager_roles` and the grant loader | `P1-15` |
| The per-route policy table | `P1-15` |
| `Decision.Invisible` and the 404/403 split | `P1-16` |

## 6. The reading of Part C, which is not obvious

`docs/PLAN/08` draws the hierarchy as a tree — `ORG_OWNER` branching to `ORG_ADMIN` and to `PROJECT_OWNER` — with "permissions flow downward only". Read strictly, the two branches are **siblings**: an `ORG_ADMIN` would not satisfy a `PROJECT_OWNER` requirement.

That reading fails three ways. The diagram's own label for `ORG_ADMIN` is "org access, except deleting org/changing owner", and a project is inside the organization. `P1-17` and `P1-18` already ship `ORG_ADMIN` creating and editing projects and applications, which is project administration by any reading — so under the strict interpretation those endpoints have been wrong since they were written. And it produces an incoherent system: an administrator who can create a project but not manage the roles inside it.

**Decided**: an organization-scoped role covers every project in its organization. Downward-only still holds where it matters — a `PROJECT_OWNER` gains nothing organization-wide and cannot reach past its own `scope_id`. Recorded as **`PG-32`**; the plan needs one sentence and choosing it is a plan change, not this task's to make.

## 7. API Contract

Unchanged. This is enforcement, not surface.

## 8. Frontend Changes

None. The console shows what the API allows; it holds `ORG_ADMIN` today and that is unaffected.

## 9. Backend Changes

**`Authorize` takes a `Target{OrgID, ProjectID}`** instead of a bare organization id. Both are needed: a `PROJECT_OWNER` is matched against the project and an `ORG_ADMIN` against the organization, and passing only one would mean either project roles cannot be checked or organization roles cannot reach a project.

The project id comes from the **route pattern**, alongside the organization id, so a project-scoped endpoint cannot be reached with a project in the body while the path names another.

**`ScopeProject`** joins `ScopeOrganization` and `ScopeInstance`. An empty project id on a project-scoped route is **refused**, not treated as a wildcard: that is how a narrow role silently becomes a wide one.

**The `satisfies` table** gains `ProjectOwner` under the three organization roles, and a row of its own containing only itself.

## 10. Authorization Rules

The whole task. Stated as the order `Authorize` checks them:

1. No requirement declared → refuse. The zero value is unsatisfiable.
2. A role the service does not grant → refuse, rather than fall through to something weaker.
3. `INSTANCE_OWNER` → allow, over any target, flagged `InstanceScoped` when acting outside their own organization.
4. Instance-scoped route, not an instance owner → refuse, visibly.
5. Project-scoped route: a grant whose `scope_id` **is the project**.
6. Any route: a grant whose `scope_id` **is the organization**.
7. Otherwise refuse, invisibly if the caller can see neither.

## 11. Validation

None beyond the above. There is no input here that is not an id the router produced.

## 12. What this task cannot do

**Steps 6 and 7 of the card have nothing to attach to.** They ask for confirmation and audit when granting `INSTANCE_OWNER` or `ORG_OWNER`, and a guard against removing the last `ORG_OWNER` — and **no endpoint anywhere writes `manager_roles`**. The word "manager" does not appear in `openapi/openapi.yaml`, and no task in any of the seven phase files creates one. Every manager role in every environment so far was a direct `INSERT`.

Recorded as **`PG-31`**, with the recommendation. The authorization model those guards would need exists as of this task; what is missing is the endpoint they would guard.

## 13. Edge Cases

- **A project id on a non-project route** — ignored. The requirement's scope decides what is checked, not what the path happens to contain.
- **A grant whose `scope_id` is a project, on an organization-scoped route** — no match, because the scope must equal the target. This is what stops a `PROJECT_OWNER` reaching organization endpoints.
- **An `INSTANCE_OWNER` inside another organization's project** — allowed, and flagged, so the request runs through the named, logged, audited instance-scoped path rather than a missing filter.
- **A caller with no grants at all** — refused, and told 404 for any organization but their own.

## 14. Abuse Cases

| # | Scenario (`docs/SECURITY/02` §3) | Control | Test |
|---|---|---|---|
| A-1 | Horizontal: an `ORG_ADMIN` in org A acting in org B | The scope must equal the target | `TestAnOrgAdminAdministersEveryProjectInItsOwnOrganization` (the second half) |
| A-2 | Horizontal, narrower: a `PROJECT_OWNER` on project A acting on project B | Same, with a project id | `TestAProjectOwnerReachesOnlyItsOwnProject` |
| A-3 | Vertical: a `PROJECT_OWNER` acting as an `ORG_ADMIN` | The table has no upward edge, asserted as a property rather than as cells | `TestAProjectOwnerIsNotAnOrganizationAdministrator` |
| A-4 | Vertical: an `ORG_ADMIN` deleting the organization | `OrgAdmin` does not satisfy `OrgOwner` or `InstanceOwner` | `TestAnAdminIsNotAnOwner`, and the policy table |
| A-5 | Disclosure: probing for project ids by watching 403 versus 404 | `Invisible` covers the project as well as the organization | `TestAStrangerCannotTellAProjectExists` |
| A-6 | A handler deciding permissions itself and skipping the scope match | An architecture test reads the source | `TestNothingOutsideThisPackageDecidesPermissions` |

## 15. Logging / Audit Requirements

The refusal reason now names the project as well as the organization. It stays in the log and never in the response — "you are an ORG_ADMIN and this needs ORG_OWNER", told to somebody probing an organization they do not administer, confirms it exists.

## 16. Security Controls

The default-refuse properties from `P1-15` are unchanged and are what make this safe to extend: an unannotated route is unreachable, an undeclared scope is unsatisfiable, and `TestEveryRouteHasAPolicy` fails on a route with no entry.

## 17. Testing Strategy

Exhaustive and table-driven, as the card asks: every (held, required) pair across all five roles, plus downward-only asserted as a **property** — if A satisfies B and they differ, B must not satisfy A — so a future edit that accidentally makes two roles equivalent fails even if somebody updates the cells to match.

Plus the architecture test, which is the only way to check a negative: a behavioural test can show the endpoints we thought of are correct; it cannot show that no other file decides permissions.

## 18. Acceptance Criteria

1. The hierarchy matches `docs/PLAN/08`'s diagram under the reading in §6, with every combination covered.
2. Upward inheritance is impossible, tested explicitly and as a property.
3. `ORG_ADMIN` cannot delete an organization.
4. Every endpoint routes through one resolution function, verified by an architecture test that has been shown to fail.
5. The last-`ORG_OWNER` guard is **not** delivered, and `PG-31` says why.

## 19. Rollback Strategy

Pure code, no migration. Reverting restores three resolving roles and makes `ScopeProject` routes unreachable — which is the safe direction, since no route declares that scope until `P2-02`.

## 20. Technical Risks

**The `PG-32` reading is load-bearing and reversible only in the strict direction.** If the plan later says `ORG_ADMIN` must *not* administer projects, the change is one row in the table and the tests fail loudly. If it says the opposite of what was implemented in some narrower way, the same. The risk is not that the reading is wrong but that it stays undocumented — which is why it is in the table's own comment as well as in the backlog.

**`PROJECT_GRANT_OWNER` still resolves to nothing**, so an endpoint requiring it is unreachable rather than open. That is the correct failure, and `TestARequirementForAnUnimplementedRoleRefuses` pins it.

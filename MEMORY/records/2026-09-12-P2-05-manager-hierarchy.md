# P2-05 — Manager Role Enforcement, Full Hierarchy

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-05 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | backend |
| **Spec** | [`MEMORY/specs/P2-05-manager-hierarchy.md`](../specs/P2-05-manager-hierarchy.md) |
| **Branch** | `feat/P2-05-manager-hierarchy` |
| **Status** | Complete, except two steps that have nothing to attach to — see below |

---

## Taken out of order, deliberately

`P2-02` is next on the board and its card requires `PROJECT_OWNER` or higher. That role existed as a constant which satisfied nothing, so an endpoint requiring it was unreachable.

Implementing the missing slice of the hierarchy inside `P2-02` would have put authorization logic in a feature task — which is exactly what this card's step 5 exists to prevent — or shipped the roles API at `ORG_ADMIN` and retrofitted it later. `P2-05` depends only on `P1-15`, so the ordering was available, and taking it respects the real dependency instead of the board's numbering.

## The reading that had to be made

`docs/PLAN/08` Part C draws the hierarchy as a tree: `ORG_OWNER` branches to `ORG_ADMIN` and to `PROJECT_OWNER`, with "permissions flow downward only".

Read strictly, those two are **siblings** — an `ORG_ADMIN` does not satisfy a `PROJECT_OWNER` requirement. That reading fails three ways at once:

- The diagram's own label for `ORG_ADMIN` is *"org access, except deleting org/changing owner"*, and a project is inside the organization.
- `P1-17` and `P1-18` already ship `ORG_ADMIN` creating and editing projects and applications. Under the strict reading those endpoints have been wrong since the day they were written.
- It produces an incoherent system: an administrator who can create a project but cannot manage the roles inside it.

**Decided: an organization-scoped role covers every project in its organization.** Downward-only still holds where it matters — a `PROJECT_OWNER` gains nothing organization-wide and cannot reach past its own `scope_id`. The reasoning sits in the `satisfies` table's own comment, not only in `PG-32`, because the next person to read that table will be looking at the table.

## What changed

`Authorize` now takes a `Target{OrgID, ProjectID}` rather than a bare organization id. Both are needed: a `PROJECT_OWNER` is matched against the project and an `ORG_ADMIN` against the organization, and passing only one means either project roles cannot be checked or organization roles cannot reach a project. Both were briefly true while it was one string.

The project id comes from the **route pattern**, beside the organization id — so a project-scoped endpoint cannot be reached with one project in the body while the path names another.

`ScopeProject` refuses an empty project id rather than treating it as a wildcard. That is how a narrow role silently becomes a wide one.

`PROJECT_GRANT_OWNER` is still reserved and still satisfies nothing. It only means anything once a `project_grant` exists to scope it (`P4-01`), and a role that resolves to nothing is the correct state for a role nobody can hold — an endpoint requiring it is unreachable rather than open.

## Two Phase-1 tests changed, which is the point of having written them

`TestPhaseTwoRolesSatisfyNothingYet` asserted that `PROJECT_OWNER` was not implemented. It is now, so the test was rewritten as `TestTheDelegationRoleSatisfiesNothingYet` covering only `PROJECT_GRANT_OWNER`.

A test guarding a "not yet" should fail the day the "not yet" ends. Deleting it quietly would have been the wrong move; so would leaving it, which is how a constant ends up implemented and still documented as absent.

## The architecture test, and why a behavioural one would not do

The card asks that every endpoint route through one resolution function, "verified by review and by an architecture test".

The failure it prevents is undramatic and nearly invisible in review: a handler needing one extra condition writes `for _, g := range caller.Grants { if g.Role == OrgAdmin ... }` inline. It looks reasonable, it works, and it quietly skips the scope match, the `INSTANCE_OWNER` path, the invisible-versus-forbidden distinction, and every future change to the hierarchy. One endpoint ends up subtly more permissive than the rest, and the difference is a single `==` in a file nobody rereads.

A behavioural test can show the endpoints we thought of are correct. It cannot show that no *other* file decides permissions. So this one reads the source, and it carries a guard against itself: if fewer than twenty files were scanned it fails, because an assertion satisfied by a search that never happened is the shape of vacuous check this project has shipped twice.

Verified by planting a bespoke check in `internal/project` — it was caught by file and line — and removing it.

One direct call to `Authorize` outside the middleware survives review: `organization.requireInstanceOwner`, a field-level check for changing an organization's status. That is a second **use** of the one function rather than a second implementation, which is what step 5 asks for.

## What this task could not do

**Steps 6 and 7 have nothing to attach to.** They ask for confirmation and audit when granting `INSTANCE_OWNER` or `ORG_OWNER`, and a guard against removing the last `ORG_OWNER` — and **nothing anywhere writes `manager_roles`**. The word "manager" does not appear in `openapi/openapi.yaml`; no task in any of the seven phase files creates such an endpoint; every manager role in every environment so far was a direct `INSERT`, including the one the console holds on staging today.

So the service has an administrative role model with no way to administer it. An administrator cannot be added or removed through any published surface, and the console cannot answer "who can administer this organization" — the first question in an incident.

Recorded as **`PG-31`** with the recommended endpoint. The authorization model those guards need now exists; what is missing is the thing they would guard.

## Verified

| | |
|---|---|
| Unit | The full hierarchy exhaustively — every (held, required) pair across all five roles — plus six new scope tests |
| Property | Downward-only asserted as a property, not as cells: if A satisfies B and they differ, B must not satisfy A. A future edit that accidentally makes two roles equivalent fails even if the cells are updated to match |
| Architecture | `TestNothingOutsideThisPackageDecidesPermissions`, proven by planting a violation |
| Integration | All 23 packages, including every endpoint suite, after the signature change |
| Gates | 43 passed, 0 failed (fast subset); full integration suite green |

One test failed first for a reason worth keeping: `TestAStrangerCannotTellAProjectExists` was written with the package's `caller()` helper, which puts the caller **in** `orgA` — so the "stranger" was a member of the organization they were supposedly strangers to, and a member already knows it exists. The code was right and the test's premise was wrong. The fixture is now built by hand with a comment saying why.

## Not deployed

The VM was unreachable this session (the SSH key lived in a scratchpad wiped on restart). Nothing here changes a surface — no route declares `ScopeProject` until `P2-02` — but the standing rule is that a task is not done until it runs on staging, and this one has not.

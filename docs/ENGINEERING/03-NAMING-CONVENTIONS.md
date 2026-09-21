# 03 - Naming Conventions

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-01, P0-03 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Fix the names that join things together: a task ID in five places, a migration filename, a
test function that says what it asserts.

## Scope

The repository.

## As Built

### The task ID is the join key

```
P<phase>-<nn>[.<sub>]        P0-04, P1-12.3, P4B-02, PF-37
```

The same ID appears in the branch name, the commit subject, the pull request, the
`MEMORY/` record, and `TASKS/PROGRESS.md`. IDs are permanent and never reused; a dropped
task keeps its number with a `DROPPED` status, because a gap in the numbering is a lost
audit trail.

`scripts/hooks/commit-msg` rejects a subject without one.

### Branches

```
feat/P4-06-granted-projects
docs/engineering-frontend-website
```

Task ID and a slug. Nothing lands on `main` directly; merges are `--no-ff`, so the branch
is visible in the history.

### Go

| Thing | Convention | Example |
|---|---|---|
| Package | domain noun, lower case, no underscores | `projectgrant` |
| File | the sub-surface it holds | `received.go`, `delegated.go`, `owners.go` |
| Store type | `Store`, one per package | `projectgrant.Store` |
| Handler type | `Handler`, one per package | `projectgrant.Handler` |
| Handler method | the operation ID from the contract, verbatim | `ListReceivedProjectGrants` |
| Sentinel error | `ErrX` | `ErrNotFound`, `ErrRevoked` |
| Typed error carrying data | the noun | `UnknownRoles`, `NotDelegated` |
| Integration test file | `*_integration_test.go` | `received_integration_test.go` |

A handler method's name is **not** a choice: it is generated into the interface from the
contract's `operationId`, so renaming it means editing the spec.

### Test names are sentences that state the claim

```go
func TestTheReceivingListLeavesOutTheGrantsThisOrganizationMade(t *testing.T)
func TestNoThirdOrganizationSeesADelegationBetweenTwoOthers(t *testing.T)
func TestARevokedGrantStaysListedAndMarked(t *testing.T)
func TestNothingOutsideThisPackageDecidesPermissions(t *testing.T)
```

Long, and worth it. A failing test's name is the first thing anybody reads, and
`TestListReceived2` tells them nothing. The name states what should be true, so a failure
is a sentence that is now false.

The same applies to console tests, where the sentence is the `it(...)` string:

```
it("offers the delegated roles and NOTHING else")
it("keeps an ended grant listed, says who ended it, and offers no assignment")
```

### Migrations

```
backend/migrations/<YYYYMMDDnnnnnn>_<slug>.{up,down}.sql
20260921000039_received_grant_context.up.sql
```

Timestamp-ordered, slug describing the change, always a pair. The number is referenced in
records and in task cards ("migration 039"), so it is effectively part of the change's
identity.

### SQL

| Thing | Convention |
|---|---|
| Table | plural, snake case — `project_grants`, `user_grants`, `manager_roles` |
| Column | snake case; foreign keys are `<singular>_id` |
| Policy | `<table>_<what it allows>` — `user_grants_granting_side_read` |
| Function | verb or noun phrase — `current_org_id()`, `received_grant_context()` |
| Constraint | Postgres's default names, referenced in code when a conflict must be recognised — `project_grants_project_granted_org_key` |

### TypeScript

| Thing | Convention | Example |
|---|---|---|
| Component file | `PascalCase.tsx` | `GrantedProjectsPage.tsx` |
| Test file | lower case, matching the subject | `grantedprojects.test.tsx` |
| Generated file | `*.gen.ts` | `schema.gen.ts`, `patterns.gen.ts` |
| Query hook | `use<Collection>` | `useReceivedGrants` |
| Query key | plural kebab string in `queryKeys` | `["received-grants"]` |

### Identifiers are UUIDs, everywhere

No prefixed identifiers (`usr_…`, `org_…`). The plan specified them; ADR `PG-23` records
the change and the reason: prefixing only part of the surface gives the same user two
identifiers, and OIDC's `sub` is a UUID that integrators have already stored.
`scripts/check-docs.py` fails on a prefixed example in documentation.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Commit subject carries a task ID | required | `scripts/hooks/commit-msg` |
| Handler method name | the contract's `operationId` | generated interface |
| Migration filename | timestamp, slug, `.up`/`.down` pair | `scripts/check.sh` |
| Identifier examples in docs | UUID, never prefixed | `scripts/check-docs.py` |
| Test names | state the claim | convention |

## Verification

- `scripts/check.sh` — the commit-msg hook is exercised, not assumed: it feeds the hook a
  bad subject and a good one and checks both answers.
- `scripts/check-docs.py` — prefixed identifier examples.

## Related Documents

- [`11-GIT-AND-REVIEW-CONVENTIONS.md`](./11-GIT-AND-REVIEW-CONVENTIONS.md)
- [`../../TASKS/00-TASK-CONVENTIONS.md`](../../TASKS/00-TASK-CONVENTIONS.md)

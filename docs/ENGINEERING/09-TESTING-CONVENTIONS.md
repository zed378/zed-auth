# 09 - Testing Conventions

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-15, P1-28, and every card's DoD &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

The conventions a test in this repository follows. The strategy — which layer proves what
— is [`../TESTING/`](../TESTING/); the console's own practice is
[`../FRONTEND/10-FRONTEND-TESTING.md`](../FRONTEND/10-FRONTEND-TESTING.md).

## Scope

`backend/**/*_test.go`, `backend/tests/`, `console/`.

## As Built

### Layers, and the build tag that separates them

| Layer | Marker | Needs |
|---|---|---|
| Unit | none | nothing |
| Integration | `//go:build integration` | Postgres and Redis via testcontainers |
| Cross-package security | `backend/tests/security/`, same tag | the whole schema |
| Console component | `*.test.tsx` | jsdom |
| End-to-end | `console/e2e/` | a running service |

Unit tests run with `-race` and **shuffled**, so an order dependency surfaces rather than
lurking. Integration tests are opt-in locally (`RUN_INTEGRATION`) and always on in CI.

### A test's name states the claim

```go
func TestTheReceivingListLeavesOutTheGrantsThisOrganizationMade(t *testing.T)
func TestNoThirdOrganizationSeesADelegationBetweenTwoOthers(t *testing.T)
```

A failing test's name is the first thing anybody reads. `TestListReceived2` tells them
nothing; these tell them what is now false.

### Every abuse case in the spec has a test, and the test names the case

`MEMORY/specs/` numbers abuse cases A-1…A-n. The integration test carries the number in a
comment:

```go
// A-3: the route lists what this organization RECEIVED, never what it gave.
```

`AGENTS.md` rule 7 makes this mandatory for anything security-sensitive.

### A test must be able to fail — the mutation rule

This repository's recurring defect class is **vacuous verification**: a check that passes
whatever the code does. Examples that actually shipped here —

- an assertion on an error that was ignored;
- a `-run` pattern quoted so that Windows `cmd.exe` passed it literally, so the mutation
  run tested nothing and reported success;
- a plan test that could not see a new RLS policy because the table it queried was empty.

So every card runs mutations: break the source deliberately, run the test that should
catch it, record the result. `P4-06` ran five; `P4-02` ran eight; `P4-03` ran six. They go
in the record, with the test each one turned red.

Where a mutation **cannot** be made to fail, the record says so rather than rounding up.
`P4-04`'s policy predicate could not be made to leak by removal, because the subquery reads
under the caller's own RLS — recorded as a limitation, not as a caught mutation.

Two operational rules, both learned the hard way:

- **Never run the mutation script and the gate at once.** Mutations edit the source the
  gate is testing; a run left a mutated line in a file.
- **Quote `-run` patterns for the shell you are actually in.**

### Integration tests seed through the API where an API exists

The console's E2E fixtures create objects through the Management API, using the endpoints
an administrator would. A fixture that reached into the database would create objects the
API might refuse, and a suite built on those tests a system nobody can operate.

Backend integration tests seed with SQL, because they are testing the layer beneath.

### A skip is never a pass

`console/e2e/fixtures.ts` throws if its environment is missing rather than skipping. A
suite that skips when its dependency is unreachable reports success having run nothing,
and a green suite that ran no tests is a false statement everybody acts on.

The same principle appears inside the gate: a check that cannot find what it is supposed to
inspect **fails**. The clock-check gate looked for an `INSERT` into a table that did not
exist, found nothing, and passed while the bug was in the tree.

### Coverage floors name the packages, not an average

`scripts/check-coverage.sh` sets an 80% floor on `internal/authn`, `internal/signing`,
`internal/authz`, `internal/oidc`, `internal/oauth/*`, `internal/session`,
`internal/login`, `internal/ratelimit` and others.

> A repo-wide percentage is satisfied by testing whatever is easiest, and the easiest code
> to test is rarely the code where a bug matters.

An absent package is reported and skipped rather than failing, so the floor starts applying
the moment the package appears rather than on the day somebody remembers to add it.

### Cross-package suites, and a coverage map that notices a rename

`backend/tests/security/` holds what no single package can: tenant isolation, multi-org
behaviour, RLS query plans at scale, and a full flow. `coveragemap_test.go` asserts that
each named scenario has a test — so renaming a test without updating the map fails, which
is how a deleted scenario is prevented from disappearing quietly.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Unit tests | `-race`, shuffled | `scripts/check.sh` |
| Integration tests | `integration` build tag, testcontainers | `backend/internal/testsupport/` |
| Abuse cases | one test each, numbered | `AGENTS.md` rule 7 |
| Mutations | run and recorded per card | `MEMORY/records/` |
| Skips | never stand in for a pass | `console/e2e/fixtures.ts`, `scripts/check.sh` |
| Coverage | per-package floors, 80% | `scripts/check-coverage.sh` |
| Scenario coverage map | kept in step | `backend/tests/security/coveragemap_test.go` |

## Verification

- `scripts/check.sh` — unit, integration, coverage floors, and the console's three layers.
- `MEMORY/records/` — the mutation table for each card.

## Not Yet Built / Open Questions

- **Mutation testing is manual.** Disciplined and recorded, but a person chooses the
  mutations; nothing enumerates them.
- **No coverage floor for the console.**
- **No fuzzing**, including for the token and SAML parsers when those arrive.

## Related Documents

- [`../TESTING/`](../TESTING/)
- [`../FRONTEND/10-FRONTEND-TESTING.md`](../FRONTEND/10-FRONTEND-TESTING.md)
- [`14-CODE-REVIEW-CHECKLIST.md`](./14-CODE-REVIEW-CHECKLIST.md)

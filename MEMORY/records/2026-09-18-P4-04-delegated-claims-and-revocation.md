# P4-04 — Delegated Role Claims and Revocation Propagation

| | |
|---|---|
| **Date** | 2026-09-18 |
| **Task** | `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-04 |
| **Phase** | Phase 4 — Enterprise Interop |
| **Surface** | backend + published documentation |
| **Branch** | `feat/P4-04-delegated-claims` |
| **Status** | Complete for the check path; tokens wait on cross-organization sign-in |

**Spec**: [`MEMORY/specs/P4-04-delegated-claims-and-revocation.md`](../specs/P4-04-delegated-claims-and-revocation.md)

---

## What shipped

**A delegated role now grants access**, through `/v1/authz/check` in the granting
organization's project. Before this card, `P4-02` could write a delegated assignment and
nothing read it as access.

Four parts, all in one change because they are only safe together (threat review T4-2):

1. **One shared statement** — `grantsql.EffectiveRoleKeys`, in a leaf package both readers
   import. A direct row confers its `role_keys`; a delegated row confers
   `role_keys ∩ granted_role_keys`, and nothing at all unless the grant is `active`.
   Both `grant.TokenClaims.ForToken` and `authz.readRoleKeys` use it, so they cannot drift.
2. **Migration 038** adds one `FOR SELECT` policy: the granting organization reads delegated
   rows **of its own grants**. Permissive policies are OR-ed, so the receiving side's
   tenant policy is untouched and a third organization still sees nothing.
3. **Revocation is one write.** Each grant has a generation in Redis
   (`authz:grantgen:{id}`); a cached decision records the grant it came through and the
   generation it was written at, and `Cache.InvalidateGrant` increments it after the
   revocation commits. Revoking a grant held by four hundred users costs one `INCR`, not
   four hundred deletes — the "scan or a guess" the cache's own comments rule out (T4-3).
4. **The claim finally carries its meaning.** A delegated role is claimed with the
   **delegating** organization's `org_id`, which is the disambiguation `docs/PLAN/08`
   Part A put the field there for.

`ProjectGrantChanges` — a metric defined in `P4-01` and incremented by nothing — is now
wired to grant creation and revocation (`docs/PLAN/13` names an unusual rate as a misuse
signal).

## The window, published

`public-site/docs/guides/authorization-checks.md` gains a Phase 4 section stating what an
integrator has to design around:

- a revocation through the API is honoured by the next check;
- the **30 seconds** cache TTL is the backstop for an unreachable cache or a grant changed
  directly in the database;
- delegated roles do **not** appear in access tokens yet.

`backend/internal/docsdrift/phase4_docs_test.go` pins each of those to the code, so the
number cannot drift away from `authz.DefaultTTL`.

## What this card does not do

**A partner's users still cannot sign in to the granting organization's applications.**
`/oauth/authorize` refuses a session whose organization differs from the client's, so no
live path issues a token carrying a delegated role. ADR-025 decided the policy question
(the stricter of both organizations' policies applies); building it is a separate card.
The claim code is implemented and tested at the claim builder and at the reader, so that
when sign-in lands the claim is already right.

## A test that had to be inverted

`P4-02` left a test asserting the granting organization sees **zero** delegated rows — the
deliberate state before this card. It is now
`TestADelegatedRowIsVisibleToBothSidesOfItsOwnGrantAndNobodyElse`, which asserts the new
rule and adds the case that matters: an unrelated organization with a grant of its own to
the same partner still sees nothing.

## Verification

| Check | Result |
|---|---|
| `internal/authz` — 5 new delegated-access tests | pass |
| `internal/grant` — 4 new claim tests | pass |
| `internal/oauth/token` — claim names the delegating organization | pass |
| `internal/projectgrant` — reader resolution and two-sided visibility | pass |
| `internal/docsdrift` — the published window matches `authz.DefaultTTL` | pass |
| Mutations: reader join, active check, intersection, cache generation, claim organization | all red, each caught by named tests |
| Mutation: the policy's `granting_org_id = current_org_id()` predicate | **green — and that is the finding**, see below |

**The policy predicate cannot be made to leak by removing it.** Widening it to `WHERE true`
broke nothing, and the reason is worth writing down rather than papering over: the policy's
subquery reads `project_grants` **as the caller**, under that table's own two-sided
row-level security. A third organization's grants are therefore not in the subquery's
result at all, and a receiving organization's own rows reach it through the tenant policy
regardless. The predicate is defence in depth and a statement of intent — the isolation
itself is `project_grants_tenant_isolation`, which has its own tests. Claiming "6 of 6
mutations caught" would have been the more flattering sentence and the less true one.

The first mutation run reported **no** failures. That was the harness, not the tests:
single-quoted `-run` patterns are passed literally by `cmd.exe`, so nothing matched and
nothing ran. Worth recording — a mutation run that proves nothing looks exactly like a
mutation run that proves everything.

## Staging

**Not deployed yet.** The owner was away from the private network the staging VM sits on
when this merged, so migration 038 and the new image are still to be rolled out. Nothing
about the change is environment-specific; the deployment order is the usual one (back up,
migrate as the owner role, roll out, verify), and the revocation window should be measured
there once it is live — the published number is the cache TTL, which is an upper bound by
construction rather than a measurement.

## Not done here

- Cross-organization sign-in (ADR-025).
- `P4-06`, the receiving organization's console.
- The revocation window has not yet been measured on staging; the number published is the
  cache TTL, which is an upper bound by construction.

# P4-01 — Project Grants: Data and Lifecycle

| | |
|---|---|
| **Date** | 2026-09-15 |
| **Task** | `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-01 |
| **Phase** | Phase 4 — Enterprise Interop |
| **Surface** | backend + Management API |
| **Branch** | `feat/P4-01-project-grants` |
| **Status** | Complete — the contract only, by design |

**Spec**: [`MEMORY/specs/P4-01-project-grants.md`](../specs/P4-01-project-grants.md)

---

## The contract, and deliberately nothing more

An owning organization's `PROJECT_OWNER` delegates a project to another live organization,
with a subset of the project's own roles. The delegation can be listed, read and revoked.

**A grant confers no access.** Four things are still missing before it does:

- the receiving organization cannot assign the roles yet (`P4-02`);
- tokens and `/v1/authz/check` do not carry them (`P4-04`);
- neither side has a screen (`P4-05`, `P4-06`);
- a partner's user cannot sign in to the granting organization's applications, and whose
  MFA mandate would apply if they could is undecided (**T4-1**, raised with the project
  owner).

That is why this card could ship first. A wrong grant before `P4-02` lists a project to an
organization that cannot act on it.

## Visibility is not authority, and the mutation run proved it mattered

`project_grants` is the one table two tenants can see. RLS shows a grant to the receiving
organization as well as the granting one, and lets only the granting side write it. The
handler therefore filters every query on `granting_org_id = <the organization in the
path>`, rather than trusting what RLS shows.

The first draft of the authorization test did not exercise that filter. Every one of its
"receiving organization" cases used the **granting** organization's project, so the
project check refused them before the store ran. The case added afterwards uses a project
the receiving organization *does* own, with the grant's id: the project check passes, and
the grant is visible under RLS. With the filter removed, **the receiving organization read
the grant through its own project path, and could revoke it.** The test now fails that
way. Before the case was added, the filter's absence would have shipped with every test
green.

## A grant only ever narrows, for every writer

There is no update endpoint. A trigger (`project_grants_only_narrow`, migration 035)
refuses any change to a grant's project, parties or role keys, and any move from `revoked`
back to `active`. The test runs from the **owner** connection, so the rule holds for an
incident-time hand-edit too, not only for the handler.

Narrowing in place is refused as well. The creation event records the delegated roles, and
a row that can change afterwards would make that record untrue. A different set of roles
means revoke, then grant again.

## Names across the boundary, and nothing else

A person revoking needs to read *who* a grant is to, and the receiving organization's row
is invisible under the granting tenant. `granted_organization_names()` is `SECURITY
DEFINER`, but it returns names only for organizations holding a grant **from the
transaction's own organization**. A bystander organization asking about the same id gets
nothing, and a test asserts that.

`organization_accepts_grants()` returns one boolean about one id the caller already holds.
Self-grants, unknown ids and suspended organizations all get **byte-identical** refusals, and
that is asserted too.

## A delegated role cannot vanish under a partner

`role.Store.Delete` now refuses while an **active** grant delegates the role, and names the
count. The existing user-grant count could not have covered this: once `P4-02` exists, a
partner's assignments live under the partner's tenant, where the owning organization's
transaction cannot see them. A revoked grant does not block deletion. It is history.

## One claims-check edit made before it was needed

`check-claims.mjs` mapped the landing card "Roles that scale to delegation" to `P4-01`
alone. Marking this card done would have made that check **demand** the card say "Shipped".
Every earlier false claim on this site was an omission; this would have been a capability
nobody can use, claimed by the check meant to prevent that. The card now waits for `P4-01`
to `P4-06`.

## Verified

| | |
|---|---|
| Integration | 8 tests through the real `/v1` chain with three organizations |
| Mutations | 4 controls broken (the granting-side filter, the role-key check, the live-organization check, the role-deletion guard), each turning its test red |
| Security map | `tests/security` maps the lifecycle row; P4-02 and P4-04 rows are named correctly |
| Contract | OpenAPI, the generated server, the console client and the API reference regenerated; the shipped-paths allowlist updated |
| Gate | see the commit |

## Not done

- **T4-1**, cross-organization sign-in, is a design decision `docs/PLAN/08` Part C does not
  make. It blocks `P4-02` and `P4-04`.
- **Delegated `user_grants` RLS and invalidation by grant** (T4-3). Designed in the spec and
  built in `P4-02` and `P4-04`.
- **The console** (`P4-05`).

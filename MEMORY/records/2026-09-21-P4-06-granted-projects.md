# P4-06 — Granted Projects: the receiving side

**Date**: 2026-09-21
**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-06 (`PF-37`)
**Spec**: `MEMORY/specs/P4-06-granted-projects.md`
**Chain**: `console/docs/implementation-chain-P4-06.md`
**Branch**: `feat/P4-06-granted-projects`

---

## The gap this closed first

The card is a console screen. It could not be built, because the API it needed did not
exist.

Since `P4-02` a receiving organization has been able to assign delegated roles — through
`POST /v1/organizations/{org_id}/project-grants/{grant_id}/user-grants`, which takes a
grant id. Nothing anywhere told it what its grant ids were. The granting side has
`GET …/projects/{project_id}/grants`; the receiving side had no equivalent, and no screen
or API would have revealed one. In practice a partner administrator would have had to be
sent the UUID by email.

So the card grew an API addition and a migration before the screen.

## What was built

**Migration 039** — `received_grant_context(uuid[])`, a `SECURITY DEFINER` function
returning the project name and the granting organization's name for a set of grant ids.
The mirror of `P4-01`'s `granted_organization_names()`, and bounded the same way: a row
comes back only for a grant whose `granted_org_id` is the transaction's own organization.

The need is structural rather than cosmetic. A `project_grants` row is visible to both
parties under the two-sided isolation policy, but the **project** it names and the
**organization** that made it are the granting side's rows, which the receiving tenant's
RLS hides. Without the function this screen would show two UUIDs and no way to tell which
vendor sent them.

**`GET /v1/organizations/{org_id}/project-grants`** — `ORG_ADMIN` at organization scope,
newest first, paginated, revoked grants included. `ReceivedGrant` carries `project_name`,
`granting_org_name`, `granted_role_keys`, `holder_count`, `status` and the dates.

**The screen** at `/granted-projects`, replacing the Phase 4 placeholder: the list, and a
side panel per grant holding the current holders and — while the grant is active — a form
that assigns its roles to one of this organization's own users.

## The two filters that are the actual controls

**`granted_org_id` on the list.** RLS shows this tenant *both sides* of its own
delegations, so a receiving-side route that relied on visibility would also list the
grants this organization **made**. The same lesson `P4-01`'s mutation run taught about the
granting side, in the other direction: seeing a row and acting through it are different
questions.

**`granted_org_id = current_org_id()` inside the name lookup.** The function is
`SECURITY DEFINER`, so RLS does not bound it and its own `WHERE` clause is the whole
control. The route only ever passes ids it has already listed, but a function that
bypasses RLS is not allowed to depend on its caller being careful, so it is tested
directly: asked for a grant between two other organizations, from both the granting side
and a bystander, it answers nothing.

## The rule the screen exists to keep

`docs/UI-UX/08` says a role the grant does not delegate is **not rendered** — not
disabled, not greyed. What is implemented is the stronger property: the screen never
learns the partner's other roles exist. `granted_role_keys` is its only role source, and a
test asserts the console makes no request for the granting project's roles at all.

A disabled checkbox would be the worse failure. It tells a partner administrator that a
role exists and that somebody could switch it on, which starts a conversation between two
organizations about a decision only one of them gets to make.

## Verification

Six integration tests (`internal/projectgrant/received_integration_test.go`), fourteen
component tests (`console/src/pages/grantedprojects.test.tsx`), and Flow 3 end to end
(`console/e2e/grantedprojects.spec.ts`) against the real service, checking the Management
API after the assignment and again after the removal.

**Five mutations, each red:**

| Mutation | Test that failed |
|---|---|
| The list filter admits `granting_org_id` too | the grants this organization MADE appear in its received list |
| The names are never resolved | the row shows a UUID instead of "pos" / "Acme Vendor" |
| The name lookup drops its `granted_org_id` bound | a bystander resolves names for somebody else's grant |
| The order is flipped to oldest first | the paged order no longer matches newest-first |
| `holder_count` is never counted | an assigned grant still reports nobody |

## Two things found on the way

**The cross-organization path answers `404`, not `403`.** Written expecting a refusal, the
test met a not-found: an organization the caller holds no role in is not confirmed to
exist. The test now asserts the real convention.

**A latent race in the `P4-05` E2E spec.** Its
`getByRole("heading", { name: "Project Grants" })` also matches the empty state's "No
Project Grants yet", and a fresh project always has no grants — so it failed strict mode
whenever the list resolved before the assertion ran. It had been passing on timing.
Fixed with an exact match.

## What this still cannot do

The people assigned here cannot sign in to the granting organization's applications:
cross-organization sign-in is ADR-025 and has no task card yet. The roles are real — they
are what the granting organization's `/v1/authz/check` returns for those users (`P4-04`) —
and nothing else consumes them. The screen says as much rather than implying more.

---

## Staging (2026-09-21)

Migrations 038 (`P4-04`) and 039 (`P4-06`) applied together — staging had been at 037
since `P4-03`. Service on `zed-auth:p4-06`; console and public site rebuilt and extracted
**in place** over the bind mounts, inode unchanged before and after.

`scripts/acceptance-delegation.sh`, written for this rollout, reports **14 passed, 0
failed** against the deployment, and its three throwaway organizations remove themselves.

The first run was 13 of 14, and the failure was the check's own: it asserted the
bystander's received list is *empty*, which is wrong — the fixture makes that organization
the receiving side of the partner's own grant, and check 2 needs that grant to exist.
An empty list would also be produced by a route that returns nothing at all, so the
assertion was weak as well as incorrect. It now asserts both directions: the delegation
between the other two is absent, and the grant made *to* the bystander is present.

### Two operational findings

**The image built on the VM disappeared between two SSH sessions**, along with `p4-01`,
`p4-02` and `p4-05`. Disk was at 18% and no prune timer exists. Rebuilding and migrating
in one session worked; the cause is unexplained and worth watching on the next rollout.

**`vmput.py` fails on an absolute remote path** and succeeds on a relative one — the
absolute form answers `ENOENT` from the server while `sftp.stat('/home/infra')` succeeds.

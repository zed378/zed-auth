# P0-08 — Row-Level Security

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Task** | `TASKS/PHASE-0-FOUNDATION.md` P0-08 |
| **Phase** | Phase 0 — Foundation |
| **Surface** | backend |
| **Author** | Claude Code |
| **Branch** | `feat/P0-08-row-level-security` |
| **Status** | Completed, with one open deviation (DV-02) |

---

## What Changed

Cross-tenant isolation moved from being something the application does to something the database guarantees. Eleven tables gained org-scoped RLS policies; the storage layer gained a scoped-transaction API with no way to query outside a declared scope; the service gained a startup assertion that refuses to boot if its database role can bypass RLS; and CI gained a gate that fails if a tenant-scoped table appears without a policy.

## Why

`PLAN/08-AUTHORIZATION.md` Part B asks for isolation that holds "even when the application layer forgets to filter". The application will keep filtering by `org_id` — that is not going away. But application-layer filtering is a code-review outcome, and this is a guarantee. One forgotten `WHERE` clause in one repository method is all it takes, and it is the kind of omission that reads as correct in a diff.

`PLAN/18` R-02 rates cross-tenant leakage via a misscoped query as **Critical**, and names RLS as the mitigation.

## How, and What Was Decided

### Fail-closed by construction, not by a check

Policies compare `org_id` against `current_org_id()`, which returns `NULL` when the session setting is unset. `org_id = NULL` is `NULL`, which is not true, so **a query with no tenant context returns zero rows rather than every row**.

That is the property worth having. The alternative design — validate that a context is set, error if not — depends on the validation being present at every call site. This one is a consequence of how SQL evaluates NULL, so it holds everywhere by default. Tested explicitly.

The `NULLIF` guard handles the adjacent case: an empty string would raise a cast error rather than filtering, and an error is a worse failure mode than an empty result for a health check or a background job.

### `SET LOCAL`, not `SET`

The tenant is set with `set_config(..., true)` — transaction-scoped. A plain `SET` persists on the connection, and with a pool that means one request's tenant leaks into whichever request reuses that connection next. It would appear only under concurrency, intermittently, in production, as one tenant seeing another's data. There is a test that runs ten instance-scoped reads after a tenant-scoped one specifically to catch it.

### No way to query without a scope

`DB` deliberately does not embed `*sql.DB`. There is no exported `Query` or `Exec` on it. Everything goes through `WithTenant` or `WithInstanceScope`, which set the tenant inside a transaction before returning a queryable handle.

This is a constraint rather than a convenience: a repository method cannot forget to scope, because it never holds a handle that has not already been scoped. The database policy only delivers its guarantee if the application reliably sets the context the policy reads, and this is what makes that reliable.

### The instance-scoped path is deliberately uncomfortable

`PLAN/08` Part B requires cross-tenant access to be "explicit, documented, and auditable — not the normal path with the filter omitted". So `WithInstanceScope` is a differently-named function that requires a reason string and logs it. Using it is a visible choice in a diff rather than an omission.

It also still cannot read a policy-protected table by accident: instance scope sets the tenant to empty, which makes `current_org_id()` return NULL, which every policy evaluates as false. A second line of defence behind the naming.

### `project_grants` is visible to two tenants, on purpose

The only table with a dual-tenant policy. A Project Grant is the delegation contract between a granting and a receiving organization (`PLAN/08` Part C), and each side needs it — the granter to manage and revoke it, the receiver to know which roles it may assign.

Writes are restricted to the granting side. A receiving organization that could insert or alter a grant row could widen its own delegation, which is exactly the privilege escalation `PLAN/09` § Delegation abuse names.

Worth being precise about what this does not do: RLS makes the grant row *visible*, it does not enforce that assigned roles are a subset of `granted_role_keys`. That is a comparison between a request and a row, not a question of row visibility, and it stays application logic revalidated on every request (`P4-02`).

### `FORCE ROW LEVEL SECURITY` is not used

It would apply the policies to the table owner too, and the owner runs migrations that legitimately span tenants. Safety comes from the application connecting as `auth_app`, which owns nothing and has no `BYPASSRLS` — which is now asserted at startup rather than assumed.

### The startup assertion

I flagged this in the `P0-01…P0-13` record as the most likely way this work gets silently undone:

> "The `auth_app` role split is one careless DSN away from silently disabling RLS. If anyone points `AUTH_POSTGRES_DSN` at `auth_owner` — plausible while debugging a permissions error — every isolation guarantee vanishes with no test failure."

The service now refuses to boot if its role is a superuser, has `BYPASSRLS`, or owns any table. Verified both ways: starts as `auth_app`, refuses as `auth_owner` with an error that states the consequence rather than just the fact.

## Files Touched

| Path | Change |
|---|---|
| `backend/migrations/20260908000007_row_level_security.{up,down}.sql` | `current_org_id()`, policies on 11 tables |
| `backend/internal/storage/postgres/postgres.go` | Scoped transaction API, startup assertion |
| `backend/internal/storage/postgres/rls_integration_test.go` | 12 isolation tests |
| `backend/internal/storage/postgres/schema_integration_test.go` | Append-only test updated for tenant context |
| `backend/cmd/authservice/main.go` | Postgres wired in; `/readyz` now genuinely checks it |
| `.github/workflows/ci.yml` | Gate: tenant-scoped table without RLS fails the build |
| `TASKS/BACKLOG.md` | DV-02 |

## Tests Added

| Layer | Coverage |
|---|---|
| Integration | 12 RLS tests. **Every one issues a deliberately unfiltered query** — no `WHERE org_id`. A test that filtered correctly would prove nothing about RLS |

Covered: unfiltered reads across three tables, writes into another tenant, updating another tenant's row by known id, the audit log, dual-tenant grant visibility with granter-only writes, context leakage across pooled connections, rollback on error, empty-org rejection, instance-scope reason requirement, and the startup assertion both ways.

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Cross-tenant read via a misscoped query | `SECURITY/02` §2, `PLAN/18` R-02 | `TestUnfilteredQueryReturnsOnlyTheCurrentTenant` |
| IDOR: acting on another tenant's row by known id | `SECURITY/02` §2, §14 | `TestCannotUpdateAnotherTenantsRowEvenWithItsID` |
| Writing into another tenant | `SECURITY/02` §3 | `TestCannotWriteIntoAnotherTenant` |
| Receiving org widening its own delegation | `PLAN/09` § Delegation abuse | `TestProjectGrantIsVisibleToBothSidesButWritableOnlyByTheGranter` |
| Audit log leaking across tenants | `SECURITY/02` §19 | `TestAuditLogIsTenantIsolated` |
| Tenant context leaking via connection pooling | — | `TestTenantContextDoesNotLeakBetweenTransactions` |
| Service run with an RLS-bypassing role | `PLAN/08` Part B | `TestAssertRoleIsNotPrivileged` |

## Definition of Done Verification

- [x] An unfiltered `SELECT * FROM users` under org A's context returns only org A's rows
- [x] The application role is confirmed non-owner and non-`BYPASSRLS` — now at startup, not only in a test
- [x] The instance-level path is explicit, documented, requires a reason, and is logged
- [x] A query with no tenant context fails closed
- [x] Deployed to the VM and verified there
- [ ] **The instance-level path writes an audit event** — the audit writer is `P0-12`. It logs now; the event follows when there is a writer to emit it

## What Did Not Work

**An existing test started failing, and that was the feature working.** `TestEventsAreAppendOnlyForTheApplicationRole` from `P0-07` inserted into `events` without a tenant context. RLS refused it.

The interesting part was the fix. Setting the context on the INSERT was obvious. But its `UPDATE` and `DELETE` sub-tests needed it too — otherwise RLS would refuse those statements *before* the privilege check was reached, and they would pass for the wrong reason: proving isolation works rather than proving the audit log is append-only. A test that passes for the wrong reason is the same category of problem as the two flawed verifications in the VM deployment, and this is the third instance in two days.

**Windows path handling wasted time again** on the deployment tarball, this time on the scratchpad path rather than `/tmp`. Not novel.

## Follow-Ups

- **DV-02**: `manager_roles` has no tenant policy. It is read during permission resolution — before a tenant context exists, because what the caller may access is exactly what is being determined. The correct policy keys on the current *user*, needing an `app.current_user_id` that arrives with `P2-05`. A half-working policy would give the appearance of isolation without the substance. `P2-05` cannot be marked done while this is open.
- The instance-scoped path's audit event waits on `P0-12`.

## What to Watch

**A new table added without a policy is silently unisolated.** It works, tests pass, and it returns every organization's rows. The CI gate catches the `org_id` case; a table that scopes by tenant through some other column would slip past it. The gate should grow when such a table appears.

**The startup assertion is now the load-bearing check.** It closes the DSN-pointed-at-owner hole, but it only runs at boot. A role's privileges changed while the service is running would not be noticed until the next restart. That is acceptable — changing `auth_app` to `BYPASSRLS` requires owner access, and someone with that has easier options — but it is worth knowing the check is not continuous.

**`WithInstanceScope` will attract misuse.** It is the obvious escape hatch when a query is awkward to scope, and the reason string is the only friction. If reasons start reading like "listing users" rather than naming a genuinely cross-tenant operation, the discipline has failed and the reasons are the place it will show first.

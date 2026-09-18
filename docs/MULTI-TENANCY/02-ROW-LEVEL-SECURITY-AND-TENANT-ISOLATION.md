# 02 - Row-Level Security and Tenant Isolation

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-08, P1-06, P1-07, P1-11, P2-08, P2-09, P2-16 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State the tenant-isolation guarantee this system makes, how it is mechanically enforced end to end (from "which organization does this request belong to" through to "which database rows can this transaction see"), and how that guarantee is verified by an adversarial test suite rather than assumed from the presence of `WHERE org_id = ...` clauses in application code.

## Scope

This document is the tenancy-guarantee narrative. The exact SQL for every policy, trigger and `SECURITY DEFINER` function is the authoritative reference in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` — this document does not repeat that text, only the guarantee it produces and how the guarantee is tested. How the tenant is *determined* for a given request (as opposed to how, once determined, it is enforced) is `03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`.

## As Built

### The guarantee

`docs/PLAN/08-AUTHORIZATION.md` Part B states the goal directly: isolation must hold "even when the application layer forgets to filter." This codebase treats that as a database-level property, not an application-code discipline: every tenant-scoped table has a PostgreSQL row-level security policy comparing `org_id` (or, for `organizations` itself, `id`) against a session-local setting, and the application has no way to run a query without first setting — or explicitly and auditably not setting — that value.

### The mechanism, end to end

1. **A request's tenant is determined** — by the OIDC client for authentication flows, or by the URL path (checked against the caller's `manager_roles`) for the Management API. See [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md).
2. **The tenant is set as a transaction-local database setting.** `postgres.DB.WithTenant(ctx, orgID, fn)` opens a transaction and runs `SELECT set_config('app.current_org_id', $1, true)` — the `true` argument makes it `SET LOCAL`, scoped to the transaction, specifically so a pooled connection cannot leak one request's tenant into whichever request the pool hands that connection to next. `orgID` is passed as a bind parameter, never interpolated into SQL text.
3. **Every RLS policy reads that setting through `current_org_id()`**, a `STABLE` SQL function that returns `NULL` when unset (`NULLIF(current_setting('app.current_org_id', true), '')::uuid`). Because `org_id = NULL` is `NULL`, not `true`, a query issued with no tenant context returns **zero rows**, never every row — the isolation mechanism fails closed by construction.
4. **There is no exported unscoped query path.** `postgres.DB` does not expose `Query`/`Exec` directly; only `WithTenant` and `WithInstanceScope` yield a `*Tx` that can run statements, and both set the tenant context first. The one exception, `DB.SQL()`, is reserved for connection-pool statistics and is itself an enumerated, tested exception (see Verification).
5. **A handful of reads must run before any tenant is known** — resolving a session cookie, an OIDC `client_id`, a refresh token, an invite token, an MFA factor id. These are answered by narrowly-scoped `SECURITY DEFINER` functions rather than by relaxing any policy; the full catalogue, and the specific bound each one respects, is in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`.
6. **Genuinely cross-tenant work** (an `INSTANCE_OWNER` listing every organization, signing-key rotation, partition maintenance) uses `WithInstanceScope(reason, fn)` instead of `WithTenant`. This sets the tenant to the empty string — which every ordinary policy reads as "no tenant," not "every tenant" — requires a non-empty, logged `reason`, and is a differently-named function specifically so that reaching for it is a visible choice in a code diff rather than an omitted filter.

### The one two-sided exception, by design

`project_grants` is visible to **two** organizations at once — the granting and the receiving side of a delegation — because the row is the delegation contract itself and each side needs to see it for a different reason. Writes are still one-sided: only the granting organization may insert or update. This is documented in full, with the exact policy text, in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`; the tenancy point to take from it here is that "visible to" and "may act through" are different questions even within a single policy, and RLS answers only the first — the delegation trigger described in `docs/MULTI-TENANCY/01-ORGANIZATION-AND-PROJECT-HIERARCHY.md` answers the second.

### The one deliberately unprotected table

`manager_roles` has no tenant-scoped RLS policy at all — not a weaker one, none. It cannot be scoped by organization because it is read *during* permission resolution, before a tenant context can be said to exist (what the caller may access is exactly what is being determined at that moment), and the correct policy would need to key on the current *user*, which requires an `app.current_user_id` session setting this codebase does not implement anywhere in `backend/internal/storage/postgres`. This is recorded in the migration itself as a real, tracked gap (`DV-02`, `TASKS/BACKLOG.md`) rather than an oversight — full text and the current status discrepancy (the backlog says this should have closed by `P2-05`, which is separately marked done) are in `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Tenant context propagation | `SET LOCAL` via a parameterized `set_config` call, once per transaction | `backend/internal/storage/postgres/postgres.go` |
| No tenant set ⇒ zero rows | fail-closed by construction (`NULL` comparison) | `current_org_id()`, `docs/DATABASE/02` |
| No unscoped query path | enforced by API shape (`WithTenant`/`WithInstanceScope` only) and checked mechanically | `postgres.go`; `TestOnlyNamedPlacesBypassTheTenantScope` |
| Cross-tenant access is a named, logged, reasoned exception | `WithInstanceScope(reason, fn)` | `postgres.go` |
| `manager_roles` tenant policy | none — open, tracked gap | `DV-02`, `docs/DATABASE/02` |

## Interfaces

Not applicable — see `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` for the SQL surface.

## Security Considerations

`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` names the abuse cases this mechanism closes; the ones most specific to multi-tenancy:

- **A forgotten `WHERE org_id = ...` in application code** — the exact failure `docs/PLAN/08` Part B's "even when the application layer forgets to filter" targets. Closed by RLS rather than by code review, and proven by tests that deliberately issue the unfiltered query (see Verification).
- **An IDOR reading another tenant's row by a known id** — a different failure from the one above (a correctly-filtered list endpoint can still honor a direct lookup by id incorrectly). Closed by the same policies, proven by a separate test.
- **A connection accidentally running with elevated database privileges** (the owner, or any role with `BYPASSRLS`) — RLS is a property of the *role*, not the *code*, so this is the one way the guarantee above can be silently defeated. Closed by the two-role split and the startup self-check (`docs/DATABASE/00-DATABASE-ARCHITECTURE.md`).
- **A tenant asserted by the request itself** — headers, form fields, query parameters — is a distinct failure mode from a database-level leak, and is closed one layer up, in tenant *resolution* rather than tenant *enforcement*; see [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md).

## Verification

The suite in `backend/tests/security/` exists specifically so "have we covered the threat model" is answerable by running `go test ./tests/security/...` rather than by memory (`backend/tests/security/isolation_test.go`'s own package comment). Relevant to tenant isolation specifically:

- `TestUnfilteredQueryCannotSeeAnotherTenant` — issues a deliberately unfiltered `SELECT` and asserts only the caller's own tenant's row comes back.
- `TestIsolationTestsAreNotVacuous` — the control for the test above: connects as the schema owner (bypassing RLS) and asserts both tenants' rows actually exist, so a passing isolation test cannot mean "the data was never there."
- `TestCrossTenantReadsAreEmpty` — the IDOR case: a direct lookup by another tenant's known id returns `ErrNoRows`, not a row.
- `TestRuntimeRoleCannotBypassRLS` — asserts the connection used by every other test in the file is actually `auth_app`, not a superuser, not `BYPASSRLS`.
- `TestEveryTenantScopedTableIsInvisibleAcrossOrganizations`, `TestNoRowCanReferenceAnotherOrganizationsProject`, `TestASecondOrganizationNeedsNoMigration` (`backend/tests/security/multiorg_test.go`) — isolation and cross-reference checks with real multi-organization data through the runtime role.
- `TestNoRequestInputCanNameTheTenant`, `TestTrustingProxyHeadersDoesNotExtendToTheTenant` (`backend/tests/security/tenant_resolution_test.go`) — source-scans for any header, form field or query parameter that could name a tenant, and confirms the trusted-proxy-header exception (`AUTH_TRUST_PROXY_HEADERS`) is scoped to client address only, never to tenant identity.
- `TestOnlyNamedPlacesBypassTheTenantScope` (`backend/tests/security/tenancy_test.go`) — scans every non-test `.go` file for `.SQL()` and fails unless the caller is in an explicit, reasoned allowlist (currently: OIDC client resolution, refresh-token/session bootstrap reads, and infrastructure metrics/signing-key code in `cmd/authservice/main.go`).
- `TestRowLevelSecurityDoesNotForceSequentialScansAtScale`, `TestTheTenantPredicateReachesTheIndex` (`backend/tests/security/rls_plans_test.go`) — RLS holding at 400-tenant scale without degrading into sequential scans; see `docs/DATABASE/03-INDEXING-AND-QUERY-OPTIMIZATION.md`.
- CI: "Every tenant-scoped table has row-level security policy" (`.github/workflows/ci.yml`) — a mechanical, schema-introspecting check that runs on every PR, independent of any Go test.

## Not Yet Built / Open Questions

- `manager_roles` has no tenant (or user) RLS policy — see [`00-MULTI-TENANCY-ARCHITECTURE.md`](./00-MULTI-TENANCY-ARCHITECTURE.md) and `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` for the full status, including the unresolved contradiction between `TASKS/BACKLOG.md`'s `DV-02` and `TASKS/PROGRESS.md`'s "P2-05 done" mark.
- The granting-side RLS read policy on delegated `user_grants` rows is designed but not built (`P4-04`); see `docs/MULTI-TENANCY/01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`.

## Related Documents

- [`00-MULTI-TENANCY-ARCHITECTURE.md`](./00-MULTI-TENANCY-ARCHITECTURE.md), [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md), [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md)
- `docs/DATABASE/00-DATABASE-ARCHITECTURE.md`, `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`, `docs/DATABASE/03-INDEXING-AND-QUERY-OPTIMIZATION.md`
- `docs/PLAN/08-AUTHORIZATION.md` Part B, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
- `TASKS/BACKLOG.md` DV-02

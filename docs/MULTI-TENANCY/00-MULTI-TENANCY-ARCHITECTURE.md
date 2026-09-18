# 00 - Multi-Tenancy Architecture Overview

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-08, P1-06, P2-05, P2-08, P2-09, P4-01…P4-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Give the top-level picture of how this codebase implements multi-tenancy: a single shared PostgreSQL schema, one deployment instance today, organizations as the tenant boundary, and a tenant-resolution rule that differs from what `docs/PLAN/08-AUTHORIZATION.md` Part B originally specified — deliberately, and recorded as such.

## Scope

This document is the entry point; it summarizes and links rather than duplicating each mechanism in full:

- entity hierarchy and the manager-role hierarchy that administers it — [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md);
- the isolation guarantee itself (row-level security, the tenant-context mechanism, verification) — [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md);
- how a request's tenant is actually determined, and why it is not by custom domain — [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md);
- exact schema, SQL policy text, and index detail — `docs/DATABASE/`.

Design intent lives in `docs/PLAN/08-AUTHORIZATION.md` Parts B and C; this category says how it was actually built and names every place the two diverge, with the ADR or backlog entry that recorded the decision.

## As Built

**Shared schema, not schema-per-tenant or database-per-tenant.** Every organization's rows live in the same tables, distinguished by an `org_id` column (or, for `organizations` itself, by `id`), with PostgreSQL row-level security as the isolation mechanism rather than separate schemas or databases. See [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md) for the full mechanism and its verification.

**Entity hierarchy**: `instances → organizations → projects → applications`, with `roles` and `user_grants` scoped to a project, and `manager_roles` layered on top to say who *administers* each level. One `instances` row exists in any deployment today; the schema supports more, but `organization_create()` refuses to run against more than one (see `docs/DATABASE/00-DATABASE-ARCHITECTURE.md`). Full detail in [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md).

**Cross-organization delegation** (Project Grants) is a deliberate, narrow exception to "one tenant per row": `project_grants` and delegated `user_grants` rows are how one organization's project can be partially administered by another organization's people, without either organization gaining blanket access to the other. This is Phase 4 work, partially built — see [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md) for exactly what is and is not implemented.

**Tenant resolution is by OIDC client, not by domain, subdomain, path or header** (ADR-023, `MEMORY/DECISIONS.md`). `docs/PLAN/08` Part B lists four combinable options — subdomain, path segment, email domain at login, or a single default organization for MVP — and the service implements none of them for the authentication path. Instead: every OAuth/OIDC request names a `client_id`; that client belongs to a project; the project belongs to exactly one organization; so the tenant is known before a password is ever typed, from a value the protocol already requires the caller to supply. This was recorded as a deliberate deviation from the plan (`TASKS/BACKLOG.md` PG-33) rather than applied silently. The Management API uses a different, complementary rule: the organization is named explicitly in the URL path and checked against the caller's `manager_roles`. Full detail, including why this is considered *stronger* than the plan's four options, in [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md).

**The tenant context is a database session setting, set once per transaction.** `postgres.DB.WithTenant`/`WithInstanceScope` (`backend/internal/storage/postgres/postgres.go`) issue `SET LOCAL app.current_org_id` before any other statement in the transaction; every RLS policy reads that setting through `current_org_id()`. This is the single mechanism that connects "which organization does this request belong to" (resolved in Go, per the paragraph above) to "which rows may this transaction see" (enforced in PostgreSQL). See [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md).

**Per-organization configuration** — password policy, MFA requirement, session lifetime, allowed login methods — lives in `organizations.settings jsonb`, with a per-key deep merge on `PATCH` (`docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` — `jsonb_deep_merge`) so that changing one setting cannot silently discard a sibling. When a user reaches a project through a Project Grant, **both organizations' policies apply and the stricter wins** for MFA, sign-in methods (intersection) and session lifetime (shorter) — ADR-025 — though this rule's enforcement point (`P4-04`) is not yet built; see [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md).

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Isolation model | Shared schema, row-level security by `org_id` | `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md` |
| Tenant resolution (authentication flows) | By OIDC `client_id` → project → organization | ADR-023; `backend/internal/oauth/client/store.go` |
| Tenant resolution (Management API) | Organization named in the URL path, checked against caller's `manager_roles` | `backend/internal/management/roles.go`, `policy.go` |
| Deployment instances today | One (`instances` has exactly one row; enforced by `organization_create()`) | `docs/DATABASE/00-DATABASE-ARCHITECTURE.md` |
| Custom domain-based routing | Not built — `organizations.domain` is stored but not read by any routing code | [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md) |
| Cross-organization policy conflict | Stricter of both organizations wins (MFA, session lifetime); sign-in methods intersect | ADR-025 — decided, enforcement point (`P4-04`) not yet built |

## Interfaces

Not applicable at this overview level — see the linked documents for endpoints, tables and configuration keys.

## Security Considerations

- **Tenant forgery via request input** is the abuse case client-based resolution is chosen specifically to close: "there is nothing for a caller to supply and therefore nothing to forge" (ADR-023). Verified mechanically by `TestNoRequestInputCanNameTheTenant` (`backend/tests/security/tenant_resolution_test.go`), which scans the source tree for headers, form fields or query parameters that could name an organization.
- **Confused deputy across organizations** in the delegation model is covered in [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md) and `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`.
- See `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` for the full threat model this category sits inside.

## Verification

- `backend/tests/security/tenant_resolution_test.go`, `tenancy_test.go`, `multiorg_test.go`, `isolation_test.go`, `rls_plans_test.go` — see [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md) for what each proves.

## Not Yet Built / Open Questions

- Cross-organization sign-in (a session from one organization reaching a client in another, even under a Project Grant) is refused outright today; ADR-025 decides the policy that will govern it once built, but the enforcement point (`P4-04`) does not exist yet.
- Custom-domain tenant routing — see [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md).
- Multi-instance deployments (plural `instances` rows) — schema-ready, not built or exercised.

## Related Documents

- [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md), [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md), [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md)
- `docs/DATABASE/00-DATABASE-ARCHITECTURE.md`, `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`, `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`
- `docs/PLAN/08-AUTHORIZATION.md` Parts B and C
- `MEMORY/DECISIONS.md` ADR-023, ADR-025; `TASKS/BACKLOG.md` PG-33

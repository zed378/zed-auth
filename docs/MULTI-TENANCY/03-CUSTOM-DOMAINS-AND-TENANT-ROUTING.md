# 03 - Custom Domains and Tenant Routing

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-06, P2-09 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State plainly how a request's tenant is actually determined in this codebase, and that it is **not** by custom domain. `organizations.domain` exists as a column, but no authentication, routing or branding code reads it to resolve a tenant. The routing mechanisms that do exist — client-based for authentication, path-based (checked against roles) for the Management API — are implemented and tested; custom-domain routing is not built and has no scheduled owning task (see `TASKS/BACKLOG.md` PG-33).

## Scope

This document covers request-level tenant routing only. The database-level isolation that applies once a tenant is determined is [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md). The `organizations.domain` column's exact type and constraints are in `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`.

## As Built

### What actually resolves the tenant

**Authentication and token flows (OAuth/OIDC): by `client_id`.** Every `/oauth/authorize` and `/oauth/token` request names a `client_id` in the request itself (query string or POST body) — a value the protocol already requires the caller to supply for the flow to work at all. `backend/internal/oauth/client/store.go` resolves that client (via the pre-tenant `SECURITY DEFINER` function `application_by_client_id`, `docs/DATABASE/02-ROW-LEVEL-SECURITY-POLICIES.md`) to its `project_id`, and the project resolves to exactly one `org_id`. The tenant — and with it, that organization's branding and password policy (`P1-12`) — is known before the login page is even rendered, let alone before a password is typed.

**The Management API: by URL path, checked against the caller's roles.** Every `/v1/organizations/{org_id}/...` route names the organization explicitly as a path segment. This is **not** trusted blindly the way a domain or header would be: `backend/internal/management/roles.go`'s `Authorize` function checks the path's `org_id` against the caller's own `manager_roles` (or, for `ScopeInstance` routes, requires `INSTANCE_OWNER` outright) on every request. `backend/tests/security/tenant_resolution_test.go`'s source-scan test explicitly excludes `chi.URLParam(r, "org_id")` from its list of forbidden tenant-naming patterns, with the reasoning stated in the test itself: "the Management API takes the organization in the path BY DESIGN and checks it against the caller's manager roles... The path is part of the contract; a header is not."

**No header, form field, query parameter, or `Host` header names the tenant anywhere else.** `TestNoRequestInputCanNameTheTenant` (`backend/tests/security/tenant_resolution_test.go`) scans every non-test `.go` file for `X-Org-Id`/`X-Organization`/`X-Tenant` headers and `org_id`/`organization`/`tenant` query or form parameters, and fails if any appear outside the Management API's own path-parameter contract. `TestTrustingProxyHeadersDoesNotExtendToTheTenant` confirms separately that the one header the service does trust when configured to (`X-Forwarded-For`, gated by `AUTH_TRUST_PROXY_HEADERS`) is used only for the caller's network address, never for tenant identity.

### Why this rather than domain-based routing — ADR-023

`docs/PLAN/08-AUTHORIZATION.md` Part B § Tenant Resolution lists four combinable options, naming a single default organization as the MVP choice: **by domain** (`acme.auth.company.com`), **by path**, **by email domain at login**, or a single default organization. The service, as built, implements **none of the four as the primary mechanism** for authentication — it resolves the tenant from the OIDC client instead, a fifth option the plan does not name. This was surfaced and recorded deliberately as ADR-023 (`MEMORY/DECISIONS.md`) and PG-33 (`TASKS/BACKLOG.md`), not discovered as a defect:

> The client-based rule has a property none of [the plan's four options] has: there is nothing for a caller to supply and therefore nothing to forge. A request carrying `X-Org-Id: other-tenant` changes nothing, because nothing reads it. (ADR-023)

ADR-023 also notes that this approach *subsumes* the plan's MVP mode rather than replacing it: with a single organization owning every client (today's actual deployment shape), every request resolves to that one organization with no special case — exactly the "single default organization" behavior the plan asked for, arrived at without a hard-coded default. The plan's four options are explicitly left available as **additions**, not replacements, for a deployment that needs a tenant chosen before a client is even named. None is needed today, and none is implemented.

### The `domain` column: stored, not routed on

`organizations.domain` (`text`, nullable, unique on `lower(domain)` where set — `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`) is a real, working column: it can be set and cleared through `PATCH /v1/organizations/{org_id}` (`backend/internal/organization/handler.go`, `store.go`), it is included in every organization read, and a soft-deleted organization is required to release it (`organizations_deleted_has_no_domain` constraint) precisely so the address can be reused by a future tenant.

**No code path reads it to route a request, resolve a tenant, or select branding.** A repository-wide search of `backend/internal/oauth`, `backend/internal/login` and `backend/internal/httpserver` for `domain` finds no match outside comments about subdomain cookie scoping (`csrf.go`, `mfa.go`) — nothing that looks up an organization *by* its domain. The column was added in the baseline migration with its own comment naming exactly this situation: "For domain-based tenant resolution and email-domain routing... Verification itself is a later phase; the column exists now so activating it needs no migration" (`backend/migrations/20260908000002_organizations_and_users.up.sql`). That later phase has not arrived: there is no DNS ownership verification for a claimed domain, no per-organization TLS certificate provisioning, and no `Host`-header-based organization lookup anywhere in the HTTP server or middleware.

**In short: this system does not do custom-domain tenant routing today. Tenant routing is by OIDC client for authentication, and by path (checked against the caller's roles) for the Management API — not by domain.**

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Tenant resolution — authentication flows | By OIDC `client_id` → `project_id` → `org_id` | `backend/internal/oauth/client/store.go`; ADR-023 |
| Tenant resolution — Management API | URL path segment, checked against caller's `manager_roles` | `backend/internal/management/roles.go` |
| No other request input may name a tenant | headers, form fields, query params all excluded | `TestNoRequestInputCanNameTheTenant` |
| Trusted proxy headers scope | client network address only, never tenant identity | `TestTrustingProxyHeadersDoesNotExtendToTheTenant` |
| `organizations.domain` | stored, unique, editable; not read by any routing code | `docs/DATABASE/01-SCHEMA-DEFINITIONS.md` |
| Domain released on organization deletion | `organizations_deleted_has_no_domain` constraint | `backend/migrations/20260910000016_organizations_soft_delete.up.sql` |

## Interfaces

| Method & path | What it does with `domain` |
|---|---|
| `PATCH /v1/organizations/{org_id}` | Sets or clears `domain` (subject to the uniqueness constraint) — storage only, no routing effect |
| `GET /v1/organizations/{org_id}` (and list) | Returns `domain` as a field — display only |

No endpoint, middleware, or configuration key currently maps an incoming `Host` header to an organization.

## Security Considerations

- **Tenant identity asserted by the request** is the abuse case client-based (and role-checked path-based) resolution is chosen to close; see [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md) and `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`. Any future domain-based routing would need to resolve a `Host` header to an organization only through a value verified out-of-band (DNS ownership proof), not merely presented — otherwise it reopens exactly the forgery surface client-based resolution exists to close.
- A `Host` header is client-supplied and, without a trusted, verified mapping, no more trustworthy than any other request-supplied value `TestNoRequestInputCanNameTheTenant` already refuses to accept as tenant identity.

## Verification

- `TestNoRequestInputCanNameTheTenant`, `TestTrustingProxyHeadersDoesNotExtendToTheTenant` — `backend/tests/security/tenant_resolution_test.go`.
- No test exists for domain-based routing, because no such code exists to test.

## Not Yet Built / Open Questions

**Custom-domain routing is not built**, and is not currently a scheduled task. It was one of four tenant-resolution options the original plan weighed generally (`docs/PLAN/08` Part B), not a committed feature with its own specification; once client-based resolution was adopted for the actual authentication path (falling out of `P1-05`'s per-application `org_id` and `P1-06`'s client-first resolution, per ADR-023), the motivating use case — a branded per-organization login hostname — was never separately scoped. `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`'s phase files through Phase 4 ("Enterprise & Interop") do not include it; SAML (`P4-07`–`P4-09`) and social login (`P4-10`/`P4-11`) are the enterprise-interop items actually scheduled there.

If a future task takes this on, constraints already decided by the surrounding architecture apply:

- Tenant identity must never be something a caller's request can merely assert (ADR-023) — a `Host` header would need out-of-band DNS ownership verification before being trusted, not just presented.
- The `domain` column and its uniqueness index already exist and need no migration to activate — only verification, TLS and routing code is missing.
- A domain must be released on organization deletion (already enforced by `organizations_deleted_has_no_domain`), and any new feature must preserve that.

Open questions such a task would need to resolve: the DNS verification method; TLS issuance and renewal per domain; whether a `Host`-based lookup would gate only *which* organization's login page is shown (with the OIDC `client_id` remaining the actual authorization boundary) or would replace client-based resolution outright for that request; and whether `docs/PLAN/08` Part B should simply be amended to name the client-based rule as the default with domain-based routing demoted to an available, unbuilt addition (PG-33's recommendation, not yet applied to the plan document).

## Related Documents

- [`00-MULTI-TENANCY-ARCHITECTURE.md`](./00-MULTI-TENANCY-ARCHITECTURE.md), [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md)
- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md` (`organizations.domain`)
- `docs/PLAN/08-AUTHORIZATION.md` Part B § Tenant Resolution
- `MEMORY/DECISIONS.md` ADR-023; `TASKS/BACKLOG.md` PG-33

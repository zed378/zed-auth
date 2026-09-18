# 00 - API Overview

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-15, P1-16, P1-17, P1-18, P1-19, P1-20, P2-02, P2-03, P2-05, P2-06, P2-13, P3-04, P3-07, P3-09, P3-10, P3-12, P4-01, P4-02, P4-03, P4-05 (built); P3-15 threat review; P4-04, P4-06, P4-07…13, Phase 4B (not built) &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Give the complete, accurate map of what the Auth Service exposes over HTTP: every `/v1` resource family with its base path, the OAuth/OIDC protocol surface, and — as plainly as `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`'s governance rule and `CLAUDE.md` require — which surfaces are not built yet.

## Scope

This document is the index. Cross-cutting HTTP behaviour (standards, auth, versioning, errors, rate limiting, pagination, idempotency) is `01`–`07`; each resource family's endpoints, bodies and validation are `08`–`16`. It does not restate the authorization model (`docs/PLAN/08-AUTHORIZATION.md`, `docs/AUTHORIZATION/`) or the data model (`docs/PLAN/04-DATA-MODEL.md`, `docs/DATABASE/`).

## As Built

The service exposes two distinct HTTP surfaces, not one:

1. **The OAuth 2.1 / OIDC protocol surface** — `/oauth/authorize`, `/oauth/token`, `/oauth/introspect`, `/oauth/revoke`, `/oauth/userinfo`, `/oidc/logout`, and the discovery documents `/.well-known/openid-configuration` and `/.well-known/jwks.json`. Unauthenticated (`security: []` in `openapi/openapi.yaml`), largely form/query encoded, and answering with its own error vocabularies (`OAuthError`, or a redirect carrying `?error=...`) rather than the `Error` envelope. Documented in `08-AUTH-API.md`, which links to `docs/IDENTITY-PROTOCOL/` for the protocol-level detail.
2. **The `/v1` Management REST API** — Bearer-token authenticated, JSON in and out, one error envelope, one pagination shape, one idempotency mechanism. `docs/PLAN/02-REQUIREMENTS.md` FR-14 requires every console capability to exist here; the console is a client of this API with no privileged path around it (`openapi/openapi.yaml` `info.description`).

Two unversioned operational probes exist outside both: `/healthz` (liveness) and `/readyz` (readiness) — see `openapi/openapi.yaml` and `docs/PLAN/14-DEPLOYMENT.md`.

**Base URL** (staging): `https://auth.zedth.my.id` (`openapi/openapi.yaml` `servers`). There is no separate host or path prefix per environment beyond this — `/v1` is a path prefix, not a subdomain.

**The contract is `openapi/openapi.yaml`, not this document.** It is hand-written and generates `backend/internal/api/api.gen.go` (the Go `StrictServerInterface` every handler implements), the console's TypeScript client, and `public-site/docs/api-reference/` (ADR-013, `openapi/README.md`). A handler whose signature no longer matches the spec fails to compile. **The canonical list of endpoints that have actually shipped is `scripts/openapi-shipped-paths.py`'s `SHIPPED` set** — CI (`.github/workflows/ci.yml`) and `scripts/check.sh` fail the build if `openapi.yaml` documents a path that is not in `SHIPPED`, which is what stops the public API reference from ever claiming an endpoint that returns 404.

**Every `/v1` route passes through one middleware chain**, composed once in `backend/internal/management/chain.go`:

```
Require (authenticate + authorize) -> RateLimit -> BufferBody -> Idempotency -> AuditGuard -> handler
```

`Chain.Guarded` wraps the generated router (every operation, one chain); `Chain.Handle` is the same chain for a hand-registered route. This is why `01`–`07` describe uniform, cross-cutting behaviour rather than per-endpoint rules: a control implemented once here is enforced identically on every resource in `08`–`16`.

**Every route's required role and scope live in one table**: `Policy` in `backend/internal/management/policy.go`, keyed by `"METHOD /route/{pattern}"`. A route absent from the table gets the zero `Requirement`, which no caller can satisfy — unreachable by default rather than open by omission. `TestEveryV1RouteHasADeclaredPermission` (`backend/internal/httpserver/v1_routes_test.go`) fails if the generated router registers a route the table does not cover. See `02-AUTHENTICATION-AND-AUTHORIZATION.md` for the full table rendered as a reference, and `docs/PLAN/08-AUTHORIZATION.md` for what the roles mean.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Staging base URL | `https://auth.zedth.my.id` | `openapi/openapi.yaml` `servers` |
| `/v1` path prefix | Every Management API route | `backend/internal/httpserver/`, generated router mount |
| Request/response media type (`/v1`) | `application/json`, except raw file uploads (none exist today) | `openapi/openapi.yaml` |
| Request media type (OAuth/OIDC) | `application/x-www-form-urlencoded` (`/oauth/token`, `/oauth/introspect`, `/oauth/revoke`); query parameters (`/oauth/authorize`, `/oidc/logout`) | `openapi/openapi.yaml` |
| Authentication | OAuth 2.0 Bearer access token (`Authorization: Bearer <token>`); no API-key header, no `X-Organization-Id` header — the tenant comes from the token's `org_id` claim or the path | `backend/internal/management/middleware.go` |
| `Cache-Control` on every `/v1` response | `no-store` | `backend/internal/management/errors.go`, `middleware.go` |
| Full endpoint list, statuses, request/response schemas | Generated reference | `public-site/docs/api-reference/`, source `openapi/openapi.yaml` |

## Interfaces

Every `/v1` resource family, its base path, and where it is documented:

| Resource family | Base path | Documented in | Manager role floor |
|---|---|---|---|
| Caller's own account | `/v1/me`, `/v1/me/password` | `14-SESSION-API.md` | none — `Member`/`ScopeSelf` |
| Caller's own organizations | `/v1/me/organizations` | `16-ADMIN-API.md` | none — `Member`/`ScopeSelf` |
| Caller's own MFA | `/v1/me/mfa`, `/v1/me/mfa/totp*`, `/v1/me/mfa/factors/{id}`, `/v1/me/mfa/recovery-codes` | `14-SESSION-API.md` | none for reads; recent authentication for writes |
| Caller's own sessions | `/v1/me/sessions*` | `14-SESSION-API.md` | none — `Member`/`ScopeSelf` |
| Organizations (instance-wide) | `/v1/organizations` | `16-ADMIN-API.md` | `INSTANCE_OWNER` |
| Organizations (single tenant) | `/v1/organizations/{org_id}` | `10-ORGANIZATION-API.md` | `ORG_ADMIN` read, `ORG_OWNER` write, `INSTANCE_OWNER` for `status` and delete |
| MFA mandate impact | `/v1/organizations/{org_id}/mfa-impact` | `10-ORGANIZATION-API.md` | `ORG_ADMIN` |
| Users | `/v1/organizations/{org_id}/users*` | `09-USER-API.md` | `ORG_ADMIN` |
| A member's MFA (admin view/reset) | `/v1/organizations/{org_id}/users/{user_id}/mfa`, `/mfa-reset` | `14-SESSION-API.md` | `ORG_ADMIN` |
| A member's sessions (admin) | `/v1/organizations/{org_id}/users/{user_id}/sessions*` | `14-SESSION-API.md` | `ORG_ADMIN` |
| Direct user grants (roles a user holds per project) | `/v1/organizations/{org_id}/users/{user_id}/grants*` | `12-ROLE-AND-PERMISSION-API.md` | `ORG_ADMIN` |
| Projects | `/v1/organizations/{org_id}/projects*` | `11-PROJECT-API.md` | `ORG_ADMIN` read/write, `ORG_OWNER` delete |
| Applications (OIDC clients) | `/v1/organizations/{org_id}/projects/{project_id}/applications*` | `11-PROJECT-API.md` | `ORG_ADMIN`, `ORG_OWNER` delete |
| Roles | `/v1/organizations/{org_id}/projects/{project_id}/roles*` | `12-ROLE-AND-PERMISSION-API.md` | `PROJECT_OWNER` or an organization-scoped role |
| Project Grants — granting side | `/v1/organizations/{org_id}/projects/{project_id}/grants*` | `13-PROJECT-GRANT-API.md` | `PROJECT_OWNER` or an organization-scoped role |
| Delegated user grants and grant owners — receiving side | `/v1/organizations/{org_id}/project-grants/{grant_id}/*` | `13-PROJECT-GRANT-API.md` | `ORG_ADMIN` or `PROJECT_GRANT_OWNER` |
| Authorization decisions | `/v1/authz/check` | `12-ROLE-AND-PERMISSION-API.md` | `Member` (own organization and project only) |
| Audit log | `/v1/organizations/{org_id}/events` | `15-AUDIT-LOG-API.md` | `ORG_ADMIN` |
| OAuth/OIDC protocol | `/oauth/*`, `/oidc/logout`, `/.well-known/*` | `08-AUTH-API.md` | none — public |
| Operational probes | `/healthz`, `/readyz` | this document | none — public, unversioned |

## Security Considerations

- The `SHIPPED` allowlist in `scripts/openapi-shipped-paths.py`, gated in CI, is the control that keeps this documentation category (and the generated public reference) from ever describing a capability beyond the current roadmap phase — the exact governance rule `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` and `CLAUDE.md` state.
- There is no `X-Organization-Id` or similar client-supplied tenant header anywhere in this API. The tenant is always the token's `org_id` claim or a path parameter checked against the caller's grants — see `02-AUTHENTICATION-AND-AUTHORIZATION.md` and `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2/§3 for why a client-supplied tenant would be a cross-tenant hole.

## Verification

- `TestEveryV1RouteHasADeclaredPermission` — `backend/internal/httpserver/v1_routes_test.go`: every route the generated router registers has a `Policy` entry.
- `python3 scripts/openapi-shipped-paths.py` — `.github/workflows/ci.yml`, `scripts/check.sh`: fails the build if the spec documents a path outside `SHIPPED`, or if `SHIPPED` names a path the spec no longer documents.
- Staging acceptance through Phase 3: `MEMORY/records/2026-09-15-P3-phase-3-summary.md`. Phase 4 so far (`P4-01`, `P4-02`, `P4-03`, `P4-05`): `MEMORY/records/2026-09-15-P4-01-project-grants.md`, `2026-09-17-P4-02-delegated-user-grants.md`, `2026-09-17-P4-03-project-grant-owner.md`, `2026-09-15-P4-05-project-grants-tab.md`.

## Not Yet Built / Open Questions

Per `TASKS/PROGRESS.md` (current phase: Phase 4 — Enterprise Interop, 4/16 cards done) and `TASKS/PHASE-4-ENTERPRISE-INTEROP.md`, the following do **not** exist in this API and must not be implied by documentation or marketing copy:

- **Delegated roles in tokens and `/v1/authz/check`, and revocation propagation** — `P4-04` (`TODO`). A Project Grant and its delegated user grants exist and are readable (`13-PROJECT-GRANT-API.md`), but a delegated role does not yet appear in an access token's claims and `/v1/authz/check` does not yet evaluate delegated grants at all.
- **A "granted projects" read endpoint for the receiving organization** — the console feature `P4-06` (`TODO`) has no dedicated API to list Project Grants received by an organization; today a caller must already hold a `grant_id` to reach `/v1/organizations/{org_id}/project-grants/{grant_id}/*`. Flagged in `13-PROJECT-GRANT-API.md`.
- **SAML 2.0** as an identity provider — `P4-07`, `P4-08`, `P4-09` (`TODO`). `docs/IDENTITY-PROTOCOL/04-SAML-20-FEDERATION.md` is design intent, not a built surface.
- **Social login federation and account linking** — `P4-10`, `P4-11` (`TODO`).
- **Webhooks for platform events** — `P4-12` (`TODO`). No `/v1/webhooks*` namespace exists.
- **SCIM provisioning** — `P4-13` (`TODO`, marked optional in the roadmap).
- **Attribute-based access control (ABAC)** — Phase 4B, not started. `/v1/authz/check` accepts `resource.attributes` and `context` today and ignores them (`openapi/openapi.yaml` `AuthorizationCheck`); they are reserved for this phase.
- **A `/v2` or formal deprecation/sunset mechanism** — see `03-API-VERSIONING.md`.

## Related Documents

- `openapi/openapi.yaml`, `openapi/README.md`
- `scripts/openapi-shipped-paths.py`
- `docs/PLAN/05-API-CONTRACT.md`, `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`, `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`
- `TASKS/PROGRESS.md`, `TASKS/PHASE-4-ENTERPRISE-INTEROP.md`
- `docs/API/01-API-STANDARDS.md` through `docs/API/16-ADMIN-API.md`

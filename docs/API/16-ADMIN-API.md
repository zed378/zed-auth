# 16 - Admin API (Instance-Scoped Routes)

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-16, P2-13 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the routes that are genuinely instance-scoped rather than belonging to one tenant: listing and creating organizations, and a caller's own view of which organizations they administer. State clearly that there is **no `/v1/admin` namespace** in this service — instance-level administration is a small number of ordinary `/v1` routes reachable by the right role, not a separate admin surface with its own health/metrics/key-rotation endpoints.

## Scope

`GET/POST /v1/organizations` (no `{org_id}` — the instance-wide list and create) and `GET /v1/me/organizations`. A single organization's own routes (`GET/PATCH/DELETE /v1/organizations/{org_id}`, `mfa-impact`) are `10-ORGANIZATION-API.md`, not this document, even though both live under `/v1/organizations`.

## As Built

**There is no `/v1/admin/*` path anywhere in this API.** Instance-level operations are ordinary `/v1` routes distinguished only by requiring `INSTANCE_OWNER` and `ScopeInstance` in the policy table (`02-AUTHENTICATION-AND-AUTHORIZATION.md`), not a separate namespace, separate authentication scheme, or separate error format. A document or integration that assumes a `/v1/admin` prefix, a health-check-with-dependency-status endpoint, or a key-rotation-trigger endpoint is describing a surface this service does not have:

- **System health with dependency status** (`{"database": "up", "redis": "up"}`) does not exist. `/healthz` and `/readyz` (`00-API-OVERVIEW.md`) are the only health probes, and `readyz`'s failure body deliberately names no dependency — it is reachable from the public ingress, and "which of our dependencies is down" is not something to tell an anonymous caller (`openapi/openapi.yaml` `getReadiness` description).
- **A signing-key-rotation trigger endpoint** does not exist. Key rotation is an operational procedure (`docs/PLAN/14-DEPLOYMENT.md`), not an API call; `signing_key.rotated` is an audit event a rotation writes, not something `/v1` can invoke.
- **A Prometheus metrics scrape endpoint** is not part of the Management API's authenticated surface; metrics exposure (if any) is an observability/deployment concern (`docs/PLAN/13-OBSERVABILITY.md`), not a documented `/v1` route.

**`GET/POST /v1/organizations` (no path parameter) is instance-scoped because it is genuinely cross-tenant** — a list that spans every tenant, and a create that happens before the tenant it makes exists — and requires `INSTANCE_OWNER`. An organization administrator reads their own organization through `GET /v1/organizations/{org_id}` instead (`10-ORGANIZATION-API.md`), which requires only `ORG_ADMIN`.

**`GET /v1/me/organizations` is the one route in the entire `/v1` surface that requires no role at all, and it is safe precisely because it grants nothing.** It reports the organizations the caller holds a manager role over, derived entirely from their own `manager_roles` rows — there is nothing it could disclose about an organization the caller does not already administer. `INSTANCE_OWNER` administers every organization, so this answers the same set as `GET /v1/organizations` for such a caller, paginated the same way. It exists because the console's organization switcher must show exactly the organizations the caller may act in, and deriving that from the access token's `urn:authservice:manager_roles` claim would be wrong twice — the claim carries role names without their scopes, and a role in a token is a snapshot up to ten minutes stale.

**`AdministeredOrganization` is deliberately a smaller shape than `Organization`.** No `settings`, no `status`, no timestamps — this is a switcher's list, and an endpoint that requires no permission returns the least it can; an organization's settings remain readable only through `GET /v1/organizations/{org_id}`, which does require `ORG_ADMIN`. `PROJECT_OWNER` is deliberately absent from `roles` here (and so is an organization reached only through one) — its scope is a project, and offering a switch into an organization where every screen beyond the switcher would answer `403` would be a switcher whose entries lead nowhere.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| `/v1/admin` namespace | Does not exist | — |
| Health check with dependency detail | Does not exist; `/readyz` names no dependency | `openapi/openapi.yaml` `getReadiness` |
| Key rotation trigger endpoint | Does not exist; rotation is operational, not an API call | `docs/PLAN/14-DEPLOYMENT.md` |
| Metrics scrape endpoint | Not part of the documented `/v1` contract | `docs/PLAN/13-OBSERVABILITY.md` |
| `GET/POST /v1/organizations` role | `INSTANCE_OWNER`, `ScopeInstance` | `backend/internal/management/policy.go` |
| `GET /v1/me/organizations` role | None — `Member`/`ScopeSelf` | `policy.go` |
| `AdministeredOrganization.roles` | `INSTANCE_OWNER` \| `ORG_OWNER` \| `ORG_ADMIN` only — never `PROJECT_OWNER` | `openapi/openapi.yaml` |

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET /v1/organizations` | `listOrganizations` | `INSTANCE_OWNER` |
| `POST /v1/organizations` | `createOrganization` | `INSTANCE_OWNER` |
| `GET /v1/me/organizations` | `listAdministeredOrganizations` | none |

Full schemas: `public-site/docs/api-reference/list-organizations.api.mdx`, `create-organization.api.mdx`, `list-administered-organizations.api.mdx`.

Idempotency: `Idempotency-Key` accepted on `createOrganization`.

Audit events: `organization.created`.

**Instance-wide visibility into the audit log** is not a separate endpoint here — see `15-AUDIT-LOG-API.md`'s Not Yet Built section: an `INSTANCE_OWNER` reads any single organization's events through that organization's own `GET .../events` route (their grant satisfies the `ORG_ADMIN` requirement), and there is no endpoint that aggregates events across every organization in one response.

## Security Considerations

- `GET /v1/me/organizations` is safe with no role requirement specifically because its output is a strict function of the caller's own `manager_roles` rows — see `02-AUTHENTICATION-AND-AUTHORIZATION.md` for why this is the one deliberate exception, not a gap.
- The absence of a `/v1/admin` namespace means there is no separate authentication or authorization model to keep in sync with the rest of `/v1` — instance-level routes go through exactly the same `Require`/`Policy` mechanism as every other route, which is also why they are covered by `TestEveryV1RouteHasADeclaredPermission`.

## Verification

- `backend/internal/organization/administered_integration_test.go` (`listAdministeredOrganizations`).
- `backend/internal/organization/store_integration_test.go`, `endpoints_integration_test.go` (`listOrganizations`, `createOrganization`).

## Not Yet Built / Open Questions

- No aggregated, cross-organization audit query exists (see `15-AUDIT-LOG-API.md`).
- No system-health, key-rotation, or metrics endpoint exists under any authenticated API surface; if one is ever wanted, it does not yet have an owning task in `TASKS/`.

## Related Documents

- `docs/API/10-ORGANIZATION-API.md` (single-tenant organization routes)
- `docs/API/15-AUDIT-LOG-API.md`
- `docs/API/00-API-OVERVIEW.md`
- `docs/PLAN/14-DEPLOYMENT.md`, `docs/PLAN/13-OBSERVABILITY.md`

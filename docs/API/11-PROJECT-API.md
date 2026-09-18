# 11 - Project API (Projects & Applications)

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-17, P1-18 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document projects (the container roles, applications, and Project Grants hang from) and applications (registered OIDC clients) nested under them.

## Scope

`/v1/organizations/{org_id}/projects*` and `/v1/organizations/{org_id}/projects/{project_id}/applications*`. Roles nested under a project are `12-ROLE-AND-PERMISSION-API.md`; Project Grants nested under a project are `13-PROJECT-GRANT-API.md`.

## As Built

**A project has no configuration of its own beyond a name.** It is a container — for applications, roles, and (from Phase 4) Project Grants. Settings that could conceivably live here have a home already: an application has its own, and an organization's policy already applies to everything inside it.

**Project delete refuses while anything is attached, and is not cascaded.** `DELETE` on a project with any application or role still attached is `409`, with `details` naming what blocks it and how many. It is deliberately not cascaded: deleting a project would take every OIDC client inside it down at once, silently, for every consumer application configured against those client ids — a far larger blast radius than "delete this container" appears to ask for. A project with nothing attached is deleted outright (not soft-deleted) — unlike an organization, a project owns no audit history of its own and holds no tenant boundary, so there is nothing for a tombstone to preserve.

**An application's `id` *is* its OIDC `client_id`.** There is no separate client identifier; two identifiers for one thing is how a console (or an integrator) ends up showing the wrong one.

**A client secret is shown exactly once, at creation or rotation, and never again.** It is stored as a hash and cannot be recovered — an application whose secret is lost is rotated, not recalled. Every other read of an application answers only `has_secret: boolean`. A public client (`type: spa` or `type: native`) never receives a secret at all — it cannot keep one confidential in a browser or a shipped binary, so it authenticates with PKCE instead, and issuing it a secret would be a false reassurance rather than a control.

**`type` is immutable after creation, and naming it on update is an error, not a no-op.** A client's type decides whether it can hold a secret; changing it in place would either strand a secret on a now-public client or leave a confidential one with none. Reclassifying means delete and re-register, which appears as two audit events rather than one silent reclassification.

**Redirect URIs and post-logout redirect URIs are matched by exact string comparison, both at authorization time and here.** The same validator runs on create and on update — a validator that only ran on one of the two paths would not be a validator (prefix/pattern matching on a redirect URI is the open-redirect vulnerability itself: `docs/PLAN/09-SECURITY.md` § Protection Against Common Attacks).

**`allowed_origins` is empty by default, meaning no cross-origin browser access at all**, and is per-application rather than instance-wide — an origin one organization registers for its SPA must not let it read another organization's data through a shared allowlist. Matched against the browser's `Origin` header by exact string comparison; `https` only, except loopback addresses for local development. `/oauth/token` and the discovery documents are **not** governed by this list — they answer any origin, because neither is authenticated by anything a browser attaches automatically.

**Secret rotation keeps the previous secret working for an overlap window, by default.** `POST .../rotate-secret?overlap_hours=N` (default 24, max 168, `overlap_hours: 0` retires the old secret immediately — the compromised-secret path, where the outage for anything still using it is the point). Rotation requires only `ORG_ADMIN`, deliberately not `ORG_OWNER`: it is the response to a suspected leak, and a control that requires waking the organization owner is a control that gets skipped at 3am — it is loud in the audit log instead. `overlap_hours` is a query parameter rather than a request-body field, a deliberate deviation from the original contract shape: `oapi-codegen` decodes a JSON body unconditionally, so a bare `POST` with no body answered `400`; the query parameter keeps `POST .../rotate-secret` working with nothing at all. A public client has no secret to rotate and is refused.

**Application delete requires `ORG_OWNER`**, the one operation on an application an `ORG_ADMIN` cannot perform — every user signing in through that client stops being able to, at once, without warning to the consumer application.

## Rules and Defaults

| Field / rule | Value | Enforced in |
|---|---|---|
| Project `name` | Required, 1–200 chars; unique within the organization, case-insensitive | `openapi/openapi.yaml` `ProjectCreate`/`Project` |
| Project delete | Refused (`409`) while an application or role is attached; hard-deleted otherwise, not soft-deleted | `openapi/openapi.yaml` `deleteProject`; `backend/internal/project/store.go` |
| Application `name` | Required, 1–200 chars | `openapi/openapi.yaml` `ApplicationCreate` |
| Application `type` | `web`, `native`, `spa`, `api`, `saml`; immutable after creation | `openapi/openapi.yaml` `ApplicationType` |
| `redirect_uris` / `post_logout_redirect_uris` | Exact-match at authorization/logout time; same validation on create and update | `backend/internal/oauth/authorize/`, `backend/internal/login/logout.go` |
| `allowed_origins` | Empty by default (no cross-origin access); `https` only except loopback; per application | `openapi/openapi.yaml` `Application.allowed_origins` |
| `grant_types` | Defaults to `["authorization_code", "refresh_token"]`; `implicit` and `password` refused for every type | `openapi/openapi.yaml` `ApplicationCreate` |
| `client_secret` visibility | Returned once, at create (`ApplicationCreated`) or rotation (`RotatedSecret`); never again | `openapi/openapi.yaml` |
| Secret rotation overlap | Query param `overlap_hours`, 0–168, default 24 | `openapi/openapi.yaml` `rotateApplicationSecret` |
| Required role — projects & applications, read/write | `ORG_ADMIN` | `backend/internal/management/policy.go` |
| Required role — project delete, application delete | `ORG_OWNER` | `policy.go` |
| Required role — secret rotation | `ORG_ADMIN` | `policy.go` |

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET/POST /v1/organizations/{org_id}/projects` | `listProjects` / `createProject` | `ORG_ADMIN` |
| `GET/PATCH /v1/organizations/{org_id}/projects/{project_id}` | `getProject` / `updateProject` | `ORG_ADMIN` |
| `DELETE /v1/organizations/{org_id}/projects/{project_id}` | `deleteProject` | `ORG_OWNER` |
| `GET/POST .../projects/{project_id}/applications` | `listApplications` / `createApplication` | `ORG_ADMIN` |
| `GET/PATCH .../applications/{application_id}` | `getApplication` / `updateApplication` | `ORG_ADMIN` |
| `DELETE .../applications/{application_id}` | `deleteApplication` | `ORG_OWNER` |
| `POST .../applications/{application_id}/rotate-secret` | `rotateApplicationSecret` | `ORG_ADMIN` |

Full schemas: `public-site/docs/api-reference/create-project.api.mdx`, `get-project.api.mdx`, `update-project.api.mdx`, `delete-project.api.mdx`, `create-application.api.mdx`, `get-application.api.mdx`, `update-application.api.mdx`, `delete-application.api.mdx`, `rotate-application-secret.api.mdx`.

Idempotency: `Idempotency-Key` accepted on every `POST`/`DELETE` here.

Audit events: `project.created`, `project.updated` (a rename to the same name writes nothing), `project.deleted`, `application.created`, `application.updated` (payload carries redirect-URI changes **with their values** — not secret, and an entry saying only "redirect_uris changed" cannot answer whether an attacker widened one), `application.secret_rotated`, `application.deleted`.

## Security Considerations

- Exact-string redirect URI matching, applied identically on create and update, is the open-redirect defence for this resource — see `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.
- `allowed_origins` scoped per application, not instance-wide, is what stops one organization's registered origin from reading another organization's token-authenticated responses (ADR-020 covers the general cross-origin split this implements).
- Rotation's overlap window is a deliberate availability/security trade the caller controls per incident: `overlap_hours: 0` for a confirmed leak, the 24-hour default otherwise.

## Verification

- `backend/internal/project/project_integration_test.go`.
- `backend/internal/application/application_integration_test.go`.

## Not Yet Built / Open Questions

- `type: saml` exists in the `ApplicationType` enum as a forward-declared value; the SAML Identity Provider surface it would need (`P4-07`…`P4-09`) is not built. Registering a `saml` application today does not make SAML sign-in work.

## Related Documents

- `docs/API/12-ROLE-AND-PERMISSION-API.md`, `docs/API/13-PROJECT-GRANT-API.md`
- `MEMORY/DECISIONS.md` ADR-020 (cross-origin access split)
- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`

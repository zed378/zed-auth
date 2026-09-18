# 10 - Organization API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-16, P3-07 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document reading and updating a single tenant: profile, status, and settings (password policy, MFA mandate, session lifetime, allowed login methods), plus the MFA-mandate impact preview. Instance-wide organization listing and creation are `16-ADMIN-API.md`, not this document.

## Scope

`/v1/organizations/{org_id}` and `/v1/organizations/{org_id}/mfa-impact`. `GET/POST /v1/organizations` (no `{org_id}`) is instance-scoped and documented in `16-ADMIN-API.md`.

## As Built

**`GET`/`PATCH` here are organization-scoped; only `status` and delete require `INSTANCE_OWNER`.** Reading requires `ORG_ADMIN`; updating name/domain/settings requires `ORG_OWNER`. Changing `status` (`active`/`suspended`) requires `INSTANCE_OWNER` specifically — suspending locks out every user in the tenant, and an organization owner suspending their own tenant is not a capability anyone asked for.

**`settings` is a partial merge, key by key, never a replace.** `OrganizationUpdate.settings` merges onto whatever is already stored; an update naming one setting leaves the rest untouched. Both on create and on update, **unknown keys inside `settings` are rejected, not stored** — a misspelled key (`mfa_requried`) kept silently would be a policy that is not in force and looks like it is, and nobody notices until the audit that was supposed to catch it does not. This is enforced by comparing the caller's raw JSON keys against the schema (see `01-API-STANDARDS.md`), not by `additionalProperties: false` alone.

**`domain` may be released with an explicit `null`.** `domain` is unique across the whole instance (not just within one organization), stored lowercased — two tenants holding the same domain in different cases would make tenant resolution ambiguous, which is a cross-tenant access bug waiting to happen. Domain-ownership *verification* is a later phase; today setting a domain only reserves the string.

**Delete is soft, and the confirmation is server-enforced.** `DELETE /v1/organizations/{org_id}` requires `INSTANCE_OWNER` and a `confirm_name` query parameter that must match the organization's current name exactly (`400` on mismatch) — enforced server-side rather than only by a console confirmation dialog, per `docs/PLAN/08-AUTHORIZATION.md` § Least Privilege, so a script that deletes the wrong organization cannot bypass it by skipping the console. The row survives, the organization becomes `404` everywhere (including to an `INSTANCE_OWNER` — a deleted tenant is not a suspended one), its domain is released for reuse, and its audit history is untouched (`events` carries no foreign key to `organizations`, precisely so history outlives the tenant).

**`mfa_required` has a 14-day grace period, and `mfa-impact` previews it before it is turned on.** `GET /v1/organizations/{org_id}/mfa-impact` answers, in counts only — **never names** — how many active members hold no second factor, so an administrator can decide whether to announce the mandate before enabling it rather than discovering the impact one support ticket at a time. Deactivated users are excluded from the count (they cannot sign in and so cannot be affected). The grace period start (`mfa_required_since`) is recorded by the service and cannot be supplied by the caller.

## Rules and Defaults

| Field / rule | Value | Enforced in |
|---|---|---|
| `name` | Required on create, 1–200 chars; not unique (two customers may share a display name) | `openapi/openapi.yaml` `OrganizationCreate`/`Organization` |
| `domain` | Optional, nullable, max 253 chars; unique instance-wide, case-insensitive, stored lowercased; `null` releases it | `openapi/openapi.yaml` |
| `status` | `active` \| `suspended`; requires `INSTANCE_OWNER` to change | `openapi/openapi.yaml` `OrganizationUpdate` |
| `settings.password_policy.min_length` | 12–128, default 12; an organization may raise the platform floor, never lower it | `openapi/openapi.yaml` `OrganizationSettings` |
| `settings.password_policy.max_age_days` | 0–3650, default 90; `0` means passwords never expire (a deliberate NIST SP 800-63B-aligned choice, not an absent value) | `openapi/openapi.yaml` |
| `settings.mfa_required` | boolean, default `false`; 14-day grace period from the moment it is enabled | `openapi/openapi.yaml`; `backend/internal/organization/mfamandate.go` |
| `settings.session_lifetime_hours` | 1–720, default 12 | `openapi/openapi.yaml` |
| `settings.allowed_login_methods` | Array, min 1 item, default `["password"]`; only `password` exists today — `passkey`/`social` are rejected until implemented | `openapi/openapi.yaml` |
| Unknown `settings` key | Rejected (`400`), never silently stored | `backend/internal/organization/settings.go` |
| `confirm_name` (delete) | Required query param, must equal the organization's current name exactly | `openapi/openapi.yaml` `deleteOrganization` |
| Delete semantics | Soft delete; `404` everywhere afterward, including to `INSTANCE_OWNER`; audit history preserved | `backend/internal/organization/store.go` |
| MFA mandate grace period | 14 days | `openapi/openapi.yaml` `MfaImpact.grace_period_days` |

## Interfaces

| Method & path | `operationId` | Required role |
|---|---|---|
| `GET /v1/organizations/{org_id}` | `getOrganization` | `ORG_ADMIN` |
| `PATCH /v1/organizations/{org_id}` | `updateOrganization` | `ORG_OWNER` (`INSTANCE_OWNER` for `status`) |
| `DELETE /v1/organizations/{org_id}` | `deleteOrganization` | `INSTANCE_OWNER` |
| `GET /v1/organizations/{org_id}/mfa-impact` | `getMfaImpact` | `ORG_ADMIN` |

Full schemas: `public-site/docs/api-reference/get-organization.api.mdx`, `update-organization.api.mdx`, `delete-organization.api.mdx`, `get-mfa-impact.api.mdx`.

Idempotency: `Idempotency-Key` accepted on `DELETE` (`07-IDEMPOTENCY.md`); `PATCH` has no idempotency parameter in the contract (a partial merge is naturally safe to repeat).

Audit events: `organization.updated` (payload carries changed settings **with their new values** — "settings changed" cannot answer what an incident review needs), `organization.suspended`, `organization.reactivated`, `organization.deleted`, `organization.mfa_required.enabled`, `organization.mfa_required.disabled` (recorded as their own event types, not folded into `organization.updated`, because disabling a security control is specifically what an incident reviewer searches for).

## Security Considerations

- `mfa-impact` returns counts only, never a list of who lacks a factor — that list is precisely what an attacker holding a stolen `ORG_ADMIN` token would want (`openapi/openapi.yaml` `getMfaImpact` description).
- Server-enforced `confirm_name` on delete closes the path where a script bypasses a console-only confirmation (`docs/PLAN/08-AUTHORIZATION.md` § Least Privilege).
- A deleted organization answers `404` to every caller including `INSTANCE_OWNER` — there is no "read a deleted tenant" escape hatch.

## Verification

- `backend/internal/organization/endpoints_integration_test.go`, `settings_test.go`, `settingsmerge_integration_test.go`, `specdefaults_test.go` (the spec's documented defaults and the service's actual defaults are asserted to match), `mfamandate_test.go`, `mfamandate_integration_test.go`, `mandatebackfill_integration_test.go`.

## Not Yet Built / Open Questions

- Domain *ownership verification* (DNS TXT record or similar) is not implemented; setting `domain` today only reserves the string instance-wide.

## Related Documents

- `docs/API/16-ADMIN-API.md` (instance-wide organization list/create)
- `docs/API/14-SESSION-API.md` (the mandate's effect on sign-in and self-service MFA)
- `docs/PLAN/08-AUTHORIZATION.md` § Least Privilege

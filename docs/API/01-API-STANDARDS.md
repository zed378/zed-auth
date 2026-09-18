# 01 - API Standards & Conventions

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-16, P1-15, P1-16 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the formatting conventions that hold across every `/v1` request and response: field naming, identifiers, timestamps, and how the contract treats unknown or absent fields.

## Scope

Covers wire-level conventions only. The error envelope is `04-ERROR-HANDLING.md`; pagination is `06-PAGINATION.md`; the OAuth/OIDC surface follows its own conventions (form encoding, `OAuthError`) documented in `08-AUTH-API.md`, not this one.

## As Built

- **JSON, `snake_case` field names, throughout `/v1`.** Every schema in `openapi/openapi.yaml` uses `snake_case` properties (`display_name`, `granted_org_id`, `page_size`); there is no camelCase anywhere in the Management API.
- **Identifiers are UUIDs, not prefixed strings.** The `ResourceId` schema (`openapi/openapi.yaml` `components/schemas/ResourceId`) is `type: string, format: uuid`. A prefixed, sortable identifier (`usr_...`, `org_...`) was the original design and was deliberately reverted — recorded as `TASKS/BACKLOG.md` `PG-23` — because OIDC's `sub` claim (already shipped in Phase 1) is a UUID that integrators already store as a user's permanent key, and prefixing only the Management API would give one user two identifiers. **Any documentation, example, or test fixture that uses `usr_123`, `org_456` or similar is wrong** and should be corrected to a UUID like `8f3e6b2a-1c4d-4e5f-9a0b-7c8d9e0f1a2b`.
- **Timestamps are RFC 3339 / ISO-8601, UTC**, `format: date-time` throughout (e.g. `created_at`, `updated_at`, `revoked_at`). `openapi/openapi.yaml` schemas are consistent on this; no endpoint returns a Unix timestamp except OAuth-protocol-specific fields that are integers by spec (`expires_in` in `TokenResponse`, `exp`/`iat` in `Introspection`, `updated_at` in `UserInfo` per OIDC Core 5.1 — seconds since the epoch, not a date-time string).
- **`additionalProperties: false` on every request and response object.** This is a documentation-level promise, not a runtime guarantee on its own — see the note on `BufferBody` below — but it is applied consistently: `Role`, `Organization`, `User`, `Project`, `Application`, `Grant`, `ProjectGrant`, `Error`, and every `*Create`/`*Update` schema declare it.
- **Unknown JSON keys are a validation error where it matters, not a silent drop**, and this is a real implementation detail worth knowing rather than a schema annotation taken on faith. `additionalProperties: false` in the spec is enforced by the generated decoder for named fields, but `oapi-codegen` decodes into a Go struct and `json.Unmarshal` silently discards a key the struct has no field for — it does not itself reject an unrecognised key at runtime. For fields where a dropped key would be a security-relevant silent failure (an organization's `settings`, most notably — a typo like `mfa_requried: true` would otherwise vanish and the administrator would believe MFA was on), the handler compares the caller's raw JSON keys against the schema itself, using the buffered request body (`backend/internal/management/body.go`, `BufferBody`/`RawBody`). See `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §11 (mass assignment).
- **A partial update (`PATCH`) leaves an absent property alone; explicit `null` has a defined meaning where it is allowed.** For example `OrganizationUpdate.domain: null` releases the domain; `UserUpdate` has no such nullable-release semantics on `email`. Each resource document states which fields are nullable and what `null` means for that field.
- **No client-supplied tenant header.** There is no `X-Organization-Id` or equivalent request header anywhere in this API. The organization a request addresses comes from the URL path (`{org_id}`) or, where there is no path, from the caller's access token `org_id` claim — never from a header a client could set independently of authentication. See `02-AUTHENTICATION-AND-AUTHORIZATION.md`.
- **Every `/v1` response carries `Cache-Control: no-store`.** Management responses are per-caller and permission-dependent; an intermediary caching one would serve it to somebody whose permissions differ (`backend/internal/management/errors.go`, `middleware.go`).

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Field naming | `snake_case` | `openapi/openapi.yaml` schemas |
| Resource identifiers | UUID (`ResourceId`) | `openapi/openapi.yaml`; `TASKS/BACKLOG.md` PG-23 |
| Timestamp format | RFC 3339 / ISO-8601 UTC (`date-time`) | `openapi/openapi.yaml` schemas |
| Unknown top-level keys on security-sensitive bodies (e.g. organization `settings`) | Rejected, not dropped | `backend/internal/management/body.go`; `backend/internal/organization/settings.go` |
| Request body size (buffered for hashing/key comparison) | 1 MiB (`maxRequestBody`) | `backend/internal/management/body.go` |
| Every response's `Cache-Control` | `no-store` | `backend/internal/management/errors.go` |
| Tenant selection | Path parameter or token claim; never a header | `backend/internal/management/middleware.go` |

## Interfaces

Not applicable — this document covers conventions applied across every resource's endpoints, not a resource of its own.

## Security Considerations

- **Mass assignment** (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §11): the raw-body key comparison in `body.go` exists specifically because `additionalProperties: false` in the spec is not, by itself, enforced by the generated decoder at runtime.
- **UUIDs as identifiers**: not sequential and not guessable (`openapi/openapi.yaml` `ResourceId` description), which matters for the enumeration defences described throughout `02-AUTHENTICATION-AND-AUTHORIZATION.md` and `04-ERROR-HANDLING.md`.

## Verification

- `backend/internal/organization/settings_test.go`, `settingsmerge_integration_test.go` — unknown settings keys are refused.
- `backend/internal/management/body.go` is exercised by every integration test that asserts a mass-assignment attempt is refused; see `backend/internal/organization/endpoints_integration_test.go`.

## Not Yet Built / Open Questions

None — these conventions are applied uniformly today.

## Related Documents

- `openapi/openapi.yaml`
- `docs/API/04-ERROR-HANDLING.md`, `docs/API/06-PAGINATION.md`
- `TASKS/BACKLOG.md` PG-23
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §11

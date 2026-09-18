# Category: API

This category specifies the HTTP contract the Auth Service actually exposes: the OAuth 2.1/OIDC protocol surface, and the `/v1` REST Management API. It documents the wire format — paths, methods, request/response shapes, error codes, pagination, idempotency and rate limiting — as they are implemented today. It does not restate authorization policy (`docs/PLAN/08-AUTHORIZATION.md`, `docs/AUTHORIZATION/`), the data model (`docs/PLAN/04-DATA-MODEL.md`, `docs/DATABASE/`), the threat model (`docs/SECURITY/`), or the OIDC/SAML/WebAuthn protocol internals (`docs/IDENTITY-PROTOCOL/`); it links to each instead.

The single source of truth for the contract itself is `openapi/openapi.yaml` (ADR-013): it is hand-written, generates the Go server interfaces, the console's TypeScript client, and the public site's `/docs/api-reference`. Nothing in this category restates a schema in full — each document links to the generated reference and to the exact schema name.

## Documents in Category

| File | Topic | Status |
|---|---|---|
| [`00-API-OVERVIEW.md`](./00-API-OVERVIEW.md) | Every `/v1` resource family, base paths, what is and is not built | Partially implemented |
| [`01-API-STANDARDS.md`](./01-API-STANDARDS.md) | JSON conventions, identifiers, timestamps, `additionalProperties: false` | Implemented |
| [`02-AUTHENTICATION-AND-AUTHORIZATION.md`](./02-AUTHENTICATION-AND-AUTHORIZATION.md) | Bearer token verification and the one policy table that guards every route | Implemented |
| [`03-API-VERSIONING.md`](./03-API-VERSIONING.md) | The `/v1` prefix and what a breaking-change policy would still need | Partially implemented |
| [`04-ERROR-HANDLING.md`](./04-ERROR-HANDLING.md) | The `Error` envelope and error codes | Implemented |
| [`05-RATE-LIMITING.md`](./05-RATE-LIMITING.md) | Per-client request quota, headers, fail-open behaviour | Implemented |
| [`06-PAGINATION.md`](./06-PAGINATION.md) | Cursor pagination, page size bounds | Implemented |
| [`07-IDEMPOTENCY.md`](./07-IDEMPOTENCY.md) | `Idempotency-Key` claim/replay/release semantics | Implemented |
| [`08-AUTH-API.md`](./08-AUTH-API.md) | `/oauth/*`, `/oidc/logout`, discovery, JWKS | Implemented |
| [`09-USER-API.md`](./09-USER-API.md) | Organization user administration | Implemented |
| [`10-ORGANIZATION-API.md`](./10-ORGANIZATION-API.md) | Tenant CRUD, settings, MFA mandate impact | Implemented |
| [`11-PROJECT-API.md`](./11-PROJECT-API.md) | Projects and OIDC applications | Implemented |
| [`12-ROLE-AND-PERMISSION-API.md`](./12-ROLE-AND-PERMISSION-API.md) | Project roles, direct user grants, `/v1/authz/check` | Implemented |
| [`13-PROJECT-GRANT-API.md`](./13-PROJECT-GRANT-API.md) | Cross-organization delegation: grants, delegated user grants, grant owners | Partially implemented |
| [`14-SESSION-API.md`](./14-SESSION-API.md) | Sessions, MFA enrolment/reset, account self-service | Implemented |
| [`15-AUDIT-LOG-API.md`](./15-AUDIT-LOG-API.md) | The append-only organization event log | Implemented |
| [`16-ADMIN-API.md`](./16-ADMIN-API.md) | Instance-scoped routes: organization list/create, `/v1/me/organizations` | Partially implemented |

## Related Documents

- `openapi/openapi.yaml` — the contract itself; `openapi/README.md` explains why it is hand-written and generates the code.
- `docs/PLAN/05-API-CONTRACT.md` — the governing plan document; where this category and the plan disagree, the plan is authoritative and the discrepancy is called out in the relevant file.
- `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` — how `openapi.yaml` becomes `public-site/docs/api-reference/`.
- `docs/AUTHORIZATION/`, `docs/PLAN/08-AUTHORIZATION.md` — the RBAC/delegation model the policy table in `02-AUTHENTICATION-AND-AUTHORIZATION.md` enforces.
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` — abuse cases referenced throughout.
- `docs/IDENTITY-PROTOCOL/` — OIDC/OAuth/SAML/WebAuthn protocol detail behind `08-AUTH-API.md`.

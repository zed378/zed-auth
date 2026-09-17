# Category: API

Versioned REST Management API contracts, HTTP standards, authentication/authorization requirements, rate limiting, and endpoint specifications.

## Category Mandate

The `API/` directory specifies the **HTTP REST contract** exposed by the Auth Service. Every resource endpoint, payload schema, error code, pagination rule, and rate-limiting header is governed by specs in this category.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-API-OVERVIEW.md` | API Overview & Architecture | Principles, base URLs, OpenAPI spec integration. |
| `01-API-STANDARDS.md` | Standards & Conventions | JSON formatting, ISO-8601 timestamps, UUID identifiers. |
| `02-AUTHENTICATION-AND-AUTHORIZATION.md` | API AuthN & AuthZ | Bearer token verification, API keys, route authorization. |
| `03-API-VERSIONING.md` | Versioning & Deprecation | `/v1` prefix, breaking change policy, sunset headers. |
| `04-ERROR-HANDLING.md` | Error Handling & RFC 7807 | Standardized JSON error response format & error codes. |
| `05-RATE-LIMITING.md` | Rate Limiting & Quotas | Redis token bucket, response headers, HTTP 429 semantics. |
| `06-PAGINATION.md` | Pagination & Sorting | Cursor-based & offset-based pagination standards. |
| `07-IDEMPOTENCY.md` | Idempotency Keys | `Idempotency-Key` header handling for mutating requests. |
| `08-AUTH-API.md` | Auth Endpoints | `/v1/auth/login`, `/v1/auth/token`, `/v1/auth/logout`. |
| `09-USER-API.md` | User Management API | `/v1/users` CRUD, status changes, password reset. |
| `10-ORGANIZATION-API.md` | Organization API | `/v1/orgs` tenant creation, settings, domain verification. |
| `11-PROJECT-API.md` | Project & App API | `/v1/projects` & `/v1/applications` OIDC client config. |
| `12-ROLE-AND-PERMISSION-API.md` | Role & Permission API | `/v1/roles` creation, permission array assignment. |
| `13-PROJECT-GRANT-API.md` | Project Grant API | `/v1/project-grants` cross-org delegation CRUD. |
| `14-SESSION-API.md` | Session & Token API | `/v1/sessions` management, active session list, revocation. |
| `15-AUDIT-LOG-API.md` | Audit Log API | `/v1/audit-logs` querying & filtering event stream. |
| `16-ADMIN-API.md` | System Admin API | `/v1/admin` instance-level settings, health, metrics. |

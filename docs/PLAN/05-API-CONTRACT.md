# 05 — API Contract

This document covers both the **OIDC/OAuth2 authentication endpoints** and the **REST Management API**, since both together form the full public contract of the Auth Service.

## Part A — Authentication Endpoints (OIDC/OAuth2)

### Standards Used

| Standard | Purpose | Priority |
|---|---|---|
| **OAuth 2.1 / OIDC** | Login & authorization for web/mobile/SPA | Mandatory, Phase 1 |
| **PKCE** | Mandatory for all clients (not just public clients) | Mandatory, Phase 1 |
| **SAML 2.0** | Compatibility with enterprise/legacy applications | Phase 4 |
| **SCIM** | Automated user provisioning from an external IdP | Phase 4 (optional) |

### Supported Grant Types

| Grant | When it's used |
|---|---|
| **Authorization Code + PKCE** | User login via browser (web app, SPA, mobile) — primary default |
| **Client Credentials** | Service-to-service communication with no user context |
| **Refresh Token** | Extending access without re-login |
| ~~Implicit Flow~~ | **Not supported** — deprecated in OAuth 2.1 |
| ~~Resource Owner Password Credentials~~ | **Not supported** except very specific legacy migration needs |

### Core Endpoints

```
GET  /.well-known/openid-configuration   → discovery document
GET  /oauth/authorize                    → start the login flow
POST /oauth/token                        → exchange a code/refresh token for access+id tokens
GET  /oauth/userinfo                     → user info from the access token
GET  /.well-known/jwks.json              → public key for verifying JWTs
POST /oauth/revoke                       → revoke a token
POST /oauth/introspect                   → check token validity (for a resource server)
GET  /oidc/logout                        → end-session (single logout)
```

### Standard Login Flow (Authorization Code + PKCE)

1. The application generates a `code_verifier` + `code_challenge`, and redirects the user to `/oauth/authorize?...&code_challenge=...`.
2. Auth Service shows a login page (or proceeds immediately if an SSO session already exists).
3. User submits credentials → validated → (if `mfa_required`) directed to the MFA step.
4. Auth Service redirects to the `redirect_uri` with `?code=...`.
5. The application's backend exchanges the code at `/oauth/token` with the `code_verifier` → receives `id_token`, `access_token`, `refresh_token`.

### MFA, Passwordless, Social Login, Logout

- **MFA**: TOTP first (Phase 3), then WebAuthn/Passkey. Recorded in the `amr` token claim for step-up auth decisions by consumer apps.
- **Passwordless**: magic link via email, or WebAuthn as the sole factor (later phase).
- **Social login**: Auth Service acts as an OIDC Relying Party toward Google/Microsoft/GitHub; linked accounts still get a `users` record so roles stay centrally managed.
- **Session & logout**: SSO session as an `HttpOnly + Secure + SameSite=Lax` cookie. Back-channel logout (RP-Initiated Logout) is a later phase; MVP needs per-app logout + a "log out of all sessions" button.

### Rate Limiting & Brute-Force Protection

Limit login attempts per account (cooldown, not permanent lockout) and per IP (credential stuffing protection); CAPTCHA after repeated failures is optional/later phase.

## Part B — Management REST API

### Design Principles

- **API-first**: every console action must also be available through this same public API.
- **Explicit versioning**: `/v1/...` prefix; breaking changes go to `/v2/...` with a clear deprecation period for the old version.
- **Authentication**: OAuth2 Bearer access tokens, including Client Credentials for service accounts.
- **Consistency**: plural resource names, standard HTTP verbs, consistent pagination and error format.

### Endpoint Structure (Summary)

```
/v1/instances/{instance_id}
/v1/organizations
/v1/organizations/{org_id}
/v1/organizations/{org_id}/projects
/v1/organizations/{org_id}/projects/{project_id}/applications
/v1/organizations/{org_id}/projects/{project_id}/roles
/v1/organizations/{org_id}/projects/{project_id}/grants          (Project Grants — 08-AUTHORIZATION.md)
/v1/organizations/{org_id}/users
/v1/organizations/{org_id}/users/{user_id}
/v1/organizations/{org_id}/users/{user_id}/grants
/v1/organizations/{org_id}/users/{user_id}/sessions
/v1/authz/check                                                   (RBAC + ABAC decision endpoint)
```

### Example: Create a User

```
POST /v1/organizations/{org_id}/users
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "email": "budi@company.com",
  "username": "budi",
  "profile": { "display_name": "Budi Santoso" },
  "send_invite_email": true
}

→ 201 Created
{
  "id": "usr_01H...",
  "email": "budi@company.com",
  "status": "invited",
  "created_at": "2026-09-08T10:00:00Z"
}
```

### Example: List Users with Pagination

```
GET /v1/organizations/{org_id}/users?page_size=20&page_token=eyJ...

→ 200 OK
{
  "users": [ { ... }, { ... } ],
  "next_page_token": "eyJ..."
}
```

### Standard Error Format

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Field 'email' is invalid",
    "details": [
      { "field": "email", "issue": "email format is not valid" }
    ]
  }
}
```

### Authorization Check Endpoint (RBAC + ABAC)

```
POST /v1/authz/check
{
  "subject": { "user_id": "usr_01H..." },
  "action": "approve",
  "resource": {
    "type": "purchase_request",
    "id": "pr_9931",
    "attributes": { "department": "finance", "amount": 8000000 }
  },
  "context": { "ip": "10.0.4.2", "time": "2026-09-08T14:00:00Z" }
}

→ 200 OK
{
  "allowed": true,
  "matched_policy": "finance_approval_by_department_and_limit",
  "reasons": ["subject.department == resource.department", "resource.amount <= subject.approval_limit"]
}
```

Full authorization semantics (RBAC roles, Project Grants, ABAC policies) are documented in `08-AUTHORIZATION.md`.

### Rate Limiting, Idempotency, Webhooks

- Rate limits applied per `client_id`/API key (not just IP), with standard `X-RateLimit-*` headers.
- `POST` endpoints support an `Idempotency-Key` header to make automated provisioning retries safe.
- Webhooks (later phase) for `user.created`, `user.deleted`, `role.assigned`, `role.revoked`, `login.success`, `login.failed`.

### Documentation

OpenAPI 3.x specification kept in sync with the implementation (generated from code or validated in CI), with a Swagger UI/Redoc explorer for internal developers.

Continue to [06 — Frontend Architecture](./06-FRONTEND-ARCHITECTURE.md).

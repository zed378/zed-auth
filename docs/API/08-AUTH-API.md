# 08 - Auth API Endpoints

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for user authentication, token acquisition, and session termination.

## Category Mandate

Provides HTTP API specification for `/v1/auth/login`, `/v1/auth/token`, `/v1/auth/logout`, `/v1/auth/mfa`.

## Key Topics To Specify

- `POST /v1/auth/login`: Authenticate email/password or passwordless code.
- `POST /v1/auth/token`: OAuth 2.1 token endpoint (Authorization Code, Refresh Token, Client Credentials).
- `POST /v1/auth/logout`: Revoke active session and refresh token.
- `POST /v1/auth/mfa/verify`: Submit MFA TOTP / WebAuthn proof.

## Reference Architecture & Specification

Endpoint Request Example (`POST /v1/auth/login`):
```json
{
  "email": "admin@company.com",
  "password": "SecretPass123!",
  "org_id": "11111111-2222-3333-4444-555555555555"
}
```

## Acceptance Criteria

- [x] Request & response schemas specified for all auth endpoints.
- [x] Security error codes defined.

## Open Questions

None.

## Related Documents

- `docs/IDENTITY-PROTOCOL/00-IDENTITY-PROTOCOL-OVERVIEW.md`
- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`

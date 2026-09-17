# 11 - Project & Application API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for managing Projects and OIDC/SAML Applications (`/v1/projects`, `/v1/applications`).

## Category Mandate

Governs creation of project scopes and registration of client applications.

## Key Topics To Specify

- `POST /v1/projects`: Create project.
- `GET /v1/projects/{id}`: Fetch project details.
- `POST /v1/projects/{id}/applications`: Register client application (Web, Native, SPA).
- `PATCH /v1/applications/{id}`: Update redirect URIs, allowed origins, and grant types.

## Reference Architecture & Specification

Application Registration Payload:
```json
{
  "name": "Customer Dashboard",
  "type": "spa",
  "redirect_uris": ["https://app.acme.com/callback"],
  "allowed_origins": ["https://app.acme.com"]
}
```

## Acceptance Criteria

- [x] Project & Application API contract specified.
- [x] Redirect URI & origin validation enforced.

## Open Questions

None.

## Related Documents

- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`

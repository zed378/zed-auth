# 10 - Organization API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for organization tenant management (`/v1/orgs`).

## Category Mandate

Provides API capabilities to manage organizations, domain verification, and tenant settings.

## Key Topics To Specify

- `POST /v1/orgs`: Provision new tenant organization.
- `GET /v1/orgs/{id}`: Retrieve organization metadata.
- `PATCH /v1/orgs/{id}`: Update tenant settings (password policy, MFA mandate).
- `POST /v1/orgs/{id}/domains`: Add and verify custom domain.

## Reference Architecture & Specification

Organization Response:
```json
{
  "id": "11111111-2222-3333-4444-555555555555",
  "name": "Acme Corp",
  "domain": "acme.com",
  "settings": {"require_mfa": true, "password_min_length": 12}
}
```

## Acceptance Criteria

- [x] Organization lifecycle endpoints documented.
- [x] Domain verification workflow defined.

## Open Questions

None.

## Related Documents

- `docs/MULTI-TENANCY/00-MULTI-TENANCY-ARCHITECTURE.md`

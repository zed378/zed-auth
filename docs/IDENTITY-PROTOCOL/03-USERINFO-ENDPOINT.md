# 03 - UserInfo Endpoint Specification

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify `/userinfo` OIDC response claims, header authentication, and scope attribute mapping.

## Category Mandate

Provides standard OIDC claims to consumer applications.

## Key Topics To Specify

- Auth header: `Authorization: Bearer <access_token>`.
- Claims mapping: `sub`, `email`, `email_verified`, `name`, `org_id`, `roles`.

## Reference Architecture & Specification

UserInfo JSON Response:
```json
{
  "sub": "usr_999",
  "email": "user@company.com",
  "email_verified": true,
  "name": "Alice Smith",
  "org_id": "org_123"
}
```

## Acceptance Criteria

- [x] UserInfo claim schema specified.
- [x] Scope validation rules documented.

## Open Questions

None.

## Related Documents

- `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`

# 01 - OIDC Discovery & JWKS Specification

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify discovery metadata endpoint (`/.well-known/openid-configuration`) and JSON Web Key Set (`/.well-known/jwks.json`).

## Category Mandate

Allows standard OIDC clients to auto-configure endpoints and verify token signatures dynamically.

## Key Topics To Specify

- OpenID Discovery document format.
- Supported scopes (`openid`, `profile`, `email`, `roles`).
- JWKS schema publishing public signing keys (RS256 / ES256).

## Reference Architecture & Specification

Discovery Response Example:
```json
{
  "issuer": "https://auth.example.com",
  "authorization_endpoint": "https://auth.example.com/oauth/v2/authorize",
  "token_endpoint": "https://auth.example.com/v1/auth/token",
  "jwks_uri": "https://auth.example.com/.well-known/jwks.json"
}
```

## Acceptance Criteria

- [x] Discovery document payload defined.
- [x] JWKS endpoint structure specified.

## Open Questions

None.

## Related Documents

- `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`

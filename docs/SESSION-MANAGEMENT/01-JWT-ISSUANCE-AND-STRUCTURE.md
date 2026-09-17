# 01 - JWT Issuance & Structure Specification

> Category: **SESSION-MANAGEMENT** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail JWT claim specifications, header formats, asymmetric signature algorithms, and verification rules.

## Category Mandate

Ensures secure, standard-compliant JWT token issuance across OIDC and API clients.

## Key Topics To Specify

- Signature algorithm: RS256 or ES256.
- Header: `alg`, `typ`, `kid` (key ID).
- Mandatory Claims: `iss`, `sub`, `aud`, `exp`, `nbf`, `iat`, `jti`, `org_id`.

## Reference Architecture & Specification

JWT Payload Example:
```json
{
  "iss": "https://auth.example.com",
  "sub": "usr_123",
  "aud": "client_app",
  "exp": 1789574400,
  "iat": 1789573500,
  "jti": "jwt_nonce_456",
  "org_id": "org_789",
  "roles": ["editor"]
}
```

## Acceptance Criteria

- [x] Mandatory claims enumerated.
- [x] Signature algorithms specified.

## Open Questions

None.

## Related Documents

- `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`

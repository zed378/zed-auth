# 03 - Security Controls Baseline

> Category: **SECURITY** (`docs/SECURITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define mandatory cryptographic standards, transport security, and browser security headers.

## Category Mandate

Enforces baseline security controls across all environments.

## Key Topics To Specify

- Transport: TLS 1.3 mandatory; HSTS header (`max-age=31536000; includeSubDomains`).
- Encryption at Rest: AES-256-GCM for secrets/tokens; Argon2id / bcrypt for passwords.
- Browser Security Headers: CSP, X-Frame-Options DENY, X-Content-Type-Options nosniff.

## Reference Architecture & Specification

Standard Response Headers:
`Strict-Transport-Security: max-age=31536000; includeSubDomains`
`X-Frame-Options: DENY`
`X-Content-Type-Options: nosniff`
`Content-Security-Policy: default-src 'self'`

## Acceptance Criteria

- [x] Cryptographic algorithms specified.
- [x] Required HTTP security headers defined.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`

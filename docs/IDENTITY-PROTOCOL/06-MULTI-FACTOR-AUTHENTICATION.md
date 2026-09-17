# 06 - Multi-Factor Authentication & TOTP

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify Time-based One-Time Password (TOTP) enrollment, HMAC-SHA1 generation, and emergency recovery codes.

## Category Mandate

Provides secondary authentication factor enforcement.

## Key Topics To Specify

- RFC 6238 TOTP specification (30-second step, 6 digits).
- Secret key storage encrypted at rest (AES-256-GCM).
- 10 single-use recovery codes hashed with bcrypt/argon2.

## Reference Architecture & Specification

TOTP Setup Workflow:
`Server generates secret -> sends otpauth:// QR code URI -> user enters 6-digit code -> MFA factor enabled`

## Acceptance Criteria

- [x] TOTP spec and window drift specified.
- [x] Recovery code hashing strategy defined.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/03-SECURITY-CONTROLS-BASELINE.md`

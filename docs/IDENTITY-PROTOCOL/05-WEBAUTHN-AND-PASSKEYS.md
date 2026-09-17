# 05 - WebAuthn & Passkeys Specification

> Category: **IDENTITY-PROTOCOL** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail FIDO2 WebAuthn registration and authentication flows for passwordless login and passkeys.

## Category Mandate

Delivers hardware-backed, phishing-resistant authentication via TouchID/FaceID/YubiKeys.

## Key Topics To Specify

- W3C WebAuthn Level 2 specification.
- Attestation statement verification.
- Credential ID & public key storage in `user_mfa_factors`.
- Origin matching against client domain.

## Reference Architecture & Specification

Passkey Auth Sequence:
`Browser requests challenge -> Authenticator signs challenge with private key -> Server verifies signature with stored public key`

## Acceptance Criteria

- [x] WebAuthn challenge/response specs defined.
- [x] Credential storage schema specified.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`

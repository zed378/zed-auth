# 03 - JWKS Key Rotation Specification

> Category: **SESSION-MANAGEMENT** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail asymmetric key pair generation, key state lifecycle (Active, Passive, Revoked), and rotation schedules.

## Category Mandate

Guarantees seamless key rotation without disrupting active client sessions.

## Key Topics To Specify

- Key lifecycle states: `Active` (signing & verification), `Passive` (verification only, 30 days), `Revoked`.
- Key type: RSA 2048-bit or ECDSA P-256.
- Automated rotation schedule (every 90 days) via background worker.

## Reference Architecture & Specification

JWKS Rotation Sequence:
`Generate Key N+1 -> Add to JWKS -> Promote Key N+1 to Active -> Move Key N to Passive -> Retire Key N after 30 days`

## Acceptance Criteria

- [x] Key lifecycle states defined.
- [x] Automated rotation schedule documented.

## Open Questions

None.

## Related Documents

- `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`

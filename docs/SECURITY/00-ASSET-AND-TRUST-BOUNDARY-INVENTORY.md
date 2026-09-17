# 00 - Asset & Trust Boundary Inventory

> Category: **SECURITY** (`docs/SECURITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Classify data assets by sensitivity and define trust boundaries across network zones.

## Category Mandate

Establishes a formal asset classification framework to guide defense-in-depth security controls.

## Key Topics To Specify

- Tier 1 (Critical): JWKS private signing keys, Master encryption keys, Password hashes.
- Tier 2 (Sensitive): PII, User emails, Audit logs, Active session tokens.
- Tier 3 (Public): OIDC discovery metadata, public JWKS keys.

## Reference Architecture & Specification

Trust Boundaries:
- Untrusted Zone: Public Internet / Consumer Apps
- DMZ Zone: API Gateway / Load Balancer
- Trusted Zone: Auth Service Backend
- Strictly Isolated Zone: PostgreSQL & Redis Storage

## Acceptance Criteria

- [x] Asset tiers 1 through 3 classified.
- [x] Trust boundary zones mapped.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/03-SECURITY-CONTROLS-BASELINE.md`

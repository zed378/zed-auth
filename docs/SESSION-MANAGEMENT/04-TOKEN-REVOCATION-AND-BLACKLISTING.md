# 04 - Token Revocation List Specification

> Category: **SESSION-MANAGEMENT** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify real-time JWT revocation blacklisting using Redis distributed store.

## Category Mandate

Enables immediate revocation of JWT access tokens prior to natural expiration.

## Key Topics To Specify

- Revoked JWT IDs (`jti`) pushed to Redis SET with TTL equal to remaining JWT lifetime (`exp - now`).
- High-performance check in API Gateway / Middleware (`EXISTS authz:revoked:<jti>`).

## Reference Architecture & Specification

Blacklist Rule: If `jti` is present in Redis revocation store, request is rejected immediately with 401 Unauthorized.

## Acceptance Criteria

- [x] Redis revocation store key pattern defined.
- [x] Automatic expiration matching `exp` claim enforced.

## Open Questions

None.

## Related Documents

- `docs/API/14-SESSION-API.md`

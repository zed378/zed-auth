# 00 - Session Architecture Overview

> Category: **SESSION-MANAGEMENT** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify dual-layer session architecture combining short-lived JWT access tokens with stateful refresh tokens.

## Category Mandate

Delivers high-performance token verification alongside real-time session revocation capabilities.

## Key Topics To Specify

- Access Token (JWT, TTL 15 minutes, stateless verification via JWKS).
- Refresh Token (Opaque string, TTL 30 days, stateful DB/Redis tracking).
- Session record tracking client IP, User-Agent, and last active timestamp.

## Reference Architecture & Specification

Session Lifespan:
- Access Token: 15 min
- Refresh Token: 30 days (sliding expiration on active use)

## Acceptance Criteria

- [x] Access vs Refresh token lifespans defined.
- [x] Session tracking attributes specified.

## Open Questions

None.

## Related Documents

- `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`
- `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`

# 02 - API Authentication & Authorization

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail how requests to the REST Management API are authenticated and authorized server-side.

## Category Mandate

Guarantees that no unauthenticated or unauthorized request accesses backend resource logic.

## Key Topics To Specify

- Bearer JWT access token validation.
- Service account API key authentication.
- Server-side RBAC & Project Grant evaluation middleware (`/v1/authz/check`).
- Organization context validation.

## Reference Architecture & Specification

Authorization Pipeline:
`HTTP Request -> Extract Token -> Verify Signature/JWKS -> Extract Claims -> Enforce RBAC/ABAC -> Execute Handler`

## Acceptance Criteria

- [x] Authentication methods specified.
- [x] Server-side authorization check order defined.

## Open Questions

Validate API key hash rotation mechanism.

## Related Documents

- `docs/AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`

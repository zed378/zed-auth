# 01 - Service Boundaries & Interfaces

> Category: **ARCHITECTURE** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify concrete network and process boundaries between Auth Service components and external systems.

## Category Mandate

Ensures zero coupling between identity management and consumer application business domains.

## Key Topics To Specify

- Network protocol contracts (HTTPS/TLS 1.3, JSON over HTTP, gRPC internal).
- External IdP interfaces (Google, GitHub, SAML 2.0 IdPs).
- Database connection boundaries & pooling.

## Reference Architecture & Specification

Boundary Rule: Consumer applications communicate strictly through public OIDC/OAuth 2.1 endpoints or REST APIs. Direct database access is forbidden.

## Acceptance Criteria

- [x] Network interfaces documented.
- [x] Security boundary guarantees specified.

## Open Questions

None.

## Related Documents

- `docs/ARCHITECTURE/00-SYSTEM-ARCHITECTURE.md`
- `docs/API/00-API-OVERVIEW.md`

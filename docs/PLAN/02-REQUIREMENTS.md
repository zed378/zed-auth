# 02 - Functional & Non-Functional Requirements

> Category: **PLAN** (`docs/PLAN/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Enumerate all explicit functional requirements (FR-01 to FR-14) and non-functional requirements (NFR-01 to NFR-10).

## Category Mandate

Acts as the definitive contract that every API, backend component, and database schema must satisfy.

## Key Topics To Specify

- FR-01: Single Sign-On (OIDC/OAuth 2.1).
- FR-02: Organization & Project Scoping.
- FR-03: Multi-Tenant RBAC & Project Grants.
- FR-14: Management Console API Parity.
- NFR-01: Token verification latency < 5ms.
- NFR-02: Server-side authorization enforcement on 100% of routes.

## Reference Architecture & Specification

FR-14: Every action performable in the Console UI must be accessible via the REST Management API.
NFR-02: Server-side authorization check (`/v1/authz/check`) must be invoked before executing business logic.

## Acceptance Criteria

- [x] FR-01 through FR-14 fully enumerated.
- [x] NFR-01 through NFR-10 fully enumerated.

## Open Questions

Review NFR-01 latency under high concurrency load.

## Related Documents

- `docs/PLAN/01-PRODUCT-SCOPE.md`
- `docs/API/00-API-OVERVIEW.md`
- `docs/AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md`

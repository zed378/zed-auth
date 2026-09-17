# 00 - API Overview & Architecture

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define core principles of the REST Management API and OpenAPI contract generation.

## Category Mandate

Establishes a standardized, resource-oriented REST API adhering to OpenAPI 3.1 specifications.

## Key Topics To Specify

- Resource URI design (`/v1/{resource}`).
- HTTP verbs (GET, POST, PUT, PATCH, DELETE).
- API parity with Console UI.
- Client authentication & tenant context headers.

## Reference Architecture & Specification

Base URL: `https://auth.example.com/v1`
Standard Request Headers:
- `Authorization: Bearer <token>`
- `X-Organization-Id: <uuid>`
- `Content-Type: application/json`

## Acceptance Criteria

- [x] Base URL pattern defined.
- [x] HTTP verb semantics documented.
- [x] Standard request headers specified.

## Open Questions

None.

## Related Documents

- `docs/API/01-API-STANDARDS.md`
- `docs/API/04-ERROR-HANDLING.md`

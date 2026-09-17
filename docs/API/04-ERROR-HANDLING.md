# 04 - Error Handling & RFC 7807

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify standardized error responses adhering to RFC 7807 Problem Details for HTTP APIs.

## Category Mandate

Provides predictable, structured error information to API clients without leaking internal stack traces.

## Key Topics To Specify

- Standard JSON error object (`type`, `title`, `status`, `detail`, `code`, `instance`).
- Enumerated error codes (`INVALID_CREDENTIALS`, `TENANT_NOT_FOUND`, `FORBIDDEN`).
- Validation error lists.

## Reference Architecture & Specification

```json
{
  "type": "https://auth.example.com/errors/validation-failed",
  "title": "Validation Failed",
  "status": 400,
  "code": "INVALID_PAYLOAD",
  "detail": "The email field must be a valid email address.",
  "instance": "/v1/users"
}
```

## Acceptance Criteria

- [x] RFC 7807 format adopted.
- [x] Standard error code catalog established.

## Open Questions

None.

## Related Documents

- `docs/API/01-API-STANDARDS.md`

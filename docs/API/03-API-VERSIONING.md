# 03 - API Versioning & Deprecation

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define API versioning rules, breaking change policies, and deprecation sunset timelines.

## Category Mandate

Protects downstream API consumers from unexpected breaking changes.

## Key Topics To Specify

- Major version in URI path (`/v1`).
- Deprecation notice header (`Sunset: Wed, 11 Nov 2026 00:00:00 GMT`).
- Minimum 6-month deprecation window for breaking changes.

## Reference Architecture & Specification

Deprecation Header Example:
`Sunset: 2026-12-31T23:59:59Z`
`Link: <https://auth.example.com/docs/api/deprecation>; rel="sunset"`

## Acceptance Criteria

- [x] Versioning scheme defined.
- [x] Sunset header standard specified.

## Open Questions

None.

## Related Documents

- `docs/API/00-API-OVERVIEW.md`

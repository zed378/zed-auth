# 16 - System Admin API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify instance-level administration endpoints (`/v1/admin`).

## Category Mandate

Provides platform operators system health checks, key rotation triggers, and global metrics.

## Key Topics To Specify

- `GET /v1/admin/health`: System health check (DB, Redis connection status).
- `POST /v1/admin/keys/rotate`: Trigger JWKS signing key rotation.
- `GET /v1/admin/metrics`: Prometheus metrics scrape endpoint.

## Reference Architecture & Specification

Health Check Response:
`{"status": "healthy", "database": "up", "redis": "up"}`

## Acceptance Criteria

- [x] System administration endpoints documented.
- [x] Admin authentication & authorization enforced.

## Open Questions

None.

## Related Documents

- `docs/API/00-API-OVERVIEW.md`

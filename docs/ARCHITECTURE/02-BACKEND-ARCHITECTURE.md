# 02 - Backend Architecture

> Category: **ARCHITECTURE** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify backend software architecture in Go: HTTP routing (`chi`), handler layer, service layer, repository layer (`pgx`), and domain models.

## Category Mandate

Enforces idiomatic, modular Go code with explicit error handling and parameterized database access.

## Key Topics To Specify

- Folder layout (`/internal/http`, `/internal/domain`, `/internal/repo`, `/internal/authz`).
- Dependency injection without global state.
- Middleware pipeline (RequestID, Logger, Recovery, CORS, AuthN, AuthZ).
- Parameterized SQL execution via `pgx/v5`.

## Reference Architecture & Specification

```
HTTP Request -> Chi Router -> Middleware Stack -> Handler -> Service -> Repository -> PostgreSQL
```
Rule: Handlers convert HTTP payload to domain types; Service executes business rules; Repository performs parameterized SQL.

## Acceptance Criteria

- [x] Backend package layout defined.
- [x] Middleware chain order specified.
- [x] Repository pattern guidelines established.

## Open Questions

Verify pgx pool tuning under 1000 concurrent requests.

## Related Documents

- `docs/ARCHITECTURE/00-SYSTEM-ARCHITECTURE.md`
- `docs/DATABASE/00-DATABASE-ARCHITECTURE.md`

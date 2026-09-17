# 07 - Idempotency Keys

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify handling of `Idempotency-Key` headers for mutating API requests (POST/PATCH).

## Category Mandate

Prevents duplicate execution of critical actions (e.g. user creation, role assignment) during network retries.

## Key Topics To Specify

- Header: `Idempotency-Key: <UUID/string>`.
- Key caching in Redis with 24-hour TTL.
- Concurrent request locking and replay of stored HTTP response.

## Reference Architecture & Specification

Workflow: API Gateway checks Redis for `Idempotency-Key`. If present, returns cached response immediately; if not, locks key, executes handler, and caches result.

## Acceptance Criteria

- [x] Idempotency header standard defined.
- [x] Redis key TTL and locking semantics specified.

## Open Questions

None.

## Related Documents

- `docs/API/00-API-OVERVIEW.md`

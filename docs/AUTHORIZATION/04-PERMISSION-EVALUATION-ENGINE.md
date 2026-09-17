# 04 - Permission Evaluation Engine & Caching

> Category: **AUTHORIZATION** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify the high-performance permission evaluation pipeline, local cache, and invalidation triggers.

## Category Mandate

Guarantees authorization decision latency < 10ms at peak load.

## Key Topics To Specify

- In-memory evaluation engine in Go.
- Redis + local LRU cache for resolved user permission sets.
- Invalidation pub/sub events on `user_grants` or `project_grants` mutation.

## Reference Architecture & Specification

Cache Key Format: `authz:perm:<org_id>:<user_id>:<project_id>` (TTL 60s, invalidated immediately on role update).

## Acceptance Criteria

- [x] Evaluation engine latency target (<10ms) enforced.
- [x] Cache invalidation event pipeline specified.

## Open Questions

None.

## Related Documents

- `docs/PERFORMANCE/00-PERFORMANCE-TARGETS.md`

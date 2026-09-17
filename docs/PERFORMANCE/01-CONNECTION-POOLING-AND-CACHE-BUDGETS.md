# 01 - Connection Pooling & Cache Memory Budgets

> Category: **PERFORMANCE** (`docs/PERFORMANCE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify database connection pool sizes, Redis memory allocation, and cache TTL settings.

## Category Mandate

Prevents resource exhaustion under heavy concurrency spikes.

## Key Topics To Specify

- `pgxpool` configuration (Max: 50 conns, Min Idle: 10, Acquire Timeout: 2s).
- Redis Max Memory: 2GB (Maxmemory Policy: `allkeys-lru`).
- Local in-memory LRU cache: 10,000 permission sets.

## Reference Architecture & Specification

Resource Allocation Rule: If DB connection pool is exhausted, requests fail fast with 503 Service Unavailable rather than queueing indefinitely.

## Acceptance Criteria

- [x] Database connection pool parameters specified.
- [x] Redis memory limits & eviction policies defined.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/00-DATABASE-ARCHITECTURE.md`

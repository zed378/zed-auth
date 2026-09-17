# 00 - Performance Targets & Latency Budgets

> Category: **PERFORMANCE** (`docs/PERFORMANCE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify explicit latency, throughput, and resource performance budgets.

## Category Mandate

Guarantees that the Auth Service introduces minimal overhead to downstream applications.

## Key Topics To Specify

- Token Validation Latency: p95 < 2ms, p99 < 5ms.
- Authorization Check (`/v1/authz/check`): p95 < 5ms, p99 < 10ms.
- Management API Read Endpoints: p95 < 50ms.
- Throughput: Minimum 5,000 token validations/sec per CPU core.

## Reference Architecture & Specification

Performance SLA Summary Table:
| Operation | p95 Latency | p99 Latency | Target Throughput |
|---|---|---|---|
| Token Validation | < 2ms | < 5ms | 5,000 req/sec/core |
| AuthZ Check | < 5ms | < 10ms | 2,000 req/sec/core |

## Acceptance Criteria

- [x] Latency budgets defined for all core operations.
- [x] Throughput targets per CPU core specified.

## Open Questions

None.

## Related Documents

- `docs/OBSERVABILITY/03-METRICS-AND-SLO-TRACKING.md`

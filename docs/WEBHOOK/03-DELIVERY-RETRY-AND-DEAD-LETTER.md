# 03 - Delivery Retry Policy & Dead-Letter Queue

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify exponential backoff retry schedules and dead-letter queue (DLQ) handling.

## Category Mandate

Ensures eventual event delivery during transient receiver outages.

## Key Topics To Specify

- Retry Schedule: 5 attempts over 24 hours (15s, 5m, 1h, 6h, 24h).
- Success criteria: HTTP 200-299 status code returned within 5-second timeout.
- Unsuccessful deliveries moved to Dead-Letter Queue (DLQ) with alert to tenant admin.

## Reference Architecture & Specification

Retry Matrix:
| Attempt | Delay | Timeout |
|---|---|---|
| 1 | 15 seconds | 5s |
| 2 | 5 minutes | 5s |
| 5 (Final) | 24 hours | 5s |

## Acceptance Criteria

- [x] Retry backoff schedule specified.
- [x] Dead-letter queue rules defined.

## Open Questions

None.

## Related Documents

- `docs/WEBHOOK/00-WEBHOOK-OVERVIEW.md`

# 05 - Rate Limiting & Quotas

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define rate limiting mechanisms, IP/Tenant buckets, response headers, and HTTP 429 handling.

## Category Mandate

Protects the platform against DDoS, brute-force credential attacks, and noisy neighbor resource exhaustion.

## Key Topics To Specify

- Sliding window token bucket algorithm implemented in Redis.
- Per-IP rate limits for login endpoints (10 req/min).
- Per-Tenant rate limits for Management API (1000 req/min).
- Headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.

## Reference Architecture & Specification

Rate Limit Response (429 Too Many Requests):
`HTTP/1.1 429 Too Many Requests`
`Retry-After: 60`
`X-RateLimit-Reset: 1789574400`

## Acceptance Criteria

- [x] Rate limit tiers specified.
- [x] Response headers documented.

## Open Questions

Verify Redis cluster failover behavior for rate limit counters.

## Related Documents

- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`

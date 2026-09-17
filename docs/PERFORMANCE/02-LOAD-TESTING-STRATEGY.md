# 02 - Load Testing & Benchmarking Strategy

> Category: **PERFORMANCE** (`docs/PERFORMANCE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail automated load testing workflows using k6 and benchstat.

## Category Mandate

Provides reproducible load testing scripts to validate performance targets before release.

## Key Topics To Specify

- k6 load test scenarios (`backend/test/load/token_verify.js`, `backend/test/load/authz_check.js`).
- Constant VUs & Ramping VUs test profiles up to 10,000 Virtual Users.
- Automated CI benchmark regression check against previous releases.

## Reference Architecture & Specification

k6 Scenario Example:
```javascript
export const options = {
  stages: [
    { duration: '1m', target: 1000 },
    { duration: '5m', target: 5000 },
    { duration: '1m', target: 0 }
  ],
  thresholds: { http_req_duration: ['p(99)<10'] }
};
```

## Acceptance Criteria

- [x] k6 load testing scenarios specified.
- [x] CI regression threshold benchmarks defined.

## Open Questions

None.

## Related Documents

- `docs/PERFORMANCE/00-PERFORMANCE-TARGETS.md`

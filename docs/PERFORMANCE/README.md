# Performance

What has actually been measured against `docs/PLAN/12-PERFORMANCE.md`'s latency targets, the connection-pool and cache sizing behind those measurements, and the load-testing harness that produced them. This category records evidence, not aspiration: a target is only ever reported here as "met" when a `MEMORY/records/` entry or a script output says so, and every gap between the plan's Load Testing Plan and what has actually run is named rather than assumed complete. Related signal collection (metrics the targets are judged against, the alert rules built on them) lives in `docs/OBSERVABILITY/`.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-PERFORMANCE-TARGETS.md`](./00-PERFORMANCE-TARGETS.md) | Targets vs. what has actually been measured, phase by phase | Partially implemented |
| [`01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md`](./01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md) | Postgres pool settings, Redis/in-process cache TTLs and their reasoning | Implemented |
| [`02-LOAD-TESTING-STRATEGY.md`](./02-LOAD-TESTING-STRATEGY.md) | The `scripts/loadtest/` harness, how it's run, what it hasn't covered yet | Partially implemented |

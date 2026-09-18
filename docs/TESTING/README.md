# Testing

What is tested, at which layer, and — more importantly — how the suites are kept from passing vacuously. `docs/PLAN/11-TESTING.md` is the intent; these documents describe the suites that exist and the conventions that make them mean something.

The CI gates that run all of this are in [`../DEVOPS/03-CICD-PIPELINE-SPECIFICATION.md`](../DEVOPS/03-CICD-PIPELINE-SPECIFICATION.md).

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-TESTING-STRATEGY.md`](./00-TESTING-STRATEGY.md) | The layers, what each proves, and the two conventions that matter most | Implemented |
| [`01-BACKEND-UNIT-AND-INTEGRATION-TESTS.md`](./01-BACKEND-UNIT-AND-INTEGRATION-TESTS.md) | Go tests, testcontainers, fixtures, owner-connection tests | Implemented |
| [`02-FRONTEND-COMPONENT-TESTS.md`](./02-FRONTEND-COMPONENT-TESTS.md) | Console tests: stubbing at `fetch`, states, accessibility | Implemented |
| [`03-E2E-PLAYWRIGHT-TESTS.md`](./03-E2E-PLAYWRIGHT-TESTS.md) | Browser tests against a real service, and what only they can prove | Implemented |
| [`04-ABUSE-CASE-SECURITY-TESTING.md`](./04-ABUSE-CASE-SECURITY-TESTING.md) | Abuse cases, the coverage map, and mutation verification | Implemented |

## Related

- [`../SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`](../SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md) — where abuse cases come from.
- `scripts/check.sh`, `scripts/e2e-up.sh`, `scripts/acceptance-phase*.sh`, `scripts/loadtest/`.
- `MEMORY/records/` — per-task test and mutation results.

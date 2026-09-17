# Category: TESTING

Testing pyramid, backend Go unit/integration tests, React component tests, Playwright E2E tests, and abuse-case security testing.

## Category Mandate

The `TESTING/` directory defines the platform **quality assurance strategy**. It specifies test pyramid layers, test environment isolation, automated Playwright E2E flows, and mandatory security abuse-case tests.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-TESTING-STRATEGY.md` | Testing Strategy Overview | Test pyramid & code coverage targets (>85%). |
| `01-BACKEND-UNIT-AND-INTEGRATION-TESTS.md` | Backend Testing Spec | Go `testing`, `testcontainers-go`, pgx mock tests. |
| `02-FRONTEND-COMPONENT-TESTS.md` | Frontend Testing Spec | React Testing Library & Vitest component tests. |
| `03-E2E-PLAYWRIGHT-TESTS.md` | E2E Testing Spec | Playwright automated browser flow tests. |
| `04-ABUSE-CASE-SECURITY-TESTING.md` | Abuse-Case Testing Spec | Security abuse tests matching `SECURITY/02`. |

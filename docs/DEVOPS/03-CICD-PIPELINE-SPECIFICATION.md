# 03 - CI/CD Pipeline Specification

> Category: **DEVOPS** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify GitHub Actions CI/CD workflows for building, testing, security scanning, and deployment.

## Category Mandate

Automates code quality verification and deployment pipelines.

## Key Topics To Specify

- Workflow 1 (`ci.yml`): Lint (`golangci-lint`), Unit Tests (`go test`), Security Audit (`gosec`).
- Workflow 2 (`e2e.yml`): Playwright E2E integration test suite.
- Workflow 3 (`deploy.yml`): Container build & Helm deployment on merge to main.

## Reference Architecture & Specification

Pipeline Gates:
`PR Created -> Lint & Unit Test -> Security Scan -> E2E Suite -> Code Review -> Merge -> Deploy`

## Acceptance Criteria

- [x] GitHub Actions workflow stages specified.
- [x] Quality & security gating rules defined.

## Open Questions

None.

## Related Documents

- `docs/TESTING/00-TESTING-STRATEGY.md`

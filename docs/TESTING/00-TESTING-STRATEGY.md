# 00 - Testing Strategy & Pyramid

> Category: **TESTING** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail overall testing pyramid architecture, test categories, and code coverage requirements.

## Category Mandate

Establishes multi-layered test automation to guarantee platform correctness and prevent regressions.

## Key Topics To Specify

- Unit Tests (70%): Fast, isolated tests for domain logic & utilities.
- Integration Tests (20%): DB & Redis integration tests using `testcontainers-go`.
- E2E Tests (10%): Full browser flows using Playwright.
- Code Coverage Target: > 85% statement coverage across backend packages.

## Reference Architecture & Specification

Testing Pyramid:
```
       /  E2E Playwright  \     (10%)
      / Integration Tests  \    (20%)
     /   Go & React Unit    \   (70%)
```

## Acceptance Criteria

- [x] Test pyramid distribution defined.
- [x] Minimum 85% code coverage rule specified.

## Open Questions

None.

## Related Documents

- `docs/PLAN/11-TESTING.md`
- `docs/TESTING/01-BACKEND-UNIT-AND-INTEGRATION-TESTS.md`

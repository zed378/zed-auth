# 03 - End-to-End Playwright Testing Specification

> Category: **TESTING** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify E2E browser test automation flows using Playwright.

## Category Mandate

Validates critical end-to-end user journeys (login, organization creation, role delegation, member invite).

## Key Topics To Specify

- Playwright test runner (`npm run test:e2e`).
- Isolated test environment seed data.
- Visual regression comparison snapshots.

## Reference Architecture & Specification

E2E Test Suites:
- Suite 01: OIDC Login & PKCE Authorization Code flow.
- Suite 02: Organization & Project creation.
- Suite 03: Project Grant cross-org delegation workflow.

## Acceptance Criteria

- [x] Playwright test suite scenarios defined.
- [x] CI parallel execution configured.

## Open Questions

None.

## Related Documents

- `docs/UI-UX/02-USER-JOURNEYS.md`

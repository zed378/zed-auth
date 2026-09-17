# 02 - Frontend Component Testing Specification

> Category: **TESTING** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify component testing strategy for React Management Console using Vitest and React Testing Library.

## Category Mandate

Guarantees UI components render correctly, handle user events, and display proper error states.

## Key Topics To Specify

- Vitest runner + React Testing Library.
- Mock Service Worker (MSW) to intercept REST Management API calls.
- Accessibility testing with `jest-axe`.

## Reference Architecture & Specification

Component Test Rule: Tests must interact with components via user-facing roles and text (e.g. `getByRole('button', {name: /submit/i})`), not private implementation details.

## Acceptance Criteria

- [x] Vitest & RTL setup specified.
- [x] MSW API mocking strategy documented.

## Open Questions

None.

## Related Documents

- `docs/ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md`

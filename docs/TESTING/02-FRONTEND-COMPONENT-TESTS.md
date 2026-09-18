# 02 - Frontend Component Tests

> Category: **Testing** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-22…P1-24, P2-11…P2-14, P3-12, P4-05, PF-20 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how the console is tested below the browser: what is rendered, what is stubbed, and what each test is expected to catch.

## Scope

`console/src/**/*.test.tsx`. Browser-level tests are `03-E2E-PLAYWRIGHT-TESTS.md`.

## As Built

- **Runner and tools**: Vitest with Testing Library, plus `axe` for accessibility assertions (`console/src/test/`).
- **Stubbing happens at `fetch`, not at the query layer.** The generated client, the bearer middleware and the error envelope stay in the path, because a test that mocks `useRoles` proves a component can render an array — which is not the thing that breaks. The harness (`console/src/test/harness.tsx`) provides `renderScreen`, `signIn` and `stubApi`.
- **Screens are rendered at their real route**, because a detail screen reads route params and would otherwise sit in a permanent loading state.
- **Accessibility is asserted, not assumed**: axe runs over the rendered screen and over open dialogs.
- **States are tested as separate cases**: loading, loaded, error (network, server, permission), genuinely empty versus filtered-empty, and the permission-hidden variant.

### The rules these tests exist to pin

Several project-wide rules are enforced here because nowhere else can see them:

- `color-danger` appears only on destructive actions; a warning state uses `color-warning` (`docs/UI-UX/06-VISUAL-LANGUAGE.md`).
- Client-side validation uses the **server's** generated patterns, so a form cannot accept what the API refuses or refuse what it accepts.
- Consequence-before-confirmation dialogs state the real blast radius (for example the number of users a revocation affects) rather than "are you sure?".
- Lists say when they are incomplete (`PF-20`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Stub level | `fetch` | `console/src/test/harness.tsx` |
| Accessibility | axe over table and dialog states | `console/src/test/axe.ts` |
| Validation rules | Generated from the contract | `console/src/lib/api/patterns.gen.ts` |
| Suite size at the time of writing | 276 tests across 17 files | `npm test` |

## Verification

- `cd console && npm run check` — lint, typecheck and the test suite; the same content CI runs.
- Each screen's implementation chain document (`console/docs/implementation-chain-*.md`) lists the tests that pin its decisions, and records which controls were broken to prove the tests fail.

## Not Yet Built / Open Questions

- No visual regression testing.
- Component tests run in jsdom, so layout and true focus behaviour are only observable in the E2E suite.

## Related Documents

- `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`; `docs/ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md`.

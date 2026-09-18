# 03 - Frontend Architecture (Management Console)

> Category: **Architecture** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, P1-22…P1-24, P2-11…P2-14, P3-09…P3-13, P4-05, PF-20 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how the management console is built: its client of the API, its authentication, and the conventions that keep it a client rather than a second implementation of the product.

## Scope

`console/`. Design specifications are `docs/UI-UX/`; each screen's design-to-code trail is `console/docs/implementation-chain-*.md`.

## As Built

- **Stack**: React with TypeScript, Vite, Tailwind-based design tokens, React Router, TanStack Query (`console/package.json`).
- **API client**: generated from the contract (`openapi-typescript` → `console/src/lib/api/schema.gen.ts`) and called through `openapi-fetch` (`console/src/lib/api/client.ts`). Regeneration drift is a CI gate, so a contract change that the console has not absorbed fails the build.
- **Server rules are never re-derived**: validation patterns come from `console/src/lib/api/patterns.gen.ts`, generated from the same OpenAPI schemas as the server's own, so a client rule cannot drift from the rule the API enforces.
- **Authentication**: authorization code with PKCE against this service, with silent renewal in a hidden iframe (`/auth/silent`) and a callback route (`/auth/callback`). The console is a public client and holds no secret (`console/src/lib/auth/`).
- **Tenancy**: every query key includes the active organization id, so switching organizations cannot serve the previous tenant's cached rows (`console/src/lib/api/client.ts`, `queries.ts`).
- **Lists**: `collectPages` follows `page_info.next_page_token` up to a bound and reports whether it stopped early; screens that can hit the bound say so. This replaced a single 100-row read that silently hid the newest rows (`MEMORY/records/2026-09-17-console-list-pagination.md`).
- **Route guards** mirror the roles the API requires, so a route a caller may not use is unreachable rather than merely hidden — a UX property, never the control.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Page collection bound | 20 pages × 100 rows, with an explicit notice when reached | `console/src/lib/api/queries.ts`, `pagination.test.tsx` |
| `color-danger` | Destructive actions only | `docs/UI-UX/06-VISUAL-LANGUAGE.md`, component tests |
| Accessibility | axe runs over rendered screens in component tests | `console/src/test/axe.ts` |
| Generated client and patterns | Regenerated and diffed in CI | `scripts/check.sh` |

## Interfaces

- Consumes `/v1` and the OIDC endpoints. No private endpoints (`01-SERVICE-BOUNDARIES.md`).
- Built as static files and served by nginx in the deployed environments (`deploy/console`).

## Security Considerations

- The console hides controls a caller may not use for clarity; the API refuses independently on every request (`CLAUDE.md`, `docs/PLAN/08-AUTHORIZATION.md`).
- Passkey registration deliberately happens on the issuer's own origin, not in the console, because WebAuthn binds a credential to the relying party's origin (`TASKS/BACKLOG.md` PG-40, PG-43; ADR-020).

## Verification

- Vitest + Testing Library component tests with axe (`console/src/pages/*.test.tsx`), 276 tests at the time of writing.
- Playwright E2E against a real service (`console/e2e/`), including a test that the console calls only documented paths.
- Each screen has an implementation chain document recording the design decisions and the tests that pin them (`console/docs/`).

## Not Yet Built / Open Questions

- `P4-06` (Granted Projects, the receiving side of delegation) and the ABAC policy screens (Phase 4b) are not built.
- The Phase F track rows in `TASKS/PROGRESS.md` have not been reconciled with the screens actually shipped (noted in `MEMORY/records/2026-09-15-P4-05-project-grants-tab.md`).

## Related Documents

- `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`, `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`.
- `docs/TESTING/02-FRONTEND-COMPONENT-TESTS.md`, `docs/TESTING/03-E2E-PLAYWRIGHT-TESTS.md`.

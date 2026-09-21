# 00 - Frontend Context

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, P1-21…P1-24, P2-11…P2-14, P3-10…P3-13, P4-05, P4-06, PF-20 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

State what the management console is, what it deliberately is not, and the handful of
constraints every other document in this category inherits. Read this before adding a
screen; most disagreements about "how should the frontend do X" are already settled here.

## Scope

`console/`. The hosted authentication pages are served by the Go service and are not this
application (`backend/internal/login/`). The public site is `public-site/` and shares no
code with the console — [`../WEBSITE/`](../WEBSITE/).

## As Built

The console is a **static single-page application**: React 19, TypeScript, Vite, Tailwind
v4, React Router, TanStack Query. It builds to `console/dist` and is served as files. It
holds no secret, runs no server of its own, and has no privileged path to the database.

Four properties follow from that, and they are the reason the rest of this category exists.

### It is a client of the same API everybody else uses

There is no console-only endpoint, and adding one would violate `docs/PLAN/02` FR-14 and
`AGENTS.md` hard rule 1. Every screen is built on `/v1` routes an integrator could call.
The practical consequence: when a screen needs data the API does not expose, the fix is an
API change, reviewed as one — not a query the console makes for itself.

`P4-06` is the worked example. The card was "build the Granted Projects screen"; the
screen could not be built, because nothing listed the grants made *to* an organization.
The route came first, went through the contract, and the screen followed.

### It authenticates as an ordinary OIDC public client

The console logs in through the same authorization-code-with-PKCE flow a customer's
application uses (`console/src/lib/auth/`). It is dogfooding, and deliberately so: a
console with a private back door would be a login path nobody tests.

### It re-derives nothing the server decides

Validation patterns, settings bounds, and the API's own types are **generated** from
`openapi/openapi.yaml` into `console/src/lib/api/schema.gen.ts`,
`console/src/lib/api/patterns.gen.ts` and `console/src/lib/api/settings.gen.ts`. A
hand-written rule in the console is a rule that will drift from the one the server
enforces, and the drift shows up as a form that accepts what the API refuses.

CI regenerates all three and fails on a diff (`scripts/check.sh`).

### Hiding is never a control

Route guards and conditional buttons exist for user experience: a screen the caller may
not use should be unreachable rather than half-rendered and then broken. The API enforces
every permission independently (`docs/PLAN/08`, `AGENTS.md` hard rule 2). Anyone who edits
their token, edits the guard, or calls the API directly is refused by the server.

This is repeated in `console/src/app/RequireAuth.tsx`, in every page's `mayManage` flag,
and in the review checklist, because it is the assumption most likely to erode quietly.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Tokens only — no raw hex, no arbitrary Tailwind values, no inline styles | error | `console/eslint-local-rules.js` |
| Generated client, patterns and settings match the spec | regenerated and diffed | `scripts/check.sh` |
| `color-danger` | destructive and irreversible actions only | `docs/UI-UX/06-VISUAL-LANGUAGE.md`, component tests |
| Accessibility target | WCAG 2.1 AA | `console/src/test/axe.ts` |
| Server state | TanStack Query, never a global store | `console/src/lib/api/queries.ts` |
| Access token storage | memory only | `console/src/lib/auth/tokens.ts` (ADR-019) |

## What the Console Is Not

- **Not a place for business rules.** If a decision affects access, it is the server's.
- **Not a second source of truth for copy that makes claims.** Marketing and docs copy
  lives on the public site under `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`'s governance
  rule; the console's copy describes what the screen does.
- **Not phase-independent.** `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` builds the console
  in lockstep with the API. A screen whose endpoint does not exist cannot be built against
  anything real, and building it early produces a mock-driven UI that diverges from the
  API it eventually meets. `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` makes that a per-screen gate.

## Verification

- `console/src/app/shell/AppShell.test.tsx` — the shell, its navigation, and which
  destinations still carry a "not built" badge.
- `console/src/styles/tokens.test.ts` — every token in `docs/UI-UX/05-DESIGN-SYSTEM.md` exists,
  and the contrast ratios are computed from the stylesheet rather than trusted from a comment.
- `scripts/check.sh` § Console — lint, typecheck, unit tests, and the three generation
  diffs. End-to-end tests run there too, behind `CHECK_FULL=1` locally and always in CI.

## Not Yet Built / Open Questions

- A **manual screen-reader pass** is owed before Phase 5 (`docs/UI-UX/13`). axe covers the
  mechanical failures only — see [`09-ACCESSIBILITY-PRACTICE.md`](./09-ACCESSIBILITY-PRACTICE.md).
- **Settings** and the ABAC **Policies** tab are placeholders; the instance-administration
  section is not rendered at all.
- No **performance budget is enforced** in CI beyond Vite's chunk-size warning — see
  [`11-BUILD-AND-DELIVERY.md`](./11-BUILD-AND-DELIVERY.md).

## Related Documents

- [`01-APPLICATION-STRUCTURE.md`](./01-APPLICATION-STRUCTURE.md)
- [`../ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md`](../ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md)
- [`../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`](../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md)

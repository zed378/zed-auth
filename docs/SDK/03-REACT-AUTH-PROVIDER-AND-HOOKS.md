# 03 - React Auth Provider & Hooks

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: none — no phase in `TASKS/PROGRESS.md` schedules a published `@auth/react` package &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State what a React integrator should do today, with no official React auth package available, and specify what a future published one would need to provide.

## Why It Is Not Built Yet

There is no `@auth/react` package, and no code anywhere in this repository that another React application could install. What exists is `console/src/lib/auth/AuthProvider.tsx` and its supporting files (`oidc.ts`, `pkce.ts`, `tokens.ts`, `config.ts`) — the console's **own** authentication layer, built to the same rules any other `type: spa` OIDC client would follow (`docs/PLAN/02`, `docs/PLAN/06`), but written as application code inside `console/`, not as a reusable, importable library:

- It is not published; it ships only inside the console's built bundle.
- It embeds console-specific behavior that a general-purpose package would need to make configurable or drop — for example, `console/src/lib/auth/tokens.ts`'s access token lives in a **module-scoped variable** (`let accessToken: string | null = null`), which is a singleton by construction. A package meant for arbitrary React applications, including ones that render multiple independently-authenticated widgets, cannot assume there is exactly one token in the whole process the way this file does.
- Its `Status` type (`"restoring" | "authenticated" | "anonymous" | "failed"`) and its `reauthenticate` option exist to satisfy specific console UX requirements (`docs/UI-UX/14`, step-up authentication per P3-10) rather than a general contract.

No task in `TASKS/` (Phase 0 through 5, or Phase F) schedules extracting this into a published package.

## Constraints Already Decided

These are decisions already made in the console's own implementation that a future package would need to either adopt or explicitly justify departing from:

- **Token storage: in memory only, never `localStorage`, `sessionStorage`, or a cookie** — ADR-019, because a Management API token in `localStorage` "outlives the tab, the browser restart, and the incident response" and is readable by any script on the origin.
- **Silent renewal, not eager expiry handling** — the console recovers a token via `prompt=none` against the live SSO session (ADR-019) rather than forcing an interactive login on every page load.
- **Distinct "restoring" vs. "anonymous" states** — `docs/UI-UX/14` requires these to render differently: "we are finding out" and "you are signed out" are different states to a user, and collapsing them produces a flash of an incorrect UI.
- **PKCE (`S256`) with no exemptions** — the console follows exactly the flow documented in `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`; there is no special-cased "console-only" login endpoint.
- **`sessionStorage`, not `localStorage`, for the transient PKCE verifier/state** — per-tab isolation so two concurrent logins in two tabs cannot overwrite each other's verifier (`console/src/lib/auth/oidc.ts`).
- **Authorization decisions are never trusted client-side.** Any future `usePermissions()`-style hook can only ever answer "should I show this button," never "is this action allowed" — the API independently re-checks every request (`CLAUDE.md` non-negotiable constraint).

## Key Topics To Specify

- Whether the package supports more than one authenticated "instance" per page (the console's module-scoped token variable does not need to; a general package might).
- Configuration surface: issuer, `client_id`, redirect URIs, scope — equivalent to `console/src/lib/auth/config.ts` today, generalized for arbitrary consumers.
- Hook surface and naming (e.g. `useAuth()`, `usePermissions()`) and exactly what each returns, matching the shape already proven in `AuthProvider.tsx` (`status`, `claims`, `error`, `login`, `logout`) rather than inventing a new one.
- SSR/Next.js compatibility, which the console's browser-only implementation does not need to consider today.
- How a `usePermissions()`-style hook would source its data — decoded token claims (fast, stale as of issuance) or a live call to `/v1/authz/check` (accurate, slower) — and how it makes that trade-off visible to the caller rather than silently picking one, consistent with `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`'s own explanation of the same trade-off.

## Acceptance Criteria

- [ ] The package's token storage defaults match ADR-019 (memory only) unless a consumer explicitly opts into a different, documented trade-off — never a silent `localStorage` default.
- [ ] The exposed status model distinguishes "still restoring" from "confirmed anonymous," matching the UX requirement already implemented in the console (`docs/UI-UX/14`).
- [ ] Every network call the package makes maps to a documented `operationId` in `openapi/openapi.yaml` — no undocumented endpoint.
- [ ] Any permission-checking hook's documentation states explicitly, next to the hook, that its result is advisory only and the server re-checks independently — matching `CLAUDE.md`'s non-negotiable constraint and `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`'s own two-tier explanation (token claims vs. `/v1/authz/check`).
- [ ] The PKCE implementation is `S256` only, with no `plain` fallback, matching `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`.
- [ ] A test suite exercises silent renewal, refresh rotation (including the reuse-detection revocation path), and the anonymous/restoring/authenticated/failed state transitions — the same scenarios `console/src/lib/auth/auth.test.ts` already covers for the console's own implementation.
- [ ] A named task with an assigned roadmap phase exists in `TASKS/` before implementation begins.

## Open Questions

- Whether this is ever extracted from the console at all — no roadmap phase names it, and the console's implementation was not written with extraction in mind (see the module-scoped token variable above).
- Multi-instance/SSR support, as noted above.

## Related Documents

- `docs/SDK/00-SDK-ARCHITECTURE.md`
- `docs/SDK/02-TYPESCRIPT-SDK.md`
- `console/src/lib/auth/AuthProvider.tsx`, `oidc.ts`, `pkce.ts`, `tokens.ts`, `config.ts`, `auth.test.ts`
- `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`
- `docs/UI-UX/14` (console loading/auth states)
- `MEMORY/DECISIONS.md` ADR-019

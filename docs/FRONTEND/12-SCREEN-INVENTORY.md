# 12 - Screen Inventory

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-22…P1-24, P2-11…P2-14, P3-10…P3-13, P4-05, P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

One table of every screen that exists today: its file, the task that built it, its tests,
and whether its implementation chain was committed. Use it to find the precedent before
building a new screen, and to see what the console does **not** have.

## Scope

`console/src/pages/`. The planned-but-unbuilt screens are
`TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`.

## As Built

### Screens

| Route | File | Built by | Tests | Chain |
|---|---|---|---|---|
| `/` | `OverviewPage.tsx` | P1-22 | `screens.test.tsx` | `console/docs/implementation-chain-P1-22.md` |
| `/projects` | `ProjectsPage.tsx` | P1-22 | `screens.test.tsx` | same |
| `/projects/:projectId` | `ApplicationsPage.tsx` | P1-22 | `screens.test.tsx` | same |
| `/projects/:projectId/roles` | `RolesPage.tsx` | P2-11 | `roles.test.tsx` | `console/docs/implementation-chain-P2-11.md` |
| `/projects/:projectId/authorizations` | `AuthorizationsPage.tsx` | P2-12 | `authorizations.test.tsx` | `console/docs/implementation-chain-P2-12.md` |
| `/projects/:projectId/grants` | `ProjectGrantsPage.tsx` | P4-05 | `projectgrants.test.tsx` | `console/docs/implementation-chain-P4-05.md` |
| `/users` | `UsersPage.tsx` | P1-23 | `screens.test.tsx` | `console/docs/implementation-chain-P1-23-P1-24.md` |
| `/users/:userId` | `UserDetailPage.tsx` | P1-23 | `screens.test.tsx` | same |
| `/users/:userId` → Sessions | `SessionsTab.tsx` | P3-11 | `sessions.test.tsx` | — |
| `/users/:userId` → MFA | `MfaTab.tsx` | P3-10 | `mfa.test.tsx` | — |
| `/granted-projects` | `GrantedProjectsPage.tsx` | **P4-06** | `grantedprojects.test.tsx` | `console/docs/implementation-chain-P4-06.md` |
| `/policies` | `PoliciesPage.tsx` | P2-14 | `policies.test.tsx` | `console/docs/implementation-chain-P2-14.md` |
| `/audit-log` | `AuditLogPage.tsx` | P1-24 | `screens.test.tsx` | `console/docs/implementation-chain-P1-23-P1-24.md` |
| `/account` | `AccountPage.tsx` | P3-12 | `account.test.tsx` | — |
| `/settings` | `PlaceholderPage.tsx` | — | — | — |
| `/auth/callback` | `CallbackPage.tsx` | P1-21 | `auth.test.ts` | — |
| `/auth/silent` | `SilentCallbackPage.tsx` | P1-21 | `auth.test.ts` | — |
| any unknown | `NotFoundPage.tsx` | P0-17 | `screens.test.tsx` | — |

The organization switcher (`P2-13`) is part of the shell rather than a screen;
its chain is `console/docs/implementation-chain-P2-13.md`.

### End-to-end coverage

| Spec | Flow |
|---|---|
| `console/e2e/login.spec.ts` | Sign in; a role-gated route refuses |
| `console/e2e/sso.spec.ts` | Single sign-on across two applications; sign-out ends the shared session; B refuses A's token |
| `console/e2e/shell.spec.ts` | The shell, navigation, deep links |
| `console/e2e/orgswitcher.spec.ts` | Switching; an organization forced into the URL is refused by the service |
| `console/e2e/authorizations.spec.ts` | Assigning a role, checked against the API |
| `console/e2e/sessions.spec.ts` | Revoking a session, including tap-target size at the narrowest supported width |
| `console/e2e/mfa.spec.ts` | Enrolment and enforcement |
| `console/e2e/account.spec.ts` | Personal settings |
| `console/e2e/projectgrants.spec.ts` | **Flow 2** — create and revoke a grant, checked against the API both times |
| `console/e2e/grantedprojects.spec.ts` | **Flow 3** — assign a delegated role, check the API, remove it, check again |
| `console/e2e/consistency.spec.ts` | API and console stay consistent — creating via one is visible via the other (`P1-28`). The one claim neither surface's own tests can make |

### Where an implementation chain is missing

`SessionsTab`, `MfaTab`, `AccountPage` and the auth callbacks have no committed chain.
`docs/UI-UX/19` and `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` make the chain mandatory
for Tracks A, D and E, so these are a gap rather than an exemption — the screens are
tested and specified, but the twelve-step table was not written for them.

## Not Yet Built

From `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` and `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`:

- **Organization settings** — `/settings` is a placeholder. Branding and domain
  verification have a specification and no roadmap task; `PG-16` settled where branding is
  stored, not when the screen is built.
- **Instance administration** — not rendered at all. `docs/PLAN/06` puts it behind
  `INSTANCE_OWNER`.
- **ABAC policies tab** (`P4B-07`, `PF-38`) — Phase 4b is conditional and has not started.
- **SAML application management** (`P4-09`) — the protocol is unbuilt.
- **A screen for `PROJECT_GRANT_OWNER`** — the role exists (`P4-03`) and no route is
  guarded for it.

## Related Documents

- [`06-ROUTING-AND-PERMISSIONS.md`](./06-ROUTING-AND-PERMISSIONS.md)
- [`../UI-UX/08-PAGE-SPECIFICATIONS.md`](../UI-UX/08-PAGE-SPECIFICATIONS.md)
- [`../UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md`](../UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md)

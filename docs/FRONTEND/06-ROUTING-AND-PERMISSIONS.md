# 06 - Routing and Permissions

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-21, P1-22, P2-11…P2-14, P4-05, P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

State the route table, how a route is guarded, and — the part that matters — why a guard
is never the control.

## Scope

`console/src/app/routes.tsx`, `console/src/app/RequireAuth.tsx`,
`console/src/app/shell/SideNav.tsx`.

## As Built

### Two levels, because one screen has a different shell

`console/src/App.tsx` routes first, and it routes on shell rather than on content:

```
/account  → AccountShell  → RequireAuth → AccountPage
*         → AppShell      → AppRoutes (console/src/app/routes.tsx)
```

Personal settings is the one screen built for a phone (`P3-12`), so it renders in its own
shell, outside the console's narrow-screen guard. Everything else keeps the console shell
and is routed by the table below.

The provider nesting in the same file is ordered for reasons worth not undoing: the
`AuthProvider` sits **inside** the router because logging in needs to know where the user
was going, and **outside** the shell because the shell renders the signed-in user's name.
`OrgProvider` sits inside `AuthProvider` because the default organization is the token's,
inside the router because the active one is carried in the URL, and outside the shell
because the shell renders the switcher.

### The route table

Every URL in the console, and the roles its guard admits. The roles are claims on the
access token; the API enforces its own policy independently
(`backend/internal/management/policy.go`).

| Route | Screen | Guard |
|---|---|---|
| `/auth/callback` | `CallbackPage` | none — completes a login |
| `/auth/silent` | `SilentCallbackPage` | none — renewal in a hidden iframe |
| `/` | `OverviewPage` | signed in |
| `/projects` | `ProjectsPage` | `ORG_ADMIN`, `ORG_OWNER`, `INSTANCE_OWNER` |
| `/projects/:projectId` | `ApplicationsPage` | same |
| `/projects/:projectId/roles` | `RolesPage` | same |
| `/projects/:projectId/authorizations` | `AuthorizationsPage` | same |
| `/projects/:projectId/grants` | `ProjectGrantsPage` | same |
| `/users` | `UsersPage` | same |
| `/users/:userId` | `UserDetailPage` | same |
| `/granted-projects` | `GrantedProjectsPage` | same |
| `/policies` | `PoliciesPage` | `ORG_OWNER`, `INSTANCE_OWNER` |
| `/audit-log` | `AuditLogPage` | `ORG_ADMIN`, `ORG_OWNER`, `INSTANCE_OWNER` |
| `/settings` | placeholder | signed in |
| `*` | `NotFoundPage` | none |

`/account` is routed one level up, as above. It is reached from the session bar rather
than the side navigation: personal settings belong to the caller, not to an organization.

### Four session states, four answers

`RequireAuth` renders a different thing for each (`console/src/app/RequireAuth.tsx`):

| State | What renders | Why not something else |
|---|---|---|
| `restoring` | "Checking your session" | The token is memory-only (ADR-019), so every reload passes through here. A blank screen is indistinguishable from a broken one |
| `failed` | "Sign-in is unavailable", with the error code | A configuration or service problem, stated as such rather than blamed on the user |
| `anonymous` | the sign-in page | Not a redirect: the URL is preserved, so signing in lands where the user was going |
| signed in, wrong role | "You do not have access to this" | Not a redirect and not a 404. The user is signed in and the page exists; saying so plainly is more useful than pretending otherwise, and the API would refuse the data anyway |

### A guard is a user-experience feature

`docs/UI-UX/08` § Cross-Screen Requirements asks that a route the caller's claims do not
permit be genuinely unreachable rather than merely hidden. `docs/PLAN/08` is equally clear
that the API enforces every permission independently. Both are true at once, and the
distinction is the one most likely to erode:

> Somebody who edits their token, or the guard, or simply calls the API directly gets
> refused by the server. What they do not get is a console screen that renders half a
> page and then fails.

The same reasoning applies one level down. Every screen computes a `mayManage` flag to
decide whether to render a control, and every one of them says in a comment that the
server refuses independently. `P4-06`'s screen is guarded to `ORG_ADMIN` and above; its
route answers another organization's path with **404**, not 403, because an organization
the caller holds no role in is not confirmed to exist.

### Navigation reflects what is built

`console/src/app/shell/SideNav.tsx` carries an optional `phase` badge — "Arrives in phase
P4" — on destinations that do not exist yet. It is a maintenance hazard: a "P1" badge sat
on Projects, Users, Policies and the Audit Log long after all four shipped, which `P3-13`
fixed. `console/src/app/shell/AppShell.test.tsx` now asserts the exact set of badged
destinations, so a badge left behind fails the build. `P4-06` removed the badge from
Granted Projects in the same commit that built the screen.

### The error boundary

`console/src/app/ErrorBoundary.tsx` catches a render failure and shows a page that says
what happened, rather than an empty document. A crashed console with no message looks
identical to an authentication failure, a network failure and a deployment mistake.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Route guards mirror the API's roles | see table | `console/src/app/routes.tsx` vs `backend/internal/management/policy.go` |
| A guard is never a control | invariant | `console/src/app/RequireAuth.tsx` |
| Unbuilt destinations are badged | exact set asserted | `console/src/app/shell/AppShell.test.tsx` |
| Unknown URL | `NotFoundPage`, not a redirect | `console/src/app/routes.tsx` |

## Verification

- `console/src/app/RequireAuth.test.tsx` — each of the four states.
- `console/src/app/shell/AppShell.test.tsx` — the navigation's destinations, labels and
  badge set.
- `console/e2e/login.spec.ts` — a role-gated route refusing against the real service.
- `console/e2e/orgswitcher.spec.ts` — an organization forced into the URL is refused by
  the service, which is the guard-is-not-a-control property observed end to end.

## Not Yet Built / Open Questions

- **`PROJECT_OWNER` cannot reach any project tab.** The guards admit organization-level
  roles only, while the API's policy for those routes is `PROJECT_OWNER` at project scope.
  A project owner who is not an organization administrator is refused by the console
  before the API would admit them. This is a known limitation of every project tab, not
  specific to one screen, and it is the guard being *stricter* than the API rather than
  looser — the safe direction, but still wrong.
- **`PROJECT_GRANT_OWNER`** (`P4-03`) has no console path at all: the role is scoped to one
  grant, and no screen is guarded for it.
- **The instance-administration section is not rendered**, pending a screen for it.

## Related Documents

- [`05-AUTHENTICATION-AND-SESSION.md`](./05-AUTHENTICATION-AND-SESSION.md)
- [`12-SCREEN-INVENTORY.md`](./12-SCREEN-INVENTORY.md)
- [`../AUTHORIZATION/`](../AUTHORIZATION/)

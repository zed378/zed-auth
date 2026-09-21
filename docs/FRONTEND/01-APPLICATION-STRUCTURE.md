# 01 - Application Structure

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, P1-21, P1-22, P2-13, PF-01…PF-11 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Say where each kind of code lives, and which direction dependencies are allowed to run.
The layout is shallow on purpose: an admin console is mostly screens over a shared table,
and a deep folder hierarchy in that shape produces long import paths and no extra clarity.

## Scope

`console/src/`. Build configuration is [`11-BUILD-AND-DELIVERY.md`](./11-BUILD-AND-DELIVERY.md).

## As Built

```
console/
  src/
    app/          the application shell: routing, guards, error boundary, navigation
      shell/      AppShell, SideNav, the organization switcher
    components/   shared UI — Table, Modal, SidePanel, Button, Badge, states
    pages/        one file per screen, plus its test
    lib/
      api/        generated client, query hooks, generated patterns and bounds
      auth/       PKCE, token storage, the AuthProvider
      org/        which organization the console is acting in
    branding/     the brand mark, derived from the same source as the public site
    styles/       tokens.css and its tests
    test/         harness, axe helper, vitest setup
  e2e/            Playwright specs and fixtures
  docs/           one implementation chain per screen
  scripts/        gen-patterns.mjs
```

### The four layers, and the direction

```
pages/ ─────────► components/ ─────────► styles/
   │                                        ▲
   └──────────► lib/ (api, auth, org) ──────┘
```

- **`pages/` may import anything.** A screen is the composition point.
- **`components/` may import `components/` and `styles/`.** It must not import `lib/api`:
  a shared component that fetches is a component that cannot be reused on a screen with a
  different data shape, and it hides a request inside something that looks like markup.
- **`lib/` must not import `components/` or `pages/`.** Data does not know about
  presentation.
- **`app/` sits above `pages/`** and holds the things that exist once: the route table,
  the guard, the shell, the error boundary.

There is no automated check on these directions today — it is a review item on
[`../ENGINEERING/14-CODE-REVIEW-CHECKLIST.md`](../ENGINEERING/14-CODE-REVIEW-CHECKLIST.md).
The backend has the equivalent rule enforced by a test
(`backend/internal/management/architecture_test.go`); the console does not, and that is an
honest gap rather than a decision.

### One file per screen

`console/src/pages/GrantedProjectsPage.tsx` holds the page, its side panel, its
confirmation dialog and its small helpers. Splitting a screen across four files makes the
flow harder to follow than the file length does, and every screen is read far more often
than it is edited.

A sub-component graduates to `components/` when a **second** screen needs it, not when it
looks reusable. `RoleSourceBadge` is the counter-example that earned its place early: it
was built in `P2-12` for a case that could not occur until Phase 4, because
`docs/UI-UX/08` makes it mandatory wherever roles appear and retrofitting it would have
meant auditing every screen that shows a role.

### Tests live beside what they test

`console/src/pages/grantedprojects.test.tsx` sits next to the page. A separate `__tests__`
tree makes it possible to move a file and leave its test behind, which is how a screen
ends up with no coverage and a green build.

### Generated files are named for it

`schema.gen.ts`, `patterns.gen.ts`, `settings.gen.ts`. The suffix is the warning: editing
one is pointless, because the next `npm run api:generate` discards it and CI fails on the
diff. `schema.gen.ts` is excluded from linting for the same reason —
`console/eslint.config.js` says so in place.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| A shared component fetches nothing | review | this document, [`03-COMPONENT-LIBRARY.md`](./03-COMPONENT-LIBRARY.md) |
| `lib/` imports no UI | review | this document |
| Test beside subject | `src/**/*.test.{ts,tsx}` | `console/vite.config.ts` |
| Generated files carry `.gen.` | convention | `console/package.json` scripts |
| One page component per route | convention | `console/src/app/routes.tsx` |

## Interfaces

- **Into the application**: `console/src/app/routes.tsx` is the only place a URL is
  associated with a screen.
- **Out to the API**: `console/src/lib/api/client.ts` is the only module that constructs a
  request. A hand-written `fetch` against a path string anywhere else is the drift the
  generated client exists to prevent.

## Verification

- `console/src/app/shell/AppShell.test.tsx` — the shell renders, the navigation lists the
  destinations in order, and only unbuilt ones are badged.
- `npm run typecheck` in `console/` — the generated types make a wrong path or a wrong
  body a compile error rather than a runtime 400.

## Not Yet Built / Open Questions

- **No architecture test on import direction.** The backend has one; the console relies on
  review. Worth closing with an ESLint `no-restricted-imports` rule.
- `console/src/branding/` duplicates a small amount of brand derivation with the public
  site. They are kept in step by `scripts/check.sh` § Public site rather than by sharing
  code, because `docs/PLAN/20` requires the two surfaces to stay independent.

## Related Documents

- [`03-COMPONENT-LIBRARY.md`](./03-COMPONENT-LIBRARY.md)
- [`04-STATE-AND-DATA-LAYER.md`](./04-STATE-AND-DATA-LAYER.md)
- [`../ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md`](../ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md)

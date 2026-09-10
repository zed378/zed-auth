# 06 — Frontend Architecture

> **Scope note**: this document covers the **management console** specifically (the authenticated admin/self-service UI). The **public-facing website** (landing page, docs, about, changelog) is a separate frontend surface with a different audience, tech stack, and deploy cadence — see `20-PUBLIC-SITE-ARCHITECTURE.md` for that. The two should never share a codebase or deployment pipeline, only the visual/brand language (`UI-UX/06-VISUAL-LANGUAGE.md`).

This covers the **technical architecture** of the management console. Design and UX planning for the same console lives in `UI-UX/`; this document is the engineering counterpart.

## Tech Stack

| Concern | Choice | Reason |
|---|---|---|
| Framework | **React + TypeScript** | Large ecosystem, matches teams already building on Go/REST backends, easy to hire for |
| Styling / component library | Design-token-based system (e.g. Tailwind + a small internal component set) | Keeps the console visually distinct and easy to re-theme per organization later; see `UI-UX/05-DESIGN-SYSTEM.md` for the actual tokens |
| API client | Generated from the OpenAPI spec (`05-API-CONTRACT.md`) | Keeps UI types in sync with the backend automatically; regenerate in CI when the spec changes |
| State/data fetching | A server-state library (e.g. TanStack Query) rather than a global store for everything | Most console data is cached server data, not client-only state |
| Auth of the console itself | The console is **just another OIDC client** (Authorization Code + PKCE, `type: spa`) | "Dogfooding" — see below |
| Build/deploy | Static build (SPA), served via CDN/static hosting, calling the Management API over HTTPS | Decouples UI deploys from backend deploys |

## Why the Console Must Log In Through the Same OIDC Flow

Since the console is registered as a normal `application` (`04-DATA-MODEL.md`) with `type: spa`, it:
- Gets a normal SSO session cookie like any other app — an admin already logged in elsewhere doesn't need to log in again to reach the console.
- Receives an access token whose role claims (`08-AUTHORIZATION.md`) determine what the UI shows — e.g. a `project_admin` simply won't see the "delete organization" button, because their token doesn't carry `ORG_OWNER`.
- Forces the team to keep the OIDC flow itself solid early, since the team's own daily tool depends on it.

## Information Architecture (Navigation)

```
┌─ Instance (visible only to INSTANCE_OWNER)
│   ├─ Organizations (list, create)
│   ├─ Instance-wide policies
│   └─ Instance audit log
│
└─ Organization (the main workspace for most admins)
    ├─ Overview
    ├─ Projects
    │   └─ [Project detail]
    │       ├─ Applications
    │       ├─ Roles
    │       ├─ Authorizations (user ↔ role grants)
    │       └─ Project Grants (delegation to other orgs)
    ├─ Users
    │   └─ [User detail]: Profile, Grants, Sessions, MFA
    ├─ Granted Projects (projects delegated *to* this org)
    ├─ Policies (password policy, MFA requirement, session lifetime; also ABAC policies once Phase 4b is active)
    ├─ Audit Log
    └─ Settings (branding, domain verification)
```

This mirrors `04-DATA-MODEL.md` almost 1:1 on purpose — navigation should never require a concept the data model doesn't have, and vice versa. The full page-by-page specification (fields, states, copy) is in `UI-UX/08-PAGE-SPECIFICATIONS.md`.

## Testing

- **Component tests** for form validation logic (redirect URI format, role key format).
- **E2E tests** (Playwright, shared setup with `11-TESTING.md`) for the highest-risk flows: user invite + first role assignment, Project Grant creation/revocation, session revocation.
- **Contract testing** against the generated API client, so a backend API change not reflected in the OpenAPI spec breaks CI before it breaks the console at runtime.

Continue to [07 — Backend Architecture](./07-BACKEND-ARCHITECTURE.md).

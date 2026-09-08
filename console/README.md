# console/

The management console: a React + TypeScript SPA that authenticates as an ordinary OIDC client (`type: spa`, Authorization Code + PKCE) — the dogfooding constraint from `PLAN/02-REQUIREMENTS.md` and `PLAN/06-FRONTEND-ARCHITECTURE.md`.

**Governing documents**: `PLAN/06-FRONTEND-ARCHITECTURE.md` (engineering), the whole `UI-UX/` folder (design), starting at `UI-UX/00-DESIGN-DIRECTION.md`. Task breakdown: `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`.

## Rules That Are Not Negotiable

- **Tokens only.** No raw hex colors, no arbitrary spacing. New tokens and components go into the design system before a page uses them (`UI-UX/05-DESIGN-SYSTEM.md` § Governance).
- **`color-danger` is reserved** for destructive and irreversible actions (`UI-UX/06-VISUAL-LANGUAGE.md`).
- **Permission-gated routes are genuinely unreachable, not hidden.** The API enforces independently regardless (`UI-UX/08-PAGE-SPECIFICATIONS.md`).
- **Every screen runs through the twelve-step chain** in `UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` before it is considered implementable.
- The API client is **generated** from `openapi/openapi.yaml`; never hand-written.

## Commands

Populated by `P0-17`.

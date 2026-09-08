# 08 — Page Specifications

> This document gives the full screen inventory as a summary table. For the concrete, checkable format (primary objective, visual hierarchy, above-the-fold, interaction, responsive grid mapping, keyboard behavior) applied to the highest-traffic screens, see `18-DETAILED-PAGE-SPECIFICATIONS.md`. For how any given component on these screens gets turned into implementable behavior end-to-end, see `19-FRONTEND-IMPLEMENTATION-CHAIN.md`.

Full screen inventory, built from `PLAN/06-FRONTEND-ARCHITECTURE.md`'s IA and `01-USER-PERSONAS.md`'s needs. Each entry names the primary users, key actions, and the components from `07-COMPONENT-SPECIFICATION.md` it's composed from.

| Screen | Primary users | Key actions | Composed of | Notes |
|---|---|---|---|---|
| Organization switcher | Any admin belonging to >1 org | Switch active org context | Search input, list | Only relevant once multi-org is active (`PLAN/16-IMPLEMENTATION-ROADMAP.md` Phase 2) |
| Organization list (instance level) | Instance Owner (Dian) | Create org, view org, suspend org | Table, primary button, confirmation dialog | |
| Project list | Org Admin (Budi) | Create project | Table, primary button | |
| Project detail — Applications tab | Project Owner (Sari) | Register application, view `client_id`/secret, set redirect URIs | Table, form, modal | Secrets shown once at creation only, never retrievable again (`PLAN/09-SECURITY.md`) |
| Project detail — Roles tab | Project Owner (Sari) | Create/edit role, define permission keys | Table, form | |
| Project detail — Authorizations tab | Project Owner (Sari) | Search user, assign/revoke role | Search input, table, role-source badge | Implements `04-USER-FLOWS.md` role-source visibility requirement |
| Project detail — Project Grants tab | Project Owner (Sari) | Create a grant, revoke a grant | Table, modal, confirmation dialog (typed-confirmation variant) | Implements `04-USER-FLOWS.md` Flow 2 exactly |
| Granted Projects list | Vendor Admin (Reza) | View delegated projects, assign allowed roles to own users | Table, restricted role-select form | Implements `04-USER-FLOWS.md` Flow 3 — never shows non-granted roles |
| User list | Org Admin (Budi) | Invite user, search/filter, deactivate | Table, search input, side panel (invite flow) | Implements `04-USER-FLOWS.md` Flow 1 |
| User detail — Profile tab | Org Admin (Budi) | Edit profile, deactivate user | Form, secondary/danger-text button | |
| User detail — Grants tab | Org Admin (Budi) | View all project roles this user holds | Table, role-source badge | |
| User detail — Sessions tab | Org Admin (Budi), self-service (Ayu) | View active sessions, revoke a session | Table, lightweight (non-modal) confirm | Implements `04-USER-FLOWS.md` Flow 4 — immediate, low-friction revoke |
| User detail — MFA tab | Org Admin (read-only), self-service (Ayu, full control) | Enroll/remove TOTP or passkey | Form, badge (enrolled/not enrolled) | |
| Policies — Access tab | Org Admin (Budi) | Edit password policy, toggle mandatory MFA, set session lifetime | Form | Maps to `organizations.settings`, `PLAN/04-DATA-MODEL.md` |
| Policies — ABAC tab (Phase 4b) | Policy Author (Fajar) | Write/edit Rego policy, dry-run, activate/rollback | Rego editor, diff view, confirmation dialog | Implements `04-USER-FLOWS.md` Flow 5 exactly; optional, only if Phase 4b undertaken |
| Audit Log | Org Admin (Budi), Instance Owner (Dian) | Filter by event type/date/actor, export | Table, filter controls | Reads `events` table, `PLAN/04-DATA-MODEL.md` |
| Personal account settings | Any logged-in user (Ayu) | Change password, manage own MFA, manage own sessions, view linked social logins | Form, table (sessions), badge (MFA status) | Self-service counterpart of the admin-facing screens above |

## Cross-Screen Requirements

- **Every screen showing roles** (Authorizations tab, User Grants tab, Granted Projects) must use the role-source badge from `06-VISUAL-LANGUAGE.md`/`07-COMPONENT-SPECIFICATION.md` — this is not optional per-screen.
- **Every list screen** (Organization list, Project list, User list, Audit Log) follows the same table anatomy, pagination pattern, and empty/loading/error states (`14-EMPTY-LOADING-ERROR-STATES.md`) — no screen invents its own table pattern.
- **Every screen reachable by a role that shouldn't see it is not merely hidden by CSS but genuinely unreachable** — the frontend must check the same role claims the API enforces (`PLAN/06-FRONTEND-ARCHITECTURE.md` "dogfooding"), so there's never a gap between what's visible and what's actually permitted.

## Responsive Scope Per Screen

Per `12-RESPONSIVE-BEHAVIOR.md`, all screens above must remain usable down to tablet width; full mobile optimization is only required for the **Personal account settings** screen (since end users, unlike admins, may reasonably use it from a phone) — see `16-MOBILE-UX.md`.

Continue to [09 — Interaction Design](./09-INTERACTION-DESIGN.md).

# 00 — Design Direction

## Purpose of This Document

Sets the high-level design intent for the management console before any screens or components are specified in detail. Everything in `UI-UX/` should trace back to the principles here.

## What Kind of Product Is This?

The console is an **operational admin tool**, not a marketing site or a consumer app. Admins use it repeatedly to perform structured, often security-sensitive tasks (granting access, revoking a session, delegating a project). This shapes every subsequent decision:

- **Clarity over decoration.** An admin should never have to guess what a control does before clicking it, especially for destructive or access-granting actions.
- **Density over whitespace-for-its-own-sake.** Admins managing many users/roles benefit from seeing more at once (tables, not endless cards), as long as it stays scannable.
- **Trust through transparency.** Since this tool controls access to everything else, every screen that shows or changes a permission must make the *consequence* of an action legible before it's taken (see `UI-UX/04-USER-FLOWS.md` and `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`).
- **Consistency with the API.** The UI must never imply a capability or state that the underlying data model (`PLAN/04-DATA-MODEL.md`) doesn't actually have — no decorative "coming soon" toggles that look real.

## Design Principles

1. **Predictable before novel.** Use conventional admin-console patterns (left nav, breadcrumb, table + detail panel) rather than inventing new interaction metaphors. Novelty here creates friction, not delight.
2. **Progressive disclosure.** Complex concepts (Project Grants, ABAC policies) are hidden from admins who don't need them yet, and introduced with enough context when they first appear (`UI-UX/02-USER-JOURNEYS.md`).
3. **Reversible by default, irreversible actions clearly marked.** Most admin mistakes should be easy to undo; the few that aren't (deleting an organization, permanently revoking a Project Grant with many dependents) get extra friction, not less.
4. **Accessible from the first screen**, not retrofitted — see `UI-UX/13-ACCESSIBILITY.md`.

## Tone & Voice

- Direct, factual, and calm — especially in error and confirmation copy. Avoid exclamation points and forced enthusiasm; an admin revoking access to a compromised account is not a moment for cheerful microcopy.
- Never blame the user in error messages; describe what happened and what to do next (`UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`).

## Visual Direction (Summary — Full Detail in `UI-UX/06-VISUAL-LANGUAGE.md`)

- A restrained, neutral palette with a single strong accent color reserved for primary actions and focus states — not used decoratively.
- Typography optimized for data-dense tables and forms (a workhorse UI typeface, not a display font).
- Minimal use of illustration; icons are functional (status, action affordances), not decorative.

## Non-Goals for Visual Design

- No heavy theming/skinning system in the first version beyond basic per-organization branding (logo, accent color) — see `PLAN/01-PRODUCT-SCOPE.md`.
- No marketing-style visual flourishes (large hero sections, illustrations, animation-heavy landing screens) — this tool is opened dozens of times a day by the same admins, and should optimize for speed of repeated use over first impression.

Continue to [01 — User Personas](./01-USER-PERSONAS.md).

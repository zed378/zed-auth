# 05 — Design System

Defines the token-level foundation everything else in `UI-UX/` is built from. Implemented technically as described in `PLAN/06-FRONTEND-ARCHITECTURE.md` (Tailwind + a small internal component set).

## Design Tokens

### Color

| Token | Purpose | Notes |
|---|---|---|
| `color-bg-base` | Page background | Neutral, low-saturation |
| `color-bg-surface` | Card/table/panel background | One step lighter/darker than base for layering |
| `color-text-primary` | Main body/heading text | Must meet WCAG AA contrast against both bg tokens (`13-ACCESSIBILITY.md`) |
| `color-text-secondary` | Metadata, helper text | Lower emphasis, still AA-compliant |
| `color-accent` | Primary actions, active nav item, focus rings | One accent only — not used decoratively (`00-DESIGN-DIRECTION.md`) |
| `color-danger` | Destructive actions, error states | Reserved exclusively for irreversible/destructive meaning — never reused for anything else, so its appearance is always a reliable signal |
| `color-warning` | Caution states (e.g. "this Project Grant has no roles selected yet") | Distinct from danger — warning ≠ destructive |
| `color-success` | Confirmation states (e.g. "invite sent") | |
| `color-border` | Dividers, table borders, input borders | |

Per-organization branding (`PLAN/01-PRODUCT-SCOPE.md`) overrides `color-accent` and the logo only — never `color-danger`/`color-warning`, since those must stay universally recognizable regardless of tenant branding.

### Typography

| Token | Use |
|---|---|
| `font-family-base` | A workhorse UI typeface optimized for legibility in dense tables and forms, not a display font (`00-DESIGN-DIRECTION.md`) |
| `font-size-body` / `font-size-small` / `font-size-heading-*` | A constrained type scale (5–6 steps), not an open-ended set |
| `font-weight-regular` / `font-weight-medium` / `font-weight-bold` | Weight is used to establish hierarchy in dense tables (e.g. a user's name vs. their email) more often than size is |

### Spacing & Layout

- An 4px/8px-based spacing scale, applied consistently so tables, forms, and cards share a rhythm.
- A consistent content max-width for form-heavy screens (so long forms on wide monitors don't stretch into unreadable line lengths), while tables can use full available width.

### Elevation

- A minimal set of elevation levels (flat, raised — for modals/dropdowns, overlay — for confirmation dialogs) rather than an elaborate shadow system; this is an admin tool, not a visually rich consumer app.

## Component Foundations (Detailed Specs in `07-COMPONENT-SPECIFICATION.md`)

- Buttons: primary (accent), secondary (neutral), destructive (danger) — exactly three visual weights, mapped 1:1 to action severity so an admin can gauge risk from color alone before reading the label.
- Tables: the primary data-display pattern for this console (per `00-DESIGN-DIRECTION.md`'s "density over whitespace") — sortable columns, row-level actions, consistent empty/loading states (`14-EMPTY-LOADING-ERROR-STATES.md`).
- Forms: consistent field-label-helper-error pattern across the whole console (`15-FORM-UX.md`).
- Badges/tags: used for status (active/invited/deactivated) and for role-source indicators (direct vs. delegated — see `04-USER-FLOWS.md` Flow 2/3).

## Governance

- Any new token or component must be added here before being used in a page spec (`08-PAGE-SPECIFICATIONS.md`) — the design system is the source of truth, not individual screens.
- Token names, not raw values, are referenced everywhere else in `UI-UX/`, so a rebrand or theme adjustment never requires hunting through every page spec.

Continue to [06 — Visual Language](./06-VISUAL-LANGUAGE.md).

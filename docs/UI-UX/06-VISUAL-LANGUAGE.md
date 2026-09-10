# 06 — Visual Language

Expands on `00-DESIGN-DIRECTION.md` and `05-DESIGN-SYSTEM.md` with concrete visual treatment guidance — how the tokens actually get applied to communicate meaning.

## Iconography

- Icons are **functional signals**, not decoration: a lock icon means "restricted/MFA," a link/chain icon means "delegated via Project Grant," a person icon means "direct assignment." Once an icon is assigned a meaning in `07-COMPONENT-SPECIFICATION.md`, it must not be reused for a different meaning elsewhere in the console.
- A single consistent icon set (one stroke weight, one style — outline or filled, not mixed) throughout.

## Status Communication

Status is always communicated through **both color and text/icon**, never color alone (this is also an accessibility requirement, `13-ACCESSIBILITY.md` for users with color vision deficiency):

| Status | Color token | Icon/text pairing |
|---|---|---|
| Active | `color-success` | Filled dot + "Active" label |
| Invited (pending) | `color-warning` | Outline dot + "Invited" label |
| Deactivated | `color-text-secondary` (neutral, not danger) | Outline dot + "Deactivated" label |
| Revoked (grant) | `color-danger` | Slash icon + "Revoked" label |

Note "Deactivated" deliberately does **not** use `color-danger` — a deactivated user isn't a threat signal, and reserving danger-red exclusively for actually alarming states (per `05-DESIGN-SYSTEM.md`) keeps that color meaningful when it does appear.

## Role-Source Visual Treatment

Since `04-USER-FLOWS.md` identifies "where did this role come from" as a critical piece of information (direct grant vs. Project Grant delegation), this gets a consistent visual pattern wherever roles are listed:

- **Direct grant**: plain role badge.
- **Delegated grant**: role badge + a small link icon + the source organization name on hover/tap, so the origin is always one interaction away, never hidden behind a separate screen.

## Data Density Treatment

Per `00-DESIGN-DIRECTION.md`'s "density over whitespace" principle:
- Tables use compact row height by default (not the airy, oversized rows common in consumer apps), since admins scan many rows at once.
- Detail panels (e.g. user detail) use tabs (`PLAN/06-FRONTEND-ARCHITECTURE.md` IA) rather than one long scrolling page, so related information stays reachable without excessive scrolling.

## Illustration & Imagery

- No illustrations in the core console flows — illustration is reserved only for genuinely rare states (e.g. a fully empty organization with zero projects yet) where a small, simple graphic can soften an otherwise bare screen (`14-EMPTY-LOADING-ERROR-STATES.md`). Even then, keep it simple and on-brand, not elaborate.

## What "On-Brand" Means Here

Per-organization branding (`05-DESIGN-SYSTEM.md`) is limited to logo + accent color. The overall visual language (iconography, status treatment, density) stays constant across every organization's instance of the console — an admin who manages multiple organizations should never have to relearn the UI's visual grammar when switching context, only see a different accent color and logo.

Continue to [07 — Component Specification](./07-COMPONENT-SPECIFICATION.md).

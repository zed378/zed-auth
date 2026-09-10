# 12 — Responsive Behavior

Per `08-PAGE-SPECIFICATIONS.md`'s scope note: the console is primarily a **desktop tool**, since admin work (structured data entry, reviewing tables of users/roles) is fundamentally desktop-oriented. Full breakpoint strategy below; full mobile-specific treatment for the one screen that needs it is in `16-MOBILE-UX.md`.

## Grid System (Concrete Breakpoints)

| Breakpoint | Width | Grid |
|---|---|---|
| Large desktop | ≥1440px | 12-column grid, full gutter |
| Desktop | 1024–1439px | 12-column grid, reduced gutter |
| Tablet | 768–1023px | 8-column grid |
| Mobile | <768px | 4-column grid — applies only to the screens in scope per `16-MOBILE-UX.md` |

Every page spec in `08-PAGE-SPECIFICATIONS.md`/`18-DETAILED-PAGE-SPECIFICATIONS.md` should state which columns of this grid its layout regions occupy at each breakpoint (e.g. "KPI cards: 3 columns each on the 12-column grid, full-width stacked on the 8-column grid"), not just "it's responsive."

## Breakpoint Strategy (Behavioral, Not Just Grid)

| Breakpoint | Target | Behavior |
|---|---|---|
| Desktop (primary target) | ≥1024px | Full layout: persistent left nav, multi-column detail panels, wide tables with all columns visible |
| Tablet | 768–1023px | Left nav collapses to an icon-only rail (expandable on demand); tables prioritize essential columns, secondary columns move behind a "show more" toggle or into an expandable row |
| Mobile (Personal account settings only) | <768px | See `16-MOBILE-UX.md` — the only screen group required to be fully optimized below tablet width |

## What "Usable Down to Tablet" Means for Admin Screens

Per `08-PAGE-SPECIFICATIONS.md`, every admin-facing screen must remain **usable** (not necessarily optimal) at tablet width:
- No horizontal scrolling required for primary actions.
- Tables degrade by hiding secondary columns before ever requiring horizontal scroll for primary ones.
- Multi-step flows (`04-USER-FLOWS.md`) that use a side panel on desktop switch to a full-screen takeover on tablet, since a narrow side panel would be too cramped for a two-step flow.

## What Is Explicitly Not Required

- Full mobile optimization (thumb-reachable primary actions, mobile-specific navigation patterns) for any admin-facing screen — per `08-PAGE-SPECIFICATIONS.md`, this is out of scope for the initial version. Admins are expected to primarily use desktop/laptop devices for structured management tasks (`00-DESIGN-DIRECTION.md`).
- A responsive redesign of the Rego policy editor (`08-PAGE-SPECIFICATIONS.md` ABAC tab) below tablet width — code/policy editing is not a task suited to small screens regardless of how well it's optimized, so this screen can reasonably require desktop width.

## Testing

Responsive behavior should be verified at minimum at four widths matching the grid system above: 1440px+ (large desktop), 1024–1439px (desktop), ~900px (tablet), and confirmed as "not supported, shows a message suggesting a larger screen" below ~600px for admin-facing screens — an explicit unsupported-width message is better than a silently broken layout.

Continue to [13 — Accessibility](./13-ACCESSIBILITY.md).

# 18 — Detailed Page Specifications

`08-PAGE-SPECIFICATIONS.md` gives the full screen inventory as a summary table. This document goes one level deeper for the highest-traffic, highest-stakes screens, in the concrete format every future page spec should follow: **primary objective → visual hierarchy → above-the-fold content → interaction rules → responsive grid mapping → keyboard behavior**. This is not "use a modern, clean, minimal design" — every statement below is checkable.

Apply this same format to every remaining screen in `08-PAGE-SPECIFICATIONS.md`'s inventory as it enters active development (see `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md` §8 "Frontend Changes").

---

## Page: Organization Overview (the console's "Dashboard")

**Primary objective**: an Org Admin (Budi, `01-USER-PERSONAS.md`) can tell the organization's operational status — pending invites, recent access changes, anything needing attention — in ≤5 seconds of landing on the page.

**Visual hierarchy**:
1. Anything needing immediate attention (e.g. an expiring Project Grant, a failed webhook — surfaced as a critical status banner if present, absent entirely if not)
2. Primary counts: active users, active projects, pending invites (KPI cards)
3. Recent activity trend (a simple sparkline/count of audit events over the last 7 days)
4. Secondary information: organization settings summary (MFA required? password policy summary)
5. Navigation into detail: links into Projects, Users, Audit Log

**Above-the-fold** (at the ≥1440px breakpoint, `12-RESPONSIVE-BEHAVIOR.md`):
- Header with organization name/switcher
- Critical status banner (only rendered if something needs attention — see `14-EMPTY-LOADING-ERROR-STATES.md` empty-state discipline: absence of a banner is itself information, not a missing feature)
- 3 KPI cards (active users, active projects, pending invites) — 3 columns each on the 12-column grid
- Primary activity trend visualization — full 12-column width below the KPI row

**Interaction**:
- KPI cards are clickable **only** where a real destination exists (active users → Users list, pending invites → Users list filtered to "invited" status). The activity trend is not clickable in the initial version (no drill-down destination defined yet) and must not carry hover/cursor affordance implying it is (`09-INTERACTION-DESIGN.md`).
- Loading uses skeleton cards matching the exact KPI card geometry (`09-INTERACTION-DESIGN.md`, `14-EMPTY-LOADING-ERROR-STATES.md`).
- A failed KPI fetch shows an inline retry within that specific card, not a full-page error — the rest of the dashboard remains usable.

**Responsive** (per `12-RESPONSIVE-BEHAVIOR.md`'s grid system):
- ≥1440px → 12-column grid, KPI cards at 3 columns each
- 1024–1439px → 12-column grid, reduced gutter, KPI cards remain 3 columns each
- 768–1023px → 8-column grid, KPI cards at 4 columns each (2 per row)
- <768px → not supported for this screen (admin-facing, per `12-RESPONSIVE-BEHAVIOR.md`'s scope decision) — shows the "screen too small" message

**Keyboard**:
- All KPI cards and the org switcher are reachable via Tab in visual order.
- Focus indicator uses `color-accent` per `05-DESIGN-SYSTEM.md`.
- No keyboard trap; this screen has no modal by default.

---

## Page: Users List

**Primary objective**: an Org Admin can find any user and take an action (invite, deactivate, view detail) in as few steps as possible — this is the single most frequently visited screen per `01-USER-PERSONAS.md`'s Budi persona.

**Visual hierarchy**:
1. Search/filter controls (always visible, never requiring a click to reveal)
2. Primary action ("Invite user")
3. The table itself (name, email, status badge, project/role count, last active)
4. Pagination

**Above-the-fold**:
- Search input + status filter (All / Active / Invited / Deactivated)
- "Invite user" primary button, top-right, consistently positioned across all list screens in the console (`07-COMPONENT-SPECIFICATION.md`)
- As many table rows as fit the viewport at compact row height (`06-VISUAL-LANGUAGE.md`)

**Interaction**:
- Clicking anywhere on a row (not just the name) navigates to that user's detail — the entire row is the click target, not just a text link, to reduce precision required for a very frequently repeated action.
- Row-level kebab menu reveals on hover **and** is always present (not hover-only) on touch/tablet widths, per `09-INTERACTION-DESIGN.md`'s hover rule.
- Status badge uses the color+icon+text pattern from `06-VISUAL-LANGUAGE.md` — never color alone.
- Bulk actions (e.g. bulk deactivate) are out of scope for the initial version — single-row actions only, to avoid the added confirmation-flow complexity (`09-INTERACTION-DESIGN.md`'s consequence-before-confirmation pattern) that bulk destructive actions would require.

**Responsive**:
- ≥1440px / 1024–1439px → full table: name, email, status, project/role count, last active, actions
- 768–1023px (8-column grid) → drop "last active" column behind an expandable row detail; remaining columns keep full width
- <768px → not supported (admin-facing screen)

**Keyboard**:
- Search input auto-focuses on page load via a keyboard shortcut (documented in a help/shortcuts panel, not silently).
- Table rows are focusable and activate their detail navigation on Enter.
- Kebab menu opens on Enter/Space when focused, closes on Escape, per standard disclosure-menu accessibility pattern (`13-ACCESSIBILITY.md`).

---

## Page: Project Grants Tab

**Primary objective**: a Project Owner (Sari) can see exactly who has delegated access to their project and confidently create or revoke a delegation, fully understanding the consequence before acting (`04-USER-FLOWS.md` Flow 2).

**Visual hierarchy**:
1. Primary action ("Create Grant")
2. Existing grants table (organization, granted roles, status, created date)
3. Empty state if no grants exist yet, with contextual explanation of what a Project Grant is (since this is one of the more advanced concepts per `09-INTERACTION-DESIGN.md`'s progressive disclosure principle)

**Above-the-fold**:
- "Create Grant" button
- Existing grants table (if any exist) or the contextual empty state (if none)

**Interaction**:
- Every row's granted-roles column uses the role-source visual pattern from `06-VISUAL-LANGUAGE.md`, listing exactly which roles (out of the project's full role set) are included — never just a count ("3 roles") without naming them, since knowing exactly which roles is the entire point of this screen.
- "Revoke" always follows the full consequence-before-confirmation pattern (`09-INTERACTION-DESIGN.md`), including the typed-confirmation escalation when the grant has active dependent user-grants.
- Creating a grant opens the modal flow specified exactly in `04-USER-FLOWS.md` Flow 2 — this page spec does not duplicate that flow's steps, only the entry point into it.

**Responsive**:
- ≥1024px → full table with all columns
- 768–1023px → granted-roles column wraps to multiple lines rather than truncating (truncating role names on this specific screen would undermine the screen's entire purpose)
- <768px → not supported (admin-facing screen)

**Keyboard**:
- "Create Grant" reachable and activatable via keyboard as the first tab-stop after any filter controls.
- The typed-confirmation input (for high-impact revocations) receives focus automatically when that confirmation dialog opens.

Continue to [19 — Frontend Implementation Chain](./19-FRONTEND-IMPLEMENTATION-CHAIN.md).

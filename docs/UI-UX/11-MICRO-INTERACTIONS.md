# 11 — Micro-interactions

Small, specific interaction details that make individual components feel considered — these sit below the level of `09-INTERACTION-DESIGN.md`'s broader patterns.

## Copy-to-Clipboard (Client ID, Client Secret)

- Per `08-PAGE-SPECIFICATIONS.md`'s Applications tab: a client secret is shown exactly once at creation. That one-time reveal screen must have a prominent copy button with clear success feedback (icon change + brief label change, e.g. "Copy" → "Copied"), since the admin cannot come back to retrieve it later — a failed silent copy here has real consequences (having to regenerate the secret and update every consumer app).

## Inline Validation Timing (Forms)

- Field-level validation (e.g. redirect URI format, `07-COMPONENT-SPECIFICATION.md`) fires on blur, not on every keystroke — validating while the admin is still mid-typing produces distracting, premature error states. Full rules in `15-FORM-UX.md`.

## Search-as-You-Type

- The global search and in-table search (`07-COMPONENT-SPECIFICATION.md`) debounce input briefly before querying, and show a lightweight loading indicator only if results take longer than a brief instant — for fast responses, no loading indicator should flash at all, since a flickering spinner on every keystroke is more distracting than helpful.

## Role Checkbox Selection (Project Grant Creation)

- Per `04-USER-FLOWS.md` Flow 2: when selecting roles to share in a Project Grant, checking/unchecking a role should immediately update the confirmation-step preview text in the background — not just at the final confirmation screen — so the admin sees the plain-language consequence build up in real time as they make selections, rather than only at the end.

## Table Row Hover

- Hovering a table row reveals the row-level action menu (kebab icon, `07-COMPONENT-SPECIFICATION.md`) which stays hidden otherwise — reduces visual noise across a dense table while keeping actions one hover away.

## Toggle Switches (Policy Settings)

- Toggles for settings like "Require MFA" (`08-PAGE-SPECIFICATIONS.md` Policies tab) apply immediately on click (no separate "Save" button for simple boolean settings), paired with a brief inline confirmation ("MFA requirement enabled") — since these are single, atomic, easily reversible settings, adding a separate save step would be pure friction. This deliberately differs from the multi-field forms in `15-FORM-UX.md`, which do use an explicit save action.

## Session List "Last Active" Freshness

- The "last active" timestamp on the Sessions tab (`08-PAGE-SPECIFICATIONS.md`) should be relative ("2 minutes ago") rather than absolute, and should visually distinguish the admin's **current** session from other active sessions (e.g. a "This device" label) — important so a user reviewing sessions in `04-USER-FLOWS.md` Flow 4 doesn't accidentally revoke the session they're currently using.

## Empty Checkbox States vs. Disabled States

- Consistent with `04-USER-FLOWS.md` Flow 3's rule: a role a receiving organization isn't allowed to assign is **never shown as a disabled checkbox** — it's simply absent from the list. A disabled checkbox implies "you could have this, but not right now," which is the wrong signal here.

Continue to [12 — Responsive Behavior](./12-RESPONSIVE-BEHAVIOR.md).

# 07 — Component Specification

Concrete specs for the reusable components implied by `05-DESIGN-SYSTEM.md` and `06-VISUAL-LANGUAGE.md`. Each entry lists variants, states, and the rule governing when to use it — implemented technically per `PLAN/06-FRONTEND-ARCHITECTURE.md`.

## Button

**Variants**: `primary` (accent, one per screen/section max — there should never be ambiguity about the single main action), `secondary` (neutral, for supporting actions), `destructive` (danger, for irreversible/access-revoking actions only).

**States**: default, hover, focus (visible ring, `13-ACCESSIBILITY.md`), disabled (with a reason communicated elsewhere, never a silently-disabled button — `15-FORM-UX.md`), loading (spinner replaces label, button stays same width to avoid layout shift).

**Rule**: a `destructive` button must never be the visually dominant action on a screen where a non-destructive action is the expected common path (e.g., on a user detail screen, "Deactivate user" is styled as `secondary` with danger-colored text, not a full `destructive` button, reserving the strongest visual weight for truly rare, high-consequence confirmations like the Project Grant revoke dialog in `04-USER-FLOWS.md` Flow 2).

## Table

**Anatomy**: header row (sortable columns where relevant), body rows (compact height per `06-VISUAL-LANGUAGE.md`), row-level action menu (kebab icon, not inline buttons, to avoid visual clutter at high row density), pagination footer.

**States**: default, loading (skeleton rows, not a blocking spinner over the whole table — `14-EMPTY-LOADING-ERROR-STATES.md`), empty (`14-EMPTY-LOADING-ERROR-STATES.md`), error (inline banner above the table, table area shows last-known-good data if available rather than going blank).

**Rule**: every table that displays roles or grants must support the role-source badge from `06-VISUAL-LANGUAGE.md` as a column or inline indicator — this is not optional per-table, since it's a cross-cutting requirement from `04-USER-FLOWS.md`.

## Form Field

**Anatomy**: label, input, helper text (optional), error text (replaces helper text when present, never shown simultaneously).

**States**: default, focus, error, disabled.

**Rule**: full behavior specified in `15-FORM-UX.md`; this entry only fixes the visual anatomy.

## Modal / Side Panel

**Two variants**: modal (center-screen, for focused single-purpose actions like "Create Grant") vs. side panel (slides from the right, for multi-step or detail-heavy flows like "Invite user," which per `04-USER-FLOWS.md` Flow 1 needs a two-step progression).

**Rule**: destructive confirmations (`04-USER-FLOWS.md`'s "consequence before confirmation" pattern) always use the modal variant, never the side panel, so they visually interrupt rather than blend into an ongoing flow.

## Badge / Tag

**Variants**: status badge (color + icon + text, per `06-VISUAL-LANGUAGE.md`'s status table), role-source badge (direct vs. delegated, per `06-VISUAL-LANGUAGE.md`).

**Rule**: badges are never the sole carrier of critical information — always paired with text, never icon/color alone (`13-ACCESSIBILITY.md`).

## Confirmation Dialog

**Anatomy**: title stating the action plainly ("Revoke Project Grant?"), a consequence summary in plain language (not a generic "are you sure?"), a `destructive`-styled confirm button labeled with the actual verb ("Revoke," not "OK" or "Confirm"), a `secondary` cancel button.

**Rule**: for the highest-impact actions identified in `04-USER-FLOWS.md` (Project Grant revocation with many dependents), require typing the resource name to enable the confirm button — this is the one deliberate friction point in the whole design system, reserved for cases where an accidental click would be very costly.

## Breadcrumb

Reflects the actual data hierarchy per `03-INFORMATION-ARCHITECTURE.md`; every segment is clickable.

## Search Input

Global variant (per `03-INFORMATION-ARCHITECTURE.md`) and scoped/in-table variant (filters the current table only) — visually similar but the global variant is always reachable via a persistent keyboard shortcut, since it's used dozens of times a day (`00-DESIGN-DIRECTION.md`).

Continue to [08 — Page Specifications](./08-PAGE-SPECIFICATIONS.md).

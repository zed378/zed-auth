# 15 — Form UX

Forms are one of the two dominant interaction types in this console alongside tables (`00-DESIGN-DIRECTION.md`), used for everything from creating an application to editing password policy. This document standardizes their behavior across every screen in `08-PAGE-SPECIFICATIONS.md`.

## Field Anatomy (Ties to `07-COMPONENT-SPECIFICATION.md`)

Label → input → helper text (optional, explains format/constraints before an error occurs, e.g. "Must be a valid HTTPS URL" under a redirect URI field) → error text (replaces helper text when present).

## Validation Timing

- **On blur**, not on every keystroke (`11-MICRO-INTERACTIONS.md`) — validating mid-typing produces distracting, premature error states for fields the admin hasn't finished entering.
- **On submit**, re-validate everything regardless of blur history, to catch fields the admin never interacted with (e.g. browser autofill).
- **Async validation** (e.g. checking an email isn't already in use for an invite) shows a brief inline loading indicator next to the field, not a full-form block.

## Error Presentation

- Field-level errors appear directly under their field, replacing helper text (`14-EMPTY-LOADING-ERROR-STATES.md`).
- On submit, if any field has an error, focus moves to the **first** invalid field automatically, and a summary is announced to screen readers (`13-ACCESSIBILITY.md`) — don't leave the admin to hunt through a long form for what's wrong.
- Server-side validation errors returned per `PLAN/05-API-CONTRACT.md`'s error format are mapped back to their specific field wherever the API provides a `field` in `details[]`.

## Multi-Step Forms

Per `04-USER-FLOWS.md` Flow 1/2/5 (Invite User, Create Project Grant, Author ABAC Policy):
- A visible step indicator (e.g. "Step 1 of 2") at all times.
- Data entered in an earlier step is retained if the admin navigates back to review/edit it — never reset on back-navigation.
- The final step always shows a full summary of everything entered across prior steps before the final confirm action, consistent with `09-INTERACTION-DESIGN.md`'s consequence-before-confirmation pattern where the form leads into an access-changing action.

## Save Behavior

Two distinct patterns, used deliberately in different contexts:

| Pattern | When used | Example |
|---|---|---|
| Explicit "Save" button | Multi-field forms where partial/accidental submission would be disruptive | Editing a role's display name and permission keys |
| Immediate apply on change | Single, atomic, easily reversible settings | Toggling "Require MFA" (`11-MICRO-INTERACTIONS.md`) |

**Rule**: never mix the two patterns within a single form — a form is either "type things then click Save" or "each control applies itself," never a combination that leaves the admin unsure whether their most recent change was actually saved.

## Destructive Fields (e.g. Deleting a Redirect URI from a List)

Removing an item from a multi-value field (e.g. one of several redirect URIs) is a lightweight, immediately-reversible-within-the-form action (an "undo" link appears briefly) — distinct from the heavier "Consequence Before Confirmation" pattern (`09-INTERACTION-DESIGN.md`) reserved for actions that take effect immediately against the live system, since removing a URI from a draft form has no effect until the whole form is saved.

## Placeholder Text

Placeholder text is never used as a substitute for a label (labels are always visible, per `13-ACCESSIBILITY.md`) — placeholders are reserved only for format examples (e.g. `https://app.example.com/callback` inside a redirect URI field that already has a visible "Redirect URI" label above it).

Continue to [16 — Mobile UX](./16-MOBILE-UX.md).

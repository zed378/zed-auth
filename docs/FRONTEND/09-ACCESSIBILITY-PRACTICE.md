# 09 - Accessibility Practice

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: PF-04, PF-51, P0-17, P3-11, P3-12, P4-05, P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

State what the console does about accessibility, what is automated, and — the honest part
— what `docs/UI-UX/13-ACCESSIBILITY.md` asks for that has not been done.

The status above is `Partially implemented` for one reason: the manual screen-reader pass
is owed and has not happened.

## Scope

`console/`. The hosted login pages are the Go service's (`backend/internal/login/`) and
have their own treatment.

## As Built

### The target is WCAG 2.1 AA, and the rule set is restricted to it

`console/src/test/axe.ts` runs axe-core with `runOnly: wcag2a, wcag2aa, wcag21a, wcag21aa`.
Restricting the rule set keeps the suite honest: a best-practice warning failing the build
teaches people to disable the check.

The helper is hand-written rather than a matcher library. `vitest-axe` augments the `Vi`
global namespace, which Vitest 5 no longer uses, so its matcher works at runtime and fails
to typecheck. The other reason is the failure message: "expected no violations" tells
whoever broke it nothing, so the helper prints the rule, its impact, the offending
selector, and the URL of the page explaining the fix.

**13 test files run axe** over rendered screens.

### Lint catches the mechanical failures before the tests do

`eslint-plugin-jsx-a11y`'s recommended rules are configured as **errors**, not warnings
(`console/eslint.config.js`). A warning in a lint run of a hundred files is a line people
scroll past.

### The shell

- A **skip link** to `#main-content`, which is `tabIndex={-1}` so it is a programmatic
  focus target without entering the tab order. Without that, focus continues from the
  navigation and the skip link does nothing — the bug that makes skip links look
  implemented and not be.
- The navigation landmark is **labelled** (`aria-label="Console"`), because a document with
  two navigation landmarks gives a screen reader a list that says "navigation" twice.
- The narrow-width message **replaces** the application rather than covering it. Covering
  it leaves a screen-reader user free to walk into an interface the message just said was
  unsupported.

### Dialogs

`Modal` and `SidePanel` behave identically: focus moves into the panel on open, is trapped
while it is open, and returns to the element that opened it on close. `role="dialog"`,
`aria-modal="true"`, labelled by the title.

One subtlety already paid for: the focus effect holds the latest `onClose` in a ref. Taken
as a dependency, the effect re-ran on **every keystroke** and its cleanup called `focus()`,
so typing in a field moved focus back to the dialog after the first character.

### Colour is never the only carrier

- A `Badge`'s text is the message; colour reinforces.
- Status is spelled out: "Active", "Revoked", "Ended".
- A delegated role's source organization is a `title` **and** `sr-only` text, because hover
  is an affordance for a mouse and nothing else.
- Contrast ratios are computed from the stylesheet, not asserted in a comment —
  `--color-border` is deliberately heavier than a hairline so that input borders meet 1.4.11.

### Motion respects the user's preference

One `@media (prefers-reduced-motion: reduce)` block in `console/src/styles/tokens.css`
collapses every animation, transition and smooth scroll. It is global, so no component has
to remember.

### Forms

`aria-invalid` and `aria-describedby`, with the error **replacing** the help text so one
statement is announced rather than two. Role checklists are real `fieldset`/`legend`.

### Two content decisions that are accessibility decisions

- **The TOTP QR code is never the accessible form of the secret.** `QrCode` is
  `role="img"` with a label; the typed secret is always shown beside it, because a QR code
  alone is an enrolment only sighted users can finish.
- **Personal account settings renders in its own shell**, outside the narrow-screen guard.
  It is the one screen built for a phone (`P3-12`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Target | WCAG 2.1 AA | `console/src/test/axe.ts` |
| `jsx-a11y` recommended rules | error | `console/eslint.config.js` |
| Focus ring | `:focus-visible`, defined once, never suppressed for aesthetics | `console/src/styles/tokens.css` |
| Reduced motion | global override | `console/src/styles/tokens.css` |
| Dialog focus | taken, trapped, restored | `Modal`, `SidePanel` |
| Colour as sole carrier | never | `Badge`, `RoleSourceBadge`, status columns |

## Verification

- `console/src/test/axe.ts` used by 13 test files.
- `console/src/app/shell/AppShell.test.tsx` — skip link focus and target, landmark
  labelling, the narrow-width replacement.
- `console/src/styles/tokens.test.ts` — computed contrast.
- `console/e2e/sessions.spec.ts` — a revoke control is a usable tap target at the narrowest
  supported width.

## Not Yet Built / Open Questions

- **The manual screen-reader pass is owed** (`docs/UI-UX/13` § Testing & Sign-off, `PF-51`,
  `P5-11`). axe catches mechanical failures; it cannot judge whether an announcement makes
  sense, whether a reading order is sane, or whether a live region interrupts at the wrong
  moment. Nothing in this document should be read as a claim that the console has been
  tested with a screen reader, because it has not.
- **No automated keyboard-path test** beyond dialog focus. Tab order through a table with
  row actions is unverified.
- **No `aria-live` discipline.** A few screens use it; there is no rule about which
  announcements are polite, which are assertive, and which should not announce at all.
- **Below 768px is unsupported** for the console proper, by design. The message says so;
  it is still a limitation.

## Related Documents

- [`02-DESIGN-TOKENS-AND-STYLING.md`](./02-DESIGN-TOKENS-AND-STYLING.md)
- [`10-FRONTEND-TESTING.md`](./10-FRONTEND-TESTING.md)
- [`../UI-UX/13-ACCESSIBILITY.md`](../UI-UX/13-ACCESSIBILITY.md)

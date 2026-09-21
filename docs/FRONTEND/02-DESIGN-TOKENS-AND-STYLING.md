# 02 - Design Tokens and Styling

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, PF-01, PF-02 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Describe the token layer: what exists, how a token becomes a Tailwind utility, what an
organization may override, and the lint rules that stop a raw value from entering the
codebase. `docs/UI-UX/05-DESIGN-SYSTEM.md` names the tokens and their intent; this
document is the implementation and the enforcement.

## Scope

`console/src/styles/tokens.css`, `console/src/branding/`,
`console/eslint-local-rules.js`. The public site derives its palette from the same brand
source but keeps its own stylesheet — [`../WEBSITE/`](../WEBSITE/).

## As Built

### One stylesheet, `@theme static`

`console/src/styles/tokens.css` is the only file in the application that contains a colour
value, a pixel size or a shadow. Tailwind v4's `@theme` maps each entry to **both** a CSS
custom property and a utility class, so `bg-bg-surface` and `var(--color-bg-surface)` are
the same token rather than two things to keep in step.

`static` rather than a bare `@theme`: by default Tailwind emits only the variables some
utility actually references, which is right for a stylesheet and wrong for a design
system. A token `docs/UI-UX/05` names should exist whether or not a screen happens to use
it yet, because hand-written CSS and future components read it by name.

### What the token set contains

| Group | Count | Note |
|---|---|---|
| Colour | 9 | Exactly the nine `docs/UI-UX/05` names. A tenth is a design-system change, not a stylesheet edit |
| Typography | 5 sizes, 3 weights, 2 line heights, 2 families | A constrained scale; an open set is how two screens end up 1px apart |
| Spacing | 8 steps on a 4px/8px rhythm | Plus two container widths: a form max-width and a fixed nav track |
| Elevation | 3 | Flat, raised, overlay — three meanings, not a scale |
| Breakpoints | 3 | Named `tablet`, `desktop`, `wide`, so a component says what it means |
| Motion | 2 durations, 1 easing | Set as Tailwind's defaults, so a bare `transition-colors` already carries them |

### Two Tailwind namespace traps, both already paid for

Tailwind v4 reads type sizes from `--text-*` and easing from `--ease-*`. Writing only the
`docs/UI-UX/05` names (`--font-size-body`) produces a stylesheet that looks complete and a
`text-body` class that does not exist — every piece of text silently renders at the
browser default. **This project shipped that for one build.** The fix in place: the values
live under the `docs/UI-UX/05` names and the Tailwind namespace aliases reference them, so
there is one place to change a size and no pair to keep in step.

There is no `--duration-*` namespace at all, so `duration-quick` generates nothing. The
durations are installed as `--default-transition-duration` instead.

### Contrast is computed, not asserted in a comment

`console/src/styles/tokens.test.ts` parses the stylesheet and computes the ratios. Two
consequences worth knowing before changing a colour:

- `--color-border` is heavier than an admin table's usual hairline (3.1:1 on base). WCAG
  2.1 AA 1.4.11 requires 3:1 for the visual information identifying a UI component, and an
  input's border is exactly that. A prettier, lighter value makes every text input fail.
  `docs/UI-UX/05` gives one token for both dividers and control borders; splitting it is a
  design-system change and is raised as a plan gap rather than made here.
- `--color-text-secondary` is a hierarchy statement, never a licence to drop below the
  threshold.

### Branding: one overridable token, expressed as a type

An organization may override **the accent colour and the logo**, and nothing else
(`console/src/branding/branding.ts`). The overridable set is a union of string literals,
so `applyBranding` cannot be called with `--color-danger` even by a caller holding one in
a variable — adding a third token is a type change, which is a code review, rather than a
value that happens to arrive in an API response.

`--color-danger` and `--color-warning` are reserved. An administrator who manages several
organizations should never have to relearn what red means, and a tenant who set danger to
their brand green would remove the signal exactly where someone is about to delete an
organization. The runtime guard exists alongside the type because branding arrives as
untyped JSON.

### The lint rules

`console/eslint-local-rules.js` makes three things build failures rather than review
comments:

| Rule | Rejects | Why |
|---|---|---|
| `local/no-raw-color` | `#abc`, `rgb(…)`, `oklch(…)` in application code | A rebrand must not mean hunting through page specs |
| `local/no-arbitrary-value` | `p-[13px]`, `text-[#abc]` | Tailwind's escape hatch lets a component leave the 4px/8px scale without anything noticing |
| `local/no-inline-style` | `style={{…}}` | The same hole in a different shape |

Exempt: `tokens.css` (where the values live, by definition), `tokens.test.ts` (which reads
them), and `branding.ts`/`branding.test.ts` (which handle tenant values).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Raw colour values in application code | rejected | `console/eslint-local-rules.js` |
| Arbitrary Tailwind values | rejected | `console/eslint-local-rules.js` |
| Inline `style` | rejected | `console/eslint-local-rules.js` |
| Overridable tokens | `--color-accent` and the logo | `console/src/branding/branding.ts` |
| Focus ring | `:focus-visible`, defined once, never suppressed | `console/src/styles/tokens.css` |
| Reduced motion | every transition wrapped | `console/src/styles/tokens.css` |

## Verification

- `console/src/styles/tokens.test.ts` — every `docs/UI-UX/05` token exists; contrast ratios
  computed from the stylesheet.
- `console/src/styles/utilities.test.ts` — the utility classes the tokens are supposed to
  generate actually exist, which is the test that would have caught the `--text-*` trap.
- `console/src/branding/branding.test.ts` — a reserved token is refused at runtime.
- `scripts/check.sh` § Public site — the public site's brand tokens match the console's.

## Not Yet Built / Open Questions

- **No dark theme.** The token layer would support one; no design exists for it.
- **`--color-border` carries two jobs** (dividers and control borders) at the accessible
  weight. Splitting it is a plan gap, not a code change.

## Related Documents

- [`03-COMPONENT-LIBRARY.md`](./03-COMPONENT-LIBRARY.md)
- [`09-ACCESSIBILITY-PRACTICE.md`](./09-ACCESSIBILITY-PRACTICE.md)
- [`../UI-UX/05-DESIGN-SYSTEM.md`](../UI-UX/05-DESIGN-SYSTEM.md)
- [`../UI-UX/06-VISUAL-LANGUAGE.md`](../UI-UX/06-VISUAL-LANGUAGE.md)

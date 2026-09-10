# 13 — Accessibility

Target: **WCAG 2.1 AA**, built in from the first screen rather than retrofitted — per `00-DESIGN-DIRECTION.md` and reinforced as an explicit sign-off gate in `PLAN/17-ACCEPTANCE-CRITERIA.md`. Retrofitting accessibility later is far more expensive than building it in from the start, and this console is a daily tool for its users, making accessibility failures a recurring, not one-time, cost.

## Color & Contrast

- All `color-text-*` tokens (`05-DESIGN-SYSTEM.md`) meet AA contrast ratios against both `color-bg-base` and `color-bg-surface`.
- Status is never communicated by color alone — every status badge pairs color with an icon and text label (`06-VISUAL-LANGUAGE.md`), satisfying users with color vision deficiency.
- Per-organization branding overrides (`05-DESIGN-SYSTEM.md`) must be contrast-checked before being applied — the console should validate a custom accent color against contrast requirements at the point an org admin sets it, rather than allowing an inaccessible combination to ship silently.

## Keyboard Navigation

- Every interactive element (buttons, table row actions, form fields, tabs) is reachable and operable via keyboard alone, per `09-INTERACTION-DESIGN.md`'s keyboard/focus rules.
- Focus order follows visual/logical order; no keyboard traps outside of intentional modal focus-traps (which must always have a clear, keyboard-accessible close action, e.g. Escape).
- Visible focus indicators (a focus ring using `color-accent`, per `05-DESIGN-SYSTEM.md`) on every focusable element — never suppressed for aesthetic reasons.

## Screen Reader Support

- All icons that carry meaning (`06-VISUAL-LANGUAGE.md`) have appropriate accessible labels — an icon-only kebab action menu (`07-COMPONENT-SPECIFICATION.md`) must announce as "Actions for [row name]," not just "button."
- Form errors (`15-FORM-UX.md`) are announced to screen readers at the moment they appear, not only shown visually.
- Live regions used for asynchronous feedback that isn't tied to a specific focused element (e.g. a toast confirmation, `10-MOTION-DESIGN.md`), so screen reader users aren't left unaware an action succeeded.

## Motion & Animation

- Every animation in `10-MOTION-DESIGN.md` has an instant, no-motion fallback honoring `prefers-reduced-motion` — this is a hard requirement, not a nice-to-have.

## Target Sizes

- Interactive elements (buttons, checkboxes in role-selection lists, kebab menus) meet minimum touch/click target sizes even in the compact-density table rows described in `06-VISUAL-LANGUAGE.md` — density and accessibility are not in conflict if target sizing is planned for from the component level (`07-COMPONENT-SPECIFICATION.md`), not added after the fact.

## Testing & Sign-off

- Automated accessibility linting (e.g. axe-core) in CI, catching regressions on every change.
- Manual screen reader testing (at minimum one pass with a common screen reader) before the Phase 5 hardening milestone (`PLAN/16-IMPLEMENTATION-ROADMAP.md`).
- Full sign-off checklist in `17-UX-ACCEPTANCE-CRITERIA.md`, feeding into `PLAN/17-ACCEPTANCE-CRITERIA.md`'s Phase 5 gate.

Continue to [14 — Empty, Loading & Error States](./14-EMPTY-LOADING-ERROR-STATES.md).

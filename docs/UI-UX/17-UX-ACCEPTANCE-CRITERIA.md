# 17 — UX Acceptance Criteria

Sign-off checklist for the console's design/UX quality, feeding directly into `PLAN/17-ACCEPTANCE-CRITERIA.md`'s phase gates. A screen is not "UX complete" merely because it renders and functions — it must meet the checklist below.

## Per-Screen Checklist (Apply to Every Entry in `08-PAGE-SPECIFICATIONS.md`)

- [ ] Empty, loading, and error states are all implemented per `14-EMPTY-LOADING-ERROR-STATES.md` — not just the happy path with data.
- [ ] All interactive elements meet the keyboard and screen-reader requirements in `13-ACCESSIBILITY.md`.
- [ ] Any access-changing action follows the "Consequence Before Confirmation" pattern from `09-INTERACTION-DESIGN.md` where applicable.
- [ ] Any role/grant displayed uses the role-source badge from `06-VISUAL-LANGUAGE.md`.
- [ ] Remains usable (per `12-RESPONSIVE-BEHAVIOR.md`'s definition) down to tablet width.

## Flow-Level Checklist (Apply to Every Flow in `04-USER-FLOWS.md`)

- [ ] **Flow 1 (Invite + first role)**: entering info and assigning a role is one continuous flow, not two separate screens the admin must remember to visit.
- [ ] **Flow 2 (Project Grant create/revoke)**: both creation and revocation show a plain-language consequence summary before the final confirm; revocation of a grant with dependents requires typed confirmation.
- [ ] **Flow 3 (Vendor self-management)**: a receiving organization never sees a role it isn't allowed to assign, not even in a disabled state.
- [ ] **Flow 4 (Session self-revoke)**: revocation is single-tap/click, immediately effective, and the user's own current session is visually distinguished from others.
- [ ] **Flow 5 (ABAC policy authoring)**: activation is disabled until at least one dry-run has been performed; rollback to a prior version is a single action.

## Design System Consistency Checklist

- [ ] No screen introduces a color, spacing value, or component variant not defined in `05-DESIGN-SYSTEM.md` / `07-COMPONENT-SPECIFICATION.md`.
- [ ] `color-danger` is used exclusively for destructive/irreversible meaning, nowhere else, across the entire console (`06-VISUAL-LANGUAGE.md`).
- [ ] Status is always communicated with both color and text/icon, never color alone.

## Accessibility Sign-off (Feeds `PLAN/17-ACCEPTANCE-CRITERIA.md` Phase 5 Gate)

- [ ] Automated accessibility linting (e.g. axe-core) passes in CI with no unaddressed critical findings.
- [ ] A manual screen-reader pass has been completed and any findings resolved or explicitly risk-accepted (logged in `PLAN/18-RISK-REGISTER.md` if deferred).
- [ ] Contrast ratios verified for the default theme and at least one custom per-organization accent color.

## Mobile Sign-off (Personal Account Settings Only, per `16-MOBILE-UX.md`)

- [ ] Manually tested on at least one real/emulated iOS and Android device.
- [ ] TOTP enrollment has a working manual-entry fallback alongside the QR code.
- [ ] Session revocation and MFA management both work single-handed on a typical phone screen size.

## Overall UX Definition of Done

The console's UX for a given phase (`PLAN/16-IMPLEMENTATION-ROADMAP.md`) is not complete until every checklist item above is satisfied for every screen/flow introduced in that phase — this document is the UX-side counterpart to `PLAN/17-ACCEPTANCE-CRITERIA.md`, and both should be checked together before a phase is marked done.

---

This concludes the `UI-UX/` folder.

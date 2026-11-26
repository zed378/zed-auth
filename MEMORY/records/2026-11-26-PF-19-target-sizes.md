# PF-19 — Checkbox and radio target sizes

Date: 2026-11-26. Status: implementation slice; PF-19 remains WIP.

The user asked to continue implementation in parallel. The console's shared
checkbox and radio primitives now use a 24px square hit area, while keeping
their existing focus treatment and labels. Component regressions assert the
size utility, the Tailwind compilation test confirms it produces CSS, and the
Chromium workbench suite measures both controls at tablet width.

Validation: `npm exec vitest -- run src/components/FormField.test.tsx
src/styles/utilities.test.ts` from `console` — 42 tests passed.

Manual assistive-technology acceptance remains open; this slice does not claim
the full PF-19 definition of done.

# Console accessibility verification

Implements the repeatable procedure in PF-19 and `docs/UI-UX/13`.
Automated checks support the WCAG 2.1 AA gate; manual results must be recorded
before acceptance.

Run `npm run check`, `npm run build`, and `npm run test:workbench` from `console`.
CI runs the component axe checks, accessibility lint, contrast checks, and
Chromium workbench checks. The production E2E job remains separate.

## Manual pass

Use NVDA with Chrome on Windows. Record the OS, NVDA and browser versions,
commit or working-tree identity, date, tested routes and outcomes. Use a test
organization and non-sensitive fixture data.

1. At 1440px and about 900px, navigate using Tab, Shift+Tab and Enter. The skip
   link must move focus into the main region. Each page must have a clear title
   and identifiable navigation/main landmarks. Focus must remain visible.
2. Open a modal, a side panel and a confirmation. Check their accessible names,
   focus containment, Escape ownership and return focus. A typed confirmation
   must announce its label and reject a name with different case. Check explicit
   acknowledgement after completion.
3. Submit an untouched form. Hear an error summary and verify focus reaches the
   first invalid field. Check labels, helper/error descriptions and browser
   autofill. Simulate a failed save and verify entered values remain. Retry only
   the failed action.
4. Exercise loading, genuine empty, filtered empty, failed refresh with stale
   rows, and permission denial. Loading must not announce empty. Filtered empty
   offers filter recovery. Permission denial must not expose cached contents.
5. Use search, tabs, step navigation, copy, pagination and toast feedback. Verify
   keyboard operation, selected/current state and announcements without moving
   focus unnecessarily. Pause toast dismissal by focusing it.
6. Repeat at 200% zoom and with reduced motion enabled. Inspect default and
   accepted organization accent themes. At widths below the admin boundary,
   verify the larger-screen message; personal account screens remain usable.
7. Measure interactive targets in the browser, including compact table actions.
   Record any target-size, overflow, contrast or focus failure with a reproduction
   and the owning component. Do not mark an unmeasured item passed.

## Evidence template

| Route / component | Theme / width / zoom | Keyboard | NVDA announcement | Result / issue |
|---|---|---|---|---|
| Record the tested surface | Record actual settings | Record actual behavior | Record actual behavior | Pass or linked issue |

Existing infrastructure: shell skip links and landmarks; `useDialog` focus and
scroll management; live regions in shared states, forms and feedback; branding
contrast rejection before applying an accent. Target-size and assistive-technology
acceptance remain open until measured. This document records a procedure, not a
completed manual audit.

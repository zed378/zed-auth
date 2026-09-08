# Phase F — Frontend Implementation (Console, Hosted Auth, Public Site)

**Goal**: every page and every component of the three frontend surfaces, specified in one place — the design system components from `UI-UX/07-COMPONENT-SPECIFICATION.md`, all 21 console screens from `UI-UX/08-PAGE-SPECIFICATIONS.md`, the hosted authentication screens, and the public site from `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md`.

---

## Read This First: How Phase F Relates to Phases 0–5

`PLAN/16-IMPLEMENTATION-ROADMAP.md` says plainly:

> "The management console is built **in lockstep with these phases, not as a separate track**."

Phase F does **not** change that, and it must not be read as permission to build console screens ahead of the API they call. A screen whose endpoint does not exist yet cannot be built against anything real, and building it early produces a mock-driven UI that diverges from the API it eventually meets.

What Phase F is: **the frontend work broken down properly, in one document, so the whole surface is visible at once.** Two kinds of task live here, and they behave differently:

| Kind | Gating | Why |
|---|---|---|
| **Foundation** (Tracks A, B, C, G) — design system, app shell, cross-cutting behavior, test infrastructure | Genuinely phase-independent. Buildable as soon as `P0-17` lands, and *should* be, because every page task depends on it | A component library built once is what makes the per-page work fast and consistent; building it per-screen is how `UI-UX/05`'s governance rule gets violated |
| **Pages** (Tracks D, E, F) — every console screen, auth screen, and public page | Each carries a **Gate** row naming the backend task that must be `DONE` first. The gate is binding | This is the lockstep rule, made explicit per screen instead of per phase |

So: work the foundation tracks early and continuously; work each page task when its gate opens. `PROGRESS.md` shows both views.

**Relationship to the console tasks already in Phases 1–5.** Those tasks are not duplicates and are not superseded — they remain the **phase gates**, verifying the screens against that phase's acceptance criteria (`PLAN/17`). Phase F holds the implementation detail. The mapping:

| Phase task | Phase F tasks holding the detail |
|---|---|
| `P0-17` Console skeleton with design tokens | `PF-01`, `PF-02` (prerequisite for everything here) |
| `P1-21` Console — OIDC login (dogfooding) | `PF-12`, `PF-20` |
| `P1-22` Console — Org overview, Projects, Applications | `PF-21`, `PF-22`, `PF-23` |
| `P1-23` Console — Users list and detail | `PF-24`, `PF-25` |
| `P1-24` Console — Audit Log screen | `PF-26` |
| `P2-11` Console — Roles tab | `PF-30` |
| `P2-12` Console — Authorizations tab | `PF-31` |
| `P2-13` Console — organization switcher | `PF-29` |
| `P2-14` Console — Policies (Access) screen | `PF-32` |
| `P3-10` Console — MFA tab | `PF-34` |
| `P3-11` Console — Sessions tab | `PF-33` |
| `P3-12` Console — personal account settings | `PF-35` |
| `P4-05` Console — Project Grants tab | `PF-36` |
| `P4-06` Console — Granted Projects list | `PF-37` |
| `P4B-07` Console — Policies (ABAC) tab | `PF-38` |
| `P5-10`/`P5-11`/`P5-12` Console polish, a11y audit, perf pass | `PF-50`, `PF-51`, `PF-52` |
| `P1-12` Hosted login page | `PF-39`, `PF-40`, `PF-41`, `PF-42` |
| `P0-18`/`P0-19`/`P1-25` Public site | `PF-43` through `PF-47` |

A page task is `DONE` when both its own DoD and its phase gate task's criteria are satisfied.

---

## The Implementation Chain Is Mandatory Here

`UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` defines twelve steps every screen and component must be run through before it is implementable:

```
Design → Component → State → Interaction → API Dependency
   → Loading → Error → Empty → Permission → Responsive → Accessibility → Test
```

**Every task in Tracks A, D, and E must commit a completed chain table alongside its code.** "Not applicable" is a valid answer for a given row; a blank row is not. This is not restated on every task card below — it is inherited by all of them, exactly like the global Definition of Done in `00-TASK-CONVENTIONS.md`.

The chain exists because, without it, Loading/Error/Empty/Permission/Accessibility get invented ad hoc by whoever implements each screen — which is the specific inconsistency `UI-UX/05` and `UI-UX/14` exist to prevent.

---

## Console-Wide Rules Every Task Inherits

Pulled from across `UI-UX/` so they are enforceable rather than remembered:

1. **Tokens only.** No raw hex, no arbitrary spacing. Any new token or component is added to the design system *before* a page uses it (`UI-UX/05` § Governance).
2. **`color-danger` is reserved for destructive and irreversible actions.** Never for errors that are merely unwanted, never decoratively (`UI-UX/06`, `CLAUDE.md`).
3. **Consequence before confirmation.** Access-changing actions compute and show the real consequence in plain language, and the confirm button carries the actual verb — never "OK" (`UI-UX/09`).
4. **Confirm-before, not undo-after**, for anything touching access control. A brief window of wrong access may already have been exploited (`UI-UX/09` § Undo vs. Confirm).
5. **Permission-gated routes are genuinely unreachable, not hidden.** The frontend checks the same claims the API enforces, and the API enforces independently regardless (`UI-UX/08` § Cross-Screen Requirements, `CLAUDE.md`).
6. **Every table showing roles or grants carries the role-source badge.** Not optional per screen (`UI-UX/07`, `UI-UX/08`).
7. **Never color or icon alone.** Status always pairs color with icon *and* text (`UI-UX/13`).
8. **Genuinely empty ≠ filtered to empty.** Different copy, different recovery action (`UI-UX/14`).
9. **Skeletons, not blocking spinners**, and skeletons preserve final geometry so content never jumps (`UI-UX/09`, `UI-UX/14`).
10. **Hover is never the only interactivity cue** — tablet has no hover (`UI-UX/09`).
11. **Every animation honors `prefers-reduced-motion`** with an instant fallback. Hard requirement (`UI-UX/13`).
12. **Nothing in the UI claims a capability that isn't shipped in the current phase** (`UI-UX/21`, `CLAUDE.md`).

---

## Task Summary

### Track A — Design System and Component Library

| ID | Task | Size | Depends on |
|---|---|---|---|
| PF-01 | Motion tokens and the reduced-motion contract | S | P0-17 |
| PF-02 | Component library scaffolding and workbench | M | P0-17 |
| PF-03 | Button — three variants, five states | S | PF-02 |
| PF-04 | Table — anatomy, density, and all four states | L | PF-02 |
| PF-05 | Form field primitives | L | PF-02 |
| PF-06 | Modal and Side Panel | M | PF-02, PF-01 |
| PF-07 | Badge and Tag, including the role-source badge | S | PF-02 |
| PF-08 | Confirmation Dialog, including the typed-confirmation variant | M | PF-06 |
| PF-09 | Breadcrumb | S | PF-02 |
| PF-10 | Search Input — global and scoped variants | M | PF-02 |
| PF-11 | Supporting components: tabs, step indicator, toast, copy-to-clipboard, KPI card, pagination | L | PF-02 |

### Track B — Application Shell

| ID | Task | Size | Depends on |
|---|---|---|---|
| PF-12 | Routing and permission-gated navigation | L | PF-02, P1-21 |
| PF-13 | Navigation chrome and organization context indicator | M | PF-12, PF-09 |
| PF-14 | Global search | M | PF-10, PF-12 |
| PF-15 | Progressive disclosure rules | S | PF-13 |

### Track C — Cross-Cutting Behavior

| ID | Task | Size | Depends on |
|---|---|---|---|
| PF-16 | The four-state system: loading, empty, error, permission-denied | M | PF-04 |
| PF-17 | Form system: validation timing, error mapping, multi-step | L | PF-05 |
| PF-18 | Responsive grid and the unsupported-width boundary | M | PF-13 |
| PF-19 | Accessibility infrastructure | M | PF-02 |
| PF-20 | Data layer conventions | M | P0-16, P1-21 |

### Track D — Console Screens (each gated on its backend task)

| ID | Screen | Size | Gate |
|---|---|---|---|
| PF-21 | Organization Overview | L | P1-16 |
| PF-22 | Project list | M | P1-17 |
| PF-23 | Project detail — Applications tab | L | P1-18 |
| PF-24 | User list | L | P1-19 |
| PF-25 | User detail shell + Profile tab | M | P1-19 |
| PF-26 | Audit Log | M | P1-20 |
| PF-27 | Organization Settings | M | P1-16 |
| PF-28 | Instance screens: org list, instance policies, instance audit log | L | P1-16, P1-20 |
| PF-29 | Organization switcher | M | P2-08 |
| PF-30 | Project detail — Roles tab | M | P2-02 |
| PF-31 | Authorizations tab and User detail — Grants tab | L | P2-03 |
| PF-32 | Policies — Access tab | M | P2-10 |
| PF-33 | User detail — Sessions tab | M | P3-09 |
| PF-34 | User detail — MFA tab | M | P3-02, P3-05 |
| PF-35 | Personal account settings (mobile-optimized) | L | P3-09, P3-02 |
| PF-36 | Project detail — Project Grants tab | L | P4-01 |
| PF-37 | Granted Projects list | L | P4-02 |
| PF-38 | Policies — ABAC tab with Rego editor | L | P4B-04, P4B-05 |

### Track E — Hosted Authentication Screens

| ID | Screen | Size | Gate |
|---|---|---|---|
| PF-39 | Login page | M | P1-12 |
| PF-40 | MFA challenge and enrollment screens | M | P3-03 |
| PF-41 | Password reset and invitation acceptance | M | P1-19.4, P1-19.5 |
| PF-42 | Logout, consent, and protocol error screens | M | P1-10 |

### Track F — Public Site

| ID | Task | Size | Gate |
|---|---|---|---|
| PF-43 | Public site layout and shared visual language | M | P0-18 |
| PF-44 | Landing page | L | P0-18 |
| PF-45 | About, Contact, Changelog | M | P0-18 |
| PF-46 | Docs shell and generated API reference rendering | L | P0-16, P0-18 |
| PF-47 | Security and trust page | M | P5-03 |

### Track G — Frontend Quality

| ID | Task | Size | Depends on |
|---|---|---|---|
| PF-48 | Component test suite | L | Track A |
| PF-49 | E2E suite for console flows | L | Track D |
| PF-50 | Accessibility: CI automation and manual audit | L | Tracks D, E |
| PF-51 | Visual regression testing | M | Track A |
| PF-52 | Frontend performance budget | M | Tracks D, F |
| PF-53 | Frontend acceptance validation | M | all above |

---

# Track A — Design System and Component Library

## PF-01 — Motion Tokens and the Reduced-Motion Contract

| | |
|---|---|
| **Status** | TODO · **Depends on** P0-17 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/10-MOTION-DESIGN.md`, `UI-UX/13-ACCESSIBILITY.md` § Motion & Animation |

**Goal** — Durations and easing curves as tokens, not hard-coded per component. `UI-UX/10` § Ownership asks for exactly this "once implementation begins" — this is that moment.

**Steps**
1. Define duration and easing tokens alongside the color and spacing tokens from `P0-17`.
2. Implement a single motion primitive that every animated component uses, so the reduced-motion fallback is written once rather than remembered thirteen times.
3. Honor `prefers-reduced-motion` by substituting an instant state change — not a shortened animation.
4. Encode `UI-UX/10`'s two "never gets motion" rules structurally: destructive confirmation dialogs appear instantly with no entrance animation, and table data never animates on sort or filter.
5. Keep durations at the fast end — this console is used dozens of times a day by the same people, and a repeat user should never be waiting on an animation.

**Definition of Done**
- [ ] Every animation in `UI-UX/10`'s table has a token-driven implementation.
- [ ] With `prefers-reduced-motion: reduce`, every animation becomes an instant state change, verified by test.
- [ ] Confirmation dialogs have no entrance animation.
- [ ] Table sort and filter produce no row animation.
- [ ] No component hard-codes a duration.

---

## PF-02 — Component Library Scaffolding and Workbench

| | |
|---|---|
| **Status** | TODO · **Depends on** P0-17 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/05-DESIGN-SYSTEM.md` § Governance, `UI-UX/07-COMPONENT-SPECIFICATION.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md` |

**Goal** — A place where components live, are viewed in every state, and are reviewed — so `UI-UX/05`'s governance rule ("any new token or component must be added here before being used in a page spec") is enforceable rather than aspirational.

**Steps**
1. Establish the component directory structure and the export surface pages consume.
2. Set up a component workbench (Storybook or equivalent) rendering every component in every documented state, including loading, error, disabled, and focus.
3. Render every story in both the default theme and an organization-branded theme, so a branding override that breaks contrast is visible immediately.
4. Add the lint rule from `P0-17` to the library itself, and a second rule preventing a page from defining a component that belongs in the library.
5. Document each component with its variants, states, and the **rule** governing when to use it — `UI-UX/07` gives a rule per component, and the rule is the part that gets forgotten.
6. Wire the workbench into CI so a component with an undocumented state fails review.

**Definition of Done**
- [ ] Every component from `UI-UX/07` has a workbench entry covering all its documented states.
- [ ] Each entry restates the usage rule from `UI-UX/07`, not just the props.
- [ ] Branded-theme rendering works and surfaces contrast problems.
- [ ] A page-level ad-hoc component definition fails lint.

---

## PF-03 — Button

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Button, `UI-UX/15-FORM-UX.md`, `UI-UX/13-ACCESSIBILITY.md` |

**Goal** — Three variants mapped one-to-one to action severity, so an admin can gauge risk from color before reading the label (`UI-UX/05`).

**Steps**
1. Variants: `primary` (accent), `secondary` (neutral), `destructive` (danger).
2. States: default, hover, focus (visible ring using `color-accent`), disabled, loading.
3. Loading state replaces the label with a spinner **at the same width** — no layout shift (`UI-UX/07`).
4. Never render a silently disabled button: the component requires a reason, surfaced via tooltip or adjacent helper text (`UI-UX/15`).
5. Encode `UI-UX/07`'s subtlety about visual weight: a `destructive` button must not dominate a screen whose common path is non-destructive. "Deactivate user" on a user detail screen is `secondary` with danger-colored text; the full `destructive` weight is reserved for genuinely rare, high-consequence confirmations like Project Grant revocation.
6. Enforce one `primary` per screen or section — a lint rule or a runtime development warning, since ambiguity about the main action is the failure this rule prevents.
7. Meet minimum target size even inside compact table rows (`UI-UX/13`).

**Definition of Done**
- [ ] All three variants and all five states are implemented and in the workbench.
- [ ] Loading causes no width change, verified by test.
- [ ] A disabled button without a reason fails in development.
- [ ] Two `primary` buttons in one section raise a warning.
- [ ] Focus ring is visible and never suppressed.
- [ ] Target size meets `UI-UX/13`'s minimum at compact density.

---

## PF-04 — Table

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Table, `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `UI-UX/11-MICRO-INTERACTIONS.md` |

**Goal** — The console's primary data-display pattern, built once. `UI-UX/08` is explicit: no screen invents its own table.

**Steps**
1. Anatomy: sortable header row, compact body rows, row-level **kebab** action menu (not inline buttons — visual clutter at density), pagination footer.
2. Four states: default; loading via **skeleton rows matching row height**, never a blocking overlay; empty; and error as an inline banner above the table that keeps showing last-known-good data rather than going blank (`UI-UX/07`, `UI-UX/14`).
3. The kebab reveals on row hover and stays hidden otherwise (`UI-UX/11`) — but is always keyboard-reachable and always announced as "Actions for [row name]", never bare "button" (`UI-UX/13`).
4. Support the role-source badge as a column or inline indicator, since `UI-UX/07` makes it a cross-cutting requirement rather than a per-table choice.
5. Cursor pagination against `PLAN/05`'s `next_page_token`, stable while rows are being inserted.
6. Responsive degradation per `UI-UX/12`: hide secondary columns behind a show-more toggle or expandable row **before** ever requiring horizontal scroll for primary ones.
7. Sorting and filtering update instantly with no row animation (`UI-UX/10`).
8. Virtualize for large datasets, without breaking keyboard navigation or screen-reader row counts.

**Definition of Done**
- [ ] All four states render correctly and are covered by component tests.
- [ ] Skeleton rows match loaded row geometry — no jump on load.
- [ ] A load error preserves last-known data with a retry that re-fetches only the failed request (`UI-UX/09`).
- [ ] The kebab is keyboard-reachable and correctly labelled per row.
- [ ] Column degradation happens before horizontal scroll at tablet width.
- [ ] Pagination is stable under concurrent inserts.
- [ ] Large datasets stay responsive with keyboard navigation intact.

---

## PF-05 — Form Field Primitives

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Form Field, `UI-UX/15-FORM-UX.md`, `UI-UX/13-ACCESSIBILITY.md`, `UI-UX/11-MICRO-INTERACTIONS.md` |

**Goal** — One field anatomy across the whole console: label → input → helper text → error text, with error text *replacing* helper text rather than appearing alongside it.

**Steps**
1. Build the field wrapper enforcing the anatomy, then the inputs: text, textarea, select, checkbox, radio, toggle switch, and multi-value list (redirect URIs, role keys).
2. States per field: default, focus, error, disabled.
3. **Labels are always visible.** A placeholder is never a label substitute — placeholders carry format examples only, e.g. `https://app.example.com/callback` under a visible "Redirect URI" label (`UI-UX/15`).
4. Programmatically associate label, helper text, and error text with the input, so a screen reader announces the error when it appears rather than only rendering it visually (`UI-UX/13`).
5. Multi-value list removal is lightweight and offers a brief inline undo, because removing an item from a draft form has no effect until save — deliberately distinct from the heavier confirmation pattern for live actions (`UI-UX/15`).
6. Toggle switches apply immediately on change with brief inline confirmation, and carry no Save button (`UI-UX/11`).
7. Async field validation shows a small inline indicator beside the field, never a full-form block (`UI-UX/15`).
8. Target sizes meet `UI-UX/13`'s minimum, including checkboxes in dense role-selection lists.

**Definition of Done**
- [ ] Helper text and error text never appear simultaneously.
- [ ] Every field has a persistently visible label.
- [ ] Errors are announced to screen readers at the moment they appear.
- [ ] Multi-value removal offers inline undo.
- [ ] Toggles apply immediately and never sit inside a Save-button form.
- [ ] Every field type is keyboard-operable and correctly labelled.

---

## PF-06 — Modal and Side Panel

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02, PF-01 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Modal / Side Panel, `UI-UX/09-INTERACTION-DESIGN.md` § Keyboard & Focus, `UI-UX/10-MOTION-DESIGN.md`, `UI-UX/12-RESPONSIVE-BEHAVIOR.md` |

**Goal** — Two deliberately distinct variants, used for distinct purposes, never interchangeably.

**Steps**
1. **Modal**: center-screen, focused single-purpose actions ("Create Grant"). Motion: fade plus slight scale.
2. **Side panel**: slides from the right, for multi-step or detail-heavy flows ("Invite user", which `UI-UX/04` Flow 1 makes a two-step progression). Motion: slide.
3. Enforce `UI-UX/07`'s rule in the API: **destructive confirmations always use the modal variant**, never the side panel, so they interrupt rather than blend into a flow.
4. Focus management per `UI-UX/09`: opening moves focus inside immediately; closing returns focus to the triggering element; Escape closes; the focus trap always has a keyboard-accessible exit.
5. At tablet width, a side panel becomes a full-screen takeover — a narrow panel is too cramped for a two-step flow (`UI-UX/12`).
6. Prevent background scroll while open, without shifting layout when the scrollbar disappears.

**Definition of Done**
- [ ] Both variants exist with their distinct motion.
- [ ] The component refuses (or warns) if a destructive confirmation is rendered as a side panel.
- [ ] Focus enters on open and returns to the trigger on close, verified by test.
- [ ] Escape closes both variants.
- [ ] Side panel becomes a takeover at tablet width.
- [ ] No layout shift when opening.

---

## PF-07 — Badge and Tag

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Badge / Tag, `UI-UX/06-VISUAL-LANGUAGE.md`, `UI-UX/13-ACCESSIBILITY.md` |

**Goal** — Status and role-source communicated reliably, never by color alone.

**Steps**
1. **Status badge**: color + icon + text, per `UI-UX/06`'s status table — active, invited, deactivated, locked, revoked, draft.
2. **Role-source badge**: direct versus delegated. Build it now, in full, even though nothing renders "delegated" until `PF-37`. Retrofitting it later means auditing every screen that shows a role a second time.
3. Enforce the rule in the component's API: text is required, so a badge cannot be constructed as icon-and-color only (`UI-UX/13`).
4. Ensure every badge color meets AA contrast against both background tokens, in the default theme and under organization branding.

**Definition of Done**
- [ ] Every status in `UI-UX/06`'s table has a badge with color, icon, and text.
- [ ] A badge cannot be rendered without text.
- [ ] The role-source badge supports both values from the start.
- [ ] Contrast passes in both themes.

---

## PF-08 — Confirmation Dialog

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-06 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Confirmation Dialog, `UI-UX/09-INTERACTION-DESIGN.md` § Consequence Before Confirmation, `UI-UX/04-USER-FLOWS.md` |

**Goal** — The component that carries the console's single governing interaction rule. Getting this right once means every access-changing action behaves predictably.

**Steps**
1. Anatomy per `UI-UX/07`: a title stating the action plainly ("Revoke Project Grant?"), a **consequence summary in plain language** — never a generic "are you sure?" — a `destructive`-styled confirm button labelled with the real verb ("Revoke", not "OK"), and a `secondary` cancel.
2. Make the consequence summary a **required** prop. A dialog that can be constructed without one will eventually be constructed without one.
3. Implement the **typed-confirmation variant**: the confirm button stays disabled until the resource name is typed exactly. `UI-UX/07` calls this "the one deliberate friction point in the whole design system" — reserve it for Project Grant revocation with dependents and organization deletion, and document that scope in the component.
4. Support a consequence summary that is computed asynchronously (for example, "this removes access for 14 users"), with its own loading state — the count must be real, not estimated.
5. No entrance animation (`UI-UX/10`).
6. Provide the explicit success state that `UI-UX/09`'s feedback table requires for high-risk actions — a silently updated list is insufficient.

**Definition of Done**
- [ ] A dialog cannot be constructed without a plain-language consequence.
- [ ] Confirm buttons carry the action verb; "OK" and "Confirm" are rejected.
- [ ] Typed confirmation gates the confirm button on an exact match.
- [ ] An async consequence has its own loading state and never shows a stale count.
- [ ] No entrance animation.
- [ ] High-risk completions show an explicit success state.

---

## PF-09 — Breadcrumb

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Breadcrumb, `UI-UX/03-INFORMATION-ARCHITECTURE.md` § Breadcrumb & Wayfinding |

**Goal** — Wayfinding that reflects the real data hierarchy, which `UI-UX/03` says matters most for admins moving between "Projects" and "Granted Projects" — where knowing *whose* project you are looking at is a correctness question, not a convenience.

**Steps**
1. Render the actual data hierarchy: `Acme Org / POS Project / Roles`.
2. Every segment is clickable and jumps directly there.
3. Disambiguate owned versus delegated projects visibly in the trail — this is the specific confusion `UI-UX/03` names.
4. Derive from the route, so a breadcrumb can never disagree with where the user actually is.
5. Truncate long names without losing the ability to identify them, and keep the full name available to assistive technology.

**Definition of Done**
- [ ] The trail matches the data hierarchy on every nested screen.
- [ ] Every segment navigates.
- [ ] Owned and delegated projects are distinguishable in the trail.
- [ ] Truncation never hides identity from a screen reader.

---

## PF-10 — Search Input

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07-COMPONENT-SPECIFICATION.md` § Search Input, `UI-UX/11-MICRO-INTERACTIONS.md` § Search-as-You-Type, `UI-UX/03-INFORMATION-ARCHITECTURE.md` § Search |

**Steps**
1. Two visually similar variants: **global** (reachable anywhere via a persistent keyboard shortcut) and **scoped** (filters the current table only).
2. Debounce input before querying.
3. Show a loading indicator **only** if results take longer than a brief instant — a spinner flickering on every keystroke is worse than none (`UI-UX/11`).
4. Announce result counts to screen readers via a live region, since the result set changes without focus moving.
5. Preserve the query in the URL for the scoped variant, so a filtered view is linkable and survives refresh.

**Definition of Done**
- [ ] Both variants exist; the global one has a documented keyboard shortcut that works from every screen.
- [ ] Fast responses produce no spinner flash.
- [ ] Result counts are announced to assistive technology.
- [ ] Scoped queries survive refresh via the URL.

---

## PF-11 — Supporting Components

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/07`, `UI-UX/11-MICRO-INTERACTIONS.md`, `UI-UX/15-FORM-UX.md` § Multi-Step Forms, `UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` |

**Goal** — The components `UI-UX/` requires by implication but does not give their own section: without these, each page invents its own.

**Steps**
1. **Tabs** — used by Project detail, User detail, and Policies. Keyboard-navigable per the standard pattern; the active tab reflected in the URL so a tab is linkable.
2. **Step indicator** — "Step 1 of 2", visible at all times in multi-step flows, with data retained on back-navigation and a full summary before the final confirm (`UI-UX/15`).
3. **Toast / inline success** — fade plus slight slide, auto-dismiss with generous read time, announced via a live region so screen-reader users know an action succeeded (`UI-UX/10`, `UI-UX/13`).
4. **Copy-to-clipboard** — prominent, with clear success feedback ("Copy" → "Copied"). `UI-UX/11` singles this out because a silently failed copy of a one-time client secret means regenerating it and updating every consumer app.
5. **KPI card** — for the Organization Overview. Critically: **not clickable-looking unless it has a real drill-down destination** (`UI-UX/09`).
6. **Pagination footer** — shared by every table, cursor-based.
7. **Inline error banner** — the table-load-failure treatment from `UI-UX/14`, with a retry that re-fetches only the failed request and never reloads the page.

**Definition of Done**
- [ ] All seven components exist in the workbench with their states.
- [ ] Tab and step state are URL-reflected where linkability matters.
- [ ] Copy feedback is unmistakable and tested.
- [ ] A KPI card without a destination has no hover or cursor affordance.
- [ ] Retry re-fetches only the failed request, preserving scroll position.

---

# Track B — Application Shell

## PF-12 — Routing and Permission-Gated Navigation

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02, P1-21 · **Surface** console · **Spec required** Yes — authorization surface |
| **Plan refs** | `UI-UX/08-PAGE-SPECIFICATIONS.md` § Cross-Screen Requirements, `PLAN/08-AUTHORIZATION.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md`, `UI-UX/03-INFORMATION-ARCHITECTURE.md` § Depth Rule |

**Goal** — Routes that a user's claims don't permit are **genuinely unreachable**, not merely hidden by CSS — the rule `UI-UX/08` states as non-negotiable.

**Steps**
1. Build the route tree from `UI-UX/03`'s IA, respecting its **three-level depth rule**: Organization → Projects → [Project] → tab. A fourth level is a signal to reconsider the IA, not to add nesting.
2. Attach a required-permission declaration to each route, expressed in the same manager-role and claim vocabulary the API enforces (`PLAN/08`).
3. On direct URL navigation to a forbidden route, render an explicit permission-denied state — not a redirect to the dashboard, which leaves the user unsure whether the page exists, and not a blank screen.
4. Keep the frontend check and the API's enforcement reading from **one shared permission-resolution module**, so the two cannot drift. The API still enforces independently; this is about consistency of experience, never about being the control.
5. Handle token expiry mid-navigation: attempt silent renewal, and on failure preserve the intended destination through re-authentication so the user lands where they were going.
6. Encode organization context in the URL so every screen is linkable and survives refresh.
7. Route-level code splitting, so reaching the Users list does not download the Rego editor.

**Definition of Done**
- [ ] A forbidden route entered directly by URL is unreachable and explains why.
- [ ] Frontend and API permission logic share one source of truth, verified by test.
- [ ] No route exceeds three navigation levels.
- [ ] Silent renewal preserves the intended destination.
- [ ] Every screen is linkable and refresh-stable.
- [ ] Bundles are route-split.

**Abuse cases to test**
- Direct URL navigation to an unauthorized route (`SECURITY/02` §14 Client-Side Trust).
- Manipulating client-side claims to reveal a route — must still fail at the API.

---

## PF-13 — Navigation Chrome and Organization Context

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-12, PF-09 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/03-INFORMATION-ARCHITECTURE.md`, `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md` |

**Steps**
1. Persistent left navigation matching `UI-UX/03`'s structure, with Instance separated at the top rather than nested inside an organization — nesting it would imply it is "one organization among others," which is wrong.
2. Collapse to an icon-only rail at tablet width, expandable on demand (`UI-UX/12`).
3. **The active organization is unmistakable on every screen.** An admin performing a destructive action in the wrong organization is a realistic and severe failure; the chrome is the last line of defense against it.
4. Show "Granted Projects" as its own top-level item, never a tab under Projects — for a receiving organization these are conceptually someone else's projects (`UI-UX/03`).
5. Reflect the current route in the nav's active state, derived from the router rather than tracked separately.
6. Apply organization branding — logo and accent only.

**Definition of Done**
- [ ] Navigation matches `UI-UX/03`'s structure exactly.
- [ ] Instance navigation appears only for `INSTANCE_OWNER`.
- [ ] The active organization is visible on every screen at every breakpoint.
- [ ] The rail collapses and expands correctly at tablet width.
- [ ] Branding applies to logo and accent only, never to danger or warning colors.

---

## PF-14 — Global Search

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-10, PF-12 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/03-INFORMATION-ARCHITECTURE.md` § Search, `UI-UX/00-DESIGN-DIRECTION.md` |

**Goal** — Resolve organizations, projects, and users by name, email, or id from anywhere — a lookup admins do dozens of times a day, which otherwise costs a full walk down the nav tree.

**Steps**
1. Reachable from every screen via a persistent keyboard shortcut.
2. Search across organizations (only where the admin has multi-org visibility), projects, and users.
3. Group results by type with the type visible — an id-shaped query could match several kinds of thing.
4. **Scope results server-side by the caller's permissions.** Search must never confirm the existence of a resource the user cannot access; that is an enumeration primitive (`SECURITY/02` §12).
5. Full keyboard operation: open, type, arrow through results, Enter to navigate, Escape to dismiss.
6. Debounced, with the no-flash loading behavior from `PF-10`.

**Definition of Done**
- [ ] Reachable by keyboard from every screen.
- [ ] Results never include a resource the caller cannot access, verified by test.
- [ ] Fully operable without a mouse.
- [ ] Results are grouped and typed.

---

## PF-15 — Progressive Disclosure Rules

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-13 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/09-INTERACTION-DESIGN.md` § Progressive Disclosure, `UI-UX/01-USER-PERSONAS.md` |

**Goal** — A first-time Org Admin is not confronted with concepts they may never need, without hiding anything from someone who does need it.

**Steps**
1. Hide advanced navigation items — Project Grants, ABAC Policies — until the organization has actually created at least one, **or** the admin holds an applicable manager role (`UI-UX/09`).
2. Make the rule data-driven, not a hard-coded list, so Phase 4 and 4b features slot in without another round of navigation surgery.
3. Never let disclosure become concealment: an admin with the relevant role always sees the item, and there is always a discoverable way to reach a concept for the first time.
4. Do not confuse progressive disclosure with permission gating (`PF-12`). Disclosure is about *relevance*; gating is about *authorization*. Conflating them produces a UI where a permitted feature is unreachable.

**Definition of Done**
- [ ] Advanced items appear once an instance exists or the role applies.
- [ ] The rule is data-driven.
- [ ] There is always a first-time path to each concept.
- [ ] Disclosure and permission logic are separate modules.

---

# Track C — Cross-Cutting Behavior

## PF-16 — The Four-State System

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-04 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `UI-UX/09-INTERACTION-DESIGN.md` |

**Goal** — Loading, empty, error, and permission-denied handled by shared machinery, so `UI-UX/19`'s chain has something concrete to point at on every screen.

**Steps**
1. Implement **state priority** exactly as `UI-UX/14` specifies: loading beats empty; error beats both once a request has definitively failed. A screen must never show "No results" while a request is in flight.
2. Empty-state component taking a factual message and, where a clear next action exists, a primary button — "No users yet" plus "Invite your first user".
3. **Distinguish genuinely empty from filtered-to-empty.** The filtered case offers "clear filters", never a create action — offering to create a user when the real problem is an over-narrow search is actively unhelpful (`UI-UX/14`).
4. Three distinct error treatments per `UI-UX/14`'s table: list-load failure (inline banner, keep stale data, retry), form validation failure (mapped to fields), form server or network failure (top-of-form banner, **input preserved**, retry).
5. Error copy states what happened *and* what to do next. "Something went wrong" alone is insufficient — pair it with a next step.
6. Permission-denied state explaining what is missing, without disclosing what the screen would have contained.
7. Retry re-attempts only the failed request and never reloads the page, preserving scroll position (`UI-UX/09`).

**Definition of Done**
- [ ] State priority is enforced centrally; no screen can show empty during loading.
- [ ] Genuinely-empty and filtered-to-empty are distinct components with distinct actions.
- [ ] All three error treatments exist and are used correctly per context.
- [ ] Form input survives a server error, verified by test.
- [ ] No error message is a bare "something went wrong".
- [ ] Retry preserves scroll and re-fetches only what failed.

---

## PF-17 — Form System

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-05 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/15-FORM-UX.md`, `UI-UX/11-MICRO-INTERACTIONS.md`, `PLAN/05-API-CONTRACT.md` § Standard Error Format, `UI-UX/13-ACCESSIBILITY.md` |

**Goal** — Forms behave identically everywhere, and a server-side validation error lands on the field that caused it.

**Steps**
1. **Validation timing**: on blur, not on every keystroke; and on submit re-validate everything regardless of blur history, to catch fields never touched — browser autofill is the common case (`UI-UX/15`).
2. Map `PLAN/05`'s `details[].field` entries back to their fields automatically. A generic top-of-form banner when field-level detail was available is a defect, not a fallback.
3. On submit with errors, move focus to the **first** invalid field and announce a summary to screen readers (`UI-UX/15`, `UI-UX/13`).
4. Multi-step support: persistent step indicator, data retained on back-navigation, and a **full summary of everything entered** before the final confirm.
5. Enforce `UI-UX/15`'s save-pattern rule at the component level: a form is either explicit-Save or apply-on-change, **never a mix**. Mixing leaves the admin unsure whether their last change saved.
6. Share validation rules with the backend wherever a format is defined once — redirect URI, permission key, role key — so client and server never disagree about what is valid.
7. Preserve input across navigation and failure; never silently discard entered data (`UI-UX/09` § Error Recovery).

**Definition of Done**
- [ ] Blur and submit validation behave per `UI-UX/15`.
- [ ] Server field errors map to fields automatically, verified against a real API error response.
- [ ] Focus moves to the first invalid field with a screen-reader summary.
- [ ] A mixed save-pattern form fails in development.
- [ ] Multi-step forms retain data and show a full pre-confirm summary.
- [ ] Client and server validation rules share one definition.

---

## PF-18 — Responsive Grid and the Unsupported-Width Boundary

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-13 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `UI-UX/16-MOBILE-UX.md` |

**Steps**
1. Implement the concrete grid: 12-column at ≥1440px (full gutter) and 1024–1439px (reduced gutter), 8-column at 768–1023px, 4-column below 768px for in-scope screens only.
2. Require each page task to **state which columns its regions occupy at each breakpoint** — `UI-UX/12` explicitly rejects "it's responsive" as a specification.
3. Implement the admin-screen tablet contract: no horizontal scroll for primary actions; tables shed secondary columns before scrolling; side panels become full-screen takeovers.
4. Below roughly 600px, admin screens show an explicit **unsupported-width message suggesting a larger screen** — `UI-UX/12` is clear that this is better than a silently broken layout.
5. Exempt the mobile-in-scope screens from that boundary: Personal account settings and the hosted login/MFA screens (`UI-UX/16`).
6. Exempt the Rego editor from below-tablet support entirely — policy editing is not a small-screen task regardless of optimization (`UI-UX/12`).
7. Test at the four widths `UI-UX/12` names: 1440px+, 1024–1439px, ~900px, and below 600px.

**Definition of Done**
- [ ] The grid matches `UI-UX/12`'s table exactly.
- [ ] Every page task documents its column occupancy per breakpoint.
- [ ] Admin screens show the unsupported-width message below ~600px.
- [ ] Mobile-in-scope screens are exempt and fully usable.
- [ ] Verified at all four test widths.

---

## PF-19 — Accessibility Infrastructure

| | |
|---|---|
| **Status** | TODO · **Depends on** PF-02 · **Surface** console · **Spec required** No |
| **Plan refs** | `UI-UX/13-ACCESSIBILITY.md`, `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md`, `PLAN/02-REQUIREMENTS.md` (WCAG 2.1 AA) |

**Goal** — Make accessibility a property of the foundation rather than a Phase 5 remediation project. `UI-UX/13` is explicit that retrofitting is far more expensive, and `PLAN/18` R-10 lists exactly this as a medium risk.

**Steps**
1. Wire automated accessibility linting (axe-core or equivalent) into CI, failing on new violations from the first component onward.
2. Build a contrast-validation utility, and apply it at the point an org admin **sets** a custom accent color — `UI-UX/13` requires validation at that moment, not a silent inaccessible ship.
3. Establish focus-management primitives once: focus trap, focus restore, skip links, landmark regions.
4. Establish live-region primitives for asynchronous feedback not tied to a focused element — toasts, search result counts, form error summaries.
5. Document the icon-labelling convention: a meaningful icon always has an accessible label, and a kebab announces "Actions for [row name]", never bare "button".
6. Enforce minimum target sizes at the component level, so compact density and accessibility do not end up in conflict (`UI-UX/13`).
7. Set up screen-reader testing as a repeatable procedure, not a one-off before sign-off.

**Definition of Done**
- [ ] Automated a11y checks run in CI and fail on new violations.
- [ ] A custom accent failing contrast is rejected at the point of entry.
- [ ] Focus and live-region primitives exist and are used by every relevant component.
- [ ] Target-size minimums are enforced at the component level.
- [ ] A repeatable screen-reader test procedure is documented.

---

## PF-20 — Data Layer Conventions

| | |
|---|---|
| **Status** | TODO · **Depends on** P0-16, P1-21 · **Surface** console · **Spec required** No |
| **Plan refs** | `PLAN/06-FRONTEND-ARCHITECTURE.md` § Tech Stack, `PLAN/05-API-CONTRACT.md`, `UI-UX/09-INTERACTION-DESIGN.md` |

**Goal** — One way to fetch, cache, invalidate, and mutate — so cached data from a previous organization can never render after a context switch, which would be a cross-tenant leak in the UI even with a correct API.

**Steps**
1. Establish query-key conventions **scoped by organization**, so switching context cannot serve the previous organization's cached data (`PF-29` depends on this).
2. Use the generated client from `P0-16` exclusively; no hand-written fetch calls, so a spec change surfaces as a type error.
3. Set staleness per resource type deliberately: an audit log page and a user list have very different tolerances.
4. Define the invalidation map — which mutations invalidate which queries — in one place rather than per call site, where it will be incomplete.
5. Restrict optimistic updates to the low-risk, reversible actions `UI-UX/09`'s feedback table permits, with rollback on error. Session revocation is the canonical example (`UI-UX/19`'s worked example); access-control mutations are **not** candidates, per the confirm-before-not-undo-after rule.
6. Handle 401 centrally: attempt silent renewal, fall back to interactive login, and never let it surface as a generic error toast.
7. Handle 403 centrally so it renders the permission-denied state rather than a generic failure.

**Definition of Done**
- [ ] Query keys are org-scoped; a context switch clears the previous org's cache, verified by test.
- [ ] All requests go through the generated client.
- [ ] The invalidation map is centralized and covers every mutation.
- [ ] Optimistic updates are limited to permitted action types and roll back on error.
- [ ] 401 and 403 are handled centrally and correctly.

---

# Track D — Console Screens

Every task below inherits: the `UI-UX/19` chain table, the twelve console-wide rules, and the column-occupancy documentation required by `PF-18`.

## PF-21 — Organization Overview

| | |
|---|---|
| **Status** | TODO · **Gate** P1-16 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Organization Overview, `UI-UX/08`, `UI-UX/02-USER-JOURNEYS.md` |

**Goal** — The workspace landing screen. `UI-UX/18` specifies it in detail — follow that spec rather than re-deriving it.

**Steps**
1. Implement `UI-UX/18`'s above-the-fold priorities and visual hierarchy exactly.
2. KPI cards using `PF-11`'s component — and only clickable-looking where a real drill-down exists (`UI-UX/09`).
3. Recent activity drawn from the audit log, respecting the same redaction the API applies.
4. Empty state for a brand-new organization that orients rather than just reporting emptiness — this is the first screen a new admin sees.
5. Document column occupancy at each breakpoint per `PF-18`.

**Definition of Done**
- [ ] Matches `UI-UX/18`'s detailed spec, including above-the-fold ordering.
- [ ] No non-navigating card looks clickable.
- [ ] The new-organization empty state orients a first-time admin.
- [ ] Chain table and column occupancy are committed with the code.

---

## PF-22 — Project List

| | |
|---|---|
| **Status** | TODO · **Gate** P1-17 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08-PAGE-SPECIFICATIONS.md` (Project list) |

**Steps**
1. Shared table from `PF-04`; create action as the single `primary` button.
2. Both empty states — no projects yet (with create action) versus no match (with clear-filters).
3. Deletion refused with an actionable error listing what blocks it, surfaced inline rather than as a generic failure (`P1-17`).
4. Row navigation into project detail; kebab for row actions.

**Definition of Done**
- [ ] Uses the shared table pattern with no bespoke variations.
- [ ] Both empty states are distinct and correct.
- [ ] A blocked deletion explains precisely what blocks it.

---

## PF-23 — Project Detail — Applications Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P1-18 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/08` (Applications tab), `UI-UX/11-MICRO-INTERACTIONS.md` § Copy-to-Clipboard, `UI-UX/15-FORM-UX.md`, `PLAN/09-SECURITY.md` |

**Goal** — Register applications and handle the one-time client secret reveal, which is the highest-consequence micro-interaction in the console.

**Steps**
1. Applications table with type badge, client id, and redirect URI count.
2. Registration form: type selection driving which fields apply, redirect URIs as a multi-value field with format helper text and blur validation, grant types constrained by the chosen type.
3. **The one-time secret reveal**: a modal shown once at creation, with a prominent copy button, unmistakable success feedback, and copy stating plainly that the secret cannot be retrieved again (`UI-UX/08`, `UI-UX/11`).
4. Make the reveal hard to dismiss accidentally — this is the one screen where an accidental dismissal costs a secret rotation and a redeploy of every consumer app.
5. Secret rotation reuses the same reveal component, with a confirmation explaining the overlap window.
6. Never render a secret anywhere else, in any state, at any time.
7. Redirect URI removal offers the inline undo from `PF-05`, since it takes effect only on save.

**Definition of Done**
- [ ] The secret appears exactly once and is never retrievable again through any UI path.
- [ ] Copy feedback is unmistakable and covered by a component test.
- [ ] The reveal cannot be dismissed by an accidental outside click.
- [ ] Type-dependent fields update correctly.
- [ ] Redirect URI validation matches the server's rule exactly.

---

## PF-24 — User List

| | |
|---|---|
| **Status** | TODO · **Gate** P1-19 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Users List, `UI-UX/04-USER-FLOWS.md` Flow 1, `UI-UX/08` |

**Goal** — Where admins spend most of their time, plus the invite flow — `UI-UX/04` Flow 1, implemented step by step.

**Steps**
1. Follow `UI-UX/18`'s detailed spec, including its row-action pattern conventions, which other screens then reference.
2. Search and status filter, with debounced server-side lookup and no layout shift while loading.
3. Status badges from `PF-07` — active, invited, deactivated.
4. **Invite flow as a side panel** with two steps (`UI-UX/07`, `UI-UX/04` Flow 1), step indicator visible, data retained on back-navigation, full summary before confirm.
5. Async email-in-use validation with the inline indicator, not a full-form block (`UI-UX/15`).
6. Deactivation as `secondary` with danger-colored text — not a full `destructive` button, per `PF-03`'s weight rule — opening a confirmation that states plainly that sessions terminate immediately.
7. At tablet width the side panel becomes a full-screen takeover (`PF-06`).

**Definition of Done**
- [ ] Flow 1 is implemented end-to-end and covered by an E2E test.
- [ ] Both empty states are correct and distinct.
- [ ] The invite panel retains data across steps and shows a pre-confirm summary.
- [ ] Deactivation's visual weight matches `UI-UX/07`'s rule.
- [ ] Async validation does not block the form.

---

## PF-25 — User Detail Shell and Profile Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P1-19 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (User detail — Profile tab), `UI-UX/15-FORM-UX.md` |

**Steps**
1. Tabbed shell using `PF-11`'s tabs: Profile, Grants, Sessions, MFA — with the tab reflected in the URL.
2. Profile form with explicit Save (a multi-field form, so never apply-on-change).
3. **Tabs whose backend does not exist yet render an explicit unavailable state**, clearly labelled with the phase that will deliver them — never a broken or ambiguously empty tab. Nothing in the console may imply a capability that isn't shipped.
4. Deactivation action consistent with `PF-24`'s treatment.
5. Breadcrumb reflecting the real hierarchy.

**Definition of Done**
- [ ] All four tabs exist; unshipped ones state their unavailability explicitly.
- [ ] The active tab is URL-reflected and linkable.
- [ ] Profile uses explicit Save with no mixed pattern.

---

## PF-26 — Audit Log

| | |
|---|---|
| **Status** | TODO · **Gate** P1-20 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (Audit Log), `UI-UX/14`, `PLAN/13-OBSERVABILITY.md` |

**Steps**
1. Table with event type, actor, timestamp, and summary; cursor pagination stable while new events are written.
2. Filters: event type, date range, actor.
3. Detail expansion showing the redacted payload. The console must never render a field the API should not have returned — if one appears, that is a backend defect to report, not a UI problem to style around.
4. Relative timestamps in the viewer's timezone, with the underlying UTC value inspectable — incident timelines get reconstructed across timezones.
5. Both empty states: no events yet versus no events matching the filters.
6. Filtering and export polish are deliberately `PF-50`-era work (`P5-10`); this task delivers the readable baseline `PLAN/17` Phase 1 requires.

**Definition of Done**
- [ ] Login success and failure are visible with correct actor and timestamp.
- [ ] Filters combine correctly; pagination is stable under writes.
- [ ] UTC values are inspectable behind relative display.
- [ ] Both empty states are handled.

---

## PF-27 — Organization Settings

| | |
|---|---|
| **Status** | TODO · **Gate** P1-16 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (Organization Settings), `UI-UX/05-DESIGN-SYSTEM.md` § Color, `UI-UX/13-ACCESSIBILITY.md`, `PLAN/08-AUTHORIZATION.md` Part B |

**Goal** — Branding and domain verification, with the branding constraints enforced rather than documented.

**Steps**
1. Logo upload with type, size, and dimension validation. Treat it as untrusted file input (`SECURITY/02` §9) — the backend validates independently.
2. Accent color picker that **validates contrast at the point of selection** and refuses an inaccessible value with an explanation (`UI-UX/13`).
3. Make it structurally impossible to override `color-danger` or `color-warning` — those must stay universally recognizable regardless of tenant branding (`UI-UX/05`).
4. Live preview showing the chosen accent applied to real components, so the admin sees the result before saving.
5. Domain verification status with a badge and clear next steps for an unverified domain.
6. Explicit Save; changes are audited server-side.

**Definition of Done**
- [ ] An inaccessible accent color is rejected at selection with a reason.
- [ ] Danger and warning tokens cannot be overridden, verified by test.
- [ ] Logo upload validates client-side and is re-validated server-side.
- [ ] Domain verification status and next steps are clear.

---

## PF-28 — Instance Screens

| | |
|---|---|
| **Status** | TODO · **Gate** P1-16, P1-20 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/08` (Organization list, Instance-wide policies, Instance audit log), `UI-UX/03-INFORMATION-ARCHITECTURE.md`, `PLAN/08-AUTHORIZATION.md` Part C |

**Goal** — The `INSTANCE_OWNER` surface: the three screens most admins will never see.

**Steps**
1. **Organization list**: table with create, view, and suspend. Suspension uses the typed-confirmation dialog — it disables login for every user in that organization.
2. **Instance-wide policies**: defaults inherited by organizations, with the inheritance relationship made explicit so an admin knows whether they are editing a default or an override.
3. **Instance audit log**: the cross-organization view, reusing `PF-26`'s components with an organization column and filter. It reads through the explicit, audited instance-level access path (`PLAN/04`) — make that visible in the UI, since using it is itself an audited event.
4. Gate all three behind `INSTANCE_OWNER` at the route level (`PF-12`), with the API enforcing independently.
5. Organization deletion, if offered at all, requires typed confirmation and states plainly that audit history is preserved while everything else is not.

**Definition of Done**
- [ ] All three screens exist and are unreachable without `INSTANCE_OWNER`.
- [ ] Suspension and deletion both require typed confirmation with a real consequence summary.
- [ ] Policy inheritance is visible rather than implied.
- [ ] The instance audit view is clearly distinguished from the org-scoped one.

---

## PF-29 — Organization Switcher

| | |
|---|---|
| **Status** | TODO · **Gate** P2-08 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (Organization switcher), `PLAN/01-PRODUCT-SCOPE.md` § Out of Scope, `PLAN/06-FRONTEND-ARCHITECTURE.md` |

**Goal** — Context switching for an admin who administers several organizations — **not** the multi-organization end-user workspace switching that `PLAN/01` explicitly defers. These are different features and must not be conflated.

**Steps**
1. Show the switcher only when the signed-in admin actually administers more than one organization.
2. Search input plus list, per `UI-UX/08`'s composition.
3. **Switching clears the previous organization's cached server state** via `PF-20`'s org-scoped query keys. Stale cross-org data in the UI is a leak even when every API call was correct.
4. Reflect the new context in the URL so the switch survives refresh and is linkable.
5. Verify server-side that the caller may act in the selected organization — the switcher is UI, never a control.
6. Make the switch unmistakable in the chrome afterward (`PF-13`).

**Definition of Done**
- [ ] Lists exactly the organizations the caller administers, per server-side truth.
- [ ] Switching clears the previous org's cache, verified by test.
- [ ] Selecting an unauthorized organization is refused server-side even if forced client-side.
- [ ] Context survives refresh.

---

## PF-30 — Project Detail — Roles Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P2-02 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (Roles tab), `UI-UX/15-FORM-UX.md`, `PLAN/08-AUTHORIZATION.md` Part A |

**Steps**
1. Table: key, display name, permission count, grant count.
2. Create and edit forms with permission keys as a multi-value field, validated **by the exact same rule the server uses** (`PF-17`) — a merely similar client rule produces confusing rejections.
3. Built-in roles render as non-editable **with a stated reason**, never as a disabled control with no explanation (`PF-03`).
4. Deletion confirmation showing the accurate count of affected grants.
5. Both empty states.

**Definition of Done**
- [ ] Client and server validation share one definition.
- [ ] Built-in roles are visibly and explicably non-editable.
- [ ] Deletion shows an accurate affected-grant count.

---

## PF-31 — Authorizations Tab and User Grants Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P2-03 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/08` (Authorizations tab, User detail — Grants tab), `UI-UX/06-VISUAL-LANGUAGE.md`, `PLAN/08-AUTHORIZATION.md` |

**Goal** — Assign and revoke project roles, with the role-source badge in place from day one — `UI-UX/08` makes it mandatory on every screen showing roles.

**Steps**
1. User search with debounced server-side lookup and a non-shifting loading state.
2. Role assignment showing each role's **permission keys**, so an admin can see what they are actually granting rather than trusting a name.
3. Render `PF-07`'s role-source badge, showing "direct" now; `PF-37` makes "delegated" appear. Building it now avoids auditing every role-displaying screen a second time.
4. Revocation with confirmation stating plainly that it takes effect immediately (`P2-03`).
5. A user with no grants shows an explicit **"no access"** state, never an ambiguous blank — least privilege should be visible, not inferred.
6. Build the User detail Grants tab from the same components, so the two views cannot drift.

**Definition of Done**
- [ ] The role-source badge renders on both screens.
- [ ] Permission keys are visible at assignment time.
- [ ] A no-grant user shows an unambiguous no-access state.
- [ ] Both screens share components.
- [ ] Assignment and revocation are covered by an E2E test.

---

## PF-32 — Policies — Access Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P2-10 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (Policies — Access tab), `UI-UX/11-MICRO-INTERACTIONS.md` § Toggle Switches, `PLAN/08-AUTHORIZATION.md` Part B |

**Steps**
1. Fields mapping exactly onto `organizations.settings`: `min_length`, `require_uppercase`, `max_age_days`, `session_lifetime_hours`, `allowed_login_methods`.
2. Toggles apply immediately with inline confirmation (`UI-UX/11`); multi-field groups use explicit Save. **Never mix the two within one form** (`UI-UX/15`) — group them into visually separate sections if both are present.
3. Warn about blast radius before applying: shortening session lifetime logs people out; restricting login methods can lock out users who only have that method.
4. Show current effective values beside the editable fields, so an admin knows what they are changing from.
5. `mfa_required` appears only if it can be described honestly. Until Phase 3 ships enforcement, either omit it or label it explicitly as taking effect when MFA ships — never present it as active.

**Definition of Done**
- [ ] Fields map exactly to `PLAN/08` Part B's documented JSON shape.
- [ ] Save patterns are not mixed within a section.
- [ ] Consequential changes warn before applying.
- [ ] Nothing claims enforcement that does not exist in the current phase.

---

## PF-33 — User Detail — Sessions Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P3-09 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` § Worked Example, `UI-UX/04-USER-FLOWS.md` Flow 4, `UI-UX/11-MICRO-INTERACTIONS.md`, `UI-UX/16-MOBILE-UX.md` |

**Goal** — `UI-UX/19` specifies this screen's revoke button down to its screen-reader label as its worked example. Follow it row by row rather than re-deriving it.

**Steps**
1. Revoke button: `secondary` variant per row, **single click with no modal** — low-risk per `UI-UX/09`'s feedback-timing table — with optimistic removal and rollback on error, a spinner at constant width, and an inline error beside the row on failure.
2. Screen-reader label is **"Revoke session on [device/browser]"**, not bare "Revoke" — `UI-UX/19` calls this out specifically because multiple sessions are listed.
3. Table columns: device, coarse location, created, last active. **"Last active" is relative** ("2 minutes ago"), not absolute (`UI-UX/11`).
4. **Mark the current session distinctly** ("This device") so a user reviewing sessions doesn't revoke the one they are using — `UI-UX/11` names this explicitly.
5. "Revoke all other sessions" as a separate, prominent action that preserves the current one.
6. Empty state distinguishes "no other active sessions" from "no sessions" — the current one always exists.
7. In scope for mobile via `PF-35`: the revoke target stays a single tap target at mobile width.

**Definition of Done**
- [ ] The implementation matches `UI-UX/19`'s worked example on all twelve chain rows.
- [ ] The screen-reader label disambiguates between sessions.
- [ ] Optimistic update with rollback is covered by a component test.
- [ ] The current session is clearly marked and protected from accidental revocation.
- [ ] Flow 4 is covered end-to-end.

---

## PF-34 — User Detail — MFA Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P3-02, P3-05 · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/08` (MFA tab), `UI-UX/16-MOBILE-UX.md`, `UI-UX/13-ACCESSIBILITY.md` |

**Goal** — Admins see status read-only; users manage their own factors fully — the split `UI-UX/08` specifies.

**Steps**
1. **Admin view**: enrolled factor types and dates as badges, strictly read-only, plus the audited reset action from `P3-04` where the role permits.
2. **Self-service view**: enroll TOTP, register a passkey, remove a factor, regenerate recovery codes.
3. TOTP enrollment shows a QR code **and a copyable manual entry code**. `UI-UX/16` gives the reason: on mobile, the user may be trying to scan a code displayed on the very device that must scan it, which is impossible.
4. QR code needs a text alternative to satisfy `UI-UX/13`; the manual code doubles as it.
5. Recovery codes display once, with copy and download and unmistakable warning copy.
6. Removing the last factor while `mfa_required` is on is blocked with a clear explanation, never a silent failure.
7. Distinct messages per error: wrong code, expired challenge, unsupported browser (`UI-UX/14`).

**Definition of Done**
- [ ] Admins cannot enroll or remove another user's factors except via the audited reset path.
- [ ] Manual entry code is present alongside every QR code.
- [ ] Recovery codes are shown once with clear warnings.
- [ ] Removing the last factor under mandatory MFA is blocked with an explanation.
- [ ] Both enrollment flows are covered by E2E tests.

---

## PF-35 — Personal Account Settings (Mobile-Optimized)

| | |
|---|---|
| **Status** | TODO · **Gate** P3-09, P3-02 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/16-MOBILE-UX.md`, `UI-UX/08` § Responsive Scope, `UI-UX/12-RESPONSIVE-BEHAVIOR.md` |

**Goal** — The self-service surface, and the **only** console screen group requiring full mobile optimization — because end users, unlike admins, will reach it from a phone, often immediately after realizing they lost a device (`UI-UX/16`, `UI-UX/02` Journey 4).

**Steps**
1. Compose from existing components: password change, own MFA (`PF-34`'s self-service view), own sessions (`PF-33`).
2. Password change requires the current password and shows the org policy requirements **before** submission, not only on rejection.
3. **Single-column, full-width layout** at mobile; no side-by-side panels (`UI-UX/16`).
4. **Bottom-anchored primary actions** for thumb reachability, rather than top-of-screen buttons requiring an awkward reach.
5. Session revocation stays single-tap and immediately effective — `UI-UX/16` notes the low-friction requirement matters *more* on mobile, since this is where a security-anxious user reaches first.
6. Linked social logins render as an explicitly unavailable placeholder until `P4-11` ships.
7. Reachable from anywhere in the console, since a user may arrive from any context.
8. Touch targets meet or exceed `UI-UX/13`'s minimums.

**Definition of Done**
- [ ] Every action works on a real mobile viewport, tested on at least one iOS and one Android device or accurate emulator (`UI-UX/16` § Testing).
- [ ] Primary actions are bottom-anchored at mobile width.
- [ ] Password change surfaces policy requirements up front.
- [ ] Social login shows as unavailable rather than broken.
- [ ] Accessibility passes at mobile width as well as desktop.

---

## PF-36 — Project Detail — Project Grants Tab

| | |
|---|---|
| **Status** | TODO · **Gate** P4-01 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Project Grants Tab, `UI-UX/04-USER-FLOWS.md` Flow 2, `UI-UX/11-MICRO-INTERACTIONS.md`, `UI-UX/05-DESIGN-SYSTEM.md` |

**Goal** — The granting side of delegation, implementing Flow 2 exactly. `UI-UX/18` specifies this page in detail.

**Steps**
1. Table: receiving organization, granted roles, status, created date.
2. Creation modal: select the receiving organization, then a **subset** of the project's roles. Make the subset relationship visually obvious — an admin should see what they are *not* granting as clearly as what they are.
3. Implement `UI-UX/11`'s role-checkbox micro-interaction: checking or unchecking a role **updates the confirmation preview text in real time**, so the plain-language consequence builds up as selections are made rather than appearing only at the end.
4. Use `color-warning` for the state `UI-UX/05` names by example — "this Project Grant has no roles selected yet." Warning, not danger: it is a caution, not a destructive action.
5. Revocation uses the **typed-confirmation** variant (`UI-UX/08` specifies this exact variant), stating that every user holding roles through the grant loses access.
6. Show the count of users currently holding roles through the grant **before** revoking — blast radius visible at the moment of decision (`PF-08`'s async consequence).
7. `color-danger` only on the revoke action.

**Definition of Done**
- [ ] Flow 2 is implemented exactly and covered by an E2E test.
- [ ] The preview updates live as roles are selected.
- [ ] Revocation requires typed confirmation and shows a real affected-user count.
- [ ] The no-roles-selected state uses `color-warning`, not `color-danger`.

---

## PF-37 — Granted Projects List

| | |
|---|---|
| **Status** | TODO · **Gate** P4-02 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/08` (Granted Projects list), `UI-UX/04-USER-FLOWS.md` Flow 3, `UI-UX/11-MICRO-INTERACTIONS.md`, `UI-UX/01-USER-PERSONAS.md` |

**Goal** — The receiving side, implementing Flow 3 — with the hard constraint from `UI-UX/08` that this screen **never shows non-granted roles**.

**Steps**
1. List projects delegated *to* this organization, naming the granting organization and the available roles.
2. **A role outside `granted_role_keys` is simply absent** — never a disabled checkbox. `UI-UX/11` is explicit: a disabled checkbox implies "you could have this, but not right now," which is the wrong signal entirely.
3. The UI restriction is convenience; the server validates the subset independently on every request (`P4-02`). Never treat the UI as the control.
4. Render the role-source badge as **"delegated"** — the second value `PF-07` built for, finally in use.
5. When the granting organization revokes, reflect it clearly rather than failing opaquely on the next action.
6. An explanatory empty state, since a vendor admin may meet the concept of a granted project here for the first time.

**Definition of Done**
- [ ] A non-granted role is never rendered, in any state, verified by test.
- [ ] The role-source badge shows "delegated".
- [ ] Flow 3 is covered by an E2E test.
- [ ] A revoked grant is reflected clearly, not as an opaque error.
- [ ] The empty state explains the concept.

---

## PF-38 — Policies — ABAC Tab with Rego Editor

| | |
|---|---|
| **Status** | TODO · **Gate** P4B-04, P4B-05 · **Size** L · **Surface** console |
| **Plan refs** | `UI-UX/08` (Policies — ABAC tab), `UI-UX/04-USER-FLOWS.md` Flow 5, `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `UI-UX/13-ACCESSIBILITY.md` |

**Goal** — Implement Flow 5 exactly: Rego editor, diff view, confirmation dialog. **Conditional** — only if Phase 4b is undertaken.

**Steps**
1. Rego editor with syntax highlighting and inline compile errors from the server-side validator (`P4B-04`).
2. **Diff view** between the active version and the draft. Activating a policy changes a live security control, so reviewing it should feel like reviewing a code change.
3. Dry-run interface presenting `P4B-05`'s comparison, with **newly-denied decisions visually dominant** — a new denial is a potential lockout, and it is the outcome dry-run exists to catch.
4. Activation confirmation using the typed-confirmation variant, stating the blast radius plainly.
5. Version history with single-action rollback per version (`PLAN/17` Phase 4b requires reverting to be one action).
6. `color-danger` only for genuinely destructive actions — an activation that would deny, and deletion.
7. **Choose the editor component for accessibility up front.** It must be keyboard-navigable and screen-reader usable, or it fails `UI-UX/13`'s AA requirement — discovering this during the Phase 5 audit means replacing the editor late.
8. Below tablet width, this screen may require a larger screen — `UI-UX/12` explicitly exempts it, since policy editing is not a small-screen task.

**Definition of Done**
- [ ] Flow 5 is implemented exactly and covered by an E2E test.
- [ ] Compile errors render inline against the offending line.
- [ ] The diff view shows changes before activation.
- [ ] Dry-run emphasizes new denials.
- [ ] Rollback is one action from version history.
- [ ] The editor meets WCAG 2.1 AA including keyboard and screen-reader use.

---

# Track E — Hosted Authentication Screens

These are served by the auth service itself, not the console SPA (`P1-12`) — they must work with JavaScript disabled and must not depend on the console's deploy cycle. They are designed under the same mobile principles as the console's in-scope screens, since end users log in from phones constantly (`UI-UX/16`).

## PF-39 — Login Page

| | |
|---|---|
| **Status** | TODO · **Gate** P1-12 · **Size** M · **Surface** backend, console |
| **Plan refs** | `UI-UX/15-FORM-UX.md`, `UI-UX/13-ACCESSIBILITY.md`, `UI-UX/16-MOBILE-UX.md`, `SECURITY/02` §5, §6, §12 |

**Steps**
1. Server-rendered, functioning without client-side JavaScript.
2. Field anatomy and error presentation per `UI-UX/15`, with an error summary linked to fields.
3. **Uniform error messaging**: a wrong password and a nonexistent account are indistinguishable in text, status, and timing (`SECURITY/02` §12).
4. Organization branding — logo and accent only, never danger or warning colors.
5. Fully mobile-optimized: single column, comfortable targets, correct input modes and autocomplete hints so password managers work.
6. Strict security headers: a CSP with no inline script, `X-Frame-Options: DENY` (a login page must never be framable), `Referrer-Policy: no-referrer`.
7. Never reflect the `error` or `state` parameter into the page unescaped (`SECURITY/02` §6).
8. Forgot-password entry point.

**Definition of Done**
- [ ] Works with JavaScript disabled.
- [ ] Wrong-password and unknown-account responses are indistinguishable.
- [ ] The page cannot be framed, verified by a header test.
- [ ] Meets WCAG 2.1 AA and completes by keyboard alone.
- [ ] Usable on a real mobile device with a password manager.

---

## PF-40 — MFA Challenge and Enrollment Screens

| | |
|---|---|
| **Status** | TODO · **Gate** P3-03 · **Size** M · **Surface** backend |
| **Plan refs** | `UI-UX/16-MOBILE-UX.md`, `UI-UX/15-FORM-UX.md`, `UI-UX/13-ACCESSIBILITY.md` |

**Steps**
1. TOTP challenge screen with a single code input using the correct input mode and one-time-code autocomplete, so mobile keyboards and OS autofill both work.
2. WebAuthn challenge screen with a clear prompt and a graceful fallback to TOTP on unsupported browsers.
3. Recovery-code entry as a clearly-signposted alternative path.
4. Forced-enrollment screen for `P3-07`, explaining **why** enrollment is now required rather than simply blocking.
5. Uniform, non-distinguishing error messages (`P3-03`).
6. QR code plus copyable manual entry code, for the same same-device reason as `PF-34`.
7. Fully mobile-optimized — this is the most likely place for a phone to be involved.

**Definition of Done**
- [ ] Code entry works with OS autofill on mobile.
- [ ] WebAuthn degrades cleanly to TOTP.
- [ ] Recovery-code path is discoverable, not hidden.
- [ ] Forced enrollment explains itself.
- [ ] Accessible and mobile-usable.

---

## PF-41 — Password Reset and Invitation Acceptance

| | |
|---|---|
| **Status** | TODO · **Gate** P1-19.4, P1-19.5 · **Size** M · **Surface** backend |
| **Plan refs** | `UI-UX/15-FORM-UX.md`, `PLAN/09-SECURITY.md`, `SECURITY/02` §12 |

**Steps**
1. Reset request form whose response is **identical whether or not the email exists** (`SECURITY/02` §12).
2. Reset form showing the organization's password policy requirements **up front**, validated live against the same rules the server applies, and checked again on submit.
3. Clear handling of an expired or already-used token, with a path to request a new one.
4. Invitation acceptance: set the initial password under the same policy, then land somewhere useful rather than a dead end.
5. Never expose the token in the page title, URL after use, or any analytics payload.
6. Mobile-optimized — these links are opened from email, frequently on a phone.

**Definition of Done**
- [ ] Reset responses do not reveal whether an email exists.
- [ ] Policy requirements are visible before submission.
- [ ] Expired and used tokens are handled with a recovery path.
- [ ] Tokens never leak into titles, referrers, or analytics.
- [ ] Works on mobile from an email client.

---

## PF-42 — Logout, Consent, and Protocol Error Screens

| | |
|---|---|
| **Status** | TODO · **Gate** P1-10 · **Size** M · **Surface** backend |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Session & logout, `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `SECURITY/02` §5 |

**Steps**
1. Logout confirmation interstitial when there is no valid `id_token_hint` — never act on an unauthenticated GET (`P1-10`).
2. Post-logout screen confirming what was ended: this session, or all sessions.
3. "Log out of all sessions" affordance with a plain-language consequence.
4. **Protocol error screens** for the cases that must not redirect — an invalid `client_id` or an unregistered `redirect_uri`. These render an error page precisely because redirecting to an unvalidated URI *is* the open-redirect vulnerability (`P1-06`).
5. Error copy that helps the *developer* integrating (what was wrong, what to check) without leaking configuration detail to an anonymous visitor.
6. A consent screen only if and when consent is actually required; until then, do not build a screen that implies a step that does not exist.

**Definition of Done**
- [ ] Logout without a valid hint shows an interstitial rather than acting.
- [ ] Protocol errors render a page and never redirect.
- [ ] Error copy is actionable for integrators without disclosing configuration.
- [ ] Post-logout state is unambiguous about scope.

---

# Track F — Public Site

## PF-43 — Public Site Layout and Shared Visual Language

| | |
|---|---|
| **Status** | TODO · **Gate** P0-18 · **Size** M · **Surface** public-site |
| **Plan refs** | `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` § Design Direction Differences, `UI-UX/06-VISUAL-LANGUAGE.md`, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` |

**Goal** — A site that shares the brand language with the console and **shares no code with it** — the separation `PLAN/20` insists on.

**Steps**
1. Implement the design direction differences `UI-UX/20` specifies. This is a marketing surface, not an admin tool: the console's density-over-whitespace principle does not transfer.
2. Duplicate the brand-level tokens from `UI-UX/06` deliberately. Do **not** import the console's component library — sharing it would force compromises in both directions, which is exactly why the two are separate projects.
3. Header, footer, and navigation for an anonymous visitor, whose needs have almost nothing in common with an authenticated admin's (`UI-UX/03` scope note).
4. Meet the cross-page requirements in `UI-UX/20`, including performance and SEO baselines.
5. Accessibility to the same WCAG 2.1 AA standard as the console.
6. Responsive properly for mobile — unlike the console, this surface has no desktop assumption at all.

**Definition of Done**
- [ ] No code is shared with `console/`; only tokens are duplicated, deliberately.
- [ ] Layout matches `UI-UX/20`'s design direction.
- [ ] Lighthouse performance and SEO meet `UI-UX/20`'s bar.
- [ ] WCAG 2.1 AA on every shared layout element.
- [ ] Fully responsive to mobile.

---

## PF-44 — Landing Page

| | |
|---|---|
| **Status** | TODO · **Gate** P0-18 · **Size** L · **Surface** public-site |
| **Plan refs** | `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` § Detailed Spec: Landing Page, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`, `CLAUDE.md` |

**Steps**
1. Build to `UI-UX/20`'s detailed landing page spec, using `UI-UX/21`'s copy blueprints — which contain real example copy, not placeholders.
2. Apply the governance rule as a hard review gate: **no claim describes a capability that isn't shipped in the current phase**. This is the page most likely to violate it, since marketing copy naturally reaches for the roadmap.
3. Make the primary call to action reflect what actually exists — during Phase 0 that is documentation, not a signup.
4. Optimize the first paint; this is an SEO and bounce-rate surface (`PLAN/20`).
5. No console code, no console component imports.

**Definition of Done**
- [ ] Matches `UI-UX/20`'s spec and `UI-UX/21`'s copy blueprints.
- [ ] A capability audit confirms every claim maps to something shipped or explicitly labelled as planned.
- [ ] First paint meets the performance bar.

---

## PF-45 — About, Contact, and Changelog

| | |
|---|---|
| **Status** | TODO · **Gate** P0-18 · **Size** M · **Surface** public-site |
| **Plan refs** | `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` § Page Inventory, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` |

**Steps**
1. About page per `UI-UX/21`'s blueprint.
2. Contact page carrying the **responsible-disclosure channel** — `PLAN/20` requires external researchers to have a documented path, and it must exist from the first publish (`P0-19`).
3. Changelog rendered from MDX in the repo, docs-as-code like everything else.
4. Keep the public changelog distinct from `MEMORY/CHANGELOG.md`: this one is user-facing and never mentions an unshipped capability.
5. Any contact form is spam-resistant without a tracking-heavy third-party widget, consistent with the privacy-respecting analytics choice in `PLAN/20`.

**Definition of Done**
- [ ] All three pages match their `UI-UX/21` blueprints.
- [ ] A responsible-disclosure path is live and monitored.
- [ ] The changelog renders from repository MDX.
- [ ] No page claims an unshipped capability.

---

## PF-46 — Docs Shell and Generated API Reference

| | |
|---|---|
| **Status** | TODO · **Gate** P0-16, P0-18 · **Size** L · **Surface** public-site, docs |
| **Plan refs** | `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` § Docs Home, § API Reference, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § API Reference Generation, `CLAUDE.md` |

**Goal** — A docs experience good enough that fast search is not a differentiator but a baseline — `PLAN/20` calls it make-or-break for adoption.

**Steps**
1. Docs shell per `UI-UX/20`'s Docs Home spec: sidebar generated from folder structure, versioning, search.
2. **API reference rendered from `openapi/openapi.yaml`** (`P0-16`). Never hand-written — `CLAUDE.md` hard rule 5, and the whole reason the pipeline exists.
3. Verify the generation pipeline while the spec is still near-empty, so the plumbing is proven before there is content depending on it.
4. Docs versioning working with the current API version as default, and older versions reachable through their deprecation window (`PLAN/20`).
5. Search across all docs pages.
6. Code samples with copy buttons and syntax highlighting; make the copy interaction as reliable as `PF-11`'s, since integrators copy these constantly.
7. Meet `UI-UX/20`'s API Reference spec for how endpoints, parameters, and examples are presented.

**Definition of Done**
- [ ] The API reference is generated, never hand-maintained.
- [ ] A spec change regenerates the reference with no manual step.
- [ ] Versioning works with the current version as default.
- [ ] Search returns useful results across all docs.
- [ ] Code samples copy reliably.

---

## PF-47 — Security and Trust Page

| | |
|---|---|
| **Status** | TODO · **Gate** P5-03 · **Size** M · **Surface** public-site |
| **Plan refs** | `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § What Never Gets Published, `UI-UX/20`, `UI-UX/21` |

**Goal** — Communicate security posture without publishing defensive documentation. Deliberately gated on `P5-03` so it describes a system that has actually been tested.

**Steps**
1. Write at a marketing and trust level: TLS everywhere, asymmetric token signing, Argon2id hashing, audit logging, penetration testing cadence, responsible disclosure.
2. Honor `PLAN/20`'s prohibitions absolutely. **Never publish**: attack scenarios or mitigation mechanics from `SECURITY/02`; infrastructure topology from `PLAN/14`; anything from `PLAN/18-RISK-REGISTER.md`.
3. Document the disclosure program: scope, how to report, expected response time, safe-harbor language.
4. State compliance posture honestly — "SOC 2 in progress" if true; never a certification not held.
5. Security-owner review before publish, specifically checking that nothing internal leaked in.

**Definition of Done**
- [ ] Published and matching `UI-UX/20`'s spec.
- [ ] A review confirms no `SECURITY/02`, `PLAN/14` topology, or `PLAN/18` content appears.
- [ ] The disclosure program is fully documented.
- [ ] Every compliance claim is accurate.

---

# Track G — Frontend Quality

## PF-48 — Component Test Suite

| | |
|---|---|
| **Status** | TODO · **Depends on** Track A · **Size** L · **Surface** console |
| **Plan refs** | `PLAN/11-TESTING.md`, `PLAN/06-FRONTEND-ARCHITECTURE.md` § Testing |

**Steps**
1. Test every component in every documented state, since the workbench documents them and an undocumented state is where bugs live.
2. Prioritize the logic `PLAN/11` names for the frontend: form validation, redirect URI format, role key format.
3. Test the behaviors that are easy to regress silently: button width stability under loading, skeleton geometry matching loaded rows, focus restore on modal close, optimistic rollback on error.
4. Test the rules the design system enforces: a badge without text, a disabled button without a reason, a mixed save-pattern form — each should fail.
5. Contract-test against the generated client, so a backend change not reflected in the spec breaks CI before it breaks the console at runtime (`PLAN/06`).

**Definition of Done**
- [ ] Every component's documented states are covered.
- [ ] Design-system rule violations fail tests.
- [ ] Contract tests catch spec drift.
- [ ] The suite runs in CI on every PR.

---

## PF-49 — E2E Suite for Console Flows

| | |
|---|---|
| **Status** | TODO · **Depends on** Track D · **Size** L · **Surface** console |
| **Plan refs** | `PLAN/11-TESTING.md` § E2E, `PLAN/06-FRONTEND-ARCHITECTURE.md` § Testing, `UI-UX/04-USER-FLOWS.md` |

**Steps**
1. Cover the flows `PLAN/06` names as highest-risk: user invite plus first role assignment, Project Grant creation and revocation, session revocation.
2. Cover all five `UI-UX/04` flows end to end as their gates open.
3. Cover the console's own OIDC login, silent renewal, and logout (`P1-21`).
4. Cover organization switching, including the assertion that the previous organization's data is gone from the UI.
5. Cover permission gating: a user without a role cannot reach a route by direct URL.
6. Use resilient selectors and isolated test data, so the suite does not become the flaky thing everyone learns to re-run.

**Definition of Done**
- [ ] All five `UI-UX/04` flows are covered as their gates open.
- [ ] The three highest-risk flows from `PLAN/06` pass in CI.
- [ ] Permission gating is verified by direct URL navigation.
- [ ] The suite is stable across repeated runs.

---

## PF-50 — Accessibility: CI Automation and Manual Audit

| | |
|---|---|
| **Status** | TODO · **Depends on** Tracks D, E · **Size** L · **Surface** console, public-site |
| **Plan refs** | `UI-UX/13-ACCESSIBILITY.md` § Testing & Sign-off, `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 5 |

**Goal** — Pass the WCAG 2.1 AA audit that `PLAN/17` makes a Phase 5 gate. This task is `P5-11`'s implementation.

**Steps**
1. Automated axe-core checks across every screen in CI, from `PF-19`'s infrastructure.
2. Audit every screen against `UI-UX/17`'s detailed checklist.
3. **Manual screen-reader testing** across the highest-traffic flows — automated tooling catches a minority of real barriers.
4. Verify keyboard-only operation of every flow, including modals, typed-confirmation dialogs, and the Rego editor if Phase 4b shipped.
5. Verify contrast for every token pair in every state, in the default theme **and** under organization branding.
6. Verify no state is communicated by color alone.
7. Verify focus management: focus moves correctly into and out of modals and panels and is never lost to the document body.
8. Remediate everything found, and keep the CI checks so it cannot regress.

**Definition of Done**
- [ ] The console passes a WCAG 2.1 AA audit — `PLAN/17` Phase 5 criterion.
- [ ] Every screen meets `UI-UX/17`'s checklist.
- [ ] Screen-reader testing covers the highest-traffic flows.
- [ ] Every flow completes by keyboard alone.
- [ ] Contrast passes under organization branding too.
- [ ] Automated checks run in CI and block regressions.

---

## PF-51 — Visual Regression Testing

| | |
|---|---|
| **Status** | TODO · **Depends on** Track A · **Size** M · **Surface** console |
| **Plan refs** | `UI-UX/05-DESIGN-SYSTEM.md` § Governance, `UI-UX/12-RESPONSIVE-BEHAVIOR.md` |

**Steps**
1. Snapshot every workbench story at the four `UI-UX/12` test widths.
2. Snapshot in default and branded themes, and in light and dark if both are supported.
3. Snapshot the reduced-motion variant, since an animation that fails to fall back is otherwise invisible to testing.
4. Keep the review step human — an approved diff should be a decision, not a rubber stamp.
5. Accept that this catches unintended change, not badness. It is a guard on the design system's consistency, not a substitute for design review.

**Definition of Done**
- [ ] Every component is snapshotted at all four widths.
- [ ] Branded-theme snapshots exist.
- [ ] Diffs require explicit human approval.
- [ ] The suite runs in CI without becoming the bottleneck.

---

## PF-52 — Frontend Performance Budget

| | |
|---|---|
| **Status** | TODO · **Depends on** Tracks D, F · **Size** M · **Surface** console, public-site |
| **Plan refs** | `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 5, `UI-UX/12-RESPONSIVE-BEHAVIOR.md`, `UI-UX/14`, `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` |

**Goal** — `P5-12`'s implementation, with a budget enforced in CI rather than a one-time measurement that decays.

**Steps**
1. Set and enforce a bundle-size budget per route; the Users list must not download the Rego editor (`PF-12`).
2. Test every list screen with large datasets — thousands of users, hundreds of projects — and virtualize where needed.
3. Tune query staleness per resource type (`PF-20`).
4. Eliminate layout shift during loading; skeletons preserve final geometry (`UI-UX/09`, `UI-UX/14`).
5. Optimize the initial load path including the OIDC redirect round-trip — the first thing every user experiences.
6. Hold the public site to a stricter budget than the console: it is an SEO and bounce-rate surface, while the console is an application users are already committed to (`PLAN/20`).
7. Measure on a mid-range device at tablet width, not a developer machine.

**Definition of Done**
- [ ] Route-level bundle budgets are enforced in CI.
- [ ] List screens perform acceptably with large datasets.
- [ ] No layout shift during loading.
- [ ] The public site meets its stricter budget.
- [ ] Measurements come from a representative device.

---

## PF-53 — Frontend Acceptance Validation

| | |
|---|---|
| **Status** | TODO · **Depends on** all Phase F tasks · **Size** M · **Surface** all |
| **Plan refs** | `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md`, `UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` |

**Steps**
1. Verify every criterion in `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md` with recorded evidence.
2. Confirm every screen has a committed implementation-chain table with no blank rows.
3. Confirm the twelve console-wide rules hold across every screen, by audit rather than assumption — particularly the `color-danger` reservation and the role-source badge, which are easy to violate quietly on a late screen.
4. Confirm FR-14 from the frontend side: every console capability corresponds to a documented API capability, with no console-only shortcut.
5. Confirm no screen claims a capability beyond its shipped phase.
6. Write the Phase F summary in `MEMORY/`; update `PROGRESS.md`.

**Definition of Done**
- [ ] Every `UI-UX/17` criterion is verified with evidence.
- [ ] Every screen has a complete chain table.
- [ ] The console-wide rules audit passes.
- [ ] No console-only capability exists.
- [ ] A phase summary exists in `MEMORY/`.

---

## Phase F Exit Checklist

- [ ] Every component in `UI-UX/07-COMPONENT-SPECIFICATION.md` exists in the library with all documented states and its usage rule.
- [ ] All 21 console screens in `UI-UX/08-PAGE-SPECIFICATIONS.md` are implemented, each gated correctly on its backend task.
- [ ] All four hosted authentication screen groups work without client-side JavaScript and on mobile.
- [ ] The public site's five page groups are live, with the API reference generated rather than written.
- [ ] Every screen has a committed `UI-UX/19` implementation-chain table.
- [ ] The console passes WCAG 2.1 AA (`PLAN/17` Phase 5 gate).
- [ ] Component, E2E, accessibility, visual regression, and performance suites all run in CI.
- [ ] No UI surface claims a capability beyond its shipped phase.

# P1-22 — Console: Organization Overview, Projects, Applications

**Date**: 2026-09-10
**Branch**: `feat/P1-22-console-screens`
**Spec**: none required — but `docs/UI-UX/19`'s chain is mandatory, and it is committed alongside the code at [`console/docs/implementation-chain-P1-22.md`](../../console/docs/implementation-chain-P1-22.md)

---

## What this is

The first three screens with real content, and the design-system components they are built from: `Button`, `Table`, `Badge`, `Modal`, `ClientSecretModal`, and the four state components `docs/UI-UX/14` names.

The components came first and are the larger half of the work. That is the point of `docs/UI-UX/08` § Cross-Screen Requirements' rule — **no screen invents its own table** — and it pays immediately: `P1-23` and `P1-24` add screens rather than tables.

## The implementation chain, and why the table is committed

`docs/UI-UX/19`'s rule is that **"not applicable" is an acceptable answer; silence is not.** The failure it prevents is a screen whose Design and Interaction were specified and whose Loading, Error, Empty, Permission and Accessibility were invented by whoever implemented it — which is how two screens end up with two answers to the same question.

Writing it out found three things that would otherwise have been decided by accident:

- **The Applications list has no filter, so "filtered to empty" is not applicable** — recorded as not applicable rather than left blank, which is the difference between a considered omission and a forgotten one.
- **`ClientSecretModal` has no Loading and no Empty state**, because it fetches nothing and is not opened for a public client. A warning about a secret that does not exist is a warning about nothing.
- **The KPI cards' error state is per card**, because `docs/UI-UX/18` says so explicitly and the obvious implementation — one error boundary for the page — would have blanked a dashboard because one of four queries failed.

## The four states are a set, and the code makes that structural

`states.tsx` holds `Skeleton`, `ErrorState`, `EmptyState` and `LoadingRegion` in one file, because `docs/UI-UX/14`'s central point is that they belong together: a screen with a designed loading state and an improvised empty one is exactly what it exists to prevent. Importing from one file makes "which of these did I not think about" answerable by looking.

Two distinctions are enforced by the types rather than by discipline:

- **`EmptyState` takes `filtered`.** "No projects yet" with a create action and "No projects match that search" with a clear-search action are different copy *and* different recovery. Showing the first to somebody who has twelve projects and typed a typo is the failure.
- **`ErrorState` takes a kind**, and a `permission` refusal gets **no retry button**. The same request will be refused again; offering the button says otherwise.

## The client secret dialog

The one screen in the console that shows a credential, and every choice follows from the fact that the service genuinely cannot show it again (`P1-18`) rather than from a convention:

- **Not dismissible by Escape or by clicking away.** An accidental dismissal costs a rotation and a redeploy of every consumer using it.
- **`Done` is disabled until a checkbox is ticked.** A single button is one reflexive click from a lost secret. This is the design system's one deliberate friction point outside a destructive confirmation.
- **The value is readable, not masked.** A masked value the user cannot check is one they paste wrong and discover at the next deploy — and it is on their screen either way.
- **The copy confirmation is `aria-live`.** A visual "Copied" that says nothing to a screen reader is a confirmation only some users receive.
- **It is not opened for a public client**, because there is no secret and a warning about nothing trains people to dismiss warnings.

## `color-danger` stayed where it belongs

`CLAUDE.md` and `docs/UI-UX/06` reserve it for destructive and irreversible actions, and the tempting misuse was right here: a `deactivated` status badge in red, a "None" secret state in red.

Neither is destructive. A deactivated user is a statement about what something *is*, and a public client having no secret is correct rather than a problem. Both are muted. The `Badge` component has no danger tone at all, which makes the rule structural rather than remembered.

## Small decisions worth keeping

- **A KPI card is a real anchor when it has a destination and a plain `div` otherwise.** `docs/UI-UX/18` says a card must not carry affordance implying a destination it does not have — and the alternative implementation, a `div` with an `onClick`, is the one a keyboard user cannot reach at all.
- **The activity trend is a `table` with a caption and a number per row**, not a sparkline. A bar chart that a screen reader cannot read needs a table beside it anyway, so this is the table. The bar is reinforcement, marked `aria-hidden`.
- **The duplicate-name check runs in the browser and at the server.** The API is the authority (`P1-17` enforces uniqueness), and telling somebody before they submit is a kindness the server cannot offer.
- **Helper text is replaced by the error, never stacked under it** (`docs/UI-UX/07` § Form Field).
- **Secondary columns are dropped below 1024px rather than scrolling everything.** A table with a horizontal scrollbar at 768px is a table nobody scrolls.

## Found while building it

**The lint rule for inline styles caught the one place a runtime value belongs in one** — the activity bar's width, which is a proportion computed per render. A token cannot express "37% of the peak", and a class per percentage would be a hundred classes carrying no meaning. Disabled on the line with the reasoning above it, which is what the rule's own message asks for.

Worth noting because the first attempt put the reasoning *in* the disable comment, which pushed the directive four lines away from the offending line and produced both an unused-directive warning and the original error. The rule was right twice.

## Verification

- **Component, 19 tests**: the table's five states including both kinds of empty; a retry offered for a server failure and withheld for a refusal; the actions column having an accessible name; the table's caption as its accessible name; a loading button keeping its accessible name and refusing a second click; a badge carrying text; the secret dialog's acknowledgement gate, its resistance to Escape, its `aria-modal` labelling and its readable value; the validation-versus-network distinction; and axe checks on the table with rows and on the dialog.
- **The existing 131** still pass, including the shell and guard tests from `P1-21`.

## What this task did not build

- **Sorting and server-side pagination.** The lists fetch a page of 100 and filter in the browser. Honest at Phase 1 volumes and wrong later; the API already paginates, so the change is in the screen rather than the contract.
- **The organization settings summary** (`docs/UI-UX/18`'s fourth priority). The settings shape exists (`P1-16`), and rendering it is a screen of its own rather than a strip on the dashboard.
- **The side-panel modal variant.** `docs/UI-UX/07` specifies it for multi-step flows; `P1-23`'s invite flow is the first that needs one, and building it now would be a component nobody has run through the chain.

# 03 - Component Library

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: PF-03…PF-11, P1-22, P2-11, P2-12, P3-10, P4-05, P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

List every shared component, state its contract, and record the rule each one exists to
enforce. `docs/UI-UX/07-COMPONENT-SPECIFICATION.md` is the specification; this is what
was built and why each component refuses certain things.

## Scope

`console/src/components/`. Screens are [`12-SCREEN-INVENTORY.md`](./12-SCREEN-INVENTORY.md).

## As Built

### The governing rule

> **No screen invents its own table, modal, badge or empty state.**
> — `docs/UI-UX/07`, `docs/UI-UX/08` § Cross-Screen Requirements

This is not tidiness. A console is mostly tables, and a user who has learned where one
table's actions are, what its loading row looks like, and what happens when a filter
matches nothing has learned all of them. Two tables with two answers means neither can be
learned.

The enforcement is structural: `Table` handles loading, error, empty-because-nothing-exists
and empty-because-filtered **itself**, so a screen cannot have a designed loading state
and an improvised empty one.

### The components

| Component | Contract | The rule it enforces |
|---|---|---|
| `Table` | `caption`, `columns`, `rows`, `rowKey`, `status`, `errorKind`, `onRetry`, `filtered`, `what`, `onClearFilter`, `emptyAction`, `actions` | Owns all four states. Columns marked `secondary` drop at tablet width, so narrowing never produces a horizontal scrollbar nobody uses |
| `Button` | `variant` ∈ `primary`/`secondary`/`danger-text`, `loading` | Three variants and no more. A destructive button is never the visually dominant action on a screen where a non-destructive one is expected — "Deactivate user" is `danger-text` among ordinary actions, and full destructive weight is reserved for the confirmation, where it is the only thing on screen |
| `Badge` | `tone` ∈ `neutral`/`positive`/`muted`/`warning`, `children: string` | A badge is never the sole carrier of critical information — colour reinforces, text is the message. **`danger` is not a tone**: a status is a statement about what something *is*, and a deactivated user is not a destructive action |
| `Modal` | `open`, `title`, `onClose`, `children`, `footer`, `dismissible` | Interrupts. Takes focus on open, traps it, restores it on close. `role="dialog"`, `aria-modal`, labelled by its title |
| `SidePanel` | same shape, plus `progress` | Accompanies. Identical focus behaviour; the choice between the two is *interrupt vs. accompany*, not appearance. Destructive confirmations are always modals |
| `ConfirmDialog` | `open`, `title`, `consequence`, `verb`, `onConfirm`, `onCancel`, `busy`, `typeToConfirm` | States the consequence, not the rule. `typeToConfirm` locks the confirm button until an exact string is typed |
| `RoleSourceBadge` / `RoleList` | `roleKey`, `delegatedFrom?` | Where a role came from, wherever a role appears. A delegated role carries a link mark, a `title`, **and** the same sentence as `sr-only` text — hover is an affordance for a mouse and nothing else |
| `ProjectNav` | `projectId` | The project detail's tabs are **links between routes**, not an ARIA tab set — see below |
| `ClientSecretModal` | the secret, once | The only screen that shows a credential |
| `QrCode` | a module grid | Never the accessible form of the secret; the typed secret is always shown beside it |
| `states.tsx` | `Skeleton`, `ErrorState`, `EmptyState` | The four states live in one file because the point is that they are a **set** |

### Three decisions worth knowing before you copy a pattern

**`ProjectNav` is links, not tabs.** ARIA's tab pattern describes panels swapped inside one
document: arrow-key navigation, a single tab stop, `aria-controls` pointing at a panel
that is present. None of that is true here — each tab is a URL with its own data, its own
loading state and its own back-button behaviour. Marking links up as `role="tab"` would
promise a keyboard model the screen does not implement.

**`ErrorState` distinguishes four kinds.** A network failure, a server failure, a
permission refusal and a validation failure are different events with different
recoveries. Telling a user to "try again" when their input is wrong is advice that cannot
work, and offering retry on a permission refusal suggests the same request might succeed.
Only `network` and `server` get a retry button.

**`EmptyState` distinguishes two kinds of nothing.** "Genuinely empty" wants a create
action; "filtered to empty" wants the filter cleared. Showing "create your first project"
to somebody who has twelve and typed a typo is the failure this prevents.

`P4-06` found the third kind: a list this organization **cannot** add to. Granted Projects
passes explanatory copy as its `emptyAction` rather than a button, because "create the
first one" would be a dead end.

### When a component graduates to `components/`

When a **second** screen needs it — not when it looks reusable. The one deliberate
exception is `RoleSourceBadge`, built in `P2-12` for a case that could not occur until
Phase 4, because `docs/UI-UX/08` makes the badge mandatory wherever roles appear and
retrofitting it would have meant auditing every screen that shows a role.

### Shared components get fixed in `components/`, not worked around

`P4-05` found two defects in `ConfirmDialog` that only its first typed-confirmation caller
could expose: typed text survived a cancel (so reopening arrived already unlocked, the
friction spent before the new consequence was read), and the typed input never received
focus. Both were fixed in the component, with tests in the calling page that fail without
the fix.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| A shared component fetches nothing | no `lib/api` import | review; see [`01-APPLICATION-STRUCTURE.md`](./01-APPLICATION-STRUCTURE.md) |
| `color-danger` | `Button` (`danger-text`, `primary` inside a confirmation) and `ConfirmDialog` only | `console/src/components/components.test.tsx` |
| Dialog focus | taken on open, trapped, restored on close | `Modal`, `SidePanel` |
| Table columns at tablet | `secondary` columns drop | `console/src/components/Table.tsx` |
| Badge tones | no `danger` | `console/src/components/Badge.tsx` |

## Verification

- `console/src/components/components.test.tsx` — variants, tones, focus behaviour, and the
  danger-colour rule.
- Every screen test renders through these components and runs axe over the result.

## Not Yet Built / Open Questions

- **No toast/notification component.** Screens report success by re-rendering the changed
  data, which is sufficient so far and will not be for a long-running action.
- **No dedicated tab component**, deliberately — see `ProjectNav` above. If a real
  in-document tab set ever appears, it needs the full ARIA keyboard model.

## Related Documents

- [`08-LOADING-ERROR-EMPTY-STATES.md`](./08-LOADING-ERROR-EMPTY-STATES.md)
- [`09-ACCESSIBILITY-PRACTICE.md`](./09-ACCESSIBILITY-PRACTICE.md)
- [`../UI-UX/07-COMPONENT-SPECIFICATION.md`](../UI-UX/07-COMPONENT-SPECIFICATION.md)

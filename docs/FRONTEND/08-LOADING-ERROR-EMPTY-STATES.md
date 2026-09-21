# 08 - Loading, Error and Empty States

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: PF-05, PF-08, P1-22…P1-24, P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Record how the console handles the three states a screen spends most of its life in, and
why they are implemented as one set rather than three features.

`docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md` is the design intent. This is the
implementation and the enforcement.

## Scope

`console/src/components/states.tsx`, `console/src/components/Table.tsx`.

## As Built

### They live in one file because the point is that they are a set

`console/src/components/states.tsx` exports `Skeleton`, `ErrorState` and `EmptyState`.
The document's central claim is that a screen with a designed loading state and an
improvised empty one is the failure mode worth preventing; importing from one file makes
"which of these did I not think about" answerable by looking.

`Table` takes them further: it owns all four states itself, so a caller supplies `status`
and `what` and cannot produce an inconsistent screen by omission.

### Loading is a skeleton shaped like the result

Not a spinner. A spinner in the middle of an empty page, followed by content, is two
layout shifts; a skeleton the same shape as the result is none, and it tells the user what
kind of thing is arriving. `Table` renders three skeleton rows.

Inside a form, the same rule applies to a list that has not arrived: the role checklist
says "Loading this project's roles…" rather than rendering as empty, because an empty
checklist reads as "this project has no roles".

### Error distinguishes four kinds, and only two offer retry

| Kind | Message | Retry |
|---|---|---|
| `network` | "Could not reach the service. Check your connection. Nothing was changed." | yes |
| `server` | "Something went wrong. The service could not complete that. Trying again is safe." | yes |
| `permission` | "You do not have access to this. Your account does not carry the role this needs." | **no** |
| `validation` | "That could not be saved. Check the values below and try again." | **no** |

Telling a user to "try again" when their input is wrong is advice that cannot work.
Offering retry on a permission refusal suggests the same request might succeed, and it
will not — it will produce another audit event.

The kind is carried on the error object by the data layer (`asFailure` in
`console/src/lib/api/queries.ts`) rather than re-derived by each screen from a status code.

### An error replaces the table rather than floating over it

`docs/UI-UX/14` asks for last-known-good data where there is some. On a first load there
is none, and a blank table under an error banner reads as "there are none" — which is a
different and worse statement than "this failed".

### Empty distinguishes which kind of nothing

`EmptyState` takes `filtered`. "Genuinely empty" wants a create action; "filtered to
empty" wants the filter cleared. Showing "create your first project" to somebody who has
twelve and typed a typo is the failure this prevents.

`P4-06` found a third kind and handled it without adding a variant: **a list this
organization cannot add to.** Granted Projects passes explanatory copy as its
`emptyAction` instead of a button —

> Only another organization can grant a project to yours. When one does, it appears here
> with the roles you may assign.

— because "create the first one" would be a dead end, and the generic "Nothing has been
created here" would be actively wrong.

### A revoked or ended thing stays visible

Not a state in the component sense, but the same principle. A revoked Project Grant is
listed, marked, and offers no action; a partner who finds the roles simply gone learns
nothing, while one who sees "Ended" knows what happened and when.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| All four states | owned by `Table` | `console/src/components/Table.tsx` |
| Loading | skeleton matching the result geometry | `console/src/components/states.tsx` |
| Retry offered | `network` and `server` only | `console/src/components/states.tsx` |
| Error placement | in place of the table, not over it | `console/src/components/Table.tsx` |
| Empty | `filtered` decides the copy and the action | `console/src/components/states.tsx` |
| Error announcement | `role="alert"` | `console/src/components/states.tsx` |

## Verification

- `console/src/pages/screens.test.tsx` — every screen renders loading, error and empty.
- `console/src/components/components.test.tsx` — the four error kinds and their retry
  behaviour.
- `console/src/pages/grantedprojects.test.tsx` — the explanatory empty state, and that no
  create button is offered.

## Not Yet Built / Open Questions

- **No partial-failure state.** A screen whose primary query succeeds and whose secondary
  query fails renders the primary and a local message; there is no shared treatment for it.
- **No offline banner.** `refetchOnReconnect` handles recovery; nothing tells the user
  they are offline in the meantime.

## Related Documents

- [`03-COMPONENT-LIBRARY.md`](./03-COMPONENT-LIBRARY.md)
- [`04-STATE-AND-DATA-LAYER.md`](./04-STATE-AND-DATA-LAYER.md)
- [`../UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`](../UI-UX/14-EMPTY-LOADING-ERROR-STATES.md)

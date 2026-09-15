# P4-05 — Console: Project Grants Tab

| | |
|---|---|
| **Date** | 2026-09-15 |
| **Task** | `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-05 (`PF-36`) |
| **Phase** | Phase 4 — Enterprise Interop |
| **Surface** | console, plus one API field |
| **Branch** | `feat/P4-05-project-grants-tab` |
| **Status** | Complete |

**Implementation chain**: [`console/docs/implementation-chain-P4-05.md`](../../console/docs/implementation-chain-P4-05.md)
**Decisions**: ADR-026 (partner named by ID), ADR-025 (stricter of both policies, recorded here because it was decided with this card's questions)

---

## What shipped

- **Table**: organization name and ID, shared roles as badges with the withheld ones in
  words beneath ("Not shared: manager, owner"), holder count, status, created and revoked
  dates. Revoked grants stay listed and show "Ended" instead of a control.
- **Create**: organization ID, every project role as a checkbox marked "Shared" or
  "Not shared", a summary sentence that rewrites itself as boxes are ticked, and the
  `color-warning` notice "This Project Grant has no roles selected yet". Review opens a
  second step that repeats the summary and the withheld list before anything is sent.
- **Revoke**: a confirmation stating how many users hold a role through the grant. Above
  zero, the partner's exact name must be typed; the input takes focus on open.
- **API**: `ProjectGrant.holder_count`, from migration 036's `project_grant_holder_counts()`.

## Why the partner is an ID

Flow 2 says "search/select". No API lists other organizations, and building one for
this screen would let any organization administrator list every organization on the
instance. The project owner chose ID entry (ADR-026). The name appears in the table once
the grant exists, through P4-01's bounded name lookup.

## `holder_count`, and the bound on it

The console cannot see the partner's users, so the blast radius must come from the server.
`project_grant_holder_counts(uuid[])` is `SECURITY DEFINER` like P4-01's name lookup, and
bounded the same way: counts only, and only for grants whose `granting_org_id` is the
transaction's organization.

The test proves the bound can fail. The granting organization reads one row through the
same query that returns zero rows to a third organization, so the zero is the bound and not
a broken query. A first draft discarded the query's error and would have passed with the
function missing entirely.

A delegated `user_grants` row cannot be written yet: P2-03's `user_grants_delegation_closed`
trigger refuses `project_grant_id` until P4-02 replaces its body. The test disables that
trigger for its one owner-side insert and re-enables it immediately. When P4-02 lands, the
insert becomes a delegated assignment through the API.

The trigger's message still says "until P4-01"; the work it waits for is P4-02's. An
applied migration is not edited, so P4-02 corrects the message when it replaces the body.

## `ConfirmDialog` bugs this was the first to expose

1. **Typed text survived a cancel.** Cancel, then reopen the dialog for the same row
   (or another with the same name), and it arrived already unlocked.
2. **The typed input never took focus.** `Modal` focuses its panel on open.
   `docs/UI-UX/18` requires the input to take focus.

Both are fixed in the component. Each has a test that fails without the fix.

## Verification

| Check | Result |
|---|---|
| `internal/projectgrant` integration tests (real Postgres) | pass, including the holder count |
| Console `npm run check` (lint, typecheck, Vitest) | 270 tests pass; 15 new |
| Mutations: typed confirmation, reset on cancel, focus, warning colour, withheld roles, review step | 6 of 6 turned a test red |
| `e2e/projectgrants.spec.ts` against a real local stack | pass: create and revoke, each checked against the API |
| Full E2E suite | 32 of 33. The failure is **not this card's**: see below |

### On staging

Deployed 2026-09-15. Backup `staging-20260915T104255Z.dump` was taken first, migration 036
was applied as the owner role, and image `zed-auth:p4-05` came up healthy. A smoke run
against the live service used a throwaway administrator, application and partner
organization, all removed afterwards. It signed in through the hosted page and created a
role and a grant. The grant read back with `holder_count` 0 and the partner's name, was
revoked and read back as revoked, and the role was then deleted. 7 of 7 checks passed. The
console serves the new bundle, and deep links to `/projects/…/grants` answer. The public
API reference, through the tunnel, carries `holder_count`.

### The E2E failure found on the way

`consistency.spec.ts` › "a user created through the API appears in the console" fails
every time on a stack with more than 100 users. `useUsers` requests one page of 100,
oldest first, and ignores `page_info`. On a long-lived stack, the newest users, who are
the ones just invited, never appear in the list, and nothing says the list is incomplete.

This is a real console defect, not test data. It is fixed on its own branch rather than
folded into this card.

## Not done here

- The typed-confirmation branch against a real service: needs holders, which need `P4-02`.
- `PF-36` in the Phase F track is not updated. No page task on that track has been, since
  P2-11; the track's rows need a reconciliation pass of their own.
- A `PROJECT_OWNER` who is not an organization administrator cannot reach any project tab.
  The same limitation applies to every project tab and is unchanged here.

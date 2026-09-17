# Console lists silently stopped at 100 rows

| | |
|---|---|
| **Date** | 2026-09-17 |
| **Kind** | Bug fix, found by `P4-05`'s full E2E run |
| **Surface** | console |
| **Branch** | `fix/console-list-pagination` |

## The defect

Every list hook in `console/src/lib/api/queries.ts` asked for one page of 100 and ignored
`page_info`. It affected users, projects, applications, roles, Project Grants, the
organizations in the switcher, and sessions. Collections are ordered oldest first, so the
rows past 100 were the **newest**:

- **Users list:** an administrator who had just invited someone could not find them.
  Nothing said the list was incomplete.
- **Overview:** the active-user and pending-invite counts were computed from that one page.
  The local stack showed 100 active users in an organization of 128.
- **Roles and Project Grants:** the roles search, and the "Not shared" list, only covered
  the first page.

`consistency.spec.ts` › "a user created through the API appears in the console" failed
deterministically once the stack held more than 100 users.

## The fix

`collectPages` follows `next_page_token` until the collection ends, up to 20 pages of 100.
It reports whether it reached the end. Where the bound is reachable, the screen says so
when it is hit:

- **Users list:** "Showing the first N users, oldest first. Search by name or email to
  find anyone else."
- **Overview:** the user counts render as "N+" (announced as "at least").

The bound exists so that a server which keeps returning a token cannot hang a screen.

`useUserGrants` was left alone: that endpoint is not paginated.

## Verification

| Check | Result |
|---|---|
| `src/pages/pagination.test.tsx` | 6 tests: second page reached (users, roles), bound reported (users, overview), collector unit tests |
| Mutation: stop after the first page | 6 of 6 red |
| Mutation: remove the incomplete-list notice | its test red |
| Console `npm run check` | 276 pass |
| Full E2E suite, local stack | 33 of 33, including the previously failing consistency test |

# 10 - Frontend Testing

> Category: **Frontend Engineering** (`docs/FRONTEND/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: PF-12…PF-14, P0-15, P1-28, and every screen task &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Describe the three test layers the console actually has, the rule about where a test stubs,
and the discipline that stops a green suite from proving nothing.

## Scope

`console/src/**/*.test.{ts,tsx}`, `console/e2e/`, `console/src/test/`. The repository-wide
strategy is [`../TESTING/`](../TESTING/).

## As Built

### Three layers

| Layer | Where | Count today | What it proves |
|---|---|---|---|
| Unit | `console/src/lib/`, `console/src/styles/`, `console/src/branding/` | part of 290 | Pure logic: PKCE, token claims, contrast ratios, brandable-token guards |
| Component / screen | `console/src/pages/`, `console/src/components/`, `console/src/app/` | part of 290 | A screen renders each state, sends the body it claims to, and has no axe violations |
| End-to-end | `console/e2e/` | 34 tests in 11 specs | The console against the **real service**, checking the API afterwards |

`npm run check` in `console/` runs lint, typecheck and the first two layers. End-to-end is
`npm run e2e`, and `scripts/check.sh` runs it behind `CHECK_FULL=1` locally and always in
CI.

### The stubbing rule: stub `fetch`, never the query layer

Every component test replaces `globalThis.fetch` (`console/src/test/harness.tsx`). That
keeps the generated client, the bearer middleware and the envelope handling in the path.

> A test that mocks `useRoles` proves the component renders an array, which is not the
> thing that breaks.

The harness also records a real trap: the client hands `fetch` a `Request` object, so
`String(request)` is `"[object Request]"` — which silently matched no branch of any handler
and answered every call with the success body. Handlers read `input.url`.

### Screen tests assert the request body, not just the screen

The failure a Project Grants screen can commit is creating a grant with a role nobody
ticked. So the write path is stubbed with a recorder and the assertion is on the body:

```ts
expect(post?.body).toEqual({ user_id: "u1", role_keys: ["cashier"] });
```

`P4-06` adds the negative form of the same idea, which is stronger than asserting absence
in the DOM: the test records **every URL requested** and asserts the console never asks for
the granting project's roles at all. A screen that cannot learn a role exists cannot render
it.

### End-to-end asserts in both directions

Every E2E flow drives the console and then calls the Management API to check what actually
happened. A console that showed a grant the API does not hold would pass every on-screen
check.

The fixtures seed **through the Management API**, using the endpoints an administrator
would (`console/e2e/fixtures.ts`). A fixture that reached into the database would create
objects the API might refuse, and a suite built on those tests a system nobody can operate.

Two deliberate constraints in that layer:

- **Required environment, never a skip.** `required()` throws if `E2E_*` is missing. A
  suite that skips when its dependency is unreachable reports success having run nothing,
  and a green suite that ran no tests is a false statement everybody acts on.
- **The E2E administrator holds `ORG_ADMIN`, not `INSTANCE_OWNER`.** A suite running with
  the most powerful role in the system cannot notice a missing permission check. The
  consequence is real: `P4-06`'s receiving-side grant had to be seeded by SQL in
  `scripts/e2e-up.sh`, because the suite's administrator has no authority in the partner
  organization and therefore no API call that could create it.

### Retries are off locally

`console/playwright.config.ts`: retries only in CI, where an infrastructure blip is a
plausible cause and a human is not watching. A retried test hides a race rather than
fixing it. `forbidOnly` in CI, so a stray `test.only` cannot turn a suite of thirty into a
suite of one and stay green.

### Mutation discipline

The repository's recurring defect class is **vacuous verification** — a check that cannot
fail. Every card records mutations: the source is deliberately broken and the test that
should catch it is run.

`P4-06` ran five, each red: the list filter widened to either side, the names never
resolved, the `SECURITY DEFINER` lookup's bound removed, the ordering flipped, the holder
count dropped. `P4-05` ran six on the console side alone (typed confirmation, reset on
cancel, focus, warning colour, withheld roles, the review step).

This is not automated. It is a step in the task's Definition of Done, and the results go in
the record (`MEMORY/records/`).

### Tests found real bugs, which is the point

- The E2E suite found the console truncating a list at 100 rows, because the API orders
  oldest first and the **newest** rows were the missing ones.
- The utility test would have caught the `--text-*` namespace trap that shipped for one
  build.
- `P4-06` found a latent race in the `P4-05` spec: its heading selector also matched the
  empty state's "No Project Grants yet", so it had been passing on timing.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Stub at `fetch` | never at the query layer | `console/src/test/harness.tsx` |
| axe over every screen | 13 test files | `console/src/test/axe.ts` |
| E2E environment | required, never skipped | `console/e2e/fixtures.ts` |
| E2E seeding | through the Management API | `console/e2e/fixtures.ts` |
| E2E administrator | `ORG_ADMIN`, not `INSTANCE_OWNER` | `scripts/e2e-up.sh` |
| Retries | CI only | `console/playwright.config.ts` |
| Mutations | recorded per card | `MEMORY/records/` |

## Verification

- `npm run check` in `console/` — 290 tests across 18 files.
- `npm run e2e` in `console/` — 34 tests across 11 specs, against a stack from
  `scripts/e2e-up.sh`.
- `scripts/check.sh` — both, plus the three generation diffs.

## Not Yet Built / Open Questions

- **No coverage floor for the console.** The backend has per-package floors
  (`scripts/check-coverage.sh`); the console has none, so a screen shipped without a test
  fails nothing.
- **Mutation testing is manual.** It is disciplined and recorded, but a person chooses the
  mutations.
- **No visual regression testing.** Layout changes are caught by review.
- **Test data accumulates on the local E2E stack.** A spec that fails after a write can
  leave a row behind; specs are written to tolerate it (scoping to their own user) rather
  than to clean up.

## Related Documents

- [`09-ACCESSIBILITY-PRACTICE.md`](./09-ACCESSIBILITY-PRACTICE.md)
- [`../TESTING/`](../TESTING/)
- [`../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`](../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md)

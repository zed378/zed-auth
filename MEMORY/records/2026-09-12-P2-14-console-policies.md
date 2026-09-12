# P2-14 — Console: Policies (Access) Screen

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-14 |
| **Phase** | Phase 2 — RBAC & Multi-Tenancy |
| **Surface** | console, **plus a backend bug fix** |
| **Branch** | `feat/P2-14-console-policies` |
| **Status** | Complete — not yet on staging |

**Spec**: none required — chain committed at [`console/docs/implementation-chain-P2-14.md`](../../console/docs/implementation-chain-P2-14.md)

---

## A promise the API had been quietly breaking

`openapi/openapi.yaml` has said this about the settings PATCH since `P1-16`:

> `settings` is merged key by key, so an update naming one setting leaves the rest as they were.

`jsonb || jsonb` is a **shallow** merge. `{"password_policy": {"min_length": 16}}` replaced the whole `password_policy` object and discarded `require_uppercase` and `max_age_days`.

Raising `min_length` from 12 to 16 is an unambiguous tightening. It silently reset a deliberate `require_uppercase: false` back to the instance default and dropped a deliberate `max_age_days: 0`. Nothing reported it, and the policy in force afterwards looked exactly like the one the administrator thought they had.

Written as a failing test first, which named both losses precisely:

```
require_uppercase was discarded by an update that never mentioned it
max_age_days was discarded by an update that never mentioned it
```

Fixed with a **general** `jsonb_deep_merge` rather than a special case for the one nested key that exists today — hardcoding it would fix this instance and leave the next nested setting to rediscover the bug. Objects recurse; everything else is replaced, which is what keeps `allowed_login_methods` removable: a merge that concatenated arrays would make it impossible to ever take a login method away, and removal is the direction that matters for a security control.

plpgsql rather than SQL, because the function is recursive and a SQL function cannot reference itself at `CREATE` time.

This screen sends the whole document, so it would never have hit the bug. Every other consumer of the API would.

## A fourth copy of the defaults, avoided

Step 6 asks for "the current effective values alongside the editable fields", which for a setting an organization has never touched means the service default.

Those numbers already existed twice, deliberately — the `organizations.settings` column DEFAULT and `authn.DefaultPolicy`/`DefaultLoginPolicy` — and `authn/policy.go` already explains why neither is redundant. Restating them in the console would have made a fourth copy in the surface furthest from enforcement.

So they went into the contract as `default:`, which is where any consumer would look, and `gen-patterns.mjs` now emits them and the bounds to `console/src/lib/api/settings.gen.ts`. `TestSpecDefaultsMatchTheService` compares what the contract publishes to what the service applies.

The generated Go file is used by nothing but that test. It is a drift gate, not a source of truth — the distinction matters, and the file says so at the top.

**One side effect.** `default:` made `openapi-typescript` mark those properties required, which is right for a response and wrong for a partial PATCH body; the build broke immediately. `--default-non-nullable false` restores the previous typing, and it is in `package.json` so the regenerate-and-diff gate uses it too.

## The screen refuses to lie about multi-factor

`mfa_required` is accepted, validated and stored by the API today, and enforced by nothing until Phase 3.

Step 3 allows omitting it or labelling it explicitly. It is shown, with "Not enforced yet" beside the heading and a sentence underneath: *setting this changes nothing today*. An administrator who switched it on and believed their organization was protected would have been misled by the console rather than by the API, and `docs/UI-UX/21`'s governance rule exists for exactly that.

## Only reductions are confirmed

`docs/UI-UX/07` is explicit that friction has to stay rare to stay meaningful, so a confirmation on every save would make the confirmation furniture.

The dialog appears only when the change takes something away, and it names what: *sessions drop from 24 to 1 hours; anyone whose session is already older than 1 hour is signed out at their next request*.

The subtle one is password expiry. `0` means "never expires", so it is the **loosest** value rather than the tightest — comparing it numerically would read a change to 0 as a reduction and a change away from 0 as an improvement, both backwards. There is a test for that, and the mutation that removes the special case turns it red.

## Verified

| | |
|---|---|
| Backend | 3 new integration tests for the merge, the first written to fail against the old behaviour |
| Console | 16 new tests in `console/src/pages/policies.test.tsx` |
| Mutation | 7 controls reverted one at a time, each turning its own test red |
| Drift gates | 2 new Go tests tying the contract's published defaults and bounds to the service's |
| Accessibility | axe over the populated form; every section a real `fieldset` with a `legend` |
| Everything else | 230 console tests, the full Go suite, `tsc`, `eslint` — clean |

## Staging

Unchanged: the VM has been unreachable since the deploy key went with the session scratchpad. The backend fix is covered by integration tests against a real Postgres; the screen is covered by component tests. Neither has run on staging.

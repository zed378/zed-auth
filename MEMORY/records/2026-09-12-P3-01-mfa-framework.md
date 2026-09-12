# P3-01 — MFA Framework and Step-Up Architecture

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-01 |
| **Phase** | Phase 3 — Advanced Security (first task) |
| **Surface** | backend |
| **Branch** | `feat/P3-01-mfa-framework` |
| **Status** | Complete — **with one step deferred, stated below** |

**Spec**: [`MEMORY/specs/P3-01-mfa-framework.md`](../specs/P3-01-mfa-framework.md)

---

## A frame with no factor in it

This task builds nothing a user can see. TOTP is `P3-02` and WebAuthn is
`P3-05`, and both are implementations of one interface rather than parallel
systems — because two systems would mean two challenge steps, two rate limits,
two audit shapes, and two chances to get the partially-authenticated state
wrong.

The consequence is that the tests carry more weight than usual: there is no
screen to look at and no login to walk through.

## The object the spec calls the highest-risk one

The partially-authenticated state is, by construction, a thing that is almost a
session. Every property that makes a session useful is one it must not have, so
the design is mostly a list of refusals:

- **It authorizes exactly one operation** — answering its own challenge.
- **It carries no claims.** The user id lives server-side, keyed by an opaque
  handle. A client cannot name a different user by editing what it holds, which
  closes abuse case A-1 by *shape* rather than by a check somebody could forget.
- **The handle is hashed before it becomes a storage key**, for the reason a
  session token is (`PG-14`): a `KEYS`, a backup, or a slow-command log then
  hands out nothing usable.
- **It does not survive.** Five minutes, enforced by Redis's TTL rather than by
  a timestamp somebody has to remember to compare — a row with an `expires_at`
  keeps working if a query omits the predicate; a key with a TTL is gone.
- **A failed attempt does not extend it.** `KEEPTTL` on the write, with an
  integration test measuring the TTL across an attempt: an attacker who could
  refresh the clock by guessing wrong would have removed the time bound by using
  the thing it bounds.

And the DoD's "cannot obtain a token", proved structurally: a handle presented
to `session.Manager.Lookup` — the single place a browser credential becomes an
identity — is refused. Everything downstream of a session reaches identity
through there.

## `amr` is derived, never asserted

The task's goal sentence is that `amr` be "accurate enough that consumer
applications can make real step-up decisions". A consumer refusing a payment
unless `amr` contains `otp` is trusting this service to have actually
challenged.

So `AuthMethods` takes what was **used** and nothing else. The case a
convenience shortcut breaks has its own test: a user with TOTP enrolled who
signed in with a password alone carries `pwd`, and not `mfa`.

`mfa` appears only when two distinct **categories** were used. Two passkeys are
two credentials of one kind, and a consumer asking for `mfa` is asking whether
something other than one kind of secret was involved.

An architecture test fails if the `mfa` value appears anywhere but the one
function that derives it — the spec names this as a technical risk, because the
pressure to write it from what is *enrolled* will appear the first time a flow
is awkward.

## Step-up refuses a downgrade

`StepUp` is a subset test and deliberately not clever: a requirement for `hwk`
is not satisfied by `otp`. That is abuse case A-4, and it is the reason
`Type.AMR` maps the two factor types to different values in the first place —
if they collapsed to one, the distinction would be decorative.

## The mutation run found three tests that proved less than their names

Ten controls reverted, one at a time. Seven turned their own test red
immediately. **Three did not**, and in every case the check was redundant *with
a later one, for the input the test happened to use* — so the test proved the
outcome rather than the control:

| Control | Why the test passed without it | What the test asserts now |
|---|---|---|
| `Registry.Empty()` early return | With an empty registry every factor also fails its per-factor verifier lookup, so no challenge is issued either way | The factor store is **not read at all** — which is what the early return actually buys: a deployment with no factors pays nothing per login |
| The `FactorIDs` membership check | A factor id that does not exist is also refused by the store lookup underneath | A factor that **exists, is confirmed, and belongs to this user** but was enrolled after the challenge was issued |
| The early `Spent()` return | The later `Spent()` catches the same input | A challenge that is spent **and still present** — what a failed delete leaves behind — and that it never reaches the verifier |

All three tests were honest mistakes of the same kind, and none would have been
found by reading. This is the fourth time this session that mutation testing has
caught a test asserting an outcome it would have got anyway.

## What is deferred, and why

**Card step 2's login-flow wiring lands with `P3-03`.**

The step says "extend the login flow with a distinct MFA challenge step". The
framework, the state, the decision function and the rules are all here and
tested. What is not here is the branch in `internal/login`'s POST handler that
renders a challenge page instead of resuming the OAuth flow.

The reason is not effort. With no factor type registered there is nothing to
challenge with, so that branch would be **unreachable on every deployment** —
and an unreachable branch in the authentication path, shipped untested against a
real factor, is worse than the seam it plugs into. `P3-03` is "the challenge
step in the login flow" and brings TOTP with it, so the branch arrives with
something that can exercise it.

What this means today: the framework is inert. `Required` returns no challenge
when the registry is empty, and that is the deployment state until `P3-02`.
Nothing about an existing login changes.

`P3-03`'s card should absorb step 2 and step 4's wiring explicitly rather than
leaving it implied.

## Also

`internal/mfa` is on the coverage floor list at 89.2%. It belongs there for the
reason the list exists — a gap in this package is a security problem, not a
style one — and the floors now cover twelve packages.

## Verified

| | |
|---|---|
| Unit | 25 tests, including every abuse case reachable without a factor |
| Integration | 9 against real Redis and Postgres, including the TTL non-extension and the handle-is-not-a-session proof |
| Architecture | 5, reading the source: the schema's factor types are all implemented, the interface has exactly the five methods, `amr` is derived in one place, the handle is minted from randomness, and nothing logs factor material |
| Mutation | 10 controls, each turning its own test red — after three tests were rewritten to assert the control rather than the outcome |
| Coverage | 89.2%, above the floor |
| Gates | 46 passed |

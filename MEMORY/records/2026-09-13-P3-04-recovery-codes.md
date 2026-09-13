# P3-04 — Recovery Codes and the Lost-Device Process

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-04 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend + Management API + docs |
| **Branch** | `feat/P3-04-recovery-codes` |
| **Status** | Complete |

**Spec**: [`MEMORY/specs/P3-04-recovery-codes.md`](../specs/P3-04-recovery-codes.md)
**Runbook**: [`deploy/RUNBOOK-mfa-recovery.md`](../../deploy/RUNBOOK-mfa-recovery.md)

---

## The bug the compiler could not see

The framework's recovery seam was declared `Unspent(ctx, userID, orgID)` and
implemented `Unspent(ctx, orgID, userID)`. Both parameters are strings, so Go
accepted the implementation as satisfying the interface, and the call site
passed them in the declared order.

The effect would have been `WithTenant(ctx, orgID: <a user id>)` — RLS matching
nothing, every lookup returning empty, and **recovery codes silently never
offered to anybody**. No compile error, no panic, no failing test: a feature
that simply does not exist, on the path a user reaches when they have already
lost their phone.

It was found by reading the two signatures side by side while adding the
adapter, not by a test. So the test now exists: `fakeRecovery` records the
scope it was called with, and `TestARecoveryCodeCompletesAChallenge` asserts
`org=o1 user=u1` rather than only that the call happened. Everything in the
package is now `(ctx, orgID, userID)`, matching `EnrolledFactors.Confirmed`.

The general lesson is the narrow one: **two adjacent parameters of the same
type are a silent-failure machine**, and an interface does not protect you from
transposing them. Where the order matters, a test has to assert the order.

## A false claim, caught by asserting it

The code said the alphabet had "no character that can be misread for another".
That is simply wrong — RFC 4648 base32 is `A–Z` plus `2–7`, so it contains `O`,
`I` and `L`. A test written to assert the comment failed on the first run.

What is true is an **asymmetry**: the digits those letters get confused with —
`0`, `1`, `8`, `9` — never appear in a generated code. That turns a hazard into
a usability win, because a reader who sees `0` and types `0` can be given the
`O` that was printed: the repair is unambiguous, since `0` could not have been
there. `2`/`Z` and `5`/`S` are left alone, because both are valid and
"correcting" either would be a guess that could spend the wrong code.

The comment is now accurate and the forgiveness is real rather than claimed.

## Why not Argon2

`docs/PLAN/04` says recovery codes are "hashed with the same rigor as a
password". This stores SHA-256, and `PG-39` records the deviation.

A slow KDF defends **low-entropy** inputs by making enumeration expensive.
These are 80 bits from the CSPRNG: there is no candidate list, so SHA-256 and
Argon2 are equally uncrackable and the stronger-sounding one is not stronger.
Meanwhile a user holds ten codes and a submission is compared against all of
them — ten memory-hard computations per attempt, hundreds of megabytes touched,
on a path an attacker holding the password can drive. The control would have
become the cheapest way to exhaust the service.

The rigor is in the entropy of the code. That is the property that decides
whether a dump is exploitable.

## What a recovery login claims

`amr` gets `mfa` and **not** `otp`.

Not `otp` because that would be a lie: a consumer refusing a payment unless
`amr` contains `otp` is asking whether a one-time-password *device* was used,
and the user no longer has one. A session opened with a recovery code therefore
fails a step-up that demands `otp` — correctly, because the right next step for
that user is to enrol again rather than be waved through.

But `mfa` is true. RFC 8176 defines it as more than one factor, and a password
plus a held credential is two. Withholding it would make a recovery login
indistinguishable from a password-only one in the session record, which is
exactly the fact an incident review needs to see.

## The administrator path destroys and never mints

The endpoint returns no codes, no session, no credential of any kind. An
administrator who could mint a working credential for another account could
take that account over, and the takeover would look like an ordinary login —
a far greater power than the one being granted.

So it can only return somebody to enrolment. Everything else about that path is
procedural, and the runbook says so in those words rather than implying the code
handles it: **no technical control can tell an administrator who verified a
caller from one who was talked into it.** The runbook names the verification
requirement, names urgency as the most common pretext, and tells the reader to
stop if they cannot verify.

## The walkthrough

`docs/PLAN/17` requires the lost-device recovery to have been **walked
through**, not written down. It is `internal/login/recovery_integration_test.go`
— executed on every run rather than once from memory.

| Step | Result |
|---|---|
| Enrol a factor, then lose the device | Password alone is refused; challenge issued |
| The page offers a recovery code | Offered, because the user holds unspent codes |
| Type the code in lower case, with the printed dashes | Accepted — signed in |
| Use the same code again on a fresh login | Refused; a different code still works (positive control) |
| The session's `amr` | `mfa`, not `otp` |
| The audit log | One `user.mfa.recovery_used`; no row contains the code |
| A user with no codes | Not offered the option |
| Administrator reset (Path C) | Factor and all ten codes destroyed; password alone then signs in |

It found one thing: the audit query named `type` where the column is
`event_type`. A test asserting on a column that does not exist fails loudly,
which is the good case — the same query written into a monitoring dashboard
would have returned nothing and looked like "no recovery logins".

## The mutation run

Seventeen controls reverted, one at a time. Sixteen turned red immediately.

The one that did not was informative rather than a gap: widening the digit range
from `2–7` to `2–9` changed nothing, because `confusable` rewrites `8` and `9`
into letters *before* the switch sees them. The range had become redundant with
the map — for ASCII.

Its remaining job is real and was untested: a **non-ASCII** digit (fullwidth
`５`, Arabic-Indic `٥`) must be dropped rather than admitted, because that is
corruption rather than a misreading, and mapping it would be a guess that could
spend the wrong code. That test now exists and the mutation targets it. All
seventeen are red.

## Verified

| | |
|---|---|
| Unit | 14 on the code's shape, normalisation and hashing; 16 on the framework's recovery path |
| Integration | 11 against real Postgres — single use, the concurrent race, regeneration, RLS, the trigger, clearing |
| End to end | 8 against real Postgres and Redis: the whole walkthrough above |
| Mutation | 17 controls, each turning its own test red |

## What this does not deliver

- **Self-service display and regeneration** of one's own codes. `P3-12`, with
  the account screen and its "requires recent authentication" — the same place
  `P3-02`'s enrolment endpoint is waiting.
- **Automatic generation at enrolment** (card step 1's wording). The mechanism
  is here; there is no enrolment endpoint to call it from yet.
- **The low-count warning in a user interface** (F-4). The count is exposed and
  `RecoveryNotice` chooses the message; the screen that shows it is `P3-12`'s.

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad. Verified against the local Docker stack.

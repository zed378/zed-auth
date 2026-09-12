# P3-02 — TOTP Enrollment

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-02 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend |
| **Branch** | `feat/P3-02-totp-enrollment` |
| **Status** | Complete — **two steps belong to the endpoint that does not exist yet**, stated below |

**Spec**: [`MEMORY/specs/P3-02-totp-enrollment.md`](../specs/P3-02-totp-enrollment.md)

---

## A silent deviation, corrected

`P3-01` created `user_factors` with a `secret` column and a `confirmed_at`
timestamp. `docs/PLAN/04` § user_mfa_factors specifies **`user_mfa_factors`,
`secret_encrypted`, and a `status` enum**.

Neither shape is better. The plan's is the one written down, and `CLAUDE.md` is
explicit that a deviation should be a visible decision rather than a silent one.
This one was silent, so it is corrected rather than defended — migration
`20260912000028` renames the table, the column, and replaces the timestamp with
the enum.

A rename is not additive and `docs/PLAN/14` normally forbids it. Safe here for a
reason that will not recur: the table was created in the immediately preceding
commit, nothing reads or writes it, and no deployment has ever held a row. Doing
it now costs nothing; doing it after a secret is stored costs a two-step
migration and a window where both names exist.

The same migration adds what the plan names and `P3-01` left out:
`credential_id`, `public_key` and `sign_count` for `P3-05`, and a trigger on
`users.mfa_enabled`.

## The flag that would have drifted

`docs/PLAN/04` calls `users.mfa_enabled` "a fast denormalized flag for the login
path" with the factor table as "the source of truth". Nothing maintained it.

A flag that can disagree with its source is worse than no flag: the login path
takes a fast decision from a value nobody updates, and the disagreement is
invisible until somebody with MFA enrolled is let in without it. So it is
maintained by a trigger rather than by whichever code path remembers — an
**active** factor sets it, losing the last one clears it, and a **pending**
enrolment does neither.

## The algorithm is in the repository, and tested against the RFC

Thirty lines, frozen since 2011. A supply-chain compromise of a library that
computes second factors is a compromise of every second factor at once, and
`PG-27` (no SBOM) is open — which makes a new dependency in the authentication
path exactly the wrong place to spend.

The risk taken in exchange is that the implementation could be wrong, and wrong
here is invisible from this side: codes no authenticator app agrees with, every
enrolment failing for a reason nobody can see. So it is checked against **RFC
6238 Appendix B's own published vectors**, not against itself.

SHA-1 is not a mistake. RFC 6238 names HMAC-SHA1 as the default and every app
implements it; SHA-256 is permitted and not interoperable in practice.
HMAC-SHA1 does not rest on SHA-1's collision resistance, which is the broken
property.

## Replay: the counter, never the code

A code is valid for its whole 30-second step, so without a bound it works as
many times as it is presented — a shoulder-surfed code is a login.

What is recorded is the **counter**. The code is a live credential until its
step ends, and storing it to prevent the reuse of a credential would be storing
a credential. The counter is a small integer that says only when somebody last
authenticated, which `last_used_at` already says.

Strictly-greater rather than not-equal, so a code from an earlier step is
refused too — otherwise a captured code becomes usable again once the clock has
moved and the skew window reaches back.

Enforced by the `WHERE` clause, not by a read-then-write: two requests
presenting the same code at the same moment would both pass a read-then-write,
and only one can win an `UPDATE … WHERE last_used_counter < $2`.

And a replay is refused as a **wrong code**, deliberately. Saying "you already
used that one" tells an attacker their stolen code was real.

## Three errors that look alike

| Situation | Answer | Why |
|---|---|---|
| Wrong code | `ErrWrongCode` | The user's problem, and they can fix it |
| Replayed code | `ErrWrongCode` | See above |
| Key cannot open the secret | `ErrSealed` | The **operator's** problem — reporting it as a wrong code sends somebody to their recovery codes for a misconfiguration |

## The mutation run, again

Eleven controls reverted. Eight turned red immediately. **Three did not**, and
all three were the same pattern `P3-01` produced: a guard redundant with a later
check for the input the test used.

The difference this time is that the redundancy was **deliberate defence in
depth**, not a test gap — so the response was different:

| Guard | Resolution |
|---|---|
| `NewSealer`'s empty-key check | **Removed.** The length check already covered it. A guard no test can distinguish is a guard nobody can maintain |
| `VerifyTOTP`'s length check | **Kept, and the comment now says what it is** — an early-out, since `hmac.Equal` is length-sensitive anyway. It stays because computing three HMACs to reach an available answer is work an unauthenticated caller can ask for a million times |
| `Begin`'s configured-sealer check | **Kept, and documented.** `Seal` refuses too; failing here means no secret is generated at all, and a secret generated and discarded is one that briefly existed in memory for no reason |

The mutation list now targets the controls that decide, and all eleven turn red.

## Verified

| | |
|---|---|
| RFC conformance | 6 published vectors, plus the skew bound asserted from both directions (±1 accepted, ±2 refused) |
| Unit | 20 tests across the algorithm, the secret encoding, the provisioning URI and the sealing |
| Integration | 21 against real Postgres: the replay bound, the trigger, tenant isolation, the one-TOTP rule, and the whole enrolment path end to end |
| Mutation | 11 controls, each turning its own test red |
| Framework | The first run of `P3-01`'s `Required`/`Answer` against something that actually verifies — until now the only implementation was a fake |

## What this does not deliver

**Step 4 — "require the user's current password to begin enrollment" — is an
endpoint concern, and there is no enrolment endpoint.** The mechanism refuses
nothing on its own because it is not reached from outside. The requirement
belongs with the endpoint in `P3-12` and is recorded on that card rather than
quietly dropped.

**Step 8's audit call sites** likewise: the event names and payload rules are
specified in the spec (§14), and nothing emits them because nothing calls
enrolment.

Both are noted on the card with the same wording, so the next person reads the
gap rather than assuming it closed.

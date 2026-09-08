# Argon2id Password Hashing

**Date**: 2026-09-08
**Task**: `P1-01` — the first Phase 1 task
**Branch**: `feat/P1-01-password-hashing`
**Spec**: [`MEMORY/specs/P1-01-password-hashing.md`](../specs/P1-01-password-hashing.md)

---

## What Was Built

`internal/authn`: hash, verify, and a report of whether the stored hash is weaker than current parameters. Argon2id, PHC-encoded, per-row parameters.

No schema change — `P0-07` already created `users.password_hash` as nullable text with a comment saying it would hold exactly this. Nullable is load-bearing: a federated user has no password, and that has to be distinguishable from "has a password that is the empty string".

The package is unreferenced until `P1-12` wires it to a login endpoint. That is deliberate: a credential verifier with no callers is a thing that can be got right in isolation.

---

## The Parameters, and How They Were Chosen

| | |
|---|---|
| Memory | 64 MiB |
| Time | 3 passes |
| Parallelism | 4 |
| Salt | 16 bytes |
| Key | 32 bytes |

Three constraints, applied in order:

**Concurrency bounds this more than latency does.** Memory cost multiplies by simultaneous logins. At 64 MiB, a hundred concurrent logins want 6.4 GiB. That is why this is not the 1 GiB some recommendations suggest — those assume a machine doing nothing else, and the staging VM runs PostgreSQL, Redis, the service, the console and the public site.

**Latency second.** ~50–100ms of hashing is invisible next to a network round trip.

**Resistance within those bounds.** Memory cost is the defence that matters: GPUs have thousands of cores and little memory per core, so memory is what makes parallel cracking expensive. Raise it before raising time.

RFC 9106's second recommended option is 64 MiB / t=3 / p=4, and this matches it exactly. Worth stating that the constraints were applied first and landed there independently — mild evidence the reasoning was not nonsense, rather than the reasoning being retrofitted to a number copied from a document.

---

## Measured, Not Assumed

`P1-01` step 6 asks for a benchmark so the choice is a measured decision and can be re-measured. Both machines, because the difference is the point.

| | Hash | Verify | Memory/op |
|---|---|---|---|
| Development laptop (16 threads) | 63 ms | 81 ms | 67 MB |
| **Staging VM (4 cores, Broadwell)** | **90 ms** | **84 ms** | **67 MB** |

The VM is the number that matters, and it sits inside the 50–100ms target with room to spare.

**The concurrency limit on that VM is CPU, not memory.** 12 GiB available would hold ~190 concurrent hashes; four cores will run four at a time and queue the rest. So the practical bound is roughly 4 logins/90ms ≈ 44 per second before latency starts climbing — which is a number worth having before load reveals it, and which rate limiting (`P1-13`) should be set below rather than above.

---

## Enumeration by Timing

The failure this package spends the most care on. If a login for an unregistered address returns faster than one for a registered address, response time is an oracle for which addresses have accounts — a password-reset target list, a phishing list, and confirmation that a particular person works somewhere.

`VerifyDummy` performs a real Argon2 computation on the not-found path. Not a `sleep`: a sleep has to guess the right duration, gets it wrong as parameters change, and does not consume the CPU that makes timings match under load.

Measured ratio 0.88 — nonexistent user against real user, well inside the 0.5–2.0 tolerance. The tolerance is wide because this runs on shared CI hardware, and a tight bound would fail for reasons unrelated to the code.

**Verified by breaking it.** Replacing the dummy hash with an early return makes the test fail at ratio 0.00 — 0s against 72ms. The failure this test exists to catch is three orders of magnitude, not a few per cent, which is why a wide tolerance still catches it.

---

## Decisions Worth Recording

**A wrong password is not an error.** `Verify` returns `(Result{}, nil)` for a mismatch and an error only when the *stored hash* is unusable. The distinction is for the caller: the first is a user mistake, the second is data corruption that should reach somebody.

**A stronger-than-current hash is never flagged for rehash.** Otherwise a deploy that lowers parameters silently weakens every password that logs in afterwards — a downgrade nobody chose, applied one user at a time, invisible in any diff. Tested.

**Only cost-bearing parameters count as "weaker".** Salt and key length are not cost; treating a shorter salt as weaker would rehash the whole table for no gain.

**No bcrypt.** `PLAN/07` names it a fallback "if compatibility is needed" and `P1-01` step 5 says not to add it speculatively. There is no legacy system to migrate from, and adding it now means maintaining a verification path that accepts a weaker algorithm for a migration that may never happen.

**Zero parameters are rejected.** `argon2.IDKey` panics on `m=0`. A stored row is not trusted input just because it is ours — it can be corrupt, truncated by a bad migration, or written by something else, and a parser that panics on it is a denial of service reachable from whatever writes that column.

---

## What gosec Found

`G115`, twice: an unbounded `int -> uint32` conversion of the salt and key lengths read back from a stored hash. Medium confidence, high severity.

It was right, and the interesting part is what the right fix was. The lengths come from a database column, and a column is not trusted input just because it is ours — it can be corrupt, truncated by a bad migration, or written by something else. But the real problem was not the conversion; it was that **nothing bounded those lengths at all**. A four-byte salt is too weak to be genuine and a megabyte of it is not something this package ever wrote, so both are malformed hashes and should have been rejected before the conversion was reached.

Bounds went in: salt 8–64 bytes, key 16–64. That closes the overflow concern and an unbounded-allocation path together, and it is a behaviour change rather than a lint fix, so it has its own test — eleven cases across both boundaries.

gosec still flags the conversion, because its analysis does not follow the bound into it. That one gets `#nosec` with the invariant written out, which is a different thing from waiving an unexamined finding: the annotation records why the bound is sufficient, and a reviewer can check the claim in three lines.

**The lesson is about the shape of the fix.** The first instinct on a lint finding is to make the tool quiet. Reading it as a question — *what would have to be true for this conversion to be safe?* — produced a real bound the code was missing.

---

## Verified

| Check | Result |
|---|---|
| Round-trip, wrong password, unicode, empty | Pass |
| 18 malformed-hash cases | All rejected as errors; none panics, none matches |
| Rehash-on-login from a deliberately weak hash | Pass |
| Stronger hash not downgraded | Pass |
| No error message contains the password or a prefix | Pass |
| Nonexistent-user timing | Ratio 0.88; fails at 0.00 when short-circuited |
| **Coverage** | **95.9%**, against the 80% floor |

`internal/authn` is the first package the `P0-15` coverage floors actually apply to — they had been reporting "not built yet" since they were written, and activated on their own the moment the package appeared. That is the behaviour they were designed for, observed rather than assumed.

---

## Outstanding

- **Not wired to anything.** `P1-12` connects it to a login endpoint, and that is where rehash-on-login is actually performed and where the audit event is written. This package logs nothing by design — it is a pure function, and the caller owns the audit.
- **Password policy is `P1-02`**, deliberately not here. Minimum length, complexity and breached-password rejection are a different concern from storage, and mixing them would mean a hash function with an opinion about what you are allowed to hash.
- The benchmark should be re-run if the VM is resized. The numbers above are the baseline to compare against.

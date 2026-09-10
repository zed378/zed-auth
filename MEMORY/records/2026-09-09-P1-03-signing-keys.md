# Signing Keys, Rotation, and a Runbook That Was Wrong Until It Was Run

**Date**: 2026-09-09
**Task**: `P1-03`
**Branch**: `feat/P1-03-signing-keys`
**Spec**: [`MEMORY/specs/P1-03-signing-keys.md`](../specs/P1-03-signing-keys.md)

---

## What Was Built

`internal/signing`: key generation, a four-state lifecycle, signing, verification, JWKS, and a bounded cache. Plus `cmd/keyctl` for the operator and a runbook.

No schema change. `P0-07` created `signing_keys` with the four states, a partial unique index allowing one `current` per purpose, and a `CHECK` refusing PEM material in the reference column — written for this task, and none of it needed adjusting.

---

## The Four States Are the Design

```
generate ──▶ next ──rotate──▶ current ──rotate──▶ previous ──retire──▶ retired
             published,      signing            still verifies,      gone;
             not signing                        no longer signs      its tokens are dead
```

Two states would have been simpler and wrong in both directions.

**A key is published before it signs**, because consumers cache JWKS. If the first token signed with a new key arrives before the consumer has fetched that key, the consumer rejects a valid token. Publishing first removes the race rather than narrowing it.

**A key keeps verifying after it stops signing**, because a token issued one second before a rotation is valid for its full lifetime. Retiring immediately kills it. This overlap is what `docs/PLAN/09` means by an overlap period, and it is the difference between a rotation and an outage.

Rotation demotes *before* it promotes, because the partial unique index allows only one `current`. That constraint is doing real work: it makes "two keys signing at once" unrepresentable rather than merely unlikely, so "which key signed this token" stays a question with one answer.

---

## The kid Is Derived, Not Generated

RFC 7638 thumbprint of the public key. Three consequences, and the first is the one that would have hurt:

**It is stable.** The same key produces the same `kid` in every process and after every restart. A randomly generated `kid` would break every consumer's cached key set on every deploy — and would pass every test that only ever ran one process. `TestKeySetSurvivesReload` exists for exactly that failure.

**It cannot be chosen.** An attacker cannot craft a key whose thumbprint collides with a legitimate one without breaking SHA-256, so the `kid`-collision abuse case is closed structurally rather than by comparison.

**It is a function of the key alone**, so two deployments holding the same key agree on its name.

---

## Verification Pins the Algorithm

The two classic JWT attacks are both defeated by the same decision: **the algorithm is the server's policy, never the attacker-supplied header.**

`jose.ParseSigned` is given a fixed list — `{RS256, ES256}` — and rejects anything else *before* a key is looked up.

**`alg: none`** never reaches a code path that could accept an empty signature.

**Algorithm confusion** — a token with `alg: HS256` signed using the RSA *public* key as the HMAC secret — is rejected because the list contains no HMAC algorithm. This is the attack that makes publishing a public key dangerous if the verifier trusts the header: the key is public by definition, so anyone could mint tokens.

Both have tests that construct the forged token properly rather than asserting on a string.

There is no "try every key" fallback either. A token with no `kid` is rejected. Iterating over keys turns a verification failure into an oracle for how many keys exist, and it is how a retired key ends up accepted by an implementation that loops without re-checking status.

---

## The Finding: Token Encodings Are Malleable

A test failed that should have passed, and chasing it turned up something worth knowing.

An RSA-2048 signature is 256 bytes, which base64url-encodes to 342 characters. 342 × 6 = 2052 bits carrying 2048 bits of data, so the final character has **four bits that decode away** — and Go's base64 accepts them set to anything.

**Sixteen distinct token strings decode to the same signature, and all sixteen verify.** Measured, not reasoned about.

This is *not* a forgery risk. The signature still has to be valid, so nobody can change what a token says.

It is a **token identity** risk, and that part has teeth. Anything treating the token string as the token's identity sees sixteen tokens where there is one:

- **Refresh token reuse detection** (`P1-07`, `P3-02`). If reuse is detected by storing a hash of the presented string, an attacker who steals a refresh token presents it with a mutated final character, the hash does not match, and the reuse goes undetected — defeating the entire mechanism.
- A revocation denylist keyed by the token string.
- An idempotency or replay cache keyed the same way.

The fix in each case is to key on something canonical: the `jti` claim, or the *decoded* signature bytes. Never the raw string.

`TestSignatureEncodingIsMalleable` pins the property, so `P1-07` meets it as a documented constraint rather than discovering it in production. It fails loudly if a library upgrade makes the encoding strict — which would be good news, and would still want noticing.

The test also taught a smaller lesson twice. Its first version flipped the *last* character, which changes only padding bits, so a "tampered" token verified correctly and the test reported a security failure that was not one. Its second version guessed eight replacement characters and found none, silently skipping. Scanning the whole alphabet was the only version that measured anything.

---

## Running the Runbook Found a Real Bug

`P1-03`'s Definition of Done asks for the runbook to be executed once against staging. It was, and it failed in two ways the document could not have revealed by being read.

**There is no Go toolchain on the VM.** The runbook said `go run ./backend/cmd/keyctl`. `keyctl` now builds in a container and the runbook says so.

**The stored key reference pointed at a path only the tool could see.** `keyctl generate` records the path it wrote to; with the secrets mounted at `/secrets`, it stored `file:/secrets/jwt-signing-<kid>.pem`. The *service* mounts the same directory at `/etc/zed-auth/secrets`.

So the database held a reference resolving for the tool and not for the service. Nothing broke, because `P1-03` does not yet wire keys into the service — it would have broken at the next restart after `P1-07`, with no obvious connection to a rotation performed hours or weeks earlier.

The fix is not a warning in the document. The runbook now mounts the secrets **at the path the service uses**, so the reference is correct by construction. Getting this wrong is no longer something to remember; it is something you have to go out of your way to do.

The two existing keys were repaired with the one-line `UPDATE` the runbook now documents, and both were verified readable by uid 65532 at the recorded path.

---

## gosec, Again Worth Reading Rather Than Silencing

`G703`, path traversal, on the file `keyctl generate` writes. Today neither part can escape: the directory is operator configuration and a `kid` is a base64url thumbprint whose alphabet has no separator or dot.

But "today the value happens to be safe" is an observation about code that can change, not a property. The `kid` is now validated against the base64url alphabet, and the joined path is asserted to stay inside the directory. The `#nosec` that remains records the invariant those two checks establish, in ten lines a reviewer can verify.

Same shape as `P1-01`'s `G115`: reading the finding as *what would have to be true for this to be safe?* produced a bound the code was missing.

---

## Verified

| Check | Result |
|---|---|
| Sign and verify, RS256 and ES256 | Pass |
| Token signed before a rotation verifies after | Pass — unit and integration |
| Only `current` signs; `next`, `previous`, `retired` refuse | Pass |
| Retired key's tokens stop verifying | Pass |
| `alg: none` | Rejected |
| Algorithm confusion (HS256 with the RSA public key) | Rejected |
| Forged token with a legitimate `kid` | Rejected |
| Stripped, truncated, altered signature; tampered payload | All rejected |
| JWKS excludes retired keys, contains no private material | Pass |
| Database refuses two `current` keys | Pass |
| Schema refuses PEM material as a reference | Pass |
| Key set survives reload | Pass |
| **Runbook executed against staging** | **Two rotations, both keys published, service healthy throughout** |

---

## The Coverage Floor Was Measuring the Wrong Thing

`internal/signing` is cryptographic core, so it belongs behind a `P0-15` coverage floor. Adding it exposed a flaw in how those floors were measured.

The script ran `go test` without the integration tag. For this package that reports **44%** — the store, the rotation state machine and the database constraints are all integration-tested, and all of it was invisible. With the tag it is **83%**.

The second number is the real one, and the first would have driven exactly the wrong behaviour: a floor of 80% against unit tests alone rewards testing the pure functions and skipping the rotation logic, which is the part where a bug logs everyone out.

The floors now measure with `-tags=integration`. It is the same mistake as the console's design tokens — testing a compiler's input rather than its output — appearing in a different place.

Four tests were added to reach the floor, and each is a behaviour that was asserted in a comment and never checked: the cache serving its last good set when a reload fails, TTL expiry driving exactly one reload, JWKS byte-stability across calls, and `AlgorithmFor` rejecting a P-384 key claiming to be ES256.

---

## A Flake, Pinned Rather Than Tolerated

Adding a fourth integration package made the suite fail intermittently with `rootless Docker is not supported on Windows` — which reads like a configuration error and is not one.

Each package starts its own PostgreSQL and Redis, which is correct: separate test binaries cannot share a container. Four testcontainers providers initialising concurrently is what breaks, and it broke in a *different* package on each run.

That is precisely the shape of a failure people learn to re-run rather than read, and re-running until green is how a real bug eventually gets past a suite. `go test -p 1` for the integration tag, in `check.sh` and CI, with the reason written where the flag is.

The cost is wall-clock time. The alternative was a suite whose failures nobody believes.

---

## Outstanding

- **Not wired into the service.** `P1-04` publishes the JWKS endpoint and the discovery document; `P1-07` signs actual tokens. This package has no callers yet, which is the same deliberate choice as `P1-01`: a cryptographic core with no callers is a thing that can be got right in isolation.
- **`jwt-signing-current.pem` is still in the secrets directory**, left from the pre-`P1-03` arrangement. Nothing references it. It should be removed once `P1-04` confirms the service reads keys from the database, not from a configured path.
- **The 90-day cadence needs a calendar entry.** It is in the runbook, and a runbook is not a reminder.
- **`AUTH_JWT_SIGNING_KEY_REF` in `.env` is now vestigial.** The service will read keys from `signing_keys`; a single configured key reference is the thing this task replaced. `P1-04` should remove it and fail loudly if it is still set, rather than leaving two sources of truth.

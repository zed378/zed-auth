# P1-01 — Argon2id Password Hashing

Feature specification, per `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. `CLAUDE.md` requires one for anything touching authentication.

---

## 1. Business Objective

Store passwords so that a database disclosure does not become an account compromise, and so that the cost of cracking rises with hardware rather than being fixed at the moment the code was written.

The second half is what makes this more than "call a hash function". Parameters chosen today are wrong in three years, and a scheme that cannot raise them without a password reset for every user is a scheme that never gets raised.

## 2. Actors

| Actor | Interaction |
|---|---|
| End user | Sets a password; authenticates with it |
| The service | Hashes on set, verifies on login, silently upgrades on successful login |
| An attacker holding a database dump | Attempts offline cracking |
| An attacker without a dump | Attempts to learn whether an email is registered, by timing |

## 3. Functional Requirements

- **FR-1** Hash with Argon2id (`PLAN/09` § Passwords, `PLAN/07` § Cryptography).
- **FR-2** Encode parameters in the stored string using PHC format, so each row records the cost it was hashed at.
- **FR-3** Verify a password against a stored hash, honouring that row's parameters rather than the current ones.
- **FR-4** Report, on successful verification, whether the stored hash used weaker parameters than current — so the caller can rehash and store.
- **FR-5** Verification of a nonexistent user costs approximately the same wall-clock time as a real one.
- **FR-6** Reject malformed hashes as a verification failure, never as a panic or a pass.

**Not implemented**: bcrypt. `PLAN/07` names it a fallback "if compatibility is needed", and `P1-01` step 5 says not to add it speculatively. There is no legacy system to migrate from. Adding it now would mean a verification path that accepts a weaker algorithm, maintained for a migration that may never happen.

## 4. Non-Functional Requirements

| Concern | Requirement |
|---|---|
| Cost | ~50–100ms per hash on the target server. Fast enough not to be a login bottleneck, slow enough to hurt offline cracking |
| Memory | Argon2id's memory cost is its main defence against GPU attack; it must dominate the parameter choice |
| Measurability | A benchmark, so the parameters are a measured decision and can be re-measured on new hardware |
| Concurrency | Memory cost × concurrent logins must not exhaust the server. This bounds the parameter choice more than latency does |

## 5. Dependencies

`golang.org/x/crypto/argon2`. Nothing else — no new service, no schema change.

## 6. Database Changes

**None.** `users.password_hash` already exists as `text`, nullable, with a comment recording that it holds a PHC-encoded Argon2id string with per-row parameters. `P0-07` anticipated this.

Nullable is load-bearing: a federated or passwordless user has no password, and that must be distinguishable from "has a password that is the empty string".

## 7. API Contract

**None.** This is an internal package. It becomes reachable through `P1-12`'s login endpoint.

## 8. Frontend Changes

None.

## 9. Backend Changes

A new `internal/authn` package:

- `Hash(password string) (string, error)` — PHC-encoded output
- `Verify(encoded, password string) (Result, error)` — reports match and whether a rehash is warranted
- `Params` — the current parameter set, and the encoding/decoding of it

## 10. Authorization Rules

None at this layer. This package makes no authorization decisions; it answers whether a password matches a hash. Conflating the two is how "verified the password" becomes "authorized the request".

## 11. Validation

| Input | Rule |
|---|---|
| Password on hash | Non-empty. Length policy is `P1-02`, deliberately not here |
| Password on verify | Any value, including empty — an attacker controls this and it must not be a special case |
| Encoded hash | Must parse as PHC with the expected algorithm and parameter set; anything else is a verification failure |

Password **length** is capped at 1 KiB before hashing. Argon2's cost is dominated by memory and iterations rather than input length, but an unbounded input is still an unbounded read, and there is no legitimate 10 MB password.

## 12. Error Handling

Verification returns `(false, nil)` for a wrong password and `(false, err)` only when the *stored hash* is unusable. The distinction matters to the caller: the first is a user error, the second is data corruption that should page somebody.

No error, log line, or panic message carries the password or any prefix of it (`PLAN/09`: never log passwords, even failed attempts).

## 13. Edge Cases

| Case | Behaviour |
|---|---|
| Empty password on verify | Verified normally against the stored hash; fails. Not short-circuited — a short-circuit is a timing signal |
| `NULL` password_hash (federated user) | The caller must not reach verification. The package is given a string; a caller passing `""` gets a malformed-hash error, which is correct |
| Hash from a future, stronger parameter set | Verifies correctly. Rehash is *not* suggested — downgrading is worse than doing nothing |
| Unicode, emoji, very long passwords | Bytes are bytes. No normalisation, which would make two different passwords equal |
| Concurrent verification | Stateless and safe. Memory cost is per call, which is the concurrency bound in §4 |

## 14. Abuse Cases

Cross-referenced with `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.

| Abuse | Mitigation | Tested |
|---|---|---|
| Offline cracking after a database dump | Argon2id with a memory cost that makes GPU parallelism expensive; per-row parameters so cost rises over time | Benchmark |
| **User enumeration by timing** (§12) | Verification against a nonexistent user performs a real hash against a dummy, so both paths cost the same | Timing test with a defined tolerance |
| Timing attack on the comparison itself | `subtle.ConstantTimeCompare` | Unit test |
| Denial of service by expensive hashing | Parameters bounded so concurrent logins cannot exhaust memory; the 1 KiB input cap | Documented; rate limiting is `P1-13` |
| A malformed hash crashing the verifier | Every parse failure is an error, never a panic | Unit test with malformed inputs |
| Password reaching a log or an error | The password is never included in any returned error or log field | A test asserts no error string contains the input |

## 15. Logging / Audit Requirements

This package logs **nothing**. It is a pure function; the caller owns the audit event.

`PLAN/09` § Audit: the login attempt is audited by `P1-12` with its outcome, never with the credential.

## 16. Security Controls

- Argon2id, not Argon2i or Argon2d — the hybrid is what resists both side-channel and GPU attack.
- A per-hash random salt from `crypto/rand`. A failure to read randomness is a hard error, never a fallback to something weaker.
- Constant-time comparison of the derived key.
- Equal-cost verification for nonexistent users.
- Parameters in the hash, so raising them is a deploy rather than a migration.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Round-trip; wrong password; malformed hashes (truncated, wrong algorithm, bad base64, absurd parameters); empty input; the password never appearing in an error |
| Unit | Rehash-on-login, starting from a deliberately weak stored hash |
| Timing | Nonexistent-user verification within tolerance of a real one |
| Benchmark | Cost of the chosen parameters, recorded in the MEMORY record |
| Security package | The enumeration-by-timing case belongs in `tests/security/` with the other abuse cases |

## 18. Acceptance Criteria

Every item in `P1-01`'s Definition of Done, plus: the benchmark numbers are in the MEMORY record, so the next person to re-tune has a baseline rather than a guess.

## 19. Definition of Done

As the task card states.

## 20. Implementation Sequence

1. Parameters and PHC encoding, with tests for the encoding alone
2. `Hash`
3. `Verify`, including the malformed-hash cases
4. `NeedsRehash`
5. Equal-cost dummy verification
6. Benchmark, then tune parameters against its output
7. Timing test

## 21. Rollback Strategy

Nothing to roll back. No schema change, no stored data, no endpoint. The package is unreferenced until `P1-12`.

Once passwords exist, the rollback question inverts: parameters can be raised freely because each hash records its own, but they cannot be *lowered* for hashes already written at a higher cost — those still verify, they simply cost more.

## 22. Technical Risks

| Risk | Mitigation |
|---|---|
| Parameters tuned on a developer machine, wrong for the server | The benchmark is committed; re-run on the target and record it. The VM is smaller than a laptop, which is the direction that matters |
| Memory cost × concurrency exhausting the server | Bounded deliberately; the concurrency limit is stated in the record rather than discovered under load |
| A future rehash path that silently downgrades | `NeedsRehash` returns false for stronger-than-current hashes, and is tested |

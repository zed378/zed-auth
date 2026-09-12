# P3-02 — TOTP Enrollment

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-02 |
| **Depends on** | `P3-01` (the factor framework) |
| **Surface** | backend |
| **Written** | 2026-09-12 |

---

## 1. Business objective

The first real second factor. `P3-01` built a frame with nothing in it; this
puts an authenticator app behind it, which is the factor most users already have
and the one that needs no hardware purchase.

The card's goal sentence carries the requirement that shapes everything here:
**the shared secret is treated as the credential it is.** A TOTP secret is not a
configuration value. Anybody holding it can generate every future code for that
account, forever, without the user noticing.

## 2. Actors

| Actor | What they do |
|---|---|
| **End user** | Enrols an authenticator app, proves it works, later uses it at login |
| **Attacker with a live session** | Would enrol *their own* factor if allowed — §12 |
| **Attacker with a database dump** | Would hold every user's second factor if the secret were plaintext — §15 |
| **Operator** | Configures the encryption key, and must be told if it is missing rather than discovering it later |

## 3. Functional requirements

**FR-1** A cryptographically random secret, of a length RFC 4226 recommends.

**FR-2** Stored encrypted at rest in `user_mfa_factors.secret_encrypted`.

**FR-3** The provisioning URI and the secret are returned **once**, during
enrolment only.

**FR-4** The factor is not active until a generated code has been verified.

**FR-5** Clock skew tolerance of exactly one step each way.

**FR-6** A code already used cannot be accepted again within its window.

**FR-7** The secret is never logged and never returned after enrolment.

**FR-8** Enrolment start and completion are audited.

## 4. Non-functional requirements

- **Interoperability is the hard requirement.** An implementation that is
  self-consistent but disagrees with RFC 6238 produces codes no authenticator
  app accepts, and the symptom is every enrolment failing for a reason invisible
  from this side. Verified against the RFC's own published vectors.
- **Constant-time comparison** on the code. A timing difference across six
  digits is a meaningful oracle when an attacker may present a million of them.
- No new dependency. See §20.

## 5. Dependencies

`P3-01`'s `Verifier` interface, registry, and challenge machinery. This task is
an implementation of that interface and adds no new concept to the framework.

## 6. Data model changes

`P3-01` created `user_factors`. **`docs/PLAN/04` specifies `user_mfa_factors`
with `secret_encrypted` and a `status` enum**, and P3-01 diverged silently.
Migration `20260912000028` corrects it:

| Change | Why |
|---|---|
| `user_factors` → `user_mfa_factors` | The plan's name |
| `secret` → `secret_encrypted` | The plan's name, and it says what the column holds |
| `confirmed_at` → `status` (`pending` / `active`) | The plan's shape. A timestamp was carrying a boolean in a timestamp's clothes; `last_used_at` already answers "when" |
| `+ last_used_counter bigint` | The replay bound — see §12 |
| `+ credential_id`, `public_key`, `sign_count` | The plan names them; `P3-05` fills them. Better visible and unused than discovered the week WebAuthn lands |
| `+ trigger on users.mfa_enabled` | The plan calls it "a fast denormalized flag" with this table as "the source of truth" — see §12 |

A rename is not additive and `docs/PLAN/14` normally forbids it. It is safe here
for a reason that will not recur: the table was created in the immediately
preceding commit, nothing reads or writes it, and no deployment has ever held a
row.

## 7. API contract

None in this task. The enrolment endpoints are `P3-10`/`P3-12`'s surface; this
is the mechanism behind them.

## 8. Frontend changes

None. `P3-12` is the self-service screen.

## 9. Authorization rules

Enrolment acts on the caller's own account. An administrator cannot enrol a
factor for somebody else — there is no sane version of that, and `P3-10` gives
them read-only status instead.

**Step 4 requires recent authentication to begin enrolment**, and that is
enforced at the *endpoint*, which this task does not add. See §21.

## 10. Validation rules

- An unimplemented factor type is refused before the database sees it.
- A factor with no secret is refused: it would read as enrolled and verify
  nothing.
- One TOTP secret per user, by unique index. Not a limit on factors in general —
  several passkeys are several devices — but a second TOTP is not a second
  device, it is an older enrolment nobody removed.

## 11. Error handling

Three failures that look alike and must not be conflated:

| Situation | Answer | Why |
|---|---|---|
| Wrong code | `ErrWrongCode` | The user's problem, and they can fix it |
| Replayed code | `ErrWrongCode`, deliberately | Saying "you already used that" tells an attacker their stolen code was real |
| Key cannot open the secret | `ErrSealed` | The **operator's** problem. Reporting it as a wrong code sends somebody to their recovery codes for a misconfiguration |

## 12. Edge cases

- **Replay inside the window.** A code is valid for its whole 30-second step, so
  without a bound it works as many times as it is presented. The **counter** is
  recorded, never the code — the code is a live credential until its step ends,
  and storing it to prevent reuse of a credential would be storing a credential.
  Strictly-greater rather than not-equal, so a code from an earlier step is
  refused too.
- **The denormalized flag drifting.** `users.mfa_enabled` is maintained by a
  trigger rather than by whichever code path remembers. A flag that can disagree
  with its source is worse than no flag: the login path takes a fast decision
  from a value nobody maintains, and the disagreement is invisible until
  somebody with MFA enrolled is let in without it.
- **A half-finished enrolment.** Pending, and invisible to everything.
- **A user retyping a secret** — lower case, padding, spaces from a screen. All
  tolerated; refusing over case is a support ticket, not a control.

## 13. Abuse cases

| # | Abuse | Control |
|---|---|---|
| A-1 | Replay a shoulder-surfed code | The counter bound, enforced by a `WHERE` clause so two concurrent presentations cannot both win |
| A-2 | Read every second factor from a database dump | AES-256-GCM at rest; the column does not contain the plaintext or its base32 form |
| A-3 | Widen the accepted window | The skew is a constant with a test asserting both that ±1 works and that ±2 does not |
| A-4 | Enrol silently with a hijacked session | Fresh authentication required — at the endpoint, §21 |
| A-5 | Learn the secret after enrolment | No read path returns it; `Factor` has no field for it |

## 14. Logging and audit

`mfa.enrolled` and `mfa.confirmed`, carrying the factor **type** and never the
material. The architecture test from `P3-01` fails on any log call in this
package mentioning a secret or a code.

The audit call sites belong with the endpoints that trigger them (§21).

## 15. Security controls

- **Encryption at rest.** The threat closed is everything reaching the *data*
  without reaching the *process*: a stolen backup, a replica, a `pg_dump` in a
  ticket, an operator with SQL access and no business reading secrets. A key the
  service can read is a key an attacker who owns the service can read, and this
  does not pretend otherwise.
- **No key means refusal, never plaintext.** A service that stores secrets
  unencrypted because a key was missing has security depending on nobody having
  made a configuration mistake — and the mistake is invisible, because
  everything keeps working.
- Fresh nonce per seal. A counter shared across replicas of a stateless service
  is a counter that repeats, and a repeated GCM nonce is a total break.

## 16. Testing strategy

Unit against RFC 6238's vectors; integration against real Postgres for the
replay bound, the trigger, the tenant rules and the whole enrolment path;
mutation on every control.

## 17. Acceptance criteria

The card's Definition of Done.

## 18. Implementation sequence

Migration → TOTP algorithm → sealing → verifier → store. Each independently
testable.

## 19. Rollback strategy

The migration reverses. Rolling back past it loses nothing that exists today,
because no deployment has enrolled a factor.

## 20. Technical risks

**The algorithm is in the repository rather than in a dependency.** Thirty
lines, frozen since 2011, and a supply-chain compromise of a library that
computes second factors is a compromise of every second factor at once. `PG-27`
(no SBOM) is open, which makes a new dependency in the authentication path
exactly the wrong place to spend. The risk accepted in exchange is that the
implementation could be wrong — which is why it is tested against the RFC's own
vectors rather than against itself.

**SHA-1 is not a mistake.** RFC 6238 names HMAC-SHA1 as the default and every
app implements it; SHA-256 is permitted and not interoperable in practice.
HMAC-SHA1 does not rest on SHA-1's collision resistance, which is the broken
property.

## 21. What this task does not deliver

**Step 4 — "require the user's current password to begin enrollment" — is an
endpoint concern and there is no enrolment endpoint yet.** The mechanism refuses
nothing on its own, because it is not reached from outside; the requirement
lands with the endpoint in `P3-12`, and is recorded on that card rather than
silently dropped.

Likewise **step 8's audit call sites**: the events are specified and nothing
emits them, because nothing calls enrolment.

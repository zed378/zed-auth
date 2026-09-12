# P3-03 — TOTP Verification at Login

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-03 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend (the hosted login flow) |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Standard Login Flow, `docs/PLAN/04-DATA-MODEL.md` § sessions, `docs/PLAN/11-TESTING.md` § E2E, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3 |
| **Depends on** | `P3-01` (framework), `P3-02` (TOTP) |
| **Written** | 2026-09-12 |

---

## 1. Business objective

A stolen password stops being a login. `P3-02` made a factor storable and
verifiable; nothing asks for one. Until the login flow challenges, every
enrolled factor is decoration — the user believes they are protected and they
are not, which is worse than no MFA at all, because it changes what they do
with their password.

`docs/PLAN/11` names the test case literally: **correct password plus wrong
code is rejected**.

## 2. Actors

| Actor | Interest |
|---|---|
| A user with a confirmed factor | Signs in with a password and a code |
| A user with no factor | Sees no change whatsoever |
| An attacker holding a correct password | Must be stopped, and must learn as little as possible while being stopped |
| An operator | Needs MFA success and failure visible separately from password success and failure |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | After a password is proven, a user with at least one confirmed, answerable factor is shown a challenge step instead of being issued a session |
| F-2 | A session is created only after the challenge is answered |
| F-3 | A wrong code re-renders the challenge, counting against its attempt bound |
| F-4 | A challenge expires, after which the flow restarts at the password step |
| F-5 | Code attempts are bounded per challenge **and** per user across challenges |
| F-6 | A completed login records every factor used, and the `amr` claim follows |
| F-7 | MFA verification success and failure are audited as their own event types |

## 4. Non-functional requirements

- The challenge page is the login page's sibling: same CSP, same branding, same
  byte-identical-on-failure discipline (`P1-12`).
- No new round trip on the password-only path. A deployment with no enrolled
  factors does one extra Redis-free call (`Registry.Empty()`) and stops.
- The challenge step must work with JavaScript disabled, like the rest of the
  hosted login flow.

## 5. Dependencies

`P3-01`'s `Framework.Required` / `Framework.Answer`, `P3-02`'s `TOTP` verifier
and `PostgresFactors`, `P1-13`'s login rate limiter, `P1-12`'s page renderer
and CSRF, `authorize.Pending` and `Resume`.

## 6. Data model changes

**No table or column changes.** `sessions.auth_methods` already exists and
already carries the values; `user_mfa_factors` is `P3-02`'s. The challenge lives
in Redis only — it is short-lived state about an incomplete login, and a table
of half-finished logins is a table somebody has to garbage-collect.

One new Redis key: the per-user attempt counter (§ 10).

**One migration was needed and was not foreseen when this was written.**
`mfa.Verifier` is `Verify(ctx, factorID, code)`; a factor id does not name a
tenant, and `user_mfa_factors` is behind RLS. So `20260912000029` adds
`mfa_factor_org(uuid)` — SECURITY DEFINER, pinned search_path, granted only to
`auth_app`, returning an organization id and nothing else. Purely additive.

The related seam went the other way: `EnrolledFactors.Confirmed` used to resolve
the organization from the user id, which would have needed a SECOND privileged
function answering "which organization is this user in" for any id. Every caller
already holds the org — `Required` takes it as a parameter, and every other path
reads it from the challenge — so it is now passed instead. One privileged door
rather than two, and the one that remains is the one the shipped `Verifier`
interface genuinely requires.

## 7. API contract

No new REST endpoint. The challenge is a step inside the existing hosted flow:

```
POST /login          → password step; on success WITH a factor, renders the challenge
GET  /login/mfa      → re-render (a refresh, a back button); consumes nothing
POST /login/mfa      → the code step
```

The code step carries the pending authorization id in the form and the handle
in a cookie, and **the two are checked against each other**: a challenge is
issued for one authorization request and completes that one only. See § 13 A-7,
which the end-to-end test found rather than the design.

`openapi/openapi.yaml` is untouched: the hosted login flow is browser HTML, not
a documented API surface, and `docs/PLAN/05` describes it as a redirect flow
rather than as endpoints a client calls.

## 8. Frontend changes

A challenge template in the hosted login flow. **Not** the console — the
console never sees a password and must never see a code.

## 9. Authorization rules

The challenge authenticates; it authorizes nothing. The only rule that matters
is that a challenge can be answered **only for the user it was issued for**,
which `P3-01` enforces by holding the user id inside the challenge rather than
reading it from the request.

## 10. Validation rules

| Input | Rule |
|---|---|
| `code` | Exactly the digits the factor type expects; length-checked before any HMAC |
| `handle` | Opaque, from the cookie only — never from the form, never from the URL |
| `request` | The pending authorization id, as the password step |

**Per-user bound.** The framework's five-attempts-per-challenge is not enough on
its own: an attacker who holds the password can answer five codes, restart the
login, and answer five more, indefinitely. A six-digit code has a keyspace of
a million and a 30-second window, so the bound has to be across challenges,
not within one. A Redis counter keyed on the user id, with a window long
enough that exhausting it is slower than the code changes.

## 11. Error handling

Every challenge-step failure renders the **same page, same status, same
message**: wrong code, expired challenge, spent challenge, a factor removed
mid-flow. They differ only in what the operator's log says.

The one exception is a failure to *decide* — the factor store unreachable, a
seal key that will not open a secret. That is a 500, because reporting an
operator's misconfiguration as a wrong code sends a user to their recovery
codes for a problem they cannot fix.

## 12. Edge cases

| Case | Behaviour |
|---|---|
| Factor removed between challenge and answer | Refused as a wrong code; the challenge names ids and the store no longer has one |
| The user's only factor is of a type this build dropped | No challenge is issued at all (`Required` skips it), so a rollback does not lock everybody out |
| Two tabs, two challenges | Each has its own handle; answering one does not spend the other. The per-user counter bounds both |
| The browser blocks the handle cookie | The challenge cannot be continued and the flow restarts at the password step — the same outcome as an expiry |
| Password correct, challenge storage unreachable | The login is **refused**. A challenge that cannot be stored is one that cannot be enforced |

## 13. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | Brute-force the six-digit code | Five per challenge, plus a per-user bound across challenges (§ 10) |
| A-2 | Answer another user's challenge | The user id is inside the challenge, not in the request |
| A-3 | Replay a code within its 30-second window | `P3-02`'s counter bound |
| A-4 | Skip the challenge by going straight to `Resume` | `Resume` is reached from exactly one place, after the session exists; the pending request is not consumed until then |
| A-5 | Extend a challenge's life by answering wrongly | `Replace` with `KEEPTTL`, never a re-`Put` |
| A-6 | Use the challenge page as a password oracle | **Not closed.** See § 20 |
| A-7 | Complete authorization request B with a challenge issued for request A | `Challenge.PendingID` is compared with the form's request id **before** the code is verified — before, so a mismatch does not spend the user's code |

## 14. Logging and audit

Three new event types, distinct from `user.login.success` /
`user.login.failed`: `user.mfa.success`, `user.mfa.failed`, and
`user.mfa.challenged`. The payload carries the factor **type**, never the
factor's material and never the submitted code.

The third goes beyond the card and is the reason the other two are worth having
at all: an attacker holding a **working** password who is stopped by the factor
step leaves no trace whatsoever unless they also guess wrong at least once.
`user.mfa.challenged` is the line that says a credential is already lost, which
is the most actionable thing this flow can tell an operator.

A login that completes through a challenge writes `login.succeeded` as it does
today, with `auth_methods` showing what was actually used — so the audit log
alone answers "was this login second-factored".

## 15. Security controls

- Handle in an `HttpOnly`, `Secure`, `SameSite=Lax`, host-only cookie, never in
  the HTML and never in a URL. A handle in a query string reaches the referrer
  header, the browser history, and the access log.
- The handle stored in Redis is SHA-256 hashed (`P3-01`), so a Redis dump is
  not a set of usable challenge tokens.
- Constant-time code comparison (`P3-02`).
- The challenge is consumed on success, so it cannot be answered twice.
- No session cookie is set until the challenge completes.

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | The handler's branch table: factor / no factor / wrong code / expired / spent |
| Integration | Against real Postgres and Redis: the `docs/PLAN/11` case end to end — correct password, wrong code, no session cookie, no code issued |
| Security | A-1 through A-5 each as a test that fails when its control is reverted |
| E2E | Playwright: enrol, sign out, sign in with a code |
| Mutation | Every control above reverted one at a time |

## 17. Acceptance criteria

The card's Definition of Done, plus: a user with no factor observes no
behavioural change, proven by the existing login tests continuing to pass
unmodified.

## 18. Implementation sequence

1. The per-user attempt bound (it is the control the rest leans on).
2. `Required` wired into `authenticate`, before session creation.
3. The challenge page and its `POST` handler.
4. `auth_methods` from `mfa.AuthMethods`, and the `amr` claim behind it.
5. The two audit event types.
6. Tests, then the mutation run.

**What the sequence missed**: the two-transaction split. `authenticate` did
everything in one Postgres transaction, and the factor decision reads Redis and
opens its own tenant-scoped read — holding a transaction open across a call to
another service is how a connection pool is exhausted by something that is not
the database's fault. It is now `verify` → factor decision → `issue`, and
ADR-012's property is unchanged: the session and its audit record are still one
transaction.

## 19. Rollback strategy

No migration, so a rollback is a redeploy. A build without the TOTP verifier
registered issues no challenges (`Registry.Empty()`), which means a rollback
degrades to password-only rather than locking enrolled users out — deliberate,
and the reason `Required` checks the registry rather than the table.

## 20. Technical risks

**The challenge page discloses that a password was correct.** This is a real
regression introduced by this task and it is not closed here.

Today `/login` answers every failure identically, so an attacker with a
password list learns nothing about which passwords are right. With a sequential
challenge step, *reaching* the code page says the password was correct — the
page itself is the oracle, whatever it says on it.

Closing it requires a **decoy challenge**: issuing a challenge for a wrong
password too, so the step reveals nothing. That is implementable and it is
rejected, because the cost falls on the wrong person — every user who mistypes
their password, on a deployment where most have no factor at all, would be
asked for a code that does not exist. The disclosure is bounded by `P1-13`'s
existing limiter, which counts a failed password attempt here exactly as it
does today.

Every sequential-MFA implementation in wide deployment makes this same trade.
It is recorded as a plan gap rather than left in a comment, because the next
person should find a decision, not a silence.

**A deployment can leave MFA off and not notice.** Mitigated rather than
removed: `AUTH_MFA_SEAL_KEY_REF` unset means no framework at all — no challenge,
no enrolment, `users.mfa_enabled` false for everybody — so nobody believes they
hold a protection they do not have, and the startup log says so once in the
place ADR-015 requires. A key that is SET and unreadable is fatal at startup,
because that is the configuration where users would silently sign in on a
password alone.

**Per-user counter false positives.** A shared counter means one user's
attacker can lock that user out of their own code step. Mitigated by keying on
the user rather than the address and by a window measured in minutes, not
hours — the same cooldown-not-lockout shape `docs/PLAN/05` requires of the
password limiter.

## 21. What this task does not deliver

- **"Remember this device"** (card step 6). The card itself says to omit it
  rather than ship a weak version, and a device token is only as good as the
  screen that revokes it — that screen is `P3-11`. Carried there.
- **Demanding** a factor of a user who has none (`mfa_required`). That is
  `P3-07`, and it needs a transition designed for the users who have not
  enrolled yet.
- **Recovery codes** (`P3-04`). Until they exist, a lost device is a support
  ticket, and this task must not pretend otherwise in any user-facing copy.

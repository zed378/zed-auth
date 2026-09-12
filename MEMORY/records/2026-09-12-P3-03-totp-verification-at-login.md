# P3-03 — TOTP Verification at Login

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-03 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend (the hosted login flow) |
| **Branch** | `feat/P3-03-totp-verification-at-login` |
| **Status** | Complete — **one requirement is specified more strongly than any sequential flow can satisfy**, recorded as `PG-38` |

**Spec**: [`MEMORY/specs/P3-03-totp-verification-at-login.md`](../specs/P3-03-totp-verification-at-login.md)

---

## The step that makes every enrolled factor mean something

`P3-02` made a factor storable, sealed and verifiable. Nothing asked for one.
Until this task, a user who enrolled TOTP was in the worst available state:
they believed they were protected, they behaved accordingly with their
password, and they were not.

`docs/PLAN/11` names the test case literally — correct password, wrong code,
rejected — and it is the first test in the file, asserting on what the browser
is handed rather than on which branch ran.

## A requirement that cannot be met, said out loud

Step 4 asks that "whether the password or the code was wrong must not be
distinguishable to an attacker probing the flow."

In a sequential flow that cannot be done by choosing words. **Reaching the code
page is the disclosure** — it appears only when the password was right,
whatever is written on it. And `docs/PLAN/05` specifies exactly that sequential
shape, so the plan asks for a flow and then asks for a property the flow does
not have.

The two ways to actually close it both cost more than they are worth here:

| Option | Cost |
|---|---|
| A **decoy challenge** for wrong passwords too | Every user who mistypes a password is asked for a code, including the majority who have enrolled none. The cost lands on people who did nothing wrong, to deny an attacker something `P1-13`'s limiter already makes expensive |
| A **single-step form** | Breaks the plan's flow, and the page cannot know which factors to offer before the password is proven |

So: the sequential flow, no decoy, with uniformity enforced **within** the
step — wrong code, unknown factor type, expired challenge and spent challenge
all render identically. The residual is the step's existence.

It is `PG-38` rather than a comment, because the card asks for something
stronger than what was built and the next person should find a decision rather
than a silence.

## The bug the end-to-end test found

`Challenge.PendingID` exists so that "a challenge answered in one login cannot
complete a different one". Nothing compared it.

The handler took the pending request from the **form** and the challenge from
the cookie, and never asked whether they were the same login. A handle obtained
from one authorization request would complete another — a second factor
legitimately proven, attached to an authorization somebody else started.

It was not caught by any unit test, because the fake framework had no opinion
about which request it belonged to. It was caught by `TestAChallengeCompletes
OnlyTheLoginItWasIssuedFor` on the first run against the real chain.

The check now lives in `Framework.AnswerType`, which takes the pending id, and
it runs **before** the verifier. Before, because a correct answer refused after
the fact would already have recorded its counter — the user's next real code
would be the one after a step they never used.

## The bound that makes the other bound mean something

`MaxAttempts` caps one challenge at five guesses. On its own that is not a
bound: an attacker holding the password abandons the challenge and starts
another for free.

So the bound is on the **user**, across challenges, and outlives the challenge
that produced it. Ten guesses per fifteen minutes, with the arithmetic written
in the source rather than left to be worked out:

```
10 / 15 min = 960/day;  960 × 3/10⁶ ≈ 0.3% chance per day of a guess landing
```

That residual is real and it is stated. It is also the bound acting on somebody
who **already has the password**, and every failure writes `user.mfa.failed`.

It **fails closed**, which is the opposite of `ratelimit.Quotas` and would
normally be a hard call. It costs nothing here: the challenge lives in the same
Redis, so a store that cannot count cannot hold a challenge either — the login
has already failed by the time this refuses. Failing closed only stops an
outage from quietly removing the one bound on guessing a six-digit number.

Enforced inside `Framework.Answer` rather than in the handler, so no caller can
skip it — including `P3-12`'s enrolment confirmation and whatever `P3-05` adds.

## Two transactions, and what the split had to preserve

`authenticate` did everything in one Postgres transaction. The factor decision
reads Redis and opens its own tenant-scoped read, and holding a transaction
open across a call to another service is how a pool is exhausted by something
that is not the database's fault.

It is now `verify` → factor decision → `issue`. ADR-012's property survives
intact: the session and its audit record are still written in one transaction.
What moved out is verification, which writes nothing but a rehash and has no
invariant with the session that follows it.

## One privileged door, not two

`mfa.Verifier` is `Verify(ctx, factorID, code)` — a factor id, which does not
name a tenant, against a table behind RLS. Migration `20260912000029` adds
`mfa_factor_org(uuid)`: SECURITY DEFINER, pinned search_path, granted only to
`auth_app`, returning an organization id **and nothing else**.

The other seam went the other way. `EnrolledFactors.Confirmed` resolved the org
from a **user** id, which would have meant a second privileged function
answering "which organization is this user in" for any id. Every caller already
holds the org, so it is passed instead. The privilege that remains is the one
the shipped interface genuinely requires; the one that was avoidable was
avoided.

## The handle

It never reaches the HTML, a URL, or a log. `__Host-` prefixed, `HttpOnly`,
`Secure`, `SameSite=Lax`, read from the cookie and nowhere else — asserted
against the **source**, not against one response, because the claim is about
every path.

A handle in a query string reaches the Referer header, the browser history, and
every access log in between, and it is the one value standing between a proven
password and a session.

## An audit event the card did not ask for

`user.mfa.challenged`, alongside the two the card names.

Without it, an attacker holding a **working** password who is stopped by the
factor step leaves no trace at all unless they also guess wrong at least once.
That line — "somebody with a valid password reached the factor step" — is the
most actionable thing this flow can tell an operator, because it says a
credential is already lost rather than that somebody is hunting for one.

## Verified

| | |
|---|---|
| Unit | 15 across the handler's branch table, driven by a fake framework |
| Architecture | 5 reading `main.go` and `server.go`: the framework is built, the route is registered, the attempt bound is set, MFA is not half-enabled, the handle is cookie-only |
| Integration | 9 against real Postgres and Redis, with a real sealed secret and codes computed as an authenticator app computes them — including abuse cases A-1, A-2, A-3, A-5 and A-7 |
| Redis | 8 on the attempt bound: across challenges, per user, cooldown not lockout, TTL present, 25 concurrent failures all counted, fails closed, and the verifier does not run for an exhausted caller |
| Mutation | **17 controls, each turning its own test red** |

Not yet on staging: the VM at 10.1.200.13 remains unreachable since the deploy
key was lost with a session scratchpad.

## What this does not deliver

- **"Remember this device"** (step 6). The card itself says to omit it rather
  than ship a weak version, and a device token is only as good as the screen
  that revokes it — that screen is `P3-11`. Carried there.
- **Demanding** a factor of a user who has none. `P3-07`, and it needs a
  transition designed for the people who have not enrolled yet.
- **Recovery codes** (`P3-04`). Until they exist a lost device is a support
  ticket, and no user-facing copy here pretends otherwise.
- **Enrolment from the console** (`P3-12`), which still carries `P3-02`'s two
  deferred steps.

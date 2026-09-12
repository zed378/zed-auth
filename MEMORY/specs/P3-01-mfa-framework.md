# P3-01 — MFA Framework and Step-Up Architecture

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-01 |
| **Depends on** | `P1-11` (sessions with `auth_methods`), `P2-10` (per-organization policy) |
| **Surface** | backend |
| **Written** | 2026-09-12 |

---

## 1. Business objective

A password alone is one secret, and the whole industry's incident history is
about that secret being reused, phished or leaked. Multi-factor authentication
is the control, and it is table stakes for any organization this service would
be sold to.

This task builds none of the factors. It builds the **frame** they plug into,
because `P3-02` (TOTP) and `P3-05` (WebAuthn) arriving as parallel systems would
mean two challenge steps, two rate limits, two audit shapes, and two chances to
get the partially-authenticated state wrong.

The measurable objective is `P3-01`'s own goal sentence: `amr` and
`auth_methods` accurate enough that **a consumer application can make a real
step-up decision**. A consumer refusing a payment unless `amr` contains `otp` is
trusting this service to have actually challenged.

## 2. Actors

| Actor | What they do here |
|---|---|
| **End user** | Completes a challenge during login; later enrols and removes factors (`P3-02`, `P3-05`, `P3-12`) |
| **Consumer application** | Reads `amr` to decide whether an action may proceed; requests step-up when it may not |
| **Organization administrator** | Sets `mfa_required` (`P2-10`, enforced by `P3-07`); sees a user's factor status read-only (`P3-10`) |
| **Attacker with a password** | The reason this exists |
| **Attacker with a live session** | The reason enrolment is guarded, not just login |

## 3. Functional requirements

**FR-1** A factor interface covering enrol, verify and remove, implemented by
every factor type. `P3-02` and `P3-05` are implementations of it.

**FR-2** A distinct challenge step in the login flow, holding
partially-authenticated state **server-side** with a short expiry.

**FR-3** A partially-authenticated state is usable for exactly one thing:
completing the challenge. It cannot obtain a token, establish a session, or
reach any resource.

**FR-4** `sessions.auth_methods` records the factors **actually used** in that
authentication, and `amr` is built from it and nothing else.

**FR-5** Step-up: a consumer can require re-verification within a valid session
via `prompt=login` plus an `acr_values`/`amr` requirement, and the service
honours it.

**FR-6** Verification attempts are bounded per **user** and per challenge, not
per session — a new session per attempt is free.

**FR-7** Enrolment, verification success, verification failure and removal are
each audited.

**FR-8** A user may hold several factors. Losing one must not lock them out, and
the challenge offers what they actually have.

## 4. Non-functional requirements

- **Latency**: the challenge step adds one round trip to a login that already
  has several. No target beyond `docs/PLAN/12`'s existing login budget.
- **The secret never leaves its purpose**: no factor material in any log, audit
  payload, error message or metric label. `docs/PLAN/13` names tokens,
  passwords and `resource.attributes`; this adds factor secrets explicitly.
- **Availability**: a factor store outage must fail **closed** for verification
  (no challenge passed) and must not strand a partially-authenticated user with
  an unrecoverable state — the state expires on its own.

## 5. Dependencies

`sessions.auth_methods` exists (`P1-11`) and is populated (`P1-07` builds `amr`
from it). The pending-authorization state (`P1-06`) already holds a login
mid-flight in Redis with a short expiry, which is the mechanism the challenge
state follows rather than a second one invented beside it.

## 6. Data model changes

Additive, per `docs/PLAN/14`'s expand/contract rule.

**`user_factors`** — one row per enrolled factor.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `user_id` | uuid not null → `users` on delete cascade | |
| `org_id` | uuid not null | tenant scope, for RLS like every other table |
| `type` | text not null | `totp`, `webauthn`; checked |
| `label` | text | what the user calls it; never trusted for display without escaping |
| `secret` | bytea | **encrypted at rest**, null for factor types that hold no secret |
| `data` | jsonb not null default `'{}'` | per-type material (WebAuthn credential id, sign count) |
| `confirmed_at` | timestamptz | **null until a code has been verified**; an unconfirmed factor does not count |
| `last_used_at` | timestamptz | |
| `created_at` / `updated_at` | timestamptz not null | |

`confirmed_at` is the field that matters. Enrolment that stops halfway must not
leave an account holding a factor nobody can use — an unconfirmed row is
invisible to the challenge and to `mfa_required`.

RLS on `org_id` like every tenant table, plus the `org_must_match_project`-style
agreement trigger against `users` (a factor's organization is its user's).

**No schema change to `sessions`.** `auth_methods` is already there and already
populated.

## 7. API contract

Nothing public in this task. `P3-02` and `P3-05` add the enrolment endpoints;
`P3-09` adds session management. What this task adds is internal: the challenge
step lives inside the existing `/login` flow, and the step-up parameters are
already OIDC's (`prompt=login`, `acr_values`).

One contract **clarification** goes in the spec now: `amr` values are the RFC
8176 names — `pwd`, `otp`, `hwk`, `mfa` — and `mfa` appears only when two
distinct factor categories were used, never as a synonym for "a second factor
existed".

## 8. Frontend changes

None here. The challenge page is `P3-03`'s; the factor management screens are
`P3-10` and `P3-12`.

## 9. Authorization rules

Enrolment and removal act on **the caller's own** factors. An administrator
cannot enrol a factor for somebody else — there is no sane version of that — and
`P3-10` gives them read-only status.

Factor removal requires a **fresh credential presentation**, not merely a valid
session. See §12.

## 10. Validation rules

- `type` is one of the implemented set. An unimplemented type is refused rather
  than stored, exactly as `allowed_login_methods` refuses `passkey` today.
- A challenge is bound to one partially-authenticated state and one user. A code
  verified against a different state is refused even if the code itself is
  currently valid.
- Removing the **last confirmed factor** in an organization with
  `mfa_required` is refused (`P3-07` enforces; the interface exposes the
  question).

## 11. Error handling

The challenge's refusal is deliberately **not** uniform with the login page's.
`P1-13` makes every login refusal identical because they differ in facts about
the *user*, and telling them apart is an enumeration oracle. By the time a
challenge is presented the password has already been proven, so "that code is
wrong" tells the attacker nothing they did not already have — and telling the
legitimate user something vaguer wastes their time.

What stays uniform: whether a user has *any* factor enrolled is not disclosed
before the password is proven.

## 12. Edge cases

- **Enrolment with a stolen session.** An attacker with a live session enrols
  their own factor and the takeover becomes permanent and MFA-protected.
  Mitigation: enrolment and removal both require a fresh password (or a fresh
  verification of an existing factor), and both notify out of band.
- **Removing the last factor** is a downgrade and must read like one.
- **A user with TOTP enrolled who signs in with a password alone** must not
  carry `mfa` in `amr`. This is the case a convenience shortcut breaks.
- **Two factors, one lost.** The challenge offers every confirmed factor.
- **The partially-authenticated state expires mid-challenge.** The user restarts
  the login; nothing is left behind.
- **Clock skew** on TOTP is `P3-02`'s, bounded there.

## 13. Abuse cases

From the card and the [Phase 3 threat review](../records/2026-09-12-P2-17-phase-3-threat-review.md):

| # | Abuse | Control | Test |
|---|---|---|---|
| A-1 | Skip the challenge by manipulating the partial state | State is server-side, opaque, single-purpose, and carries the user id — a client cannot name a different one | A forged/edited state cookie reaches nothing |
| A-2 | Brute-force a six-digit code inside its window | Bounded per user AND per challenge; the challenge is consumed after N failures | Attempt N+1 is refused even with a correct code |
| A-3 | Remove another user's factor | Acts on the caller's own; scoped by tenant and by subject | Cross-user removal is refused |
| A-4 | Downgrade — force a weaker factor when a stronger is enrolled | The challenge offers what is enrolled; `amr` records what was used, so a consumer requiring `hwk` is not satisfied by `otp` | `amr` after an OTP challenge does not contain `hwk` |
| A-5 | Enrol a factor using a hijacked session | Fresh credential required for enrolment | Enrolment without re-authentication is refused |
| A-6 | Claim `mfa` without a second factor | `amr` is written from the session's recorded methods, never from what is enrolled | A password-only login by an MFA-enrolled user carries `pwd` and not `mfa` |

## 14. Logging and audit

Audited: `mfa.enrolled`, `mfa.confirmed`, `mfa.verification.succeeded`,
`mfa.verification.failed`, `mfa.removed`. Each carries the factor **type** and
never the factor **material**.

`mfa.removed` is the one that matters most for an investigation — it is a
favourite account-takeover step — and it records who removed it and how they
authenticated to do so.

## 15. Security controls

- Factor secrets encrypted at rest, and never returned by any read path after
  enrolment (the same one-time-display discipline as a client secret, `P1-18`).
- The partially-authenticated state is opaque, server-side, short-lived, and
  single-purpose.
- Verification is bounded, cooldown rather than lockout (`P1-13`'s shape): an
  attacker must not be able to lock a victim out by failing their challenges.
- `amr` is derived, never asserted.

## 16. Testing strategy

- **Unit**: the factor interface's contract; `amr` derivation from recorded
  methods; the step-up decision.
- **Integration**: the whole login-with-challenge flow against real Postgres and
  Redis; every abuse case in §13.
- **Architecture**: a test that reads the source and fails if a factor type is
  registered without implementing the interface, and if `amr` is written from
  anywhere but the session.
- **Mutation**: each control reverted, each required to turn its own test red.

## 17. Acceptance criteria

The card's Definition of Done, unchanged.

## 18. Implementation sequence

1. The `user_factors` table and its rules.
2. The `Factor` interface and a registry.
3. The partially-authenticated challenge state.
4. The login flow's challenge step.
5. `amr` derivation and the step-up decision.
6. Rate limiting and audit.

Each step is independently testable and nothing before step 4 changes an
existing flow.

## 19. Rollback strategy

The migration is additive. Until a factor type is registered the challenge step
never triggers, so the framework is inert on a deployment with no factors —
which is every deployment until `P3-02` lands.

## 20. Technical risks

**The partially-authenticated state is the highest-risk object in the system.**
It is, by construction, a thing that is almost a session. Every property that
makes a session useful is one it must not have.

**`amr` becoming aspirational.** The pressure to write `mfa` because the user
*has* a factor rather than because they *used* one will appear the first time a
flow is awkward. The architecture test exists for that.

**Scope.** This task is a frame with no factor in it, so nothing it builds is
end-to-end demonstrable until `P3-02`. That is the right order, and it means the
tests here carry more weight than usual — there is no screen to look at.

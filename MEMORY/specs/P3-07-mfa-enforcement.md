# P3-07 — Organization-Mandated MFA Enforcement

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`.

| | |
|---|---|
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-07 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend + the hosted login flow + Management API |
| **Plan refs** | `docs/PLAN/02-REQUIREMENTS.md` FR-6, `docs/PLAN/08-AUTHORIZATION.md` Part B, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 2 |
| **Depends on** | `P3-03` (the challenge step), `P2-10` (the setting) |
| **Written** | 2026-09-13 |

---

## 1. Business objective

`P2-10` stored `mfa_required` and deliberately did not enforce it, because MFA
did not exist. It exists now, and `docs/PLAN/17`'s **Phase 2** criterion says
the setting must be enforced rather than merely stored — so this closes a
criterion that has been outstanding for a whole phase.

The hard part is not the check. It is what happens to the people who have not
enrolled yet, and there are only two ways to get that wrong:

- **Deny outright**: everybody without a factor is locked out the moment the
  switch flips. That produces a support queue, not security, and the fastest
  way out of a support queue is to turn the setting off.
- **Warn and let through**: the setting does nothing, which is what it does
  today.

The answer is neither: route them into enrolment **at login**, so the only way
forward is through.

## 2. Actors

| Actor | Interest |
|---|---|
| An administrator enabling the policy | Needs to know how many people it will affect **before** clicking |
| A user with a factor | Sees no change |
| A user without one, after the switch | Must be able to get in, by enrolling, without contacting anybody |
| A service account | Has no user and no factor; must not be broken by a policy about people |
| An auditor | Needs the activation and every forced enrolment on the record |

## 3. Functional requirements

| # | Requirement |
|---|---|
| F-1 | With `mfa_required` on, a user with no active factor is routed into enrolment at login instead of receiving a session |
| F-2 | The forced state cannot be bypassed by navigating elsewhere or by holding a token issued before the policy changed |
| F-3 | A bounded grace period after activation, with the deadline shown |
| F-4 | The administrator sees the affected-user count before enabling |
| F-5 | Activation and every forced enrolment are audited |
| F-6 | No silent exemptions |

## 4. Non-functional requirements

- A deployment with `mfa_required` off does one extra boolean read.
- The enrolment page is part of the hosted login flow: no script, same CSP as
  the password page (the passkey step's `PG-40` exception does not extend here,
  because TOTP enrolment needs no browser API).

## 5. Dependencies

`P3-03`'s challenge step and its pending-request plumbing; `P3-02`'s
`TOTP.Begin` / `Confirm`, which have had no caller since they were written;
`P2-10`'s settings storage.

## 6. Data model changes

One settings field: **`mfa_required_since`**, a timestamp set automatically when
the flag transitions to true.

It is not derived from the audit log, though it could be. An audit log is an
append-only record of what happened, and reading policy *out* of it would make
a retention change silently alter enforcement — the grace deadline would move
because somebody pruned old rows. Policy belongs in the policy.

It is set by the service, not by the caller. An administrator who could supply
it could backdate a grace period to zero, which is the denial-of-service this
whole design is shaped to avoid.

## 7. API contract

| Endpoint | What |
|---|---|
| `GET /v1/organizations/{org_id}/mfa-impact` | How many members have no active factor (F-4) |

`ORG_ADMIN`, organization scope. It returns **counts only** — never a list of
who. "Which of my colleagues has no second factor" is a question an
administrator does not need answered to make this decision, and it is precisely
the list an attacker who reached an admin token would want.

`PATCH /v1/organizations/{org_id}` is unchanged in shape; setting
`mfa_required: true` now also stamps `mfa_required_since`.

## 8. Frontend changes

A forced-enrolment step in the hosted login flow. The console screens are
`P3-10` and `P3-12`.

## 9. Authorization rules

The policy is the organization's, read server-side from settings on every
login. Never from a token claim: a token issued before the policy changed would
carry the old answer, which is F-2's second half.

## 10. Validation rules

| Input | Rule |
|---|---|
| `mfa_required` | Boolean, as today |
| `mfa_required_since` | **Not accepted from a caller.** Service-set only |
| The enrolment code | `P3-02`'s TOTP verification, unchanged |

## 11. Error handling

A user in the forced state who cannot complete enrolment gets the enrolment
page again with a uniform message. They are not offered a way past it, because
there is not one.

A settings read that fails **refuses the login**. It is the one place where
failing open would silently disable the policy for everybody, and an
organization that turned this on would have no way to know.

## 12. Edge cases

| Case | Behaviour |
|---|---|
| The policy is enabled while a user is mid-login | Read at the password step, so they are routed on that login |
| A user holds a *pending* enrolment | Not a factor. They are routed into enrolment, and completing it uses the pending row |
| A user in the grace period | Signed in, and told the deadline |
| The grace period expires | Enrolment is required on the next login |
| A user with a factor that this build cannot serve | Treated as having none — otherwise a rollback would let them past a policy they cannot satisfy |
| `client_credentials` | Unaffected. There is no user, so there is nobody to require a factor of |
| A refresh token issued before the policy changed | Refused once the grace has passed. See § 13 A-2 |

## 13. Abuse cases

| # | Scenario | Control |
|---|---|---|
| A-1 | Skip the forced step by navigating to another URL | No session exists until enrolment completes; every other page needs one |
| A-2 | Keep using a token issued before the policy | The refresh grant re-reads the policy |
| A-3 | Enrol a factor for somebody else during the forced step | The pending authorization names the user; the request does not |
| A-4 | Enable the policy to lock colleagues out | The grace period, plus the impact count shown first, plus the audit row naming who enabled it |
| A-5 | Backdate the grace to zero | `mfa_required_since` is service-set |
| A-6 | Sit in the grace period forever | It is bounded and measured from activation, not from last login |

## 14. Logging and audit

| Event | When |
|---|---|
| `organization.mfa_required.enabled` | The policy is turned on. Payload: the actor, the affected count, the grace deadline |
| `organization.mfa_required.disabled` | Turned off. **At least as important**: this is somebody removing a security control, and `docs/SECURITY/04` wants it findable |
| `user.mfa.enrolment_forced` | A user was routed into enrolment |
| `user.mfa.enrolled` | They completed it |

## 15. Security controls

- The policy is read from the database on every login, never from a claim.
- No session is issued until enrolment completes.
- The grace deadline is computed from a service-set timestamp.
- A settings read failure refuses rather than degrades.

## 16. Testing strategy

| Level | What |
|---|---|
| Unit | The routing decision's truth table: policy × factor × grace |
| Integration | Real Postgres and Redis: the forced flow end to end, the bypass attempts, the grace boundary |
| Security | A-1 through A-6 |
| Mutation | Every control above |

## 17. Acceptance criteria

The card's Definition of Done, and with it `docs/PLAN/17`'s **Phase 2**
criterion about `mfa_required` being enforced rather than merely stored.

## 18. Implementation sequence

1. `LoginPolicy.MFARequired` and `mfa_required_since`, with the stamping.
2. The routing decision and its truth table.
3. The forced-enrolment page and its two steps.
4. The impact endpoint.
5. The audit events.
6. Tests, then the mutation run.

## 19. Rollback strategy

The settings field is additive and ignored by an older build — which means a
rollback **silently disables the policy**. That is worth stating in the record
rather than discovering: an organization that believes MFA is mandatory would
not be told otherwise.

## 20. Technical risks

**The grace period is a security control with a usability failure mode, and its
length is a judgement rather than a fact.** Too short and it is the hard cutover
this design exists to avoid; too long and the policy is decorative for as long
as it lasts. Fourteen days: two working weeks, so somebody on a one-week holiday
returns to a warning rather than a lockout, and short enough that an
administrator enabling it sees it take effect within a sprint.

**A user without a factor in the grace period is signed in.** That is the point
of a grace period and it is still a window in which the policy is not enforced.
The alternative — no grace — is the hard cutover.

**`docs/PLAN/17` puts this criterion in Phase 2.** It is being satisfied in
Phase 3 because MFA did not exist in Phase 2, which `P2-10` recorded at the
time. Worth noting so the phase-2 acceptance document is not read as having
been wrong.

## 21. What this task does not deliver

- **Forced WebAuthn enrolment.** The forced step offers TOTP only, because
  `PG-40`'s script exception is scoped to the challenge page and a registration
  ceremony would extend it. A user may enrol a passkey afterwards from `P3-12`.
- **Per-user exemptions.** None are built, which is how F-6 is satisfied: an
  exemption that does not exist cannot be granted silently. Service accounts
  need none, because `client_credentials` has no user.
- **The console's view of the impact count.** `P3-10`'s screen.

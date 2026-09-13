# P3-07 — Organization-Mandated MFA Enforcement

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-07 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend + the hosted login flow + Management API |
| **Branch** | `feat/P3-07-mfa-enforcement` |
| **Status** | Complete |

**Spec**: [`MEMORY/specs/P3-07-mfa-enforcement.md`](../specs/P3-07-mfa-enforcement.md)

---

## A Phase 2 criterion, closed in Phase 3

`P2-10` stored `mfa_required` and deliberately did not enforce it, because MFA
did not exist. `docs/PLAN/17`'s **Phase 2** acceptance says settings must be
enforced at login rather than merely stored, so this closes a criterion that had
been outstanding for a whole phase. `P2-10` recorded that at the time; this is
the other half.

## The requirement that every test said was met, and was not

The enable and disable audit events were defined. `MandateChange`, which
decides which of them a settings change is, was written and tested. **Nothing
emitted either event.**

Every unit test was green. The gap was found by checking, before writing this
record, that each thing the spec promised had a caller — not by any test. The
integration tests added after it fail with the wiring removed and pass with it,
so the next removal is loud.

This is the project's recurring defect class in its purest form: a control
whose pieces are all individually proven while the connection between them is
not. Two tests now own the connection.

## Why not deny, and why not warn

Both alternatives fail the people involved, in opposite directions:

- **Deny outright** and everybody without a factor is locked out the moment the
  switch flips. That is a support queue, not security — and the fastest way out
  of a support queue is to turn the setting off, leaving the organization less
  safe than before anybody tried.
- **Warn and let through** and the setting does nothing, which is what it did.

So a user past the grace with no factor is routed into enrolment **at login**.
No session exists until they finish, which is what makes the state unbypassable
rather than merely awkward to leave: every other page needs a session.

## The grace, and where its clock lives

Fourteen days — a judgement, stated as one in the source. Two working weeks
means somebody back from a one-week holiday meets a warning rather than a
lockout.

It is measured from **activation**, stored as `mfa_required_since`, and three
decisions about that timestamp are the security-relevant part:

| Decision | Why |
|---|---|
| Stored in the settings, not derived from the audit log | Reading policy out of an append-only log would let a retention change silently move the enforcement deadline |
| **Service-set; a caller cannot supply it** | Refused by the existing settings allow-list. An administrator who could backdate it could lock out an organization instantly |
| Stamped only on the off→on transition | Re-sending `mfa_required: true` in an unrelated PATCH must not hand everybody a fresh fourteen days |

Turning the mandate off leaves the timestamp behind, deliberately: toggling it
off and on within the window must not buy a new grace period.

## The lockout the design refuses to create

`decision.Challenge == false` means either "this user has no factor" or "this
build implements no factors". Forcing enrolment in the second case would lock
out an entire organization with no way forward — the exact outcome the forced
flow exists to prevent.

So the handler checks whether it **can** enrol before acting on the mandate. A
build that cannot lets the login through and logs at **ERROR**, because an
organization that believes MFA is mandatory is entitled to find out it is not.

## The mutation run found two tests proving less than their names

Fifteen controls. Thirteen red on the first run.

**A failure-to-begin test passed with the error ignored.** It asserted only
"not signed in" — which was true either way, because ignoring the error left the
user on an enrolment page with no key and no handle. A dead end, but not a
sign-in. It now asserts the failure *shows*: a 500 and no enrolment form.

**The attempt bound had two mutually redundant guards**, and the mutation hit
the wrong one first. The guard before `Confirm` and the block after it each
produce the "start again" message alone. The block after is the one that
destroys the spent state, so the test now asserts the state is gone and the
mutation targets deletion. The earlier guard stays, documented as the defence
for a state that survived a failed delete.

A separate slip worth recording: one of those test fixes silently did not apply
— the patch script aborted on an earlier assertion before writing, and the
mutation stayed green for a reason unrelated to the code. Found by grepping for
the assertion rather than trusting the script's first line of output.

## What a rollback does

The settings field is ignored by an older build, which means **a rollback
silently disables the mandate**. An organization that believes MFA is required
would not be told. Stated here rather than discovered.

## Verified

| | |
|---|---|
| Unit | The mandate's truth table (7 cases), the stamping rules, the transition, 14 on the forced flow |
| Integration | 9 through the real endpoint: both audit events, the stamp, the backdating refusal, the impact counts |
| Mutation | 15 controls, each turning its own test red |

## What this does not deliver

- **Forced WebAuthn enrolment.** The forced step renders with no script, and a
  registration ceremony would extend `PG-40`'s exception from one page to the
  flow. A passkey can be enrolled afterwards from `P3-12`.
- **Per-user exemptions.** None exist, which is how "exempt nothing silently" is
  met: an exemption that is not built cannot be granted quietly. Service
  accounts need none — `client_credentials` has no user.
- **The warning shown during the grace**, and the console view of the impact
  count. `P3-12` and `P3-10`.

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad.

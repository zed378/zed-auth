# P1-14 — Authentication audit events

**Date**: 2026-09-10
**Branch**: `feat/P1-13-rate-limiting` (with `P1-13`, which produced the bug this task found)

---

## What this is

`docs/PLAN/17` § Phase 1 names it: login success and failure appear in the audit log with correct actor and timestamp.

Most of the emitting was already done — `P1-12` writes both login outcomes, `P1-11` writes `session.created` and `session.revoked`, `P1-07` writes `token.issued`, `P1-13` writes `user.lockout`. So this task was mostly **verification**, and verification is where it earned its keep.

## It found that one of those events was never written

`P1-13`'s lockout audit called:

```go
_ = h.DB.WithTenant(r.Context(), "", func(tx *postgres.Tx) error { … })
```

`WithTenant` returns `ErrEmptyOrgID` for an empty organization and does nothing else. The error was discarded. **The lockout event was silently never recorded**, and `P1-13`'s Definition of Done item "lockouts appear in the audit log" had been ticked on the strength of a unit test whose fake tenant ignores the organization entirely.

Two mistakes, both mine, and they are different:

- **The code**: instance scope needs `WithInstanceScope`, not `WithTenant("")`. But the better fix was not to reach for either. A lockout is now recorded against the **client's organization** — the address may belong to no organization, which is the whole reason `P1-13` keys its counter on the submission, but the *attempt* belongs to one: it happened at that organization's login page. That is the same reasoning the failed-login event already used, and it makes the entry visible to the administrator who can act on it.
- **The verification**: ticking a DoD item about the audit log using a fake auditor. The fake proves the handler *calls* the writer; only a real database proves anything *lands*. `TestALockoutIsWrittenToTheAuditLog` now reads the row back through the owner connection, and a mutation restoring the empty organization fails it.

This is the vacuous-verification pattern arriving in a new shape: not a check that cannot fail, but a check at the wrong layer for the claim it was used to support.

## Reading the audit log in a test is itself a trap

The service's own database role is tenant-scoped, so reading `events` through it outside a transaction returns **nothing** — which would make every assertion in this file pass by seeing no rows at all. `P1-08`'s record describes the same trap; here it is again, and the tests read as the owner for that reason. Every one of them also has a control that fails when there are no rows.

## What was added

**The user agent**, on `user.login.success`, `user.login.failed` and `user.lockout`. Step 1 asks for it, and it is what distinguishes "the user signed in from a new laptop" from "somebody signed in as them" — an incident timeline without it cannot tell those apart. Bounded at 512 bytes before storage, because a user agent is attacker-controlled and unbounded and the audit table is append-only with a 24-month retention, so an unbounded one is a place to park data nothing can delete.

**Still not the submitted address**, on any of them. `P1-12` explains why at length; a lockout entry is where that list would come from fastest, so it is where the rule matters most.

## The tests that make the DoD true rather than asserted

- **Both outcomes, with the right actor.** A wrong password on a real account names the actor; an attempt against an address with no account names nobody. That difference is not a leak: it is a fact about the reading organization's own users, which an administrator can already list, and the address itself never appears.
- **No credential material in ANY event**, checked across every event one full login flow produces rather than one event type at a time — a per-event test is a test somebody forgets to add for the next event. It reads every payload in the table and looks for the password, the CSRF tokens, the pending request ids and the addresses.
- **Ordering and timestamps.** Every row is stamped inside the window the test ran in, timestamps do not go backwards, and ids increase with time — so two events in the same millisecond still have a defined order, which "ordered by time" alone does not give. `docs/SECURITY/04`'s playbooks depend on that.
- **Queryable on its indexes.** `EXPLAIN` on each of the three access patterns the DoD names (org + time, type + time, actor + time) asserts the plan is not a sequential scan. An index nothing plans against is an index that is not there, and a sequential scan on a partitioned append-only table that grows forever is the shape of a query that works in a test and times out in year two.

## What the mutation testing found

| Control removed | Test that failed |
|---|---|
| the lockout is written with an empty organization, as the bug did | `TestALockoutIsWrittenToTheAuditLog` |
| the submitted address is recorded on a failed login | `TestAFailureAgainstAnUnknownAddressNamesNobody` |
| the user agent is dropped | `TestBothLoginOutcomesAreAudited` |
| the lockout audit error is discarded | **nothing — correctly** |

The last row is worth keeping rather than hiding. Discarding the error again does not change whether the row is written, only whether a failure is *reported* — so no test covers it, and none should: its entire effect is a log line. It was, however, what let the original bug survive unnoticed, which is why the error is now reported. A control with no test is fine when its only output is a diagnostic; the thing that needed a test was the write, and now has one.

## A test I got wrong

`TestNoCredentialMaterialReachesAnyEvent` failed on its first run because the password it submitted was `"wrong"` — a substring of the legitimate reason class `"wrong_password"`. The service was doing nothing wrong. Fixed by submitting a password that cannot be confused with legitimate content, and the reason is in a comment so the next person does not reintroduce it.

## What is still missing

- **`token.issued` is per-token.** Step 3 suggests an aggregate metric rather than a row per token "to keep volume sane". `P1-07` writes a row. At Phase 1 volumes that is fine and it is more useful; it becomes a question when `P1-27`'s load test says what the volume actually is, and `PG-10`/`DF-12` already track the events table growing.
- **Nothing reads the audit log yet.** `P1-20` is the API and `P1-24` the screen. Until then these events are written correctly and looked at by nobody, which is the same "no caller" shape earlier records flagged — except that here the writing is the point and the reading is a later task rather than a missing half.

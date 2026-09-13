# P3-08 — Login Anomaly Detection

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-08 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | backend + the hosted login flow + monitoring |
| **Branch** | `feat/P3-08-login-anomaly-detection` |
| **Status** | Complete except DoD item 4 — see below |

**Spec**: [`MEMORY/specs/P3-08-login-anomaly-detection.md`](../specs/P3-08-login-anomaly-detection.md)
**Decision**: [ADR-024](../DECISIONS.md) — notify, never step up
**Gap**: [`PG-41`](../../TASKS/BACKLOG.md) — no plan document names a geolocation source

---

## A hole in a page this task did not set out to touch

`/password/set` consumed a token of **any** purpose. That was harmless for as
long as every purpose that existed — invitation, reset — legitimately sets a
password. This task adds the first one that must not: a "this wasn't me" link,
which sits in an email for a week and exists to sign its owner **out**.

Without a fix, anybody holding one could have opened `/password/set` with it and
chosen the account's password. Found while writing the report flow, before a
single report link had ever been issued, so it was never exploitable in a
shipped build. It would have been from the first deploy of this task.

`user.SetsPassword` is now an allow-list, checked before the token is consumed —
so the owner who clicks the link on the wrong page still has a working link. The
report page applies the mirror rule: a reset link cannot drive it. Both
directions have tests that fail with the check removed.

## What "forces a credential change" turned out to require

The card says the report "revokes sessions and forces a password change". Ending
sessions is the half everybody writes first, and on its own it does nothing
useful: the stranger still knows the password and signs straight back in.

So the report **clears the stored password** in the same transaction that ends
every session and refresh token and retires every outstanding link. `authn`
already answers a missing hash exactly as a wrong password, at the same Argon2
cost, so the stranger's next attempt tells them nothing. The owner gets a reset
link at the address on file.

One transaction, because every partial outcome is a user told they are safe who
is not: sessions ended with the password intact, or the password cleared with a
refresh token still minting access tokens.

## Detection that cannot break sign-in

Every detection runs after the session commits, in a goroutine with a detached
context and its own 30-second bound, with a recover. The login does not wait for
a history query or, with notifications on, an SMTP round trip — and a panic in
the detector cannot take the process, and every other login, down with it. Each
of those three properties has a test that fails when it is removed.

The history is the `sessions` table. There is no table of known devices or known
places: it would be a movement history kept for its own sake, and it would
disagree with `sessions` the first time one was pruned. Location is derived at
detection time and never stored; the audit row carries the coarse place and
never the IP beside it.

## Two gaps found beside the work

- **`PG-41`.** New-location and impossible-travel detection need an IP turned
  into a place, and no plan document says where that comes from. The service
  reads the MaxMind file format and ships no database; an operator provides one.
  Without it, new-device detection runs and the location signals are off, and
  startup says so. A path that is set but unopenable stops the service.
- **`P3-06`'s alert did not exist.** Its record and the metric's own help text
  both said refresh-token reuse "pages on any increase". There was no rule. It is
  in `alerts.yml` now, beside this task's two, and `promtool` passes all 22.

## The mutation run found one test proving nothing

Nineteen controls. Eighteen red on the first run.

`TestASignedOutDeviceIsStillFamiliar` passed with revoked sessions **excluded**
from history — the exact bug it is named for. Its only history was the revoked
session, so dropping revoked rows left an empty history, and an empty history is
a first login, which is never an anomaly. It passed for the wrong reason. It now
signs in on a second, live device first, so the history is never empty and the
revoked row is load-bearing.

Two controls are layered on purpose and were mutated together: the report
page's purpose check and the purpose passed to `ConsumeToken`. Each alone is
redundant with the other; removing both is caught.

A test that had to be changed before it could mean anything: the "outstanding
links are retired" test first used a stale **reset** link. Issuing the report's
own reset link retires old reset links anyway, so the test would have passed
with `RetireTokens` deleted. It uses an invitation link now.

## Verified

| | |
|---|---|
| Unit | Device parsing, distance and travel with synthetic coordinates (the DoD's "tested with synthetic data"), the MMDB reader against a synthetic database, the detector's failure handling, the hook's three properties, the report page's edges |
| Integration | Detection through the real login and the real history query under the service's role; the notice's link opened; the report end to end — sessions, refresh tokens, cache, cleared password, the reset link used; single use, purpose binding both ways, CSRF, quota-before-issue; tenant isolation of the history |
| Mutation | 19 controls, each turning its own test red |
| Monitoring | `auth_login_anomalies_total{signal}`, two alert rules, `promtool check rules` |

## Definition of Done

| Item | |
|---|---|
| New-device and new-location logins detected and notified | **Met.** New-location needs an operator-supplied database (`PG-41`) |
| Impossible travel detected and tested with synthetic data | **Met** |
| "This wasn't me" revokes sessions and forces a credential change | **Met** — and "forces" means the password is cleared |
| **False-positive rate measured on real staging traffic before enabling notifications broadly** | **Not met, and not claimable.** Staging has been unreachable since the deploy key was lost. What ships honours the intent: **notifications default off**, detection and the metric default on, so nothing is enabled broadly before the rate is known and the metric is the measurement. The alert thresholds are marked provisional for the same reason |
| Step-up-versus-notify decision recorded | **Met** — ADR-024 |
| Signals appear in monitoring | **Met** |

## What this does not deliver

- **A geolocation database** (`PG-41`).
- **Factor reset on report.** A stranger who enrolled their own factor before
  the report still blocks the owner; that is the administrator reset (`P3-04`).
- **A notice in any language but English**, like every other message the service
  sends.

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad.

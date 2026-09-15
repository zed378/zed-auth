# Phase 3 — Advanced Security

| | |
|---|---|
| **Closed** | 2026-09-15 |
| **Tasks** | 15 of 15 |
| **Closing task** | `P3-15`: acceptance validation, on staging |
| **Tag** | `v0.3.0-phase3` |

---

## What it is now possible to do

- **A stolen password is not enough on its own.** A person can enrol an authenticator app
  or a passkey, and get back in with a recovery code when the device is gone.
- **An organization can require a second factor** of everyone, with a grace period that
  cannot be gamed. That rule now also holds on the paths that never show a login page.
- **An application can tell how someone signed in.** `amr` records the methods actually
  used, so an application can demand a recent, stronger sign-in before a sensitive action.
- **A stolen refresh token stops working the second time it is used**, and an honest client
  retrying after a lost response is not punished for it.
- **Anyone can see where they are signed in and end it** from a phone. Revoking a session
  takes its refresh tokens with it.

## The three acceptance criteria, verified on staging

`scripts/acceptance-phase3.sh`: **19 checks, 0 failures**, against the deployed service. This
is the first phase since Phase 1 accepted where it runs rather than on a laptop. Every
refusal was credited only beside the same operation succeeding.
[P3-15](2026-09-15-P3-15-acceptance.md) has the evidence.

## What the phase found that it had not set out to find

Phase 3's findings share one shape: **a control that exists but is not connected to
anything**. Each piece is proven correct on its own, and the connection between them is
proven by nothing.

| Found at | What was disconnected |
|---|---|
| `P3-07` | Mandate audit events defined and unit-tested; nothing emitted them |
| `P3-10` | Passkeys enrolled and verified; login never challenged them, so a passkey-only user signed in with a password |
| `P3-12` | The breached-password corpus built at startup and handed to nothing since `P1-02`; its metrics read zero while alerts watched them |
| `P3-13` | An MFA mandate with no start date read as "in grace" for ever; organizations created with it on were never enforced |
| `P3-13` | The console told administrators the mandate "changes nothing today" after it had shipped |
| `P3-14` | `P3-07`'s spec named abuse case A-2 with a control, and no code implemented it: pre-mandate refresh tokens and live sessions outlived the grace |
| `P3-14` | CI had never run since `P1-03`: the workflow file did not parse, and 112 runs had no success |
| `P3-14` | Staging took no backup for four nights, the second time for the same reason |
| `P3-15` | Every sign-in answered 500 on a deployment without MFA configured, because of a nil pointer inside an interface |

What caught them was rarely a test written for the feature. It was:

- **writing the documentation**, which forced each number to be read from the code;
- **inventorying abuse cases** against the tests that claim them;
- **reading CI's actual run history**;
- **checking the backup before a deploy**;
- **an A/B experiment** that switched a setting off.

The lesson the project keeps relearning: the configuration nobody runs is the one that
breaks. Only a run that switches it on finds out.

## What it cost

`/oauth/token` refresh p50 went from 46ms to about 60ms. That is the price of rotation and
reuse detection, and a verified A/B shows none of it comes from the mandate check. It is
accepted and scheduled for Phase 5, with the round trips to start on named in `P3-15`'s
record. Interactive sign-in gains a median of 13.6ms for the TOTP step.

## What carries into Phase 4

- **The Phase 4 threat review** ([T4-1…T4-14](2026-09-15-P3-15-phase-4-threat-review.md)).
  Two findings need a design decision before delegation work starts: cross-organization
  sign-in (T4-1), and subset validation at the readers, not only at writes (T4-2).
- **`PG-29`**: neither the signing key nor, now, the MFA seal key is backed up. Losing the
  seal key makes every enrolled factor unusable.
- **`PG-19`**: behind the tunnel, per-IP rate limiting treats all traffic as one client.
- **Cross-browser passkeys**: tested in Chromium only.
- **`DF-13`**: "remember this device" is deferred.

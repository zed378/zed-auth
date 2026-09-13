# P3-12 — Console: Personal Account Settings

| | |
|---|---|
| **Date** | 2026-09-13 |
| **Task** | `TASKS/PHASE-3-ADVANCED-SECURITY.md` § P3-12 |
| **Phase** | Phase 3 — Advanced Security |
| **Surface** | console + backend (account API, password validation, forced enrolment) |
| **Branch** | `feat/P3-12-personal-settings` |
| **Status** | Complete |

No feature spec: the card marks the `docs/UI-UX/19` chain as the specification.
The API behind the screen is small and is described in `openapi.yaml`.

---

## The breached-password check guarded nothing

The most important thing this task found predates it by three phases.

`P1-02` built a breached-password corpus client, and ADR-015 decided how it fails:
open, with an audit event, a counter and an alert on every skip. `cmd/authservice`
built the client at start-up — and handed it to nothing. The only password-set
path, the hosted set-password page that every invited user passes through,
validated against the organization's policy alone. So a password from a public
breach list was accepted, no skip was ever audited, and the two metrics P1-02
defined (`auth_password_policy_rejections_total`,
`auth_password_breach_checks_total`) were never incremented — while alert rules
watched them. The start-up log still said "no password-set path exists yet".

It was found by reading the set-password page's validation while building the
second password-set path. No test could have seen it: the validator's tests and
the page's tests both passed; the gap was entirely in the wiring between them.

Fixed with one `authn.PasswordValidator` — policy, corpus, counted, audited —
used by every path that sets a password. An architecture test reads `main.go` and
fails if either path stops using it or the corpus stops being passed.

## A password change that is not a second place to guess

`POST /v1/me/password` needs the current password, and checks it against
**sign-in's own per-address bound** before the Argon2 verification. Wrong guesses
here count toward the same cooldown as wrong passwords at sign-in, so this is not
an unthrottled way to guess. The new password goes through the validator; a
change to the same password is refused. On success every other session and its
refresh tokens end — a password change is what somebody does when they think
somebody else knows the old one — and the session that made the change is kept.

`GET /v1/me` returns the profile and the password rules, so the screen can show
the requirements **before** anything is typed (card step 2). A member with no
administrative role reads their own account there; the organization's user
routes would refuse them.

## Recovery codes at forced enrolment

Carried from `P3-04` via `P3-10`: `P3-07`'s forced enrolment issued no recovery
codes, so a user pushed through it had a factor and no way back if they lost the
device. Now, after the code is confirmed and before the sign-in completes, a page
shows the codes once, and "I have saved them — continue" finishes the sign-in. The
Continue step requires a server-side `Confirmed` flag, so posting `continue`
without proving the factor completes nothing (tested); no session exists until the
codes have been acknowledged (tested).

## The phone screen

`/account` renders in its own shell **outside** the console's narrow-screen guard
— `UnsupportedWidth` had anticipated exactly this. One column: profile, password
with its requirements, the MFA component from `P3-10`, the sessions component
from `P3-11`, and linked sign-ins shown as **Not available** with nothing to press
(`docs/UI-UX/21`: never imply an unshipped capability). "Your account" is linked
from every console screen.

The low-count warning (`P3-04` F-4) now shows at three codes or fewer.

## Verified

| | |
|---|---|
| Unit / component | 5 on the account screen (rules shown up front, refusal on its field, mismatch blocks submit, success message, social sign-in unavailable, axe); the MFA tab still passes with the extracted component |
| Integration | 4 on the validator (policy refusal without asking the corpus, breach refusal, outage accepted-but-audited-and-counted, disabled counted); 8 on the account API (the rules, sign-out-others with refresh and cache, the current password, the shared cooldown, policy and breach and same-password refusals, sessionless refusal, required fields, null name); the set-password page refusing a breached password; forced enrolment showing codes before continuing |
| E2E | At 390×844 with touch: no sideways scroll, every primary action and the Sessions revoke target at least 24×24 inside the screen (`P3-11`'s carried item), and a password changed on a phone that then signs in. The full suite: 30 tests |
| Mutation | 18 controls, each turning its own test red |

## Definition of Done

| Item | |
|---|---|
| Every action works on a real mobile viewport | **Met** at 390×844 with touch emulation in Chromium. A physical device was not used |
| Password change enforces the policy and shows requirements up front | **Met** — and now the breach corpus too, which it never did |
| MFA and session management are fully self-service here | **Met** |
| Social login is shown as unavailable rather than broken | **Met** |
| Accessibility at mobile width as well as desktop | **Met** for what axe and the tests check; no screen-reader session on a device |

Not yet on staging: the VM at 10.1.200.13 has been unreachable since the deploy
key was lost with a session scratchpad.

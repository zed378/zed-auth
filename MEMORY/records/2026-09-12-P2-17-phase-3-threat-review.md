# Phase 3 Threat-Model Review

| | |
|---|---|
| **Date** | 2026-09-12 |
| **Task** | `P2-17` step 3 — `docs/PLAN/09` § Secure Development Practices |
| **Reviewing** | `TASKS/PHASE-3-ADVANCED-SECURITY.md`, before any of it is built |
| **Method** | `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`'s categories against each card |

---

Phase 3 adds second factors, recovery, refresh-token rotation, anomaly
detection and session management. The pattern across all of it: **every
mechanism that adds security adds a recovery path, and the recovery path is the
new attack surface.** A second factor without recovery produces locked-out
users; with recovery, the recovery is now the weakest link. Most of what
follows is about that.

## T3-1 — The recovery path becomes the authentication path

**Cards**: `P3-04` (recovery codes, lost device), `P3-02`.

If recovery codes can be used in place of a second factor, then MFA is only as
strong as wherever those codes ended up — a password manager, a screenshot, a
support ticket. `P3-04`'s own goal says the alternative is an "ad-hoc support
process that becomes the weakest link", which is the right framing and does not
by itself decide anything.

**Ask of P3-04**: recovery-code use must be a *distinguishable* authentication
event — a different `amr` value, its own audit event, and a notification to the
user. Single-use, and using one should prompt for re-enrolment rather than
silently leaving nine. And "an administrator resets somebody's MFA" needs to be
a named capability with an audit trail, not a database edit somebody does
quietly, or it is a privilege-escalation path with no record.

## T3-2 — `amr` that says more than it knows

**Cards**: `P3-01`, `P3-03`, `P3-05`.

`P3-01`'s goal is `amr` "accurate enough that consumer applications can make
real step-up decisions". That is a load-bearing claim: a consumer refusing a
payment unless `amr` contains `mfa` is trusting this service to have actually
challenged. `P1-11` already records factors on the session for exactly this.

**Ask of P3-01**: `amr` is written from what the session actually did, never
from what the user *has enrolled*. A user with TOTP enrolled who signed in with
a password alone must not carry `mfa` — and the test for it should be exactly
that case, because it is the one a convenience shortcut breaks.

## T3-3 — Enrolment as an escalation

**Cards**: `P3-02`, `P3-05`.

Enrolling a factor is the moment an account's security changes, and an attacker
with a stolen session wants to enrol their own. If enrolment does not re-prove
possession of the password, a hijacked session becomes a permanent takeover —
and worse, an *MFA-protected* takeover the real owner cannot undo.

**Ask of P3-02 and P3-05**: enrolment and factor removal both require a fresh
credential presentation, not merely a valid session. Both notify the user out of
band. Removal of the last factor is the one to watch: it is a downgrade, and it
should read like one.

## T3-4 — The TOTP secret is a credential in transit and at rest

**Card**: `P3-02` ("the shared secret is treated as the credential it is").

The secret is displayed once, as a QR code and a string. It will be in a browser
DOM, possibly in a screenshot, and — if anybody is careless — in a log or an
audit payload.

**Ask of P3-02**: the secret never appears in any log, audit event, or error
message; `docs/PLAN/13`'s no-secrets rule already covers tokens and passwords
and should name this explicitly. It is stored encrypted rather than plain,
because a database read that yields TOTP secrets yields the second factor for
every user at once. And enrolment is not complete until a code is verified —
otherwise a failed enrolment leaves an account holding a factor nobody can use.

## T3-5 — Verification as an oracle, and as a brute-force target

**Card**: `P3-03`.

Six digits is a million possibilities and a 30-second window. Without a bound,
an attacker with the password brute-forces the second factor in minutes.

**Ask of P3-03**: attempts are bounded per *user*, not per session — a new
session per attempt is free. The refusal for a wrong code must be
indistinguishable from the refusal for a wrong password *in what it reveals
about the account*, while still being distinguishable to the legitimate user who
needs to know which one they got wrong. That tension is real and `P1-13`'s
existing login-refusal design is the precedent to follow.

Replay: a code used once must not be usable again inside its window.

## T3-6 — Rotation that detects reuse, and what it does then

**Card**: `P3-06`.

`docs/PLAN/17`'s Phase 3 criterion is "a rotated refresh token cannot be reused,
verified by automated test". The subtler question is what happens on detection.

**Ask of P3-06**: reuse invalidates the whole family, not just the presented
token — a thief and a victim both holding tokens from one lineage means
revoking one leaves the other working, and there is no way to tell which is
which. The family lifetime cap already exists (`token.FamilyLifetime`, enforced
since Phase 1 for precisely this reason). Detection must be an audit event and
a notification, because a silent revocation looks like a bug to the victim.

## T3-7 — Anomaly detection as an information channel

**Card**: `P3-08`.

Two risks, in opposite directions. Notify too much and people filter the
notifications, which removes the control. Notify with too much detail — "a login
from Jakarta on a Pixel 7" — and a notification sent to a compromised mailbox
tells the attacker what the victim knows.

**Ask of P3-08**: the notification says enough to act on and not enough to
profile. And the detection itself must not become a lockout lever: an attacker
who can trigger "impossible travel" against a victim should not be able to
suspend their account.

## T3-8 — Session management as a cross-user surface

**Card**: `P3-09` (users revoke their own; administrators revoke their
organization's).

Every session-listing endpoint is an inventory of somebody's devices. The
authorization is the familiar shape and `P2-05`'s hierarchy already covers it;
what is new is the **data**: IP addresses, user agents, locations.

**Ask of P3-09**: a user sees their own sessions in full; an administrator sees
enough to revoke and to investigate, which is not necessarily the same thing.
Revocation takes effect immediately — the same `/v1/authz/check`-style
distinction between "revoked" and "will expire" that Phase 2 had to make about
tokens. And an administrator revoking a session is an audit event naming both
parties.

## T3-9 — The Phase 2 debt that Phase 3 inherits

`P2-10` stored `mfa_required` and enforced nothing, deliberately and
documented. `P3-07` delivers the enforcement.

**Ask of P3-07**: the moment enforcement lands, every organization that has had
`mfa_required: true` sitting in its settings starts refusing logins from users
with no factor enrolled. That is correct and it is also an outage for anybody
who set the flag optimistically. There needs to be a transition: a grace period,
or an enrolment prompt rather than a refusal, or at minimum a way for an
administrator to see who would be locked out *before* it takes effect.

The console's Policies screen (`P2-14`) currently says "setting this changes
nothing today". That sentence has to change in the same release as the
enforcement, and `P2-15`'s docs-drift test is the pattern for making sure it
does.

## T3-10 — Two more mobile surfaces

**Cards**: `P3-12` (the only screen requiring full mobile optimization),
`P3-11`.

Nothing security-specific, except that `P3-12` is the first console screen an
end user rather than an administrator will open, on a device this project has
not otherwise targeted. Session revocation and factor removal are both
destructive and both will be reached by thumb.

**Ask of P3-12**: the destructive-action confirmations that `docs/UI-UX/07`
specifies have to survive the narrow layout, and `color-danger` stays reserved.

---

## What this review did not find

No new **category** of risk. Phase 3 is the first phase where every card is
security work, and the threat model that covers it is the one already written —
`docs/SECURITY/02`'s categories map onto these cards without a gap.

The recurring shape is T3-1's: each mechanism arrives with a recovery path, and
the recovery path deserves the same scrutiny as the mechanism. Five of the ten
findings above are that observation applied to a different card.

## Recorded

Nothing here blocks Phase 3 starting. Each **Ask** belongs in its card's
specification, and `P3-01` — the framework the rest plug into — should absorb
T3-2 and T3-3 before any factor is built on it.

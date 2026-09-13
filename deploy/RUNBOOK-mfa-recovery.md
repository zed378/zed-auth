# Runbook — a user cannot complete two-step verification

**Task**: `P3-04` step 4. **Spec**: `MEMORY/specs/P3-04-recovery-codes.md`.

`docs/PLAN/17` accepts a manual recovery process. It does not accept an
undocumented one, and the reason is worth stating before the steps: **an
undocumented reset path is an authentication mechanism**. It decides who gets
into an account. If nobody wrote down what it requires, then what it requires is
whatever the person answering the phone decides under pressure, which is the
weakest authentication in the entire system and the one with no threat model.

This is that document.

---

## Before anything: which situation is this?

| The user says | Path |
|---|---|
| "I have my phone, the code is refused" | **A — not a recovery at all** |
| "I lost my phone, I have my recovery codes" | **B — self-service** |
| "I lost my phone and I don't have the codes" | **C — administrator-assisted** |

Most calls are A. Take them in order; C is the last resort and the only one that
requires another person's judgement.

---

## Path A — the code is refused and the device is present

Nothing is lost, and no reset is warranted. In order of likelihood:

1. **Clock drift.** The service accepts one 30-second step either side. A phone
   whose clock has drifted further produces codes that are correct and refused.
   Ask the user to enable automatic time on the device. On Android this is
   Settings → System → Date & time → Set time automatically; on iOS it is
   Settings → General → Date & Time → Set Automatically.
2. **The wrong entry.** A user with several accounts in one authenticator app
   may be reading a code for a different one. The entry is labelled with the
   issuer this deployment is configured with.
3. **A stale enrolment.** If they re-enrolled recently, an older entry for the
   same issuer may still be in the app and still producing codes that this
   service no longer accepts. Only one TOTP secret per user exists; any other
   entry is dead.
4. **Too many attempts.** After ten failed guesses in fifteen minutes the user
   is in a cooldown and every code is refused, including correct ones. Wait for
   the window to pass. **Do not reset the factor to work around this** — the
   cooldown is doing exactly what it is for, and resetting it for a caller who
   has been guessing is the shape a successful attack takes.

If none of these resolve it, treat the device as lost and go to B.

---

## Path B — the user has a recovery code

Self-service, no administrator involved:

1. On the two-step verification page, choose **Use a recovery code**.
2. Enter any unused code. Case, dashes and spaces do not matter.
3. They are signed in. The code is now spent and will never work again.

**Tell them to do the next part immediately**, while they are signed in and
before they forget:

- Enrol a new authenticator app, because the old device is gone.
- Generate a new set of recovery codes, because the remaining ones are the only
  thing standing between them and Path C.

The page tells them how many codes remain, and warns below three.

---

## Path C — administrator-assisted reset

This path hands an account back to somebody who cannot currently prove they own
it. Everything below exists because of that sentence.

### C.1 — Verify who you are talking to. This is the control.

**No technical control in this system can do this step for you.** The endpoint
cannot tell an administrator who verified a caller from one who was talked into
it, and an attacker who has reached this point has usually already failed at
guessing and is now trying the part that is made of people.

Before touching anything, confirm identity through a channel that is **not** the
one the request arrived on:

- **Preferred**: a video call where you can see the person, against a photo or a
  previous meeting.
- **Acceptable**: a call to the phone number in the HR record — *you* dialling
  *them*, never a number supplied in the request.
- **Acceptable**: confirmation from the person's line manager, contacted
  independently.
- **Never sufficient on its own**: an email from the account's own address (the
  account is what is in question), knowledge of facts a colleague or a public
  profile would know, or urgency. **Urgency is the most common pretext.** A real
  locked-out user is inconvenienced. Somebody insisting the reset cannot wait
  twenty minutes for a callback is telling you something.

If you cannot verify, **stop**. An account that stays locked for a day is
recoverable. An account handed to the wrong person is not.

### C.2 — Perform the reset

```
POST /v1/organizations/{org_id}/users/{user_id}/mfa-reset
```

Requires `ORG_ADMIN` in the user's own organization. It removes every enrolled
factor and every recovery code, and returns how many of each it destroyed.

It returns **no credential**. There is no new password, no session, and no set
of recovery codes for you to read out — deliberately. An administrator who could
mint a working credential for another account could take that account over, and
the takeover would look like an ordinary login.

### C.3 — Tell the user what to do next

After the reset, a password alone signs them in. They must:

1. Sign in with their existing password.
2. Enrol an authenticator app again.
3. Generate and **save** a new set of recovery codes.

Until step 2 the account is protected by a password alone. If the reason for the
reset was a suspected compromise rather than a lost device, reset the password
too (`POST .../password-reset`) and treat it as an incident.

### C.4 — It is already audited; check that it looks right

The reset writes `user.mfa.reset_by_admin`, naming you and the target. You do
not need to record anything by hand, but **do** confirm the row is there — if it
is not, something is wrong with the deployment and the next reset will be
invisible too.

Related events an incident review will read alongside it:

| Event | Meaning |
|---|---|
| `user.mfa.challenged` | Somebody with a **working password** reached the factor step |
| `user.mfa.failed` | Somebody with a working password guessed a factor wrong |
| `user.mfa.recovery_used` | A recovery code was spent |
| `user.mfa.reset_by_admin` | An administrator cleared somebody's factors |

A cluster of `user.mfa.challenged` or `user.mfa.failed` for one user, followed by
a reset request, is the pattern to stop on. It means somebody already has that
user's password and is now working on the second factor — and the reset request
may be the attacker, not the user.

---

## What to do if you think you were socially engineered

Speed matters more than certainty. In order:

1. Revoke the user's sessions.
2. Reset their password (`POST .../password-reset`), which sends a link to the
   address on record — the real user's mailbox, not the caller's.
3. Reset the factors again, so anything the attacker enrolled is destroyed.
4. Read the audit log for that user around the time of the reset: a
   `user.login.success` shortly after a `user.mfa.reset_by_admin` you performed
   is the attacker using what you gave them.
5. Follow `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md`.

Reporting this quickly is more useful than being right about it. A false alarm
costs one password reset.

---

## Walkthrough record

`docs/PLAN/17` requires this process to have been **walked through**, not merely
written. See [`MEMORY/records/2026-09-13-P3-04-recovery-codes.md`](../MEMORY/records/2026-09-13-P3-04-recovery-codes.md)
§ "The walkthrough" for the transcript of an end-to-end lost-device recovery
executed against the local stack, including what it found.

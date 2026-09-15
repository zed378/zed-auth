---
slug: phase-3-mfa-sessions-and-rotation
title: Phase 3 — two-step verification, sessions you can see, and refresh tokens that rotate
authors: [zed]
tags: [release]
date: 2026-09-14
---

A stolen password is no longer enough on its own, a stolen refresh token stops working the
moment it is used twice, and anyone can see where they are signed in and end it.

Four new guides come with it. Two are for integrators, and they cover the two things an
integration can get wrong in ways that look like the service's fault:
[refresh token rotation](/docs/guides/refresh-token-rotation) and
[step-up with `amr`](/docs/guides/step-up-with-amr).

<!-- truncate -->

### Added

- **Two-step verification with an authenticator app.** Standard six-digit TOTP codes. A
  factor is not switched on until a code proves the app and the service agree. Wrong
  guesses slow down rather than lock the account: five per challenge, and ten in fifteen
  minutes per user.
- **Passkeys and security keys** (WebAuthn), as a second step after the password.
  Registration happens on a Zed Auth page, because a passkey is bound to the website it
  was made for.
- **Recovery codes.** Ten, shown once, each single-use, and forgiving of how they are
  typed. A sign-in with one is recorded as multi-factor but not as `otp`. The user has
  lost their device, and a check that asks for `otp` should not be satisfied.
- **`amr` that tells the truth.** The ID token lists the methods actually used in that
  sign-in (`pwd`, `otp`, `hwk`, and `mfa` when more than one kind was used), never the
  methods merely enrolled. Together with `auth_time` and `prompt=login`, that is enough
  for an application to demand a recent, stronger sign-in before a sensitive action.
- **Refresh token rotation with reuse detection.** Every refresh returns a new token. A
  retired token presented again revokes its whole family, is audited and raises an
  alert. The one exception is a retry within 30 seconds whose replacement was never used,
  which is what a lost response looks like. Families end 90 days after the sign-in,
  however often they are refreshed.
- **Organizations can require a second factor.** Members without one get 14 days, then
  are taken through setting one up at sign-in before any session exists. Administrators
  see how many members it affects before switching it on. The service records when it was
  switched on, so nobody can backdate the deadline.
- **Sessions you can see and end.** Every place you are signed in, with device and last
  activity. Revoke one, or everything but this one. Administrators can do the same for a
  member. Revoking a session revokes its refresh tokens.
- **Your account**, the one console screen built for a phone: password change,
  second factors, recovery codes and sessions, for every user rather than only
  administrators. Changing your password signs out your other sessions.
- **Sign-in anomaly detection.** Sign-ins from a new device or an implausibly distant
  location are recorded and counted. A deployment can email a notice with a **This wasn't
  me** link that signs the account out everywhere and starts a password reset.

### Fixed

- **The breached-password check checked nothing.** The corpus lookup existed and was
  never called on any path that sets a password. Every such path now goes through one
  validator, and a test fails if a new one does not.
- **A passkey was never asked for at sign-in.** A user whose only factor was a passkey
  signed in with their password alone. Both factor types are now challenged, and a test
  signs in with each.
- **A required second factor could go unenforced for ever.** An organization created with
  the mandate already on, or that switched it on during Phase 2 when the console called it
  "not enforced yet", had no start date for its grace period. A missing start date read as
  "still inside the grace period", permanently. Creation now records it, and a migration
  records it for existing organizations, starting the 14 days on upgrade.
- **The console said the mandate did nothing.** The Policies screen kept its Phase 2
  label, "Setting this changes nothing today", after enforcement shipped. An administrator
  told a switch is inert has no reason to warn anybody before flipping it. It now says
  what the setting does and how many members it reaches. The navigation also stopped
  marking four working screens as "arrives in phase 1".
- **Members inside the grace period were never told.** The warning existed and was
  displayed nowhere. **Your account** now shows the deadline.

### Not in this release

Signing in with Google, Microsoft or another social provider is Phase 4, with SAML and
Project Grants. A passkey is a second step, not a replacement for the password, and cannot
be set up on the forced enrolment step. You cannot ask for a particular factor with
`acr_values`: the user chooses, and your application checks `amr`.

*Updated 2026-09-15:* Phase 3 is on the live deployment, and its acceptance checks —
enrolling a factor, signing in with it, recovering from a lost device, reusing a rotated
refresh token, revoking a session — were executed against it. That run also found one more
thing worth saying: **a deployment with no second factors configured could not sign
anybody in**, from the first Phase 3 build that challenged for a factor until the fix in
this release. Every test environment had a factor key configured, so nothing noticed.

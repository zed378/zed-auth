---
id: require-mfa
title: Require two-step verification for your organization
description: For administrators — what switching on the MFA mandate does, the 14-day grace period, measuring who it affects first, and resetting a member who is locked out.
sidebar_position: 8
---

# Require two-step verification for your organization

This page is for **organization administrators**. The people in your organization have
their own page: [set up two-step verification](/docs/guides/two-step-verification).
Consider sending it to them before you start.

## What the setting does

With `mfa_required` on, everybody who signs in through your organization needs a second
factor. Exactly what that means depends on the person:

| The member | What happens at sign-in |
|---|---|
| Already has an authenticator app or passkey | Nothing new. They are asked for it, as they already were. |
| Has no factor, **during the grace period** | They sign in normally. **Your account** in the console shows them the deadline. |
| Has no factor, **after the grace period** | After their password, they are taken to set up an authenticator app, and are given recovery codes on the same step. **No session exists until they finish**, so there is nothing to click past. |

Switching it on signs **nobody** out. It acts at each person's next sign-in.

While the mandate is on, members **cannot remove their only factor**. They can add a new
one and then remove the old one.

Service accounts using `client_credentials` are unaffected: there is no user to ask.

:::note[The forced step offers an authenticator app only]

A passkey cannot be registered on that step. People who want a passkey add it from
**Your account**, before or after.

:::

## The grace period

**Fourteen days**, measured from the moment the mandate is switched on.

- **The service records that moment.** You cannot set it. A request that tries to supply
  `mfa_required_since` is refused, so nobody can backdate the deadline to lock colleagues
  out immediately.
- **Saving other settings does not restart it.** Only a change from off to on does.
- **Switching it off and on again does not buy more time.** The original start time
  stays.

The grace exists so that switching the mandate on is not a lockout. Without it, every
member without a factor would be stuck the moment you saved, and the quickest fix for
that support queue is switching the protection back off.

## Before you switch it on: measure who it affects

```bash
curl -sS "$API/v1/organizations/$ORG_ID/mfa-impact" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "members": 42,
  "without_factor": 17,
  "mfa_required": false,
  "grace_period_days": 14,
  "grace_ends_at": null
}
```

Requires `ORG_ADMIN`. `without_factor` is how many active members hold no second factor.
Those are the people who will meet the enrolment step once the grace ends. Deactivated
users are not counted, because they cannot sign in.

It returns **counts, never names**. A list of which colleagues have no second factor is
not needed to make this decision, and it is exactly the list somebody holding a stolen
administrator token would want.

If `without_factor` is a large share of `members`, tell people first, and send them the
[user guide](/docs/guides/two-step-verification).

## Switching it on

**In the console:** **Policies** → **Multi-factor** → **Require a second factor**, then
**Save policies**. The screen shows how many members have no second factor. Saving asks
you to confirm, and names that number and the grace length.

**Through the API:** requires `ORG_OWNER`.

```bash
curl -sS -X PATCH "$API/v1/organizations/$ORG_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "settings": { "mfa_required": true } }'
```

An organization created with `"mfa_required": true` in its settings is treated the same
way. Its grace period starts when it is created.

Afterwards `mfa-impact` reports the deadline:

```json
{
  "members": 42,
  "without_factor": 17,
  "mfa_required": true,
  "grace_period_days": 14,
  "grace_ends_at": "2026-09-28T09:30:00Z"
}
```

Check `without_factor` as the deadline approaches. It should fall.

## What is recorded

| Event | When |
|---|---|
| `organization.mfa_required.enabled` | The mandate is switched on, including at creation. Carries `grace_ends_at`. |
| `organization.mfa_required.disabled` | The mandate is switched off. |
| `user.mfa.enrolled` | A member's factor becomes usable. |
| `user.mfa.removed` | A member's factor is removed. |
| `user.mfa.recovery_used` | A member signed in with a recovery code. The code is never recorded. |
| `user.mfa.reset_by_admin` | An administrator reset a member's factors. |

Switching the mandate **off** has its own event type on purpose. "Who turned MFA off,
and when" is a question asked during an incident, and it should be one filter in the
audit log, not a search through JSON payloads.

## A member cannot sign in

Most "my code does not work" reports are not lost devices. Check these first:

1. **The phone's clock.** Codes are accepted 30 seconds either side of the current time.
   A phone whose clock has drifted further produces correct codes that are refused.
   Automatic date and time fixes it.
2. **The wrong entry in the app.** Somebody with several accounts may be reading another
   one's code.
3. **Too many attempts.** After ten wrong codes in fifteen minutes, every code is refused
   for a while, including correct ones. **Do not reset their factors to get round this.**
   The pause is working, and resetting for somebody who has been guessing is what a
   successful attack looks like.

If they have lost the device and still have **recovery codes**, they do not need you: they
sign in with one, then replace the factor and the codes from **Your account**.

### Resetting a member's factors

Only when the device is lost **and** they have no recovery codes.

**First, confirm who you are talking to, through a channel the request did not come
from.** Use a video call with somebody you recognize, or call a phone number you already
had for them. Do not rely on an email from the account in question, on facts a colleague
could know, or on urgency. A person insisting the reset cannot wait for a callback is
telling you something. Nothing in the service can make this check for you. **If you
cannot verify them, do not reset.**

Then, in the console, open the user, go to the **Multi-factor** tab, and choose **Reset
multi-factor**. Or use the API, which requires `ORG_ADMIN`:

```bash
curl -sS -X POST "$API/v1/organizations/$ORG_ID/users/$USER_ID/mfa-reset" \
  -H "Authorization: Bearer $TOKEN"
```

This removes every factor and every recovery code the member had, and reports how many.
It returns **no credential**: no code, no password, no session. Their own password is
still the only way in. An administrator who could create a working credential for
somebody else's account could take it over, and it would look like an ordinary sign-in.

After a reset, the member signs in with their password. If the mandate's grace has ended,
they are taken straight to setting up a new authenticator app. Otherwise they should add
one from **Your account** straight away.

## Before you rely on it

- **The deployment must have multi-factor configured.** If it does not, **Your account**
  tells members that second factors are not available, and there is nothing to enrol in.
  The service then lets sign-ins through rather than locking everyone out, and logs an
  error saying the organization requires MFA and cannot enforce it. If you run the
  service, that log line is the one to alert on.
- **A recovery code counts as a second factor** for the mandate. A member who signs in
  with one has met it. A step-up check in an application may still ask for more; see
  [require a stronger sign-in](/docs/guides/step-up-with-amr).

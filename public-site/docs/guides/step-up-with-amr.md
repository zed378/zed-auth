---
id: step-up-with-amr
title: Require a stronger sign-in for sensitive actions
description: Read amr and auth_time from the ID token to decide whether a sign-in was strong and recent enough, and force a fresh one when it was not.
sidebar_position: 6
---

# Require a stronger sign-in for sensitive actions

A valid session says somebody signed in. It does not say they signed in **recently**, or
**with more than a password**. For most of your application that is fine. For moving
money, changing where payouts go, or deleting a project, it may not be.

Step-up means checking at the moment of the sensitive action, and sending the user back
through sign-in when the answer is no. Zed Auth gives you two claims to check and one
parameter to force a fresh sign-in.

## The two claims

Both are in the **ID token**, and only there.

| Claim | What it tells you |
|---|---|
| `auth_time` | When the user actually authenticated, in seconds since the epoch. Staying active does not move it. Only signing in again does. |
| `amr` | How they authenticated: a sorted array of the methods **actually used** in that sign-in. |

`amr` is built from what the user did, never from what they have set up. A user with an
authenticator app who was not asked for it carries `["pwd"]`, not `["mfa", "otp", "pwd"]`.

### The values `amr` can hold

| Value | Meaning |
|---|---|
| `pwd` | A password was used. |
| `otp` | A code from an authenticator app (TOTP) was used. |
| `hwk` | A passkey or security key (WebAuthn) was used. |
| `mfa` | More than one **kind** of factor was used. |

No other values are emitted. The combinations you will see are:

| How the user signed in | `amr` |
|---|---|
| Password only | `["pwd"]` |
| Password, then authenticator app | `["mfa", "otp", "pwd"]` |
| Password, then passkey | `["hwk", "mfa", "pwd"]` |
| Password, then a recovery code | `["mfa", "pwd"]` |

**A recovery code adds `mfa` and nothing else.** It is not `otp`: the user has lost their
device, and a check that asks for `otp` is asking whether they had one. So a
recovery-code sign-in passes a check for `mfa` and fails a check for `otp` or `hwk`.
That is intended. The next thing that user should do is set up a factor again, not pass
your strictest checks.

## Where the check belongs

**The access token does not carry `amr` or `auth_time`.** An API receiving a bearer
token cannot see how the user signed in. The check belongs in the application that
received the ID token: your web backend, or your client in front of the API.

**A refresh does not issue an ID token.** Nothing was authenticated during a refresh,
so there is nothing new to assert. Keep the claims from the ID token of the sign-in. A
refreshed access token does not make that sign-in more recent.

Always verify the ID token first: signature against the JWKS, `iss`, `aud` equal to your
client ID, `exp`, and `nonce` if you sent one. The claims of an unverified token are
whatever somebody typed.

## A worked example: approving a payout

The rule: approving a payout needs a sign-in from the last five minutes that used a
second factor.

```ts
type IdClaims = { auth_time: number; amr: string[] };

const MAX_AGE_SECONDS = 5 * 60;

function strongEnoughForPayout(claims: IdClaims, nowSeconds: number): boolean {
  const recent = nowSeconds - claims.auth_time <= MAX_AGE_SECONDS;
  // `mfa`, not "anything other than pwd". It is the one value that means
  // "more than one kind of factor", including a recovery code.
  const multiFactor = claims.amr.includes("mfa");
  return recent && multiFactor;
}
```

When it returns `false`, send the user back to sign in and come back to the action:

```text
GET /oauth/authorize
  ?response_type=code
  &client_id=YOUR_CLIENT_ID
  &redirect_uri=https://app.example.com/callback
  &scope=openid
  &code_challenge=...&code_challenge_method=S256
  &state=...&nonce=...
  &prompt=login
  &max_age=300
```

- **`prompt=login`** makes Zed Auth ask for credentials even though a session exists.
  Without it, a live session sends the user straight back with the same old
  `auth_time`, and your check fails forever. The user's other sessions are not signed
  out.
- **`max_age=300`** says the same thing another way: a session older than 300 seconds
  is not accepted. Sending both is harmless and makes the intent clear in your logs.

When the code comes back, exchange it, verify the new ID token, and **run the same check
again**. Do not assume the new sign-in satisfied it.

## What step-up cannot do

**You cannot ask for a particular factor.** Zed Auth ignores `acr_values`. After the
password, the user is offered every factor they have set up and chooses one. If your rule
needs `hwk` and the user answers with their authenticator app, the new ID token carries
`otp`, and your check fails again.

Handle that in your own interface rather than looping:

- **`amr` has no `otp` or `hwk` at all:** the user has no second factor, or used a
  recovery code. Signing in again will not change that. Tell them to set one up under
  **Your account**, and link to it.
- **`amr` has the wrong kind of factor:** say which kind the action needs, and offer one
  more try.
- **The sign-in was too old:** that is the case a fresh sign-in actually fixes.

**Step-up is only as strong as the user's weakest enrolled factor.** A rule that demands
`mfa` is satisfied by a TOTP code. If an action needs phishing-resistant authentication,
require `hwk`. Accept that users without a passkey cannot perform it.

## Checklist

- [ ] The ID token is verified before its claims are read.
- [ ] The check uses `auth_time` for recency, not the time your own session started.
- [ ] Multi-factor is checked as `amr` contains `mfa` (or a specific `otp` / `hwk`),
      never as "`amr` is not `["pwd"]`".
- [ ] The re-authentication request carries `prompt=login`.
- [ ] The new ID token is checked again after the redirect.
- [ ] A user who cannot satisfy the rule is told why, instead of being sent round again.

---
id: refresh-token-rotation
title: Handle refresh token rotation without logging your users out
description: Every refresh returns a new refresh token and retires the old one. How reuse detection works, the 30-second retry window, and the client mistakes that trigger a family revocation.
sidebar_position: 7
---

# Handle refresh token rotation without logging your users out

Every time your application uses a refresh token, Zed Auth returns a **new** one and
retires the one you sent. If a retired token is ever presented again, Zed Auth assumes
it was copied, and revokes every token descended from that sign-in.

That is the protection. It also means a client that stores tokens carelessly, or
refreshes from two places at once, logs its own users out. The service did exactly what
it is for. This page is about not being that client.

## The lifetimes

| Token | Lifetime |
|---|---|
| Access token | 10 minutes |
| ID token | 5 minutes |
| Refresh token | 14 days from when **it** was issued. Each rotation issues a new one with a fresh 14 days, but never past the family's end. |
| Refresh token family | 90 days from the original sign-in. **Rotation never extends it.** |

A **family** is every refresh token descended from one sign-in. After 90 days the whole
family stops working however recently it was used, and the user signs in again. That is
a deliberate ceiling: without it, a client that refreshes on a timer would keep a stolen
credential alive indefinitely.

A refresh token also stops working when **the browser session it came from ends**: the
user signs out, revokes that session from **Your account**, changes their password (which
signs out every other session), or an administrator revokes it.

You only get a refresh token if you ask for `offline_access` in the authorization
request, and your application is registered with the `refresh_token` grant type.

## A refresh

Authenticate exactly as you did for the code exchange: the client secret in the
`Authorization` header for a confidential client, or `client_id` alone for a public one.

```bash
curl -sS -X POST "$ISSUER/oauth/token" \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d grant_type=refresh_token \
  -d "refresh_token=$REFRESH_TOKEN"
```

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6…",
  "refresh_token": "q3r8V0m1k…",
  "token_type": "Bearer",
  "expires_in": 600,
  "scope": "openid offline_access"
}
```

Two things to notice:

- **There is a new `refresh_token`.** The one you sent is now retired. Store the new one
  before you do anything else.
- **There is no `id_token`.** Nobody authenticated just now. If you need `auth_time` or
  `amr`, keep them from the original sign-in (see
  [step-up](/docs/guides/step-up-with-amr)).

You may send a narrower `scope` than the original grant. You cannot widen it.

## Reuse detection, precisely

When a refresh token that has already been rotated is presented again, one of two things
happens.

**It is treated as a retry** only if **both** of these are true:

1. It is presented **within 30 seconds** of being rotated, **and**
2. **the token that replaced it has never been used.**

In that case you get a new token pair, and the replacement you never received is revoked.
This covers the case the window exists for: you sent a refresh, Zed Auth rotated and
answered, and the answer never arrived because of a dropped connection, a proxy timeout,
or a process killed mid-response. You still hold the old token, it is the only one you
have, and retrying with it works.

**Otherwise it is treated as theft.** Every token in the family is revoked, including the
newest one your legitimate client is holding. The request is answered with:

```json
{ "error": "invalid_grant", "error_description": "the refresh token is not valid" }
```

That is the same answer as for an expired, revoked or unknown token, deliberately. Whoever
presented a copied token learns nothing about whether it was ever real. On the operator's
side it is not quiet: the event is written to the audit log as
`token.refresh.reuse_detected`, with how many seconds after rotation the old token
reappeared, and it raises an alert.

**The second condition is the one that matters.** Thirty seconds alone would be a
thirty-second gift to anybody who intercepts a token, because they have every reason to
use it immediately. What separates a retry from a replay is that a retry happens *because
the new token never arrived*. If the new token has been used, it did arrive, and whoever
is presenting the old one is somebody else.

## The mistakes that trigger it

### Two refreshes at once

Two browser tabs, two worker processes or two API calls each notice that the access
token has expired and refresh with the same refresh token.

- If the second request arrives before the first request's new token has been used, the
  second counts as a retry. It succeeds, and **the first request's new token is revoked**.
  Whichever copy of your client stored the first result then fails its next refresh.
- If the first new token has already been used by then, the second is a reuse, and **the
  whole family is revoked**.

Either way somebody is signed out. **Refresh from exactly one place at a time.** In a
browser, take a lock across tabs (the Web Locks API, or a `BroadcastChannel` leader)
before refreshing, and re-read the stored token once you hold it, because another tab may
already have refreshed. On a server, keep one in-flight refresh per token, and have every
caller wait for its result.

### Storing the new token after using it

A client that sends a request with the new access token and only then writes the new
refresh token to storage can crash in between. It restarts, reads the old refresh token,
and presents a token whose replacement has already been used. That is a reuse.

**Persist the new refresh token first. Use anything second.** If the write fails, treat the
refresh as failed.

### Retrying forever

A retry with the old token works only within 30 seconds of the rotation, and only while
the replacement is unused. After that, retrying is exactly what an attacker would do, and
it is answered the same way.

On `invalid_grant`, **stop**. Discard every token you hold for that user and send them
through sign-in. Do not loop.

### Restoring tokens from a backup or another device

Syncing a refresh token to a second device, or restoring app storage from a backup, puts
an old token back into circulation. The first time it is used, the family is revoked on
every device. A refresh token belongs to one installation of your client.

## A refresh routine that gets it right

```ts
let inFlight: Promise<Tokens> | null = null;

export function refresh(): Promise<Tokens> {
  // One refresh at a time. Every caller shares the same result.
  inFlight ??= doRefresh().finally(() => {
    inFlight = null;
  });
  return inFlight;
}

async function doRefresh(): Promise<Tokens> {
  const current = await storage.read();

  for (let attempt = 0; attempt < 2; attempt++) {
    let response: Response;
    try {
      response = await postToken({ grant_type: "refresh_token", refresh_token: current.refresh });
    } catch {
      // The answer may have been lost after the service rotated. Retrying once
      // with the same token, immediately, is what the 30-second window is for.
      continue;
    }

    if (response.status === 400) {
      // invalid_grant: expired, revoked, or the family was killed. Not retryable.
      await storage.clear();
      throw new SignInRequired();
    }
    if (!response.ok) {
      // A server error. Same reasoning as a lost response: one immediate retry.
      continue;
    }

    const body = await response.json();
    // Persist before use, so a crash cannot leave the old token in storage
    // after the new one has been spent.
    await storage.write({ access: body.access_token, refresh: body.refresh_token });
    return storage.read();
  }

  throw new NetworkUnavailable();
}
```

In a browser with several tabs, wrap `doRefresh` in a cross-tab lock as well. The
in-memory `inFlight` guard only covers one tab.

## If your users are being logged out

Look in the audit log for `token.refresh.reuse_detected` events for your `client_id`.
`seconds_after_rotation` usually tells you which mistake it is:

| `seconds_after_rotation` | Likely cause |
|---|---|
| Under a second or two | Concurrent refreshes: tabs, workers, or retries racing each other |
| Seconds to minutes | A retry loop, or a crash between using a token and storing it |
| Hours or days | A token restored from storage, synced to another device, or copied. Treat this one as a possible real theft. |

# Staging: the console could not be signed in to, and there was nobody to sign in as

**Date**: 2026-09-11
**Branch**: `main` (operational, after `P1-27` and `P1-29` landed)
**Found while**: checking which applications were registered on staging, after `DV-03` closed.

---

## What was wrong

Three things, each of which alone would have been enough.

**The deployed console was from 2026-09-09.** Before `P1-21` gave it a login, before `P1-22`/`23`/`24` gave it any screens, and before the two fixes earlier that day. `console.zedth.my.id` answered `200` with the Phase 0 shell.

**No console application was registered.** The bundle had no `client_id` baked in, and the database held two applications — the two demos — and nothing else. There was no client for the console to be.

**Staging had zero users.** One organization (`deploy-smoke-test`, an early smoke-test artifact), one project, no accounts at all. Every fixture any smoke script ever created had been cleaned up, correctly.

Together: the console was unreachable *and* unusable *and* there was nobody to use it. `P1-21` through `P1-24` were merged and never deployed — which is exactly what the standing rule about staging exists to prevent, and it went unnoticed because nothing pointed a browser at staging until `P1-27` built something that could.

## Why an account could not simply be created

`P1-19` made it deliberate that no API sets a password for somebody else: an account is created without one, and the person who owns it chooses it through a link sent to their own address. That link is the only place the token exists — `user_tokens` stores a SHA-256 and nothing else.

**Staging had no mail.** So the invitation was created and dropped, and no account on that host could ever have a password. The feature was not broken; it had simply never been reachable there.

## Mail on staging, and one narrow opt-in

Mailpit as a sidecar: it accepts everything and delivers nothing, which is what a staging host wants. Bound to `127.0.0.1:10925` and **deliberately not tunnelled** — it is a mailbox of password-set links, one bearer credential per message. Read over SSH.

That needed one config change. `smtp://` outside local development is refused, because a relay with no STARTTLS hands an invitation link to anything on the path, and that default is right. It is also wrong about one arrangement it cannot see: a sink on the same host, over a container network no packet leaves.

`AUTH_SMTP_ALLOW_CLEARTEXT` is the operator asserting that. Off by default, never inferred, and logged at `WARN` on every start with the host — redacted of any credential, since an SMTP URL can carry `user:password@` — along with what it asserts and what to do when that stops being true.

The same shape as `AUTH_TRUST_PROXY_HEADERS`: a claim the *deployment* makes about its own topology, which the service cannot verify and must not guess. Four tests: refused by default, permitted when asserted, widening exactly one rule and nothing else, and local development still needing no assertion.

## What the bootstrap script does, and refuses to do

`scratchpad/console-bootstrap.sh`, 9 assertions, 0 failures.

It registers the console **through the Management API**, with `allowed_origins` — without which every call the console makes fails in a browser with "CORS" and nothing else (`P1-29`). It creates the owner's account through the API with `send_invite_email: true`, grants `ORG_OWNER` by SQL because `manager_roles` has no API until `P2`, and then reads the invitation out of Mailpit and prints the link.

**It never learns the password and never sets one.** It prints a single-use, time-limited link; a person opens it and chooses their own. That is `P1-19`'s design used as intended rather than worked around, and it is the reason no credential for a real account appears in any transcript or log.

The temporary administrator and bootstrap client it needs — because registering the *first* application requires a token only an already-registered application can obtain (`PG-26`) — are deleted on exit, and the script prints the row counts to prove it.

## Verified

| | |
|---|---|
| The new bundle is live, with the client baked in | `index-Bi7qBG6f.js`, built 09:55 |
| Deep links load (`/projects`, `/users`, `/audit-log`) | 200 |
| `/oauth/authorize` accepts the console's callback | 302 to the login page |
| Its silent-renewal URI is registered too | 302 |
| The login page says **"Sign in to Console"** | ✔ |
| Its CSP permits posting on to the console | `form-action 'self' https://console.zedth.my.id` |
| CORS preflight from the console to `/v1/*` | `access-control-allow-origin: https://console.zedth.my.id`, `vary: Origin` |
| The same preflight from an unregistered origin | `204`, **no** allow-origin — and the same status, so it cannot enumerate |

## Left as it was found

**The organization is still called `deploy-smoke-test`.** It is a leftover from an early deployment smoke test and it is what the console's overview will display. Renaming it is one `PATCH`, and it is Zed's name to choose rather than mine to invent.

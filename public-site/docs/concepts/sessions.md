---
id: sessions
title: Sessions and tokens
description: What single sign-on means in practice, and what each token is for.
sidebar_position: 3
---

# Sessions and tokens

Single sign-on is one sentence — log in once, reach every registered application —
sitting on top of a few pieces worth understanding separately, because they fail
separately.

This describes the design. Token issuance arrives in Phase 1.

## The session

When a user logs in, Zed Auth creates a **session**: the record that this person
authenticated, when, and how strongly. The session lives with Zed Auth, not with the
application the user was heading to.

That is what makes the second application's login invisible. It redirects the user to
Zed Auth, Zed Auth recognizes the existing session, and the user comes straight back
authenticated without seeing a login form. Nothing was shared between the two
applications — both asked the same authority.

Sessions are stored in PostgreSQL, with Redis in front as a cache. The distinction
matters when something breaks: if the cache is lost, sessions survive. Revoking a
session is a write to the database, so a revoked session cannot come back from a cache
that had not noticed yet.

## The tokens

An application receives up to three tokens, and confusing them is the most common
integration mistake.

**The ID token** says who the user is. It is for your application to read: the subject,
the email, when they authenticated. Verify its signature, issuer, audience and expiry,
then use its claims. Never send it to an API as credentials — it was issued to you,
about a user, and it is not proof of anything to a third party.

**The access token** is what you send to an API. It represents the permission your
application was granted to act on the user's behalf. It is short-lived deliberately: a
leaked token stops working quickly, which is the only mitigation that does not depend on
noticing the leak.

**The refresh token** obtains a new access token when the old one expires, without
sending the user through login again. It is long-lived, which makes it the most valuable
thing an attacker can steal, so it is rotated on every use — a refresh token that is
presented twice is treated as evidence of theft and the whole chain is revoked.

## Signing and verification

Tokens are signed with an asymmetric key. Zed Auth holds the private key; the public
key is published at a JWKS endpoint that your application fetches.

The consequence worth knowing: **verification does not require a call to Zed Auth.**
Your API validates a token against the cached public key locally, so an authorization
check does not depend on Zed Auth being reachable at that instant.

Keys rotate on a schedule, with an overlap window during which both the old and new
keys are published. Without the overlap, every token signed with the previous key would
become invalid the moment the key changed — logging out every user at once.

## Logging out

Ending an application's session and ending the Zed Auth session are different actions,
and conflating them surprises people in both directions.

Clearing your application's own cookie logs the user out of your application. Their Zed
Auth session is untouched, so returning sends them straight back in.

Ending the Zed Auth session means the next application to ask will require a fresh
login. Existing access tokens elsewhere remain valid until they expire — which is why
they are short-lived, and why a genuine "log out everywhere" needs session revocation
rather than only clearing a cookie.

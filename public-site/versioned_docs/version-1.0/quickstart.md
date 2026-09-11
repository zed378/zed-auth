---
id: quickstart
title: Quickstart
description: Register an application, run the Authorization Code flow with PKCE, and verify the token — against a running Zed Auth.
sidebar_position: 2
---

# Quickstart

By the end of this page you will have registered an application, sent a user through
login, exchanged the code for tokens, and verified the ID token **locally** — without
calling Zed Auth to do it.

Every command below was run against a live deployment, in order, on 2026-09-11. If one
of them does not work for you, that is a bug in this page.

:::note[What this covers, and what it does not]

This is the **integrator's** path: you have a Zed Auth to point at, and you are
connecting an application to it. Single sign-on, the management API, users and the
audit log are all built and working.

Roles do not appear in tokens yet, and Project Grants and attribute-based policies are
not built — those are Phases 2 and 4. Nothing on this page depends on them.

:::

## Before you start

You need four things, from whoever runs your Zed Auth:

| | Example | Where it comes from |
|---|---|---|
| The **issuer** URL | `https://auth.example.com` | The deployment. Everything else hangs off it. |
| An **organization** id | a UUID | Created by an instance owner. |
| A **project** id | a UUID | Projects hold applications and, later, roles. |
| An **access token** for the Management API | a JWT | Sign in to the console, or complete this flow once with an existing application. |

:::warning[Bootstrapping the very first one is not self-service yet]

Creating the first organization and the first administrator currently requires database
access, and registering your *first* application through the API needs a token that only
an already-registered application can obtain. If you are standing up a new deployment
rather than joining one, that is an operator task today — tracked as **PG-26** in the
project backlog.

Once one application exists, everything below is API-only.

:::

Set them once:

```bash
export ISSUER="https://auth.example.com"
export ORG="<your organization id>"
export PROJECT="<your project id>"
export TOKEN="<your management access token>"
```

Check you can reach the service, and that it is the issuer you think it is:

```bash
curl -sS "$ISSUER/.well-known/openid-configuration" | jq '{issuer, authorization_endpoint, token_endpoint, jwks_uri}'
```

```json
{
  "issuer": "https://auth.example.com",
  "authorization_endpoint": "https://auth.example.com/oauth/authorize",
  "token_endpoint": "https://auth.example.com/oauth/token",
  "jwks_uri": "https://auth.example.com/.well-known/jwks.json"
}
```

The `issuer` in that document must match `$ISSUER` **exactly**, trailing slash included.
A mismatch here is the single most common integration failure, and it surfaces much
later as "invalid token" with nothing pointing at the cause.

## 1. Register your application

Pick the type first, because it decides whether your application can hold a secret and
it **cannot be changed afterwards**:

| Type | Use it when | Secret |
|---|---|---|
| `web` | Your server exchanges the code — a Rails, Django, Go or Node backend | Yes |
| `spa` | A browser application exchanges the code itself | **No.** Refused at the database level |
| `native` | A desktop or mobile application | **No** |
| `api` | A service with no user, using `client_credentials` | Yes |

A browser cannot keep a secret. `spa` and `native` are refused one rather than given one
that would be published the first time anyone opened the network tab; PKCE is what
replaces it.

```bash
curl -sS -X POST "$ISSUER/v1/organizations/$ORG/projects/$PROJECT/applications" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "My Web Application",
    "type": "web",
    "redirect_uris": ["https://app.example.com/callback"],
    "post_logout_redirect_uris": ["https://app.example.com/"],
    "grant_types": ["authorization_code", "refresh_token"]
  }'
```

```json
{
  "id": "3eeb8bb7-025f-45d4-aa92-c7a37908ada7",
  "project_id": "65a30a19-9bc8-4773-b105-97840ac83590",
  "name": "My Web Application",
  "type": "web",
  "redirect_uris": ["https://app.example.com/callback"],
  "grant_types": ["authorization_code", "refresh_token"],
  "has_secret": true,
  "client_secret": "…",
  "created_at": "2026-09-11T09:00:00Z"
}
```

**`id` is the `client_id`.** There is no separate field — two identifiers for one thing
is how an interface shows you the wrong one.

**`client_secret` appears exactly once, here.** It is stored hashed and is never
returned again by any endpoint. If you lose it, rotate it; you cannot retrieve it.

```bash
export CLIENT_ID="3eeb8bb7-025f-45d4-aa92-c7a37908ada7"
export CLIENT_SECRET="…"
```

:::danger[Redirect URIs are matched by exact string comparison]

Not by prefix, not by pattern, not ignoring a trailing slash. `https://app.example.com/callback`
and `https://app.example.com/callback/` are two different URIs and registering one does
not register the other.

This is deliberate. Prefix matching is the open-redirect vulnerability: register
`https://app.example.com` and an attacker sends the code to
`https://app.example.com.evil.test`.

:::

Same call through the console: **Projects → your project → Applications → Register
application**. It is the same API — the console has no private endpoints.

## 2. Send the user to authorize

Generate a PKCE verifier, its challenge, a `state` and a `nonce`. All four are per
sign-in and single-use:

```bash
VERIFIER=$(head -c 40 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')
CHALLENGE=$(printf '%s' "$VERIFIER" | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '=')
STATE=$(head -c 16 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')
NONCE=$(head -c 16 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')
```

Then send the user's **browser** — not a `curl` — to:

```
$ISSUER/oauth/authorize
  ?response_type=code
  &client_id=$CLIENT_ID
  &redirect_uri=https%3A%2F%2Fapp.example.com%2Fcallback
  &scope=openid
  &state=$STATE
  &nonce=$NONCE
  &code_challenge=$CHALLENGE
  &code_challenge_method=S256
```

Four of those parameters are doing security work, and each fails silently if you skip
it:

- **`code_challenge_method=S256`**, never `plain`. A plain challenge *is* the verifier,
  so anyone who can see the authorization request can complete the exchange.
- **`state`** is compared on the way back, against a value your application stored.
  Without it, an attacker can complete a login in the victim's browser using their own
  code, and everything the victim does next happens in the attacker's account.
- **`nonce`** is compared against the ID token's `nonce` claim in step 4. `state`
  protects the callback; `nonce` protects the token.
- **`redirect_uri`** must be one you registered, byte for byte.

The user logs in and arrives back at your callback:

```
https://app.example.com/callback?code=<code>&state=<state>
```

**Compare the `state` before doing anything else**, and consume the stored value so the
same callback cannot be completed twice.

## 3. Exchange the code

Authorization codes are single-use and expire in under a minute.

A **confidential** client (`web`, `api`) authenticates with its secret. Put it in the
`Authorization` header rather than the form body — a secret in a body is a secret in
more logs:

```bash
curl -sS -X POST "$ISSUER/oauth/token" \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d grant_type=authorization_code \
  -d "code=$CODE" \
  --data-urlencode "redirect_uri=https://app.example.com/callback" \
  -d "code_verifier=$VERIFIER"
```

A **public** client (`spa`, `native`) sends `client_id` and no secret. PKCE is what
proves it is the same client that started the flow:

```bash
curl -sS -X POST "$ISSUER/oauth/token" \
  -d grant_type=authorization_code \
  -d "code=$CODE" \
  -d "client_id=$CLIENT_ID" \
  --data-urlencode "redirect_uri=https://app.example.com/callback" \
  -d "code_verifier=$VERIFIER"
```

`code_verifier` is required for **both**. PKCE is not a substitute for a secret that
confidential clients can skip.

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6…",
  "id_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6…",
  "token_type": "Bearer",
  "expires_in": 600,
  "scope": "openid"
}
```

**No `refresh_token`, and that is correct.** Access tokens live ten minutes. If you
want one that outlives the browser session, ask for it explicitly by adding
`offline_access` to the `scope` in **step 2** — registering the `refresh_token` grant
type makes it *permitted*, not automatic. Asking for it is how the user's consent comes
to cover a credential that keeps working after they close the tab.

:::info[Two tokens, two jobs — do not swap them]

| | `id_token` | `access_token` |
|---|---|---|
| What it is | An assertion **about the user**, addressed to you | A capability **at a resource server** |
| `aud` | Your `client_id` | The resource — for the Management API, the issuer |
| `typ` header | `JWT` | `at+jwt` (RFC 9068) |
| Use it to | Establish your application's own session | Call an API |

**Authenticate your users from the `id_token`.** Its `aud` is your `client_id`, which is
what makes "this token was meant for me" a check you can make. The access token's
audience is the same for every application this issuer serves, so a login built on it
would accept any application's token as its own.

:::

If the exchange fails you get OAuth's own error shape, not this API's envelope:

```json
{ "error": "invalid_grant", "error_description": "the authorization code is not valid" }
```

`invalid_grant` is deliberately the same answer for an unknown code, a spent code, an
expired one, and a mismatched `redirect_uri`. An error that distinguished them would
tell whoever holds a code that the code is real.

## 4. Verify the ID token — locally

This is the step people skip, and it is the one that matters. **Do not call Zed Auth to
validate a token.** Fetch the key set once, cache it, and check the token yourself. That
is the difference between an authenticated request costing a network round trip and
costing a signature verification.

```bash
curl -sS "$ISSUER/.well-known/jwks.json"
```

Six checks, all mandatory:

1. **`alg` is compared against what you accept — never used to choose.** Reject anything
   that is not `RS256`. A verifier that trusts the header accepts `none`; one that
   accepts `HS256` from an RSA issuer lets an attacker sign tokens with the *public*
   key, which is published.
2. **`typ` is `JWT`.** An access token is `at+jwt`. Refusing the wrong type here is what
   stops one being presented where the other belongs.
3. **`iss` equals your issuer**, by exact string comparison.
4. **`aud` contains your `client_id`.** This is the check that stops another
   application's token working in yours.
5. **`exp` is present and in the future.** A missing `exp` is not "never expires"; it is
   a token you cannot reason about.
6. **The signature verifies over the first two segments exactly as they arrived** — not
   over a payload you decoded and re-encoded. Re-encoding changes key order and
   whitespace, so you would be checking a signature over a string the issuer never
   signed.

Then compare the `nonce` claim against the value you generated in step 2.

A complete, dependency-free implementation is in the repository at
[`demo/internal/verify`](https://github.com/zed378/zed-auth/tree/main/demo/internal/verify)
— about 200 lines of Go, standard library only. The core of it:

```go
parts := strings.Split(token, ".")
if len(parts) != 3 {
    return fmt.Errorf("not three segments")
}

var header struct{ Alg, Kid, Typ string }
decodeSegment(parts[0], &header)

// Compared, not obeyed.
if header.Alg != "RS256" || header.Typ != "JWT" {
    return fmt.Errorf("not an RS256 ID token")
}

key := keySet[header.Kid] // fetched once, cached

// The bytes AS THEY ARRIVED. Never re-encoded.
digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
    return fmt.Errorf("the signature does not check out")
}

var claims Claims
decodeSegment(parts[1], &claims)

switch {
case claims.Issuer != expectedIssuer:
    return fmt.Errorf("wrong issuer")
case !claims.Audience.contains(myClientID):
    return fmt.Errorf("this token was not minted for me")
case claims.ExpiresAt == 0 || time.Now().Unix() >= claims.ExpiresAt:
    return fmt.Errorf("expired or has no expiry")
}
```

Cache the key set with a short TTL and refetch when a token arrives carrying a `kid` you
do not have. That is all key rotation asks of a consumer: a retiring key stays published
through an overlap window measured in days, so a rotation is invisible to you.

Two complete applications using this code — one confidential, one a public SPA — are in
[`demo/`](https://github.com/zed378/zed-auth/tree/main/demo). They are kept in the
repository precisely so this page has something real to point at.

## 5. Call the API with the access token

```bash
curl -sS "$ISSUER/v1/organizations/$ORG/users" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

```json
{
  "users": [
    { "id": "…", "email": "someone@example.com", "status": "active", "email_verified": false }
  ]
}
```

Everything the console can do is here. There are no console-only endpoints — that is a
standing constraint, not a current state of affairs.

Failures use this API's envelope, which is not OAuth's:

```json
{
  "error": {
    "code": "PERMISSION_DENIED",
    "message": "You do not have permission to perform this action."
  }
}
```

The correlation id is **not** in the body — it is the `X-Request-Id` response header,
present on every response including the successful ones:

```
x-request-id: 3cb4d30132cbe2b22003a8e87c909d34
```

Quote it when you report a problem. It is in the service's logs against the same
request.

## Where to go next

- **[Concepts](/docs/concepts/model)** — organizations, projects, applications and users,
  and why the boundaries fall where they do.
- **[API reference](/docs/api-reference)** — generated from the OpenAPI specification, so
  an endpoint documented there exists.
- **[`demo/`](https://github.com/zed378/zed-auth/tree/main/demo)** — two working
  applications you can copy.

## What is not built yet

So that you do not go looking for it:

| | Arrives in |
|---|---|
| Roles and permissions in tokens | Phase 2 |
| Refresh token rotation with reuse detection | Phase 3 |
| Multi-factor authentication | Phase 3 |
| Project Grants — delegating a project to another organization | Phase 4 |
| Attribute-based policies | Phase 4b |
| SAML | Phase 4 |

The [roadmap](https://github.com/zed378/zed-auth/blob/main/TASKS/PROGRESS.md) is the
board the project actually works from, not a marketing summary.

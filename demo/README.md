# Demo Consumer Applications

Two applications that sign in against the Auth Service. They exist to prove the
thing `docs/PLAN/01-PRODUCT-SCOPE.md` calls the MVP's definition of done — **log
into one, open the other, and you are already signed in** — and to be the
reference a consumer team copies instead of writing their own JWT handling.

They are deliberately separate: two binaries, two containers, two `client_id`s,
two hostnames, two sessions. Two routes inside one process would share a session
and demonstrate nothing.

| | `webapp/` | `spa/` |
|---|---|---|
| Client profile | Confidential | Public |
| Client secret | Yes | **None** — a browser has nowhere to keep one |
| Where the code is exchanged | On the server | In the browser |
| Where the token lives | Server memory, behind an `HttpOnly` cookie | A JavaScript variable that dies with the tab |
| PKCE | Yes (belt and braces — RFC 9700 asks for it anyway) | Yes (it *is* the client authentication) |
| Who validates the token | The application itself | The application's own `/api/me` |

Both validate **locally, against the published key set, with no call back to
the auth service**. That is `docs/PLAN/12`'s single largest available latency
win, and it is demonstrated here rather than assumed:
`TestVerifyingMakesNoNetworkCallOnceTheKeySetIsCached` counts the requests.

## What to read first

`internal/verify/` — about 200 lines, standard library only, and the only
security control either application has. Everything else here is plumbing.

The checks it makes, and why each one is not optional:

- **`alg` is compared against what we accept, never used to choose.** A
  verifier that dispatches on the header accepts `none`, and one that accepts
  `HS256` from an RSA issuer lets an attacker sign with the *public* key.
- **`typ` must be `JWT`.** The service marks access tokens `at+jwt` (RFC 9068)
  precisely so an access token cannot be used where an ID token belongs. That
  is abuse case A-5.
- **`iss` is an exact string comparison.** A trailing slash is a different
  issuer, and the resulting misconfiguration is invisible unless the error
  names both sides.
- **`aud` must contain our own `client_id`.** This is the check that stops
  another application's token working here. Without it, one compromised
  low-value client becomes a key to every high-value one.
- **`exp` must be present and in the future.** Absent is not "never expires";
  it is a token this verifier cannot reason about.
- **The signature covers the first two segments exactly as they arrived.**
  Never re-encoded — see the comment in `signature.go`, which is the subtle
  bug worth reading before you write your own.

### Which token, and why it matters

Both applications verify the **ID token**, not the access token.

The access token's `aud` is the auth service itself: it is a capability at that
API, identical for every application, and it therefore cannot tell one
application's users from another's. The ID token is the assertion addressed to
*this* client, and its `aud` is this `client_id`. A consumer that authenticated
its users from the access token would accept any application's token as its
own — which is exactly the failure `aud` exists to prevent.

## Running them locally

Register two applications through the Management API (or the console), one of
each type. `id` **is** the `client_id`:

```bash
# App A — confidential. The response carries the secret ONCE.
curl -sS -X POST "$ISSUER/v1/organizations/$ORG/projects/$PROJECT/applications" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Demo Web Application","type":"web",
       "redirect_uris":["http://localhost:8090/callback"],
       "post_logout_redirect_uris":["http://localhost:8090/"],
       "grant_types":["authorization_code","refresh_token"]}'

# App B — public. `spa` is refused a secret at the database level.
curl -sS -X POST "$ISSUER/v1/organizations/$ORG/projects/$PROJECT/applications" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Demo SPA","type":"spa",
       "redirect_uris":["http://localhost:8091/"],
       "post_logout_redirect_uris":["http://localhost:8091/"],
       "grant_types":["authorization_code"]}'
```

Redirect URIs are matched by **exact string comparison**, never by prefix — so
the trailing slash on the SPA's is load-bearing.

Then:

```bash
DEMO_ISSUER=http://localhost:8080 \
DEMO_CLIENT_ID=<app A id> DEMO_CLIENT_SECRET=<secret> \
DEMO_BASE_URL=http://localhost:8090 go run ./webapp

DEMO_ISSUER=http://localhost:8080 \
DEMO_CLIENT_ID=<app B id> \
DEMO_BASE_URL=http://localhost:8091 go run ./spa
```

Sign into one, then open the other. The second should not ask again.

## Proving the audience check by hand

The definition of done includes "both reject a token with the wrong `aud`".
With App A signed in, take an ID token minted for it and present it to App B's
API:

```bash
curl -i "$SPA_BASE_URL/api/me" \
  -H "Authorization: Bearer <an ID token minted for App A>" \
  -H "X-Demo-Expected-Nonce: <the nonce that sign-in used>"
# 401, and the body names the audience that did not match.
```

## Tests

```bash
go test ./...
```

The mutation suite for these applications lives with the P1-26 record in
`MEMORY/records/`: fifteen mutations, each removing one check, each killed by a
named test.

## What these are not

Not a library, and not production code. There is no refresh handling, no
back-channel logout endpoint, and sessions live in a map that a restart clears.
Those are deliberate omissions — the point is the token handling, and every line
that is not about token handling is a line making it harder to read.

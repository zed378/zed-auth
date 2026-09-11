# P1-26 — Two Demo Consumer Applications

**Date**: 2026-09-11
**Branch**: `feat/P1-26-demo-apps`
**Spec**: none — the card is `Spec required: No`. It builds no service capability; it consumes ones that exist.

---

## What this is

`docs/PLAN/01`'s MVP definition of done is a sentence about two applications: *log into one, open the other, and you are already signed in.* Everything Phase 1 built is in service of that sentence, and until now nothing had said it out loud.

Two applications, in `demo/`:

| | `webapp/` | `spa/` |
|---|---|---|
| Client profile | Confidential | Public |
| Client secret | Yes, in the `Authorization` header | **None** |
| Where the code is exchanged | On the server | In the browser |
| Where the token lives | Server memory, behind an `HttpOnly` cookie | A JavaScript variable that dies with the tab |
| Who decides | The application | The application's own `/api/me` |

They are genuinely separate: two binaries, two containers, two `client_id`s, two hostnames, two sessions. Two routes in one process would share a session and would be demonstrating the session rather than SSO.

## The module boundary is the point

`demo/` is its own Go module with **no dependencies outside the standard library**.

Both halves of that are deliberate. A separate module means these files can be lifted into a consumer's repository, and it means they *cannot* reach `backend/internal/...` — the round trip a real consumer makes is over HTTP, against published bytes, with no shared types. A demo that verified tokens by calling the same helper the issuer signs with would prove nothing about a consumer.

No dependencies, because a reference implementation that needs a JWT library teaches "pick a library", and picking a library is where the permissive defaults come from. `internal/verify` is about 200 lines and a reader can check all of it.

`check.sh` grew a `Demo applications` section for the same reason the module exists: `go test ./...` inside `backend/` does not cross a module boundary, so without those gates the fixture `P1-27` depends on could rot without a single check turning red. One of them asserts `demo/go.sum` does not exist — a dependency appearing here should be a decision, not a diff nobody read.

## Which token, and why that is the whole security story

Both applications verify the **ID token**. Not the access token, and this is the single most consequential decision in the task.

`internal/oauth/token/claims.go` sets an access token's `aud` to the **issuer**: it is a capability at the auth service's own API, and it is identical for every application this service has ever issued to. A consumer that authenticated its users from the access token would accept any application's token as its own — the exact failure `aud` exists to prevent, arrived at by reading the right claim off the wrong token.

The ID token's `aud` is the `client_id`. That is what makes "reject a token meant for the other application" a check rather than a sentence.

## What the verifier refuses, and why each is not optional

- **`alg` is compared against what we accept, never used to choose.** A verifier that dispatches on the header accepts `none`; one that accepts `HS256` from an RSA issuer lets an attacker sign with the *public* key.
- **`typ` must be `JWT`.** This service marks access tokens `at+jwt` (RFC 9068) precisely so each can be refused where the other belongs — abuse case A-5. Exact match, not "absent is fine": an issuer that omits `typ` should break a consumer loudly rather than silently widen what it accepts.
- **`iss` is an exact string comparison**, and the error names both sides. `P1-04` already found that a trailing-slash mismatch is invisible otherwise.
- **`aud` must contain our own `client_id`.**
- **`exp` must be present and in the future.** Absent is not "never expires".
- **The signature covers the first two segments exactly as they arrived.** Never re-encoded — `signature.go` exists as its own file so that one fact is checkable without reading anything else.

## Two things the tests found before staging did

**A stale-but-known key plus a JWKS blip rejected every valid token.** The first version refetched whenever the five-minute TTL had run out and returned the fetch error if it failed — so an auth service having a bad minute took every consumer down with it, which is the coupling local validation exists to remove. It now falls back to the cached key when it already holds one, and refuses only an unknown `kid`. The refusal for an unreachable key set deliberately does **not** wrap `ErrInvalid`: "we cannot check right now" must stay distinguishable from "your credential is bad", because only one of those should reach a user as a failed login.

**The SPA's nonce had nowhere real to be checked.** The browser never opens the token, so the client cannot compare the nonce it asked for — and a nonce nobody compares is decoration. It now travels to `/api/me` in `X-Demo-Expected-Nonce` and the API enforces it against the verified claim, in constant time.

The empty-header refusal there turned out to be load-bearing rather than defensive: a token whose `nonce` claim is absent decodes to `""`, and comparing `""` with an absent header **matches**. Without the explicit refusal, any nonce-less token is accepted by anyone presenting it with no header — which is every attacker holding a stolen one.

## Mutation testing: 15 mutations, 15 killed — after four survivors taught something

The first run killed 11 of 15. All four survivors were informative rather than embarrassing, and three of them were the same shape:

**The `alg` check, the no-expiry check and the bearer-prefix check are each subsumed by a later check for accept/reject.** An `alg: none` token has no signature, so the signature check refuses it anyway. A missing `exp` decodes to zero, so the expiry comparison refuses it anyway. An absent `Authorization` header yields an empty token, which fails to parse anyway. Removing any of those three changed no outcome, so no test that asked only "was it refused?" could see it.

What each uniquely provides is the **response**, and that is what the tests now assert:

- `alg` refuses **before a key is fetched** — the test counts key-set requests and requires zero, so a stream of `alg: none` tokens cannot be turned into load on the auth service.
- The expiry case must say **"no expiry"** rather than "expired", because the second sends whoever is debugging to look for a clock problem that does not exist.
- No credential gets a plain `WWW-Authenticate: Bearer` challenge and a bad one gets `error="invalid_token"` — RFC 6750's distinction. Both are 401, so the status proves nothing; a client told its token is invalid when it never sent one discards a good session and makes the user sign in again.

The fourth survivor was a genuine gap: the nonce-less token above.

This is worth keeping as a pattern. **A mutation that survives is not always a missing test; sometimes it is a check whose value is not the outcome.** The fix is to assert the thing that is only true when the check is present, not to delete the check.

## Staging

`https://auth.zedth.my.id`, real TLS, real login, real tokens. 33 assertions, 0 failures. The applications are reached on loopback (`127.0.0.1:10940`, `127.0.0.1:10941`) because their public hostnames are a Cloudflare tunnel mapping made in the dashboard; every other hop is the real thing.

The two applications were registered **through the Management API**, not with an `INSERT` — `CLAUDE.md`'s API-first rule, and it exercised `P1-18` on staging as a side effect. The confidential client's secret came back exactly once, at creation; the SPA was refused one and reports `has_secret: false`.

What was proven:

| | |
|---|---|
| Two distinct client IDs, two containers, both loopback-bound | ✔ |
| Application A: own state, own nonce, S256 challenge, code exchanged with the secret in the header | ✔ |
| Application A renders the subject **from the token it verified itself** | ✔ |
| **Application B got a code with no second login** | ✔ |
| Application B exchanged its code with PKCE and no secret at all | ✔ |
| Application B's API returned the same subject as A | ✔ |
| **Application B refuses application A's ID token, 401, naming the audience** | ✔ |
| An access token is refused where an ID token belongs (A-5) | ✔ |
| A token from a different sign-in is refused | ✔ |
| 25 verified requests caused **0** key-set fetches, counted in the auth service's own log | ✔ |

That last row is `docs/PLAN/12`'s biggest claimed latency win, measured at the service rather than asserted at the consumer. It is the only place that can prove nothing arrived.

## Found while running it

**The cleanup trap could not find its compose file and ran six failing statements while reporting success.** The script `cd`s into `deploy/vm` at the start, and the `C=(docker compose -f docker-compose.tunnel.yml)` array captured a *relative* path. By the time the `EXIT` trap fired, the script had `cd`'d elsewhere to build the image, so every cleanup statement failed — and because `q()` prints psql's error to stderr and returns an empty string, the summary line said "demo applications kept:" with nothing after it rather than failing.

The bootstrap client and the smoke admin were left in the staging database. Removed by hand, verified at 0 rows, and the script now holds an absolute path.

This is the same defect class as `P1-19`'s trap-armed-too-late: **a cleanup that fails quietly is worse than no cleanup**, because the tree looks clean and nobody goes to check.

**`projects` has no `deleted_at`.** The fixture query copied the organizations one, errored, returned empty, and fell through to creating a project — which happened to be the right outcome, by accident. `P1-17` deliberately gave projects no soft delete: a delete that would orphan applications is refused outright instead. The query is fixed and the comment says why, so the next person does not copy it back.

## Deliberate omissions

No refresh handling, no back-channel logout receiver, and sessions live in a map that a restart clears. The point is the token handling, and every line that is not about token handling is a line making the reference harder to read. `demo/README.md` says so explicitly, so nobody copies these expecting a framework.

## Closed the same day: the public hostnames

Zed added the two Cloudflare tunnel records on 2026-09-11, and `scratchpad/p126-public.sh` re-ran the proof through them: **15 assertions, 0 failures**, every hop a real DNS name over real TLS. `demo-a.zedth.my.id` redirects to the issuer, one password is typed, and `demo-b.zedth.my.id` then obtains a code with no second login. Demo B refuses a token minted for demo A, naming the audience.

No redeploy was needed, exactly as the note below predicted — which is the useful part of having written it down.

The original note is kept as it stood:

---

**The public hostnames.** `demo-a.zedth.my.id` and `demo-b.zedth.my.id` are registered as the applications' redirect URIs and are what the containers believe their base URLs to be, but the tunnel maps hostnames to ports in the Cloudflare dashboard, which this session cannot reach. Both are running and verified on loopback; they become publicly reachable the moment those two records exist. Until then the browser flow cannot be walked by hand, because the issuer's redirect goes to a name that does not yet resolve.

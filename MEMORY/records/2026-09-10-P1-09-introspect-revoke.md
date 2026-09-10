# P1-09 — Token introspection and revocation

**Date**: 2026-09-10
**Branch**: `feat/P1-09-introspect-revoke`
**Spec**: [`MEMORY/specs/P1-09-introspect-revoke.md`](../specs/P1-09-introspect-revoke.md)

---

## What this is

`POST /oauth/introspect` (RFC 7662) and `POST /oauth/revoke` (RFC 7009). Discovery advertises both, so a resource server that reads the document finds them without being told.

Two small endpoints where every interesting decision is about what they refuse to say.

## The rule that does the most work

**A client may only see or destroy tokens issued to itself.**

An access token carries `client_id`; a refresh token's row carries one. Either way the comparison is against the authenticated client, and a token belonging to somebody else is answered exactly as an unknown one: `{"active": false}`, or `200` and no action. Not `403` — a "forbidden" confirms the token exists and belongs to someone, which is precisely the card's second abuse case.

It is enforced inside `resolve()`, together with identifying the token, and that pairing is deliberate. Separating "what is this token" from "may this client see it" invites a call site that does the first and forgets the second, and the forgotten one is always the security check.

## One answer for everything negative

Unknown, expired, revoked, malformed, another client's, or one whose session has ended: all `{"active": false}`, and nothing else in the body.

```go
var inactive = map[string]any{"active": false}
```

A package-level value rather than a literal per branch, so the uniformity is a property of the code rather than of three call sites agreeing. The temptation is a `reason` field to help an integrator debug; it would tell an attacker holding a captured token whether it ever existed, whether it has been revoked since, and whether it belongs to the client being impersonated — three disclosures for one convenience.

Tested by comparing seven negative cases as whole responses — status, every header sorted, body — with controls at both ends: that the shared answer really is `{"active":false}`, and that an *active* token is distinguishable.

## Two decisions that differ between the endpoints

**Introspection requires a confidential client.** Stricter than RFC 7662, which leaves the method open. A public client has no secret, so admitting one makes the endpoint reachable by anyone who can read a `client_id` out of a browser URL — the token oracle step 1 of the card forbids.

**Revocation admits public clients**, which RFC 7009 §2.1 contemplates. The operation is different, so the reasoning is: a caller can only destroy a token that already belongs to its own client, so the worst a forged caller achieves is throwing away a credential it was already holding.

## What revocation actually does

A refresh token takes its **whole family**, not the row presented — RFC 7009 §2.1's "tokens based on the same authorization grant", and what `P3-06`'s reuse detection will build on.

An access token is the interesting case, because **it cannot be revoked**. It is a signed JWT that nothing looks up, by `docs/PLAN/04`'s deliberate choice. Presenting one revokes the refresh tokens for the same session **and the same client**, which stops new access tokens being minted; the presented token keeps working for up to its remaining ten minutes. The OpenAPI description says so in as many words, because an integrator who assumes `200` means the token is dead has been misled by us rather than by the RFC.

**Session AND client, never session alone.** A session commonly backs several applications — that is what single sign-on *is* — so revoking by session would let one client's logout throw away every other application's refresh token. That is a denial of service one integrator can inflict on the rest of an estate by calling a documented endpoint correctly. The integration test creates a second client with its own refresh token on the same session and asserts it survives.

## What the mutation testing found

Eight mutations, each reverted. Six confirmed their test first time; two were my own mistakes and are worth writing down, because both taught me something about the system.

| Control removed | Test that failed |
|---|---|
| the ownership check on a refresh token | `TestOneClientCannotIntrospectAnothersToken` |
| the ownership check on an access token | `TestAnotherClientsAccessTokenIsInvisible` |
| a `reason` field added to the negative answer | `TestEveryInactiveAnswerIsIdentical` |
| the same, against the shape assertion | `TestTheInactiveAnswerSaysNothingElse` |
| public clients admitted to introspection | `TestAPublicClientCannotIntrospect` |
| **one row of the family revoked instead of the lineage** | `TestRevokingARefreshTokenKillsItsFamily` |
| an access token revoked by session alone | `TestRevokingAnAccessTokenRevokesTheRefreshBehindIt` |
| **the token written into the audit payload** | `TestARevocationIsAuditedWithoutTheToken` |

**The family mutation, first attempt, proved nothing.** I substituted `RevokeForSessionAndClient` for `RevokeFamily` — and the test still passed, because the sibling token in the test shares both the session and the client, so the substitute revoked it too. The mutation was not a weakening. Replacing it with one that revokes exactly one row of the family (`WHERE id = (SELECT id … LIMIT 1)`) is the real risk, and the test catches that.

**The audit mutation found a second layer I had not accounted for.** I plumbed the token through and wrote it into the payload as `token_hash` — and the test passed, because `token_hash` is in `observability.sensitiveKeys` and `audit.redactPayload` replaced it before storage. That is the audit writer doing exactly its job.

It also shows the shape of what redaction can and cannot do. **`IsSensitiveKey` matches key NAMES**, plus the last segment after `.`/`/`/`:`/`-`. Writing the same value under `revoked_token` — not in the list, and `_` is not a separator it splits on — sails straight through, and *that* is what the test catches. So the two layers cover different things: redaction stops the keys somebody thought of, and the test stops the ones nobody did.

Worth stating plainly because the natural conclusion from "the audit writer redacts credentials" is that a call site cannot leak one. It can; it just has to name the field something new.

## The seams this task added

`LifecycleHandler` takes `RefreshTokens`, `Tenant` and `Auditor` as interfaces, the way `login.Handler` does, so the response shapes — which are most of what can go wrong here — are answerable without a database. `RefreshTokens.Lookup` drops the `*postgres.DB` parameter the store's own method takes: binding the database is the adapter's job in `cmd/authservice`, the way `clientLookup` already works.

That came out of a failure rather than a plan. The first version held `*RefreshStore` and `*postgres.DB` directly, and every negative unit test panicked on a nil database, because every negative case falls through the access-token check into the refresh lookup. The instinct was to nil-guard the lookup; that would have been wrong. A nil database in production means the service was constructed incorrectly, and silently reporting every refresh token inactive is a worse failure than a panic — tokens appear dead, clients re-authenticate, and nobody finds out the endpoint is useless. The seam is the honest fix.

## Gap raised

**`PG-19` — per-client rate limiting has a requirement and no owner.** Step 5 of this card says "rate-limit both endpoints per client". `docs/PLAN/05` Part A says only "limit login attempts per account … and per IP"; the per-`client_id` sentence is in **Part B**, about the Management API. `P1-13` builds login limiting, and nothing in Phase 1 builds a general per-client limiter.

Not implemented, and the deferral is smaller than it sounds because both endpoints require client authentication: abuse costs an attacker a valid client secret rather than merely a network connection. What it does not bound is a *compromised* client. `P1-15` is the natural owner — it is where `/v1` gets its middleware, and a per-client limiter belongs beside it rather than being invented twice.

## Also deliberately not done

**`username` is not in the introspection response**, though RFC 7662 lists it. It is an email address, the caller already has `sub`, and a resource server that wants the address can ask `/oauth/userinfo` with the user's own token. Adding it here would write a personal identifier into a response resource servers routinely log.

**Introspection is not audited.** A resource server may call it on every request, so auditing it would add volume proportional to API traffic and no signal — the same call `P1-08` makes about userinfo. Revocations are audited, and a no-op revocation is not: an entry for every retry is volume without signal.

## Verification

- `internal/oauth/token` coverage rose from 81.1% to 84.4%, above its floor.
- The integration tests use two real clients **in the same organization**, so RLS is not what refuses a cross-client request — the ownership rule is, which is the thing under test.
- An audit failure after a successful revocation is logged and **not** rolled back: a token that is actually gone is the outcome the caller asked for, and undoing it to keep the log tidy would trade a security action for bookkeeping.

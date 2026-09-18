# 03 - UserInfo Endpoint

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-08 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes `GET`/`POST /oauth/userinfo`, the endpoint a resource server calls to find out who is holding an access token and what it may say about them.

## Scope

Covers `backend/internal/oauth/userinfo/`. Token verification mechanics are `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md` and `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`.

## As Built

The endpoint is implemented and wired (`backend/internal/oauth/userinfo/handler.go`, `backend/cmd/authservice/main.go`), and is advertised in the OIDC discovery document as `userinfo_endpoint` (see `01-OIDC-DISCOVERY-AND-JWKS.md`) — it is not a placeholder.

OIDC Core 5.3.1 requires both `GET` and `POST`; both are served identically, since the token always travels in the `Authorization` header — the POST form body RFC 6750 permits is deliberately never read, for the same reason bearer credentials are kept out of anything that gets logged.

Request handling, in order:

1. The bearer token is extracted from `Authorization: Bearer <token>`. No credential at all is answered with a bare `WWW-Authenticate` challenge and **no** `error` code — per RFC 6750 §3.1, an error code describes a credential that was presented, and none was.
2. The token is verified as `typ: at+jwt` (RFC 9068) — this is where an ID token presented as a bearer credential is refused, before its payload is even parsed.
3. Claims are validated (issuer, expiry, and that the `openid` scope was granted). A token missing `openid` is refused with **403 `insufficient_scope`**, not 401 — a deliberate RFC 6750 distinction: 401 means "your token is broken," 403 `insufficient_scope` means "your token is fine and does not cover this," and conflating them would send a working client into a re-authentication loop that cannot fix the actual problem.
4. **The session behind the token must still be live** (`sid` claim, checked via the same session-liveness mechanism `/oauth/introspect` uses) — an access token whose session was revoked ten minutes ago is refused here even though its signature and expiry are still valid. See `docs/SESSION-MANAGEMENT/04-TOKEN-REVOCATION-AND-BLACKLISTING.md`.
5. Claims are built (`userinfo.Build`) strictly from the granted scopes.

Every negative outcome from step 1 onward — expired, revoked session, wrong `typ`, bad signature, deactivated user, missing user row — is answered with the **identical** `invalid_token` response; the specific reason is logged server-side and never returned, so a caller cannot use this endpoint to probe whether a particular user or session state exists.

### Claim mapping

| Claim | Present when | Notes |
|---|---|---|
| `sub` | Always | The user's UUID — never the email address (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12 names an email-as-`sub` as a specific thing to avoid) |
| `name`, `preferred_username`, `updated_at` | `profile` scope granted, and the column is non-null | Omitted (not emitted empty) when the underlying column is `NULL` |
| `email` | `email` scope granted, and an address exists | |
| `email_verified` | **Never emitted** | Deliberate: the data model has no verification column, so there is nothing true to assert. Absence means "not asserted"; a fabricated `false` would be worse, since a consumer gating on a verified email would then refuse every user forever on a value this service invented. Tracked as `PG-18` |

`userinfo.Allowed` enumerates every claim this endpoint may ever emit, and a test asserts a response contains nothing outside it — the mechanism that would catch a future claim added without a scope gate.

The response always carries `Cache-Control: no-store` and `Pragma: no-cache`, since it is personal data addressed to one caller.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Required token type | `at+jwt` | `userinfo.Handler.ServeHTTP` |
| Required scope | `openid` | `userinfo.Validate` |
| Session liveness re-checked | Every request | `Subjects.Subject`, via `sid` |
| CORS | Per-application `allowed_origins` allowlist, never `*`, never credentials | `MEMORY/DECISIONS.md` ADR-020 |
| Negative-response uniformity | One `invalid_token` body for every unusable-token reason | `userinfo.Handler.refuse` |

## Interfaces

| Method | Path | operationId |
|---|---|---|
| GET | `/oauth/userinfo` | `userinfo` |
| POST | `/oauth/userinfo` | `userinfo` |

## Security Considerations

- **Token substitution (A-5)**: closed by the `typ: at+jwt` check, shared with `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`.
- **Personal-data minimization**: only what the granted scope authorizes is ever in the response body — `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` § Information Disclosure.
- **`sub` is never an email address**, so it cannot become a permanent cross-reference to an address a user might later change.
- **Metrics never carry the refusal reason**, only a coarse outcome class (`ok`, `invalid_token`, `insufficient_scope`, `error`) — the same reasoning `/oauth/introspect` uses, since `/metrics` is scraped and retained more widely than the audit log.

## Verification

Claim-mapping and scope-gating behavior is exercised by the `oauth/userinfo` package's own test suite and by the token-lifecycle integration tests referenced in `docs/SESSION-MANAGEMENT/04-TOKEN-REVOCATION-AND-BLACKLISTING.md` (`TestARevokedTokenIntrospectsAsInactive`'s sibling coverage for `/oauth/userinfo`'s session-liveness check).

## Not Yet Built / Open Questions

- `email_verified` cannot be emitted honestly until the data model carries a verification column (`PG-18`).

## Related Documents

- `docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`
- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`
- `docs/SESSION-MANAGEMENT/04-TOKEN-REVOCATION-AND-BLACKLISTING.md`
- `MEMORY/specs/P1-08-userinfo.md`
- `MEMORY/DECISIONS.md` ADR-020

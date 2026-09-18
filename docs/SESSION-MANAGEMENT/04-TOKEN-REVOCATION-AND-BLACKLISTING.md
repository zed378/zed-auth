# 04 - Token Revocation

> Category: **Session Management** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-09, P1-10, P3-06, P3-09 &nbsp;|&nbsp; Verified against: `84eb9a2`

Filename note: this document is named `04-TOKEN-REVOCATION-AND-BLACKLISTING.md` to match the existing category structure. **There is no access-token blacklist or deny-list in this codebase.** That is the first fact this document establishes, because the two mechanisms that do exist — session revocation and refresh-token revocation — are easy to describe imprecisely as "the token is now invalid," which is only true for some token types and not immediately for others. See As Built.

## Purpose

States, per credential type, exactly what "revoked" causes and how quickly — so a reader does not assume access-token revocation exists just because sessions and refresh tokens can be revoked.

## Scope

Covers `POST /oauth/revoke` (RFC 7009), `POST /oauth/introspect`'s liveness overlay, and every code path that revokes a session or a refresh token (logout, the sessions Management API, refresh-token reuse detection). Session cache invalidation mechanics are detailed in `00-SESSION-ARCHITECTURE.md`; refresh rotation and family-kill mechanics are `02-REFRESH-TOKENS-AND-ROTATION.md`. This document is the cross-reference that ties them together by *effect*.

## As Built

### Access tokens: not revocable before expiry

Access tokens are signed JWTs, verified locally by whatever resource server holds one, with **no lookup against this service on the ordinary verification path** — deliberately, to keep verification off the hot path of every protected request across every consumer application (`docs/PLAN/12-PERFORMANCE.md`; the reasoning is stated directly in the token package's own comments: "storing them would add a lookup to the hottest path in the system for no security gain"). Consequently, **an access token cannot be individually invalidated before its 10-minute expiry.** `POST /oauth/revoke`'s own handler comment states this in the code: presenting an access token to `/oauth/revoke` revokes the refresh token *behind* it, but "the presented access token itself keeps working until it expires. That is a real limitation."

Two things partially compensate, for a resource server willing to call back to this service:

1. **`POST /oauth/introspect`** checks the session behind the token (`sid` claim) via `Sessions.IsLive` and reports `active: false` for an access token whose session has been revoked, even though the JWT's own signature and expiry are still valid. This is a session-liveness overlay on introspection, not a token blacklist — a resource server that does not call introspect never sees this.
2. **`GET /oauth/userinfo`** performs the same session-liveness check before returning claims (`docs/IDENTITY-PROTOCOL/03-USERINFO-ENDPOINT.md`).

Neither mechanism stores revoked *access* tokens; both re-derive liveness from the session row.

### Refresh tokens: revoked by marking rows, several scopes

A refresh token *is* revocable, because it is opaque and looked up (`backend/internal/oauth/token/refresh.go`). `refresh_tokens.revoked` is a boolean column; revocation is a row update, checked by `refresh_token_by_hash` on every use. Four distinct revocation scopes exist, each a different method on `token.RefreshStore`:

| Method | Scope | Triggered by |
|---|---|---|
| `RevokeFamily` | Every token descended from one original issuance | Reuse detection (`02-REFRESH-TOKENS-AND-ROTATION.md`); `/oauth/revoke` presented a `refresh_token` |
| `RevokeForSessionAndClient` | Refresh tokens for one session, **one client only** | `/oauth/revoke` presented an `access_token` (RFC 7009 §2.1) |
| `RevokeForSessions` | Refresh tokens for one or more sessions, **every client** | Session revocation via `/v1/me/sessions/...` or the admin session API |
| `RevokeAllForUser` | Every refresh token a user holds | `/oidc/logout` with `everywhere=1` (scope `all`) |

The distinction between `RevokeForSessionAndClient` and `RevokeForSessions` is deliberate: a browser session commonly backs several applications (that is what single sign-on is), so an individual application's logout revoking only its own refresh tokens for that session is what stops one client's logout from silently signing every other application out too.

### Sessions: revocation is immediate by construction

Session revocation (`session.Manager.Revoke`, `RevokeAllForUser`, `RevokeOwned`, `RevokeOthers`, `RevokeOrganization`) marks `sessions.revoked_at` and invalidates the Redis cache entry with a tombstone, in that order, after the database commit — described in full in `00-SESSION-ARCHITECTURE.md`. This is the one revocation in the system engineered to be genuinely immediate rather than bounded by a token's own lifetime, and a failed cache invalidation is surfaced as an error rather than silently tolerated.

### What is *not* automatic

Revoking a session does **not**, by itself, revoke the refresh tokens issued through it — the two are separate rows. Every call site that revokes a session for security reasons (logout, the sessions Management API) explicitly *also* calls the matching refresh-token revocation in the same transaction; this is an application-level pairing, not a database cascade (`refresh_tokens.session_id` is `ON DELETE SET NULL`, not `ON DELETE CASCADE`, and revocation is a status flag, not a delete, in both tables). A hypothetical future call site that revokes a session and forgets to revoke its refresh tokens would leave those refresh tokens minting fresh 10-minute access tokens until they separately expire or are used again after the session's liveness check on refresh (`02-REFRESH-TOKENS-AND-ROTATION.md`) catches them.

## Rules and Defaults

| Credential | Revocable before natural expiry? | Mechanism | Propagation |
|---|---|---|---|
| Access token (JWT) | No | — | Expires naturally at 10 minutes; `introspect`/`userinfo` can report it inactive early via session liveness |
| Refresh token | Yes | `refresh_tokens.revoked` | Checked on next use; not pushed to any cache |
| Session | Yes | `sessions.revoked_at` + Redis tombstone | Immediate (bounded by cache invalidation succeeding) |

## Interfaces

| Method | Path | operationId | Effect |
|---|---|---|---|
| POST | `/oauth/revoke` | `revoke` | RFC 7009; always returns 200 regardless of outcome, uniform for enumeration resistance |
| POST | `/oauth/introspect` | `introspect` | Reports `active` including session-liveness for access tokens; requires a confidential client |
| GET/POST | `/oidc/logout` | `logout`/`logoutConfirm` | Session + (with `everywhere=1`) all-refresh-token revocation |
| DELETE | `/v1/me/sessions/{session_id}` | `revokeMySession` | Session + its refresh tokens (all clients) |
| POST | `/v1/me/sessions/revoke-others` | `revokeMyOtherSessions` | Same, for every session but the caller's current one |
| DELETE | `/v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}` | `revokeUserSession` | Administrator equivalent |

Audit events: `token.revoked` (`audit.EventTokenRevoked` — client, kind, count; never the token or its hash), `session.revoked`, `refresh.reuse_detected`.

## Security Considerations

- **Never log or claim a blacklist that does not exist.** Documentation, support answers, and API descriptions must say plainly that a bearer of a stolen access token retains it for up to 10 minutes after a session or refresh token is revoked, unless a resource server actively calls `/oauth/introspect` or `/oauth/userinfo` for every request. This is the accuracy point this document exists to fix.
- **RFC 7009 §2.2's uniform revoke response** (always 200) prevents `/oauth/revoke` from being an oracle on which tokens exist.
- **The audit log never carries a token or its hash** (`PG-14`), including in revocation events — only counts, kinds, and client/user identifiers.

## Verification

| Test | File |
|---|---|
| `TestARevokedTokenIntrospectsAsInactive` | `backend/internal/oauth/token/token_integration_test.go` |
| `TestRevokingAnAccessTokenRevokesTheRefreshBehindIt` | same |
| `TestOneClientCannotRevokeAnothersToken` | same |
| `TestAPublicClientMayRevoke` | `backend/internal/oauth/token/lifecycle_test.go` |
| `TestARefreshTokenFromARevokedSessionIsRefused` | `backend/internal/oauth/token/lifetime_integration_test.go` |

## Not Yet Built / Open Questions

- **Back-channel logout** (OIDC Back-Channel Logout 1.0) is not implemented — only RP-initiated, front-channel `/oidc/logout` exists. Recorded as an open gap: `TASKS/BACKLOG.md` `PG-20`.
- **Cross-tenant/delegated revocation propagation** (a Project Grant revoked while a delegated session is live) is unbuilt — see `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` findings T4-2 and T4-3. Owning tasks `P4-01`, `P4-02`, `P4-04`.
- No mechanism exists to shorten the up-to-10-minute access-token exposure window after a revocation for a resource server that trusts claims rather than calling introspection — this is a documented limitation (T4-14 in the Phase 4 threat review), not a defect to be silently worked around.

## Related Documents

- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`
- `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`
- `docs/IDENTITY-PROTOCOL/03-USERINFO-ENDPOINT.md`
- `docs/PLAN/04-DATA-MODEL.md`
- `MEMORY/specs/P1-09-introspect-revoke.md`, `MEMORY/specs/P1-10-logout.md`, `MEMORY/specs/P3-09-session-management-api.md`
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md`

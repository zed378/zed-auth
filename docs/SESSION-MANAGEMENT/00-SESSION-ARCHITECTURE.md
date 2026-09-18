# 00 - Session Architecture

> Category: **Session Management** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-11, P2-10, P3-09 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes the browser session that makes single sign-on work: the cookie a user's browser holds after signing in at the hosted login page, and the store behind it that `/oauth/authorize`'s silent path, the login page, and the Management API's session endpoints all read and write.

## Scope

This covers the session cookie, its lifetime policy, the PostgreSQL + Redis storage split, and revocation semantics. It does not cover:

- the JWTs issued once a session backs an OAuth code exchange — see `01-JWT-ISSUANCE-AND-STRUCTURE.md`;
- refresh tokens, which outlive the browser session that created them — see `02-REFRESH-TOKENS-AND-ROTATION.md`;
- what "revoked" means for each credential type end to end — see `04-TOKEN-REVOCATION-AND-BLACKLISTING.md`;
- the MFA mandate that can turn a live session into "no session" — see `docs/IDENTITY-PROTOCOL/06-MULTI-FACTOR-AUTHENTICATION.md`.

Design intent for the authorization model this session feeds is `docs/PLAN/08-AUTHORIZATION.md`; the underlying threat model is `docs/SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`.

## As Built

A session is created by `login.Handler.issue` (`backend/internal/login/handler.go`) inside the same transaction as its audit event (`audit.EventSessionCreated`), so a session never exists without a corresponding record — this pairing is a deliberate invariant, not incidental (`MEMORY/DECISIONS.md` ADR-012, referenced in `backend/internal/session/manager.go`). The session's `auth_methods` are recorded at creation from what was actually used to sign in (`mfa.AuthMethodsWithRecovery`), never from what is merely enrolled — this is what makes the `amr` claim on later tokens (`01-JWT-ISSUANCE-AND-STRUCTURE.md`) trustworthy.

Storage is split (`backend/internal/session/manager.go`, `store.go`, `cache.go`):

- **PostgreSQL is authoritative.** The `sessions` table (`backend/migrations/20260908000005_sessions_and_tokens.up.sql`) holds `id`, `user_id`, `org_id`, `auth_methods text[]`, `ip inet`, `user_agent`, `created_at`, `last_seen_at`, `expires_at`, `revoked_at`. Revocation is a status transition (`revoked_at` set), never a delete, so the row survives for the sessions screen and for audit.
- **Redis is a short-TTL lookup copy** in front of it (`session.Cache`, `DefaultCacheTTL` = 1 minute), used on the hot path (`/oauth/authorize`'s silent SSO, budgeted at 150ms p95 per `docs/PLAN/12-PERFORMANCE.md`).

**Revocation is engineered to be immediate despite the cache**, which is the property a cache in front of an authoritative store does not get for free. `session.Cache.Invalidate` is called only after the PostgreSQL commit (never before — a `DEL` before commit lets a concurrent reader repopulate from the pre-commit row), and it writes a tombstone key (`session:revoked:<hash>`) covering the session's remaining lifetime. A concurrent cache-miss reader that is mid-flight when the revocation lands writes its own snapshot through a Lua script (`populate` in `cache.go`) that atomically checks the tombstone and refuses to write if it exists. This closes the specific race a plain `GET`-then-`SET` cache leaves open; the cache TTL remains only as a backstop for the case where the `DEL`/tombstone write itself fails.

The cookie is bootstrapped without a tenant: `session.Store.LookupByTokenHash` runs through a `SECURITY DEFINER` Postgres function (`session_by_token_hash`) rather than the row-level-security-scoped path every other tenant read uses, because resolving a cookie is what establishes which tenant a request belongs to — there is no tenant to scope to yet.

Idle timeout is checked in application code, not SQL (`Session.ExpiredIdle`), because it depends on a per-organization policy the query does not know. Absolute lifetime is enforced by `expires_at` at write time.

`last_seen_at` is throttled to at most one write per minute per session (`touchInterval`), to satisfy the requirement that ordinary request handling does not cost a database write per request.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Cookie name | `__Host-zedauth_session` | `backend/internal/session/session.go` (`CookieName`) |
| Cookie attributes | `Secure`, `HttpOnly`, `Path=/`, no `Domain`, `SameSite=Lax`, no `MaxAge` (browser session cookie) | `session.Cookie()` |
| Token entropy | 256 bits, base64url, unsalted SHA-256 hash stored | `NewToken`, `HashToken` (ADR-016 reasoning) |
| Default absolute lifetime | 12 hours | `session.DefaultPolicy` |
| Default idle timeout | 2 hours, **not** organization-configurable | `session.DefaultPolicy`; `docs/PLAN/08` Part B specifies only the lifetime |
| Absolute lifetime bounds (enforced regardless of configuration) | 5 minutes – 30 days | `session.MinAbsoluteLifetime`, `MaxAbsoluteLifetime` |
| Per-organization lifetime override | `session_lifetime_hours`, clamped 1–720 hours | `authn.LoginPolicy`, `authn.MinSessionLifetimeHours`/`MaxSessionLifetimeHours`, applied via `Policy.WithLifetimeHours` at login (P2-10) |
| Redis cache TTL | 1 minute (`DefaultCacheTTL`) | `session.Cache` |
| `last_seen_at` write throttle | 1 minute (`touchInterval`) | `session.Manager` |
| Revocation cache invalidation ordering | commit, then invalidate | `Manager.Revoke`, `RevokeAllForUser`, `RevokeOrganization` |

## Interfaces

| Method | Path | operationId | Auth | Notes |
|---|---|---|---|---|
| GET | `/v1/me/sessions` | `listMySessions` | bearer, self | Lists the caller's own live sessions |
| DELETE | `/v1/me/sessions/{session_id}` | `revokeMySession` | bearer, self | Session must belong to the caller — bound in one query (`session.Store.RevokeOwned`) so another user's id is the same 404 as a nonexistent one |
| POST | `/v1/me/sessions/revoke-others` | `revokeMyOtherSessions` | bearer, self | Ends every session but the caller's current one; refuses if the token carries no `sid` |
| GET | `/v1/organizations/{org_id}/users/{user_id}/sessions` | `listUserSessions` | bearer, admin | Administrator view |
| DELETE | `/v1/organizations/{org_id}/users/{user_id}/sessions/{session_id}` | `revokeUserSession` | bearer, admin | |

Both `revokeMySession` and `revokeUserSession` revoke every refresh token issued through the session, for every client, in the same transaction as the session revocation (`token.RefreshStore.RevokeForSessions`), via `backend/internal/sessionapi/handler.go`.

Rendered session fields deliberately omit IP address and raw user agent — only parsed browser/OS (`anomaly.ParseDevice`) and a coarse location are returned; the audit log keeps the address (`backend/internal/sessionapi/handler.go`, `render`).

Audit events: `session.created`, `session.revoked` (payload carries `session_id`, `reason` — one of `logout`, `admin`, `reauth`, `logout_all`, `self_service`, `logout_others`, or `organization_deleted` — never the token or its hash, per `PG-14`).

## Security Considerations

- **Cross-tenant session reuse** is refused at `/oauth/authorize`: a session whose `org_id` does not match the requesting client's organization is treated as no session (`backend/internal/oauth/authorize/handler.go`). See `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §3.
- **Session fixation on re-authentication**: signing in again while an old session cookie is still presented revokes the old session (`session.ReasonReauth`) before issuing the new one (`login.Handler.issue`).
- **Stolen cookie**: `Secure`+`HttpOnly`+`__Host-` prefix bound the theft vector to XSS or a compromised device; `RevokeAllForUser` ("log out everywhere") plus refresh-token revocation is the recovery path.
- **A malformed or guessed cookie value never reaches the database**: `ValidToken` shape-checks length and alphabet before any lookup.
- **`SameSite=Lax`, not `Strict`**: a deliberate trade — `/oauth/authorize` is reached by a top-level cross-site GET from the consuming application, which `Strict` would silently drop the cookie on. `Lax` still withholds the cookie on cross-site POST, which is the CSRF-relevant case.

## Verification

| Test | File |
|---|---|
| `TestCookieAttributes` | `backend/internal/session/session_test.go` |
| `TestClearCookieMatchesTheSetCookie` | same |
| `TestLiveAppliesBothBounds`, `TestZeroIdleTimeoutMeansNoIdleBound` | same |
| `TestPolicySanitize`, `TestSanitizeReportsWhatItChanged` | same |
| `TestWithLifetimeHoursClampsRatherThanRefusing`, `TestTheHourBoundsMatchTheDurationBounds` | same |
| Session ownership and revocation (integration, real Postgres) | `backend/internal/session/owned_integration_test.go` |
| Organization-wide revocation | `backend/internal/session/revoke_organization_integration_test.go` |
| Admin session list carries no fingerprint of a member's session | `backend/internal/sessionapi` package tests, named in `MEMORY/records/2026-09-15-P3-14-test-suite.md` (`TestAnAdminSeesNoFingerprintOfAMembersSession`) |

## Not Yet Built / Open Questions

- **Cross-organization sign-in** (a session from one organization used against another's application via a Project Grant) is decided but unbuilt: `MEMORY/DECISIONS.md` ADR-025 specifies that both organizations' policies apply and the stricter wins (MFA required if either requires it; sign-in methods are the intersection; session lifetime is the shorter). Owning task: `P4-04`. Today the `org_id` comparison in `/oauth/authorize` refuses this outright.
- Per-organization idle timeout is not configurable — only the absolute lifetime is (`docs/PLAN/08` Part B specifies only the lifetime).

## Related Documents

- `docs/PLAN/04-DATA-MODEL.md` § sessions, § refresh_tokens
- `docs/PLAN/08-AUTHORIZATION.md` Part B
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §3
- `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`
- `docs/SESSION-MANAGEMENT/04-TOKEN-REVOCATION-AND-BLACKLISTING.md`
- `MEMORY/specs/P1-11-session-management.md`, `MEMORY/specs/P3-09-session-management-api.md`

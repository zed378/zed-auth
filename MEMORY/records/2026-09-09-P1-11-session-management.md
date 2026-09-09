# P1-11 — Session Management and the SSO Cookie

| | |
|---|---|
| **Date** | 2026-09-09 |
| **Task** | `TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-11 |
| **Phase** | Phase 1 |
| **Surface** | backend |
| **Author** | Zed |
| **Commits / PR** | `feat/P1-11-session-management` |
| **Status** | Completed |

---

## What Changed

`internal/session` implements the browser session single sign-on runs on: a 256-bit cookie token, a PostgreSQL record, a Redis lookup cache, revocation that takes effect on the next request rather than after a TTL, and an hourly sweep. Wired into the service with a Redis client, a readiness check, metrics and two alerts.

Three migrations: `sessions.token_hash` (PG-14), and two `SECURITY DEFINER` functions for the reads that cannot be tenant-scoped.

This is what unblocks `P1-06`, `P1-10` and `P1-12`.

## Why

`PLAN/03`'s data flow, step 6 — the second application skips login. That is the whole reason to run a central identity provider rather than a login form per application, and it comes down to one cookie and one lookup.

Everything else in the task exists because that cookie is a bearer credential with the same power as the password that created it. It lives in a browser for hours, travels on every navigation to this origin, and cannot be un-issued once stolen — only revoked, and only if revocation is genuinely immediate.

## How

**The cookie is `__Host-zedauth_session`.** The prefix is not decoration: a browser refuses to store a `__Host-` cookie unless it is `Secure`, has `Path=/`, and has no `Domain` — exactly the attributes `PLAN/05` asks for. Writing them correctly protects against our own mistakes; the prefix makes the browser reject the mistake instead, including one introduced later by us. It costs nothing, because those are the attributes we want, and it works in development since browsers treat `localhost` as a secure context.

`SameSite=Lax` rather than `Strict` is a requirement, not a compromise: `/oauth/authorize` is reached by a top-level GET navigation from the consumer application, and `Strict` withholds the cookie on exactly that navigation. Silent SSO would never work.

**The interesting engineering is the revocation guarantee.** A cache in front of an authoritative store normally means revocation waits for a TTL, and the DoD forbids that. So the ordering is explicit — commit to PostgreSQL, *then* delete the cache entry, because deleting first lets a concurrent reader repopulate from the pre-commit state and the stale entry outlives the revocation.

That leaves one race, and it is stated precisely in `cache.go`: a reader misses, reads a live row, and is still holding it when the revoker commits and deletes the (absent) key; the reader then writes its stale snapshot. The window is short and the fix is cheap, so it is closed rather than documented away. Revocation writes a tombstone covering the session's remaining lifetime, and the populate is a Lua script that refuses to write when the tombstone exists — atomic, because between a plain `EXISTS` and a plain `SET` the same race reopens. One extra key per revocation, one script on cache misses only.

The TTL stays as a backstop for a failed `DEL`: bounded staleness rather than permanent, with a counter and an alert, because the guarantee is only as good as somebody noticing when it fails.

**Sessions are the one thing that cannot be tenant-scoped**, because the session is what establishes the tenant. Instance scope is not the answer — `sessions_tenant_isolation` is `org_id = current_org_id()`, so with no tenant set the comparison is NULL and nothing matches. Instance scope means "no tenant", not "every tenant", which is `P0-08` working as designed. So the bootstrap gets its own narrow door: `session_by_token_hash`, `SECURITY DEFINER` with a pinned `search_path`, granted to `auth_app`, doing exactly one thing. The authorization argument is that the caller already presented the token; possession is the credential the row exists to check.

Idle timeout and absolute lifetime both apply, shorter wins. `last_seen_at` is written at most once a minute per session — `NFR-5` forbids a write on every authenticated request, and this column is a display value and a fallback bound rather than the enforcing one.

## Files and Components Touched

| Path | Change |
|---|---|
| `MEMORY/specs/P1-11-session-management.md` | New — the spec `CLAUDE.md` requires |
| `backend/internal/session/session.go` | New — `Token`, expiry, `Policy` clamping, cookie construction |
| `backend/internal/session/store.go` | New — the PostgreSQL record |
| `backend/internal/session/cache.go` | New — Redis, write-through invalidation, the tombstone |
| `backend/internal/session/manager.go` | New — composition, audit, throttled touch |
| `backend/internal/session/*_test.go` | New — 27 tests |
| `backend/migrations/20260909000011_session_token_hash.*` | PG-14 |
| `backend/migrations/20260909000012_session_lookup_function.*` | The two bootstrap functions |
| `backend/internal/observability/logging.go` | `session_id` is no longer redacted; `token_hash` still is |
| `backend/internal/observability/metrics.go` | Four session instruments |
| `backend/cmd/authservice/main.go` | Redis client, manager, sweep loop, readiness check |
| `deploy/observability/alerts.yml` | Two alerts (19 rules, promtool-validated) |
| `scripts/check-coverage.sh` | Floor for `internal/session` |
| `backend/go.mod` | `github.com/redis/go-redis/v9` — the first Redis client in the project |

## Decisions Made

| Decision | Rationale |
|---|---|
| The cookie carries a token, not the row id | **PG-14**, below |
| `__Host-` prefix | The browser enforces what we would otherwise only assert |
| `SameSite=Lax`, not `Strict` | `Strict` withholds the cookie on the top-level navigation silent SSO depends on |
| `Secure` unconditional, not a parameter | gosec flagged it, and the flag's only reachable value was `true` |
| Write-through invalidation plus a tombstone-guarded populate | Makes "revocation is immediate" a property of the system, not of the TTL |
| A `SECURITY DEFINER` function for the token lookup | Instance scope reads nothing under the RLS policy, and relaxing the policy would open every session to any instance-scoped code |
| `last_seen_at` throttled to once a minute | A write per authenticated request would put the database on the silent-SSO hot path |
| Revoked rows kept 7 days before sweeping | They are what explains to a user why they were signed out, and what an investigation reads |

### PG-14 — the cookie must not carry the primary key

`PLAN/04` and `P0-07`'s migration comment both describe the cookie as carrying the row's `id`. Opaque it is; safe to expose it is not, and the data model already contains the places it gets exposed.

`PLAN/05` routes `/v1/organizations/{org_id}/users/{user_id}/sessions`. An administrator listing another user's sessions would receive, per row, the exact string that authenticates as that user — a screen whose purpose is to be looked at would be a credential-disclosure endpoint. `refresh_tokens.session_id` is a foreign key, so a token row would carry a live session credential. A revocation audit event naming its `session_id` would write one into an append-only table with 24-month retention.

Reusing `id` and simply never displaying it was considered and rejected: that is a rule every future endpoint, screen and log line has to remember, and one of them will not.

## Deviations from the Plan

`PLAN/04` § `sessions` should be amended to describe `token_hash` and to stop describing the cookie as carrying the id. Raised as `PG-14` rather than made, per `AGENTS.md` rule 9.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | Token entropy, non-determinism over 100 draws, redaction under seven format verbs and inside a struct, marshalling refused, and the control that `Reveal` returns something real |
| Unit | `ValidToken` across nine malformed shapes including a UUID and a SQL fragment |
| Unit | `Live` across both bounds and each boundary exactly, plus clock skew and the zero-idle case |
| Unit | `Policy.Sanitize` across floor, ceiling, idle-exceeds-absolute, and the reporting of each clamp |
| Unit | Every cookie attribute asserted on the `Set-Cookie` string, the absence of `Domain` and of `Max-Age`, the `__Host-` prefix, and that the clearing cookie matches in every attribute |
| Integration | Only the hash is stored — the whole row inspected as text, plus every audit payload |
| Integration | Revocation effective on the very next call, with the session deliberately warm in the cache first |
| Integration | The repopulate race driven deterministically: read, revoke, then attempt the populate |
| Integration | Fixation: a fresh token per login and the previous one dead |
| Integration | Lookups survive Redis being unavailable |
| Integration | Expiry, the sweep, and that it leaves live sessions alone |
| Integration | Revoke-everywhere, double revocation as a no-op, throttled `last_seen_at`, idle expiry from cache, unparseable IP, oversized user agent |

**Both guards were proven non-vacuous** by removing them: deleting the tombstone check makes the race test fail with "a stale snapshot was cached after the session was revoked"; making `Invalidate` a no-op makes the immediacy test fail with "a revoked session still resolves".

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Session fixation | `SECURITY/02` §4 | `TestLoginIssuesAFreshTokenAndTheOldOneDies` |
| Stolen cookie replayed | `SECURITY/02` §4 | Revocable and immediate — `TestRevocationTakesEffectOnTheNextRequest` |
| Cookie over plain HTTP | `PLAN/09` | `Secure` unconditional, plus the `__Host-` prefix; asserted on the header |
| Cookie read by injected script | `SECURITY/02` §6 | `HttpOnly`, asserted on the header |
| Cross-site request rides the session | `SECURITY/02` §5 | `SameSite=Lax`, asserted on the header |
| Token harvested from the sessions API or an audit log | `SECURITY/02` §16 | PG-14; `TestOnlyTheTokenHashIsStored` |
| Revoked session used during a cache window | Task DoD | `TestARevocationDuringAnInFlightReadIsNotOverwritten` |
| Token brute-forced | — | 256 bits, ADR-016's argument |
| Expired sessions accumulate | DoD item 4 | `TestExpiredSessionsAreUnusableAndSwept` |

## Definition of Done Verification

- [x] Tests at the appropriate pyramid layer
- [x] Every abuse case has an automated test
- [x] No API surface changed — `P1-06`/`P1-10`/`P1-12` add the endpoints
- [x] Session creation and revocation write audit events, neither carrying a token
- [x] Nothing sensitive is logged
- [x] `scripts/check.sh` green (40/40)
- [x] `TASKS/PROGRESS.md` and the phase file updated

Task-specific DoD:

- [x] **The session identifier changes after login.** A fresh token per login, the previous session revoked, and the old token asserted dead.
- [x] **Cookie attributes exactly as specified, asserted on the `Set-Cookie` header.** Plus the absence of `Domain` and `Max-Age`, which the struct alone would not show.
- [x] **Session lookup within `PLAN/12`'s budget.** One Redis round trip on the warm path; the histogram is labelled by source so the hit rate and the latency read from the same metric. Measured under load in `P1-27`, not here.
- [x] **Expired sessions are unusable and cleaned up.** Filtered in SQL inside the lookup function so an expired row cannot reach the cache; swept hourly with a 7-day retention.
- [x] **Revoking takes effect immediately, not after a cache TTL.** The central mechanism, proven non-vacuous both ways.
- [x] **`auth_methods` reflects reality.** Required at creation — a session without it cannot be created, because `P1-07` would build `amr` from a value that was never true.

## What Did Not Work

**Instance scope reads no sessions.** The first design had `LookupByTokenHash` use `WithInstanceScope`, on the assumption that it is the cross-tenant path. It is not a bypass: `sessions_tenant_isolation` is `org_id = current_org_id()`, so with no tenant set the comparison is NULL and every row is filtered out. Five tests failed with "not found" for a session that plainly existed.

That is `P0-08` working correctly, and the fix is not to weaken the policy — admitting NULL would open every session to any instance-scoped code path, a large hole opened for one narrow need. Instead the bootstrap got its own door: a `SECURITY DEFINER` function granted to `auth_app`, on the same pattern `P0-12` used for partition maintenance. Using instance scope would also have logged cross-tenant access on every cache miss and tripped `UnexpectedInstanceScopedAccess`, which would have been noisy and untrue — resolving a cookie is not reaching across tenants, it is finding out which one you are in.

The sweep had the identical problem for the identical reason, and got the identical fix.

**`session_id` was in the logger's redaction list, and the audit test caught it.** Correctly, when it was written: `PLAN/04` had the cookie carrying the id, so the id was a credential. PG-14 inverts that — the id is now the safe identifier and `token_hash` is the credential — and continuing to redact it would have made the audit log unable to say *which* session was revoked, defeating the point of the separation. Removed, with the reasoning written where the list is.

**gosec flagged the cookie, and it was right for a better reason than it gave.** `Secure` was a `bool` parameter so gosec could not prove it was set. The real problem is that a parameter is the only way the control could ever be off, and no case needs it: the `__Host-` prefix requires `Secure`, and browsers treat `localhost` as a secure context. Removed the parameter rather than suppressing the warning.

**`text[]` again.** `auth_methods` hit the same wall as `P1-05`'s redirect URIs — `database/sql` returns a raw Postgres array literal and there is no standard scanner. Same fix: select through `to_jsonb`.

**The sweep counted one row however many it deleted.** The first version scanned a single `RETURNING 1`, so it reported "1" for any non-empty batch. A maintenance job that cannot count is a job that cannot tell you whether it is keeping up — the exact blind spot `BL-01` was about. Counted in SQL now, and the test asserts the number.

## Follow-Ups and Open Questions

- `P1-06` reads the session for silent SSO; `P1-12` creates one after login; `P1-10` revokes one. Until then this package has no caller — the third in a row, and `P1-06` is what starts connecting them.
- `P1-07` must build `amr` from `AuthMethods`.
- Session lifetime is read from `Policy`, but nothing yet reads `session_lifetime_hours` out of `organizations.settings` — `PolicyFromHours` exists and is untested against a real row. `P1-12` should wire it, the way `P1-02`'s `PolicyStore` does for passwords.
- `PLAN/04` § `sessions` needs amending for PG-14.
- Concurrent session limits are named in the task's abuse cases and are not implemented; there is no limit today. Worth a decision in Phase 3 alongside the sessions screen.

## What to Watch

**`auth_session_cache_invalidation_failures_total`.** The immediacy guarantee holds because the cache entry is deleted when the revocation commits. If that delete fails, the session stays usable until its entry expires, and this counter is the only signal. `SessionCacheInvalidationFailing` alerts on any occurrence — not a rate to tune, a security control failing.

**The cache hit rate.** `auth_session_lookup_duration_seconds{source}` shows it. A collapsed hit rate means the silent-SSO path is running on database round trips, and it will surface as latency long before anyone connects it to Redis.

**The two `SECURITY DEFINER` functions.** They are the only paths that read or delete sessions outside a tenant scope, and they are narrow on purpose. Anything that widens them — returning more columns, accepting a different argument, adding a second caller — is widening a hole in tenant isolation and should be read as such.

**Sessions never being swept.** The loop is unsupervised, like the partition maintenance that `P0-11` later had to instrument. If it stops, nothing reports it and the table grows quietly. Worth a gauge when somebody next touches this.

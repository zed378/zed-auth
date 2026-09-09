# P1-11 — Session Management and the SSO Cookie

Feature specification, per `PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. `CLAUDE.md` requires one for anything touching authentication; the task card calls this "the mechanism SSO depends on".

---

## 1. Business Objective

Make step 6 of `PLAN/03`'s data flow work: a user who has authenticated once at the Auth Service reaches the *second* application without seeing a login screen. That is the entire value proposition of running a central identity provider rather than a login form per application, and it is one browser cookie and one lookup.

Everything else in this task exists because that cookie is a bearer credential with the same power as the password that created it. It survives in a browser for hours, travels on every navigation to this origin, and cannot be un-issued once stolen — only revoked, and only if revocation is actually immediate.

## 2. Actors

| Actor | Interaction |
|---|---|
| End user's browser | Holds the cookie, presents it on every request to this origin |
| `/oauth/authorize` (`P1-06`) | Asks "is there a live session, and whose?" — on the latency-critical silent-SSO path |
| The hosted login page (`P1-12`) | Creates a session after successful authentication |
| `/oidc/logout` (`P1-10`) | Revokes one session |
| The sessions screen (Phase 3, and `P1-19`'s API) | Lists a user's sessions and revokes them |
| `P1-07`'s token endpoint | Reads `auth_methods` to build the `amr` claim |
| An attacker who has stolen a cookie | Replays it until it is revoked or expires |
| An attacker who can set a cookie before login | Attempts session fixation |

## 3. Functional Requirements

- **FR-1** Create a session on successful authentication with `user_id`, `org_id`, `auth_methods`, `ip`, `user_agent`, `created_at`, `last_seen_at`, `expires_at` (`PLAN/04` § `sessions`).
- **FR-2** `auth_methods` records the factors actually used, accurately, from Phase 1 onward.
- **FR-3** The cookie is `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, with no `Domain` attribute.
- **FR-4** The cookie carries a cryptographically random opaque token and no user data.
- **FR-5** The session token presented before authentication is never adopted afterwards.
- **FR-6** Redis serves the lookup; PostgreSQL is authoritative (ADR-003).
- **FR-7** Enforce an absolute lifetime and an idle timeout, defaulting from `organizations.settings.session_lifetime_hours` (`PLAN/08` Part B).
- **FR-8** Record IP and user agent; do **not** fail on a change to either.
- **FR-9** Revocation of one session and of all a user's sessions, effective on the next request.
- **FR-10** Expired sessions become unusable and are removed rather than accumulating.

## 4. Non-Functional Requirements

- **NFR-1** Session lookup must fit inside `PLAN/12`'s `/oauth/authorize` silent-SSO budget: p95 < 150ms for the whole endpoint, so the lookup itself has to be a single Redis round trip in the common case.
- **NFR-2** The service stays stateless (`PLAN/12` § Design Decisions): no session state in process memory, so any instance can serve any request.
- **NFR-3** Redis being unavailable degrades latency, not correctness — the read path falls back to PostgreSQL.
- **NFR-4** No session token, and no hash of one, reaches a log, a metric label, an error message, or an audit payload.
- **NFR-5** `last_seen_at` maintenance must not put a write on every authenticated request.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P0-07` | `sessions` exists with `auth_methods`, `revoked_at`, `last_seen_at` and the expiry index |
| `P0-08` | Sessions are tenant-scoped through the RLS storage API |
| `P0-05` | Redis is in the stack and configured |
| `P0-12` | Session creation and revocation are audited |
| `P1-02` | `ParsePolicy` reads organization settings; the session lifetime comes from the same document |

Depended on by: `P1-06` (silent SSO), `P1-10` (logout), `P1-12` (login), `P1-07` (`amr`), `P1-19`/Phase 3 (the sessions screen).

## 6. Database Changes

**One additive migration**, and it closes a plan gap.

```sql
ALTER TABLE sessions ADD COLUMN token_hash text;
CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
```

### PG-14 — the cookie must not carry the primary key

`PLAN/04` § `sessions` and `P0-07`'s migration comment both say the cookie carries the row's `id`: *"Stored as an opaque cookie ID in the browser."* Opaque it is; safe to expose it is not, and the data model already contains the places it gets exposed.

`PLAN/05` Part B routes `/v1/organizations/{org_id}/users/{user_id}/sessions`. An organization administrator listing another user's sessions would receive, for each row, the exact string that authenticates as that user. The sessions screen would be a credential-disclosure endpoint whose whole purpose is to be looked at.

It does not stop there. `refresh_tokens.session_id` is a foreign key, so a token row carries a live session credential. Any audit payload naming a `session_id` — and revocation events will want to — writes one into an append-only table with 24-month retention. A support ticket quoting a session id from a screen is a handover of the account.

So: the cookie carries a fresh 256-bit token, the database stores only `sha256(token)`, and `id` stays an internal identifier that is safe to display, join on, and audit. The token is verified by looking up its hash, exactly as `P1-05` verifies a client secret.

Nullable, no backfill: there are no sessions yet. `PLAN/04` should be amended through the deliberate plan-change process (`AGENTS.md` rule 9); registered in `TASKS/BACKLOG.md`.

**Why not reuse `id` and simply never display it?** Because that is a rule every future endpoint, screen and log line has to remember, and one of them will not. Separating the credential from the identifier makes the rule unnecessary.

## 7. API Contract

No new endpoints. `P1-06`, `P1-10` and `P1-12` consume this package. The contract fixed here is the cookie:

```
Set-Cookie: __Host-zedauth_session=<43-char base64url>; Path=/; Secure; HttpOnly; SameSite=Lax
```

The **`__Host-` prefix** is a browser-enforced version of the attributes FR-3 asks for: a browser refuses to store a `__Host-` cookie unless it is `Secure`, has `Path=/`, and has no `Domain`. Writing the attributes correctly protects against our own future mistakes; the prefix makes the browser reject the mistake instead. It costs nothing — the attributes are the ones we want anyway — and it works on `http://localhost` because browsers treat localhost as a secure context.

`SameSite=Lax` rather than `Strict` is required by the flow, not a compromise: `/oauth/authorize` is reached by a top-level GET navigation from the consumer application, and `Strict` withholds the cookie on exactly that navigation. Silent SSO would never work. Lax sends it on top-level GET and withholds it on cross-site POST, which is the CSRF-relevant half.

## 8. Frontend Changes

None here. `P1-12`'s login page and Phase 3's sessions screen consume this.

## 9. Backend Changes

New package `internal/session`:

```go
type Session struct {
    ID          string    // internal identifier, safe to display and audit
    UserID      string
    OrgID       string
    AuthMethods []string
    IP          string
    UserAgent   string
    CreatedAt   time.Time
    LastSeenAt  time.Time
    ExpiresAt   time.Time
}

type Token struct{ plaintext string } // same discipline as client.Secret

type Policy struct {
    AbsoluteLifetime time.Duration // from organizations.settings
    IdleTimeout      time.Duration
}

func (m *Manager) Create(ctx, tx, New) (Session, Token, error)
func (m *Manager) Lookup(ctx, token string, now time.Time) (Session, error)
func (m *Manager) Revoke(ctx, tx, sessionID string) error
func (m *Manager) RevokeAllForUser(ctx, tx, userID string) error
func (m *Manager) Sweep(ctx) (int, error)
```

`Token` repeats `client.Secret`'s design — an unexported string, `fmt.Formatter` redaction under every verb, `Reveal()` as the single conspicuous exit. `P1-05` found that `fmt.Stringer` alone leaks under `%d`; the same trap applies here and the same fix closes it.

## 10. Authorization Rules

A session belongs to one organization and is read through the tenant-scoped storage API. `Lookup` is the exception worth naming: it happens *before* a tenant is known, because the session is what establishes it. So the lookup resolves the token to a session in an instance-scoped read, and every subsequent query uses the `org_id` it returned.

That is a deliberate, narrow use of the cross-tenant path (`P0-08`), and it is already the audited mechanism `EventInstanceScopedAccess` exists for.

## 11. Validation

| Input | Rule |
|---|---|
| Cookie value | Exactly the expected length, base64url alphabet. Rejected before any lookup — a malformed cookie is not a database query |
| `auth_methods` | Non-empty; a session with no recorded factor is a session whose `amr` claim would be a lie |
| `expires_at` | `> created_at`, enforced by the existing CHECK |
| `session_lifetime_hours` | From org settings, clamped to a floor and ceiling as `P1-02` clamps password policy — an organization must not be able to configure a one-second or a ten-year session |
| IP | Parsed into `inet`; an unparseable value is stored NULL rather than rejecting the login |

## 12. Error Handling — and the revocation race

**The hard requirement is DoD item 5: revocation takes effect on the next request, not after a cache TTL.** A cache in front of an authoritative store normally means exactly the opposite, so the mechanism has to be explicit.

Design:

1. **Read**: `GET session:<sha256(token)>` from Redis. Hit → use it. Miss → read PostgreSQL by `token_hash`, then populate Redis.
2. **Revoke**: `UPDATE sessions SET revoked_at = now()`, commit, **then** `DEL` the Redis key.
3. **Backstop**: the Redis entry carries a short TTL regardless, so a failed `DEL` is bounded rather than permanent. A failed `DEL` increments a counter and logs at WARN.

Ordering matters and is not arbitrary. Deleting the cache *before* the commit lets a concurrent reader repopulate it from the pre-commit state, and the stale entry then outlives the revocation. Deleting after the commit is correct for every ordering except one.

**The remaining race, stated precisely.** A reader misses the cache and reads a live session from PostgreSQL. A revoker then commits and deletes the (absent) key. The reader then writes its now-stale snapshot into Redis. The session appears live until the TTL expires.

The window is short and the fix is cheap, so it is closed rather than documented away: revocation also writes a tombstone `session:revoked:<hash>` with a TTL covering the session's remaining lifetime, and the populate step is a Lua script that refuses to write when the tombstone exists. One extra Redis key per revocation, one atomic script on cache misses only, and no window at all.

Other failures:

| Condition | Behaviour |
|---|---|
| Redis unavailable on read | Fall back to PostgreSQL. Slower, still correct (NFR-3) |
| Redis unavailable on revoke | The PostgreSQL write still commits and is authoritative. Log at ERROR: the cache may serve a revoked session until its TTL, and that is exactly the window an operator needs to know about |
| Session not found | Treated as no session. The caller redirects to login; no distinction between "expired", "revoked" and "never existed" reaches the browser |
| Malformed cookie | Same as not found, without a lookup |
| Clock skew making `expires_at` appear passed | Treated as expired. Failing closed on a session is a re-login, which is recoverable |

## 13. Edge Cases

| Case | Handling |
|---|---|
| User logs in while holding a live session | A new session is created and the old cookie is replaced. The old session is revoked, so a stolen copy of it does not survive a fresh login |
| Two tabs, one logs out | Revocation is by session, so the other tab's session is unaffected — it is the same session, so both are logged out. Correct and worth stating: the cookie is per-browser, not per-tab |
| Idle timeout passes, absolute lifetime has not | Session is unusable. Both bounds apply; the shorter one wins |
| `last_seen_at` write throttling loses the final update before a crash | Acceptable. It moves the effective idle timeout later by at most the throttle interval, and it is a display value plus a fallback bound, not the primary one |
| IP changes mid-session | Recorded, not enforced (FR-8). A mobile network changing address is normal; hard-failing would log out real users to inconvenience an attacker who has already stolen the cookie |
| User agent changes mid-session | Same. It is a signal for Phase 3, not a gate |
| Organization lowers `session_lifetime_hours` | Existing sessions keep their `expires_at`. A policy change is not retroactive revocation; an administrator who wants that has "revoke all sessions" |
| Session's user is deleted | `ON DELETE CASCADE` removes the rows. The cache entries expire on their own TTL, and the tombstone mechanism is not involved because there is no revocation event |

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Session fixation: attacker sets a known cookie before the victim logs in | `SECURITY/02` §4 | The token presented at login is never adopted; a fresh one is always generated and the old session revoked |
| A-2 | Stolen cookie replayed | `SECURITY/02` §4 | Visible in the sessions list with its IP and user agent; revocable, and revocation is immediate |
| A-3 | Cookie sent over plain HTTP | `PLAN/09` § Transport | `Secure`, enforced by the `__Host-` prefix rather than only by our own attribute string |
| A-4 | Cookie read by injected JavaScript | `SECURITY/02` §6 | `HttpOnly` |
| A-5 | Cross-site request rides the session | `SECURITY/02` §5 | `SameSite=Lax` withholds it on cross-site POST |
| A-6 | Session token harvested from the sessions API or an audit log | `SECURITY/02` §16 | **PG-14**: the cookie is not the row id, and only a hash is stored |
| A-7 | Revoked session used during a cache window | Task DoD | Write-through invalidation plus the tombstone-guarded populate (§12) |
| A-8 | Session token brute-forced | — | 256 bits; the same argument as ADR-016 |
| A-9 | Cross-tenant session lookup | `SECURITY/02` §2 | The token→session resolution is instance-scoped and audited; everything after it is tenant-scoped |
| A-10 | Expired sessions accumulate until the table is a liability | DoD item 4 | The sweep, on the same pattern as `P0-12`'s partition maintenance |

## 15. Logging / Audit Requirements

| Event | Payload | Never |
|---|---|---|
| `session.created` | session id (internal), user id, org id, `auth_methods`, IP, truncated user agent | the token, its hash |
| `session.revoked` | session id, reason (`logout`, `admin`, `reauth`, `sweep`) | the token, its hash |

Both event types exist in `internal/audit` already. The `reason` is new and matters: "the user logged out" and "an administrator revoked this" are different facts, and a log that conflates them cannot answer why a user was signed out.

Metrics: `auth_sessions_created_total`, `auth_sessions_revoked_total{reason}`, `auth_session_lookup_duration_seconds{source=cache|database}`, `auth_session_cache_invalidation_failures_total`.

The last one is load-bearing in the same way `P1-02`'s skip counter is: it is the only signal that a revocation did not reach the cache, and §12's guarantee is only as good as somebody noticing when it fails.

## 16. Security Controls

- 256-bit token from `crypto/rand`, stored only as SHA-256, verified by hash lookup.
- Token separated from the displayable identifier (PG-14).
- `Token` type that redacts under every format verb.
- `__Host-` prefix, so the browser enforces `Secure`, `Path=/` and no `Domain`.
- `HttpOnly`, `SameSite=Lax`.
- Fresh token on every login; the previous session revoked.
- Absolute lifetime **and** idle timeout, both clamped against organization configuration.
- Immediate revocation, with the cache race closed rather than tolerated.
- Instance-scoped lookup narrowed to exactly one query and audited.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Token generation entropy, redaction under every verb, `Reveal` control |
| Unit | Expiry: absolute passed, idle passed, both passed, neither, and each boundary exactly |
| Unit | Policy clamping: below floor, above ceiling, absent, malformed |
| Unit | Cookie construction: every attribute asserted on the `Set-Cookie` string, including the `__Host-` prefix and the absence of `Domain` |
| Integration | Create → look up → revoke → look up again, with the second lookup failing on the **next** call and no TTL wait |
| Integration | Fixation: present a token, log in, assert the returned cookie differs and the presented one is dead |
| Integration | Only the hash is stored, by direct row inspection — the same assertion `P1-05` uses, for the same reason |
| Integration | Redis stopped mid-test: lookups still succeed from PostgreSQL |
| Integration | The repopulate race, driven deterministically: read from PostgreSQL, revoke, then attempt the populate, and assert the tombstone refuses it |
| Integration | Sweep removes expired rows and leaves live ones |
| Integration | Cross-tenant: a session's org scopes subsequent reads |
| Security | Every abuse case A-1…A-10 |

The race test is the one worth writing carefully. It has to interleave deliberately rather than hope — otherwise it passes on timing and proves nothing, which is the failure this project keeps finding.

## 18. Acceptance Criteria

1. A cookie presented before login is not honoured after it; the post-login cookie is a different value.
2. `Set-Cookie` carries exactly `__Host-` + `Path=/; Secure; HttpOnly; SameSite=Lax` and no `Domain`.
3. A revoked session fails the very next lookup, with Redis still holding a pre-revocation entry at the moment of revocation.
4. With Redis stopped, lookups still succeed.
5. The database holds no value equal to the cookie.
6. Expired sessions are gone after a sweep; live ones remain.

## 19. Definition of Done

The task's six items plus the global DoD. Item 3 (latency) is measured against `PLAN/12`'s budget with the cache warm and cold, and the number recorded — a budget nobody measured is a budget nobody meets.

## 20. Implementation Sequence

1. `Token`, expiry predicates, `Policy` clamping — pure, fully tested.
2. Cookie construction and parsing, with the attribute assertions.
3. The migration and `PG-14`'s backlog entry.
4. The PostgreSQL store: create, look up by hash, revoke, revoke-all, sweep.
5. The Redis cache, write-through invalidation, and the tombstone-guarded populate.
6. Audit events, metrics, and the invalidation-failure alert.
7. Latency measurement against the budget.

Steps 1–4 give a correct, slower system. Step 5 makes it fast without changing what it means — which is the right order, because a cache added before the semantics are settled is a cache that enshrines whatever they happened to be.

## 21. Rollback Strategy

The migration is additive; its down migration drops a nullable column and its index, which loses the ability to look sessions up and therefore logs everyone out. That is recoverable — a re-login — and it is worth stating plainly rather than discovering.

Rolling back the code returns to a service with no sessions. Nothing stored becomes unreadable.

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| The invalidation-failure metric is never watched, so §12's guarantee quietly stops holding | Medium | High | An alert, as with `P1-02`'s fail-open counter. The mechanism and the monitoring ship together or the mechanism is decorative |
| A future endpoint returns `token_hash` alongside `id` | Low | Critical | It is not on the `Session` struct at all; the store maps it and drops it |
| The `__Host-` prefix breaks a deployment behind a path-rewriting proxy | Low | Medium | `Path=/` is required by the prefix and is what we want anyway; a proxy that rewrites paths would break the OIDC redirect URIs first |
| Idle timeout via a sliding Redis TTL diverges from `last_seen_at` in PostgreSQL | Medium | Low | The Redis TTL is the enforcing bound and `last_seen_at` is the fallback and the display value; the throttle interval is the documented maximum divergence |
| Session lifetime clamping surprises an administrator | Low | Low | Reported as an `Adjustment` and logged, exactly as `P1-02` does |

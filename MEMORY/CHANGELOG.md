# Changelog

Chronological summary of changes at a coarser grain than the individual records in [`records/`](./records/). If you want to know what happened and roughly when, read this. If you want to know why it was done that way, follow the link to the record.

This is the **internal** changelog. The public-facing `/changelog` on the marketing site (`PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`) is a separate, user-facing artifact that never describes an unshipped capability.

Format follows Keep a Changelog conventions, grouped by release once releases exist. Before the first release, entries are grouped by date.

---

## Unreleased

### 2026-09-08

**Added**
- `TASKS/` — the execution layer: task conventions and global definition of done, seven phase files covering 124 tasks, a progress board, and a backlog. Every task cites the plan documents it implements and names its abuse cases. ([P0-21](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md), [ADR-001](./DECISIONS.md#adr-001--establish-tasks-and-memory-as-the-execution-layer))
- `MEMORY/` — the record layer: change records, decision log, this changelog, and templates. A task is not done until its record exists. ([P0-21](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md))

**Found**
- Two contradictions between existing plan documents, recorded as `PG-08` and `PG-09` in `TASKS/BACKLOG.md`. Neither plan document has been edited — both are flagged for the deliberate plan-change process (`AGENTS.md` rule 9).
- Eleven plan gaps: capabilities the plan requires functionally but does not model, most of them missing tables in `PLAN/04-DATA-MODEL.md` (signing keys, MFA factors, invite and reset tokens, federated identity links, webhook endpoints, role permission keys). Each is recorded in `TASKS/BACKLOG.md` against the task it blocks.
- Eight open questions requiring a decision from the project owner — deployment target, email provider, RPO/RTO values, capacity assumptions, and others. Recorded in `TASKS/BACKLOG.md`.

**Changed** — plan amendments, made deliberately at the user's instruction under `AGENTS.md` rule 9
- `PLAN/04-DATA-MODEL.md` (158 → 295 lines): added `signing_keys`, `user_mfa_factors`, `user_recovery_codes`, `user_tokens`, `user_identities`, `webhook_endpoints`, `webhook_deliveries`; extended `roles` (permission keys), `sessions` (org scoping, revocation, and the Redis-versus-PostgreSQL authority note), `refresh_tokens` (rotation families); added retention/partitioning policy and a "what is deliberately not stored" table. ([record](./records/2026-09-08-plan-gap-remediation.md), [ADR-002](./DECISIONS.md), [ADR-003](./DECISIONS.md), [ADR-004](./DECISIONS.md))
- `PLAN/05-API-CONTRACT.md`: SAML 2.0 corrected from Phase 2 to Phase 4, matching `PLAN/03`, `PLAN/16`, and `PLAN/17`.
- `PLAN/18-RISK-REGISTER.md`: R-04's mitigation corrected to state that Project Grant subset validation happens **on every request**, not only at grant creation — matching `CLAUDE.md`, `AGENTS.md` rule 3, `PLAN/08` Part C, and `PLAN/19`. The weaker wording described a system where a narrowed or revoked grant would keep working.
- `PLAN/07-BACKEND-ARCHITECTURE.md`: Redis clarified as a cache in front of PostgreSQL for sessions, not a second source of truth.
- `UI-UX/08-PAGE-SPECIFICATIONS.md`: four screens added that the IA included but the "full inventory" omitted — Organization Overview, Organization Settings, Instance-wide policies, Instance audit log.

**Added** — frontend track
- `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md`: 53 tasks across seven tracks covering every component in `UI-UX/07`, all 21 console screens, the hosted authentication screens, the public site, and the frontend quality suite. Foundation tasks are phase-independent; every page task carries a binding gate naming the backend task that unblocks it, which enforces `PLAN/16`'s lockstep rule per screen rather than per phase. ([record](./records/2026-09-08-phase-f-frontend-track.md), [ADR-005](./DECISIONS.md))
- Project total: 124 → 177 tasks.

**Added** — implementation begins ([record](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md))
- Go service skeleton with fail-fast configuration, a documented middleware chain, and graceful shutdown that provably completes in-flight requests. Health endpoints separate liveness from readiness and disclose no infrastructure detail. (`P0-04`, `P0-10`)
- Structured JSON logging with redaction enforced by the logger across 23 sensitive key names, plus per-request correlation IDs. (`P0-09`)
- Local Docker stack — PostgreSQL, Redis, Mailpit — with a distroless non-root runtime image whose healthcheck is the binary itself. The Postgres init script creates the application role as `NOSUPERUSER NOBYPASSRLS` and non-owner, which is the precondition for row-level security. (`P0-05`)
- Migration tooling as a separate binary with embedded SQL, and the full 14-table Phase 0/1 schema including month-partitioned, append-only `events`. (`P0-06`, `P0-07`)
- CI pipeline: commit-convention enforcement, build and race-enabled tests, integration tests against real Postgres and Redis, migration round-trip verification, destructive-migration justification, gosec, govulncheck, secret scanning, and a non-root image assertion. Third-party actions pinned by SHA. (`P0-13`)
- ADR-006 through ADR-010 record the backend stack, embedded migrations, the distroless runtime, text-plus-CHECK over native enums, and why `events` has no foreign keys. (`P0-01`)

**Added** — cross-tenant isolation ([record](./records/2026-09-08-P0-08-row-level-security.md))
- Row-level security on 11 tables, keyed to a transaction-scoped `app.current_org_id`. A query with no tenant context returns zero rows rather than every row — fail-closed as a consequence of how SQL evaluates NULL, not as a check someone has to remember. (`P0-08`)
- A storage API with no exported way to query outside a declared scope: every path goes through `WithTenant` or `WithInstanceScope`, both of which set the tenant inside a transaction before returning a queryable handle. `SET LOCAL` rather than `SET`, so a pooled connection cannot carry one request's tenant into the next. (`P0-08`)
- The service refuses to boot if its database role is a superuser, has `BYPASSRLS`, or owns a table — closing the hole flagged in the previous record, where pointing `AUTH_POSTGRES_DSN` at the owner would silently disable every policy with no test failure. (`P0-08`)
- A CI gate failing the build when a table with an `org_id` has no RLS enabled. (`P0-08`)
- `/readyz` now genuinely checks PostgreSQL; it previously reported ready with no dependencies wired.
- Deployed and verified on the VM; `https://auth.zedth.my.id` healthy throughout.

**Operational** — clone-based deploy, and state moved out of the checkout ([record](./records/2026-09-08-clone-based-deploy-and-state-separation.md))
- The VM now deploys by `git clone` / `git pull` rather than by uploaded files. Better, and the right answer to `OQ-11`: a pull needs no inbound access to a machine with no public IP.
- Re-cloning destroyed the signing key, the metrics token, every backup, and both frontend builds — because **runtime state lived inside the code directory**. That was never a decision; it was where things landed, and it stayed until an ordinary operation turned destructive. The service kept running on already-open mounts and would not have survived a restart, which is the worst shape for a fault: invisible until you need it.
- Fixed structurally. `/home/infra/auth-state/` now holds `.env`, secrets, backups and artifacts; the checkout is disposable. The compose files already read every path from a variable, so this was a `.env` change rather than a deployment change — indirection written for another reason, paying for itself.
- **An operation is only safe if the layout makes it safe.** "Do not delete that directory" is not a control: the person deleting it is doing something ordinary and has no reason to suspect otherwise.

**Fixed**
- **No script was executable in a fresh clone.** On Windows git does not track the executable bit unless `core.fileMode` is set, so fifteen scripts were committed `100644` while being executable locally — and `check.sh`'s gate for exactly this passed, because it tested the filesystem rather than the index. A clone gets the index. It surfaced when `secrets.sh` failed with "command not found" while regenerating a destroyed signing key. The gate now reads `git ls-files -s`, and found one more on its first run: `deploy/postgres/init/01-roles.sh`, which Docker executes at database init. (`P0-14`)
- `.env` was mode `664` — world-readable, holding the database passwords. A file recreated by hand takes the shell's umask and nothing was checking.
- Backups were written to the wrong place twice: an invented variable name (`AUTH_BACKUP_DIR`; the script reads `AUTH_BACKUP_DEST`) sent them back **inside the checkout**, reintroducing the original bug. Caught because the new directory was empty after a run that reported success. Read the script rather than guessing its variable names.

**Answered**
- `OQ-11`: no GitHub Actions — the VM has no public IP. Deployment is pull-based. `P0-20`'s "merge to `main` deploys automatically" should be re-read rather than left failing.
- `OQ-12`: local backups accepted for now, S3 or NFS later. The warning on every backup run stays, because it accurately describes a known gap.

---

## Phase 1 — MVP: Core Authentication and SSO

**Added** — the hosted login page, and a human can log in ([record](./records/2026-09-09-P1-12-login-page.md), [spec](./specs/P1-12-login-page.md))
- `GET`/`POST /login` and `GET /login/forgot`, server-rendered by the auth service itself with **no JavaScript at all** — which is what makes `default-src 'none'` an achievable policy rather than an aspirational one. (`P1-12`)
- **The loop closes.** `P1-06` redirected to `/login` and `/login` was a 404; the capability audit may now call single sign-on shipped, which it correctly refused to do when `P1-06` and `P1-07` landed alone.
- **A wrong password and an unknown address are byte-identical responses** — status, every header, and the body. Tested by submitting the *same* address twice with the account deleted in between; comparing two different addresses would have forced the assertion to be loosened until it tested nothing. A locked account, a deactivated one, and an account with no password give the same answer, at the same cost.
- Two design choices fell out of wanting that test to be strict: the inline stylesheet is whitelisted by its **SHA-256 hash rather than a nonce** (a nonce changes every response), and a failed submission **does not re-issue the CSRF cookie** (so no `Set-Cookie` differs). The header comparison dumps every header in sorted order rather than checking a list — `Content-Length` is the one a list forgets.
- CSRF by double-submit with the `__Host-` prefix and `SameSite=Lax`. The prefix is the control that matters: without it an attacker holding any subdomain can set the cookie and then knows its value, which is the standard break of the naive pattern.
- **No query parameter is rendered on the page at all**, so the reflected-XSS abuse case is answered by there being no code path rather than by careful escaping.
- `P1-06` gained an exported `Peek`/`Resume` seam, and its store a `PeekPending` alongside `LoadPending`, so only a *successful* login spends the pending request.
- `PG-16`: organization branding is in `PLAN/01`, bounded by `UI-UX/05`, given a screen by `UI-UX/08` and already implemented in the console — with no column, key or documented shape to read it from. Closed with no schema change by specifying `settings.branding`. Nothing writes it until `P2-14`, so every organization renders unbranded today.

**Found**
- **Consuming the pending request on every submission would have given every account exactly one attempt at its password** — mistype it once and the flow is destroyed, with no way back to the application, because the whole point of the opaque reference is that the destination is not in the URL. Split into a free read and a consuming success. (`P1-12`)
- **The capability audit declared single sign-on shipped, and it had not.** The protocol is complete; a visitor still cannot register an application (`P1-18`) or create a user (`P1-19`), so there is nothing to sign in to and nobody to sign in as. The same lesson the audit already carried in a comment, arriving a second time one layer out — the task list has to encode *usable by a visitor*, not *implemented*. Fixed by extending the list, not by relabelling the card. (`P1-12`)
- **The same audit checked whether `"Phase 1"` appeared anywhere on the page**, and two cards carry that label — so deleting it from one would have passed. It now finds the label inside the card it belongs to, demonstrated by removing it from one card and watching the check fail. (`P1-12`)
- Two coverage floors tripped the moment the new files landed — `internal/authn` and `internal/oauth/authorize`. Closed with tests rather than by lowering the floor. (`P1-12`)

**Fixed** — the nightly backup had not run since the state relocation
- **`P0-14` moved runtime state out of the code checkout; the backup unit's `ReadWritePaths` did not follow.** systemd refuses to start a unit whose `ReadWritePaths` names a directory that does not exist, so the service died with `226/NAMESPACE` before `backup.sh` executed one line — every night from 2026-09-08. The failure was maximally quiet: `systemctl list-timers` reported the timer healthy the entire time, because the *timer* was healthy. It fired exactly as configured and the service it triggered died instantly. (`P0-20`)
- The installed unit had been hand-edited to correct `AUTH_ENV_FILE` while the repo copy had not, so the two had drifted and the repo was the stale one. Both corrected, the unit reinstalled, and a verified backup taken: 90KB, 19 tables, row counts matching the source.
- **`BL-01` opened.** This is `P0-11`'s lesson arriving somewhere new — a rule that must catch "stopped happening" cannot be a comparison against a value that is never emitted, and a backup that fails to start emits nothing. What is needed is a freshness signal (`time() - last_success > 36h`), not a failure counter.

**Added** — the token endpoint, and the flow closes ([record](./records/2026-09-09-P1-07-token.md), [spec](./specs/P1-07-token.md))
- `POST /oauth/token`: three grants, client authentication by Basic or form post, PKCE verification, signed ID and access tokens, opaque refresh tokens stored as hashes. (`P1-07`)
- **A consumer can now complete a login end to end.** `P1-06` had been issuing codes nothing could redeem; `P1-02`, `P1-05` and `P1-11` were packages with no callers. Discovery advertises both endpoints a conforming client needs, so **`P1-04`'s two deferred DoD items are now true and ticked** — which only worked because the deferral named whose job it was.
- **The three token types are deliberately different things.** An ID token is an assertion *about* the user with `aud` = the client; an access token is a capability *at* a resource server with `typ: at+jwt` (RFC 9068) and a different `aud`. Two tokens that differ only in their claims are two tokens somebody eventually swaps.
- **The refresh token is opaque, not a JWT**, and the second reason is the one worth keeping: it must be revocable, so it must be looked up — but `P1-03` also found that sixteen encodings of one signature all verify, so a JWT's string form is a poor key for the reuse detection `P3-06` will build on this. An opaque value has exactly one representation.
- **The code is consumed before the client, redirect URI or verifier is checked**, so a wrong guess burns it. Leaving it alive would turn a single-use credential into an oracle to brute-force a verifier against.
- Every redemption failure returns the same `invalid_grant`; a confidential client presenting no secret is `invalid_client` and never treated as public.
- `PG-15`: `refresh_tokens` had no scope column, so a refresh had nothing to reproduce — it could carry no scope, or re-derive one from the client's registration, which is a different thing. Scope is what the *user* consented to.

**Found**
- **`AuthenticateClient` checked that a secret was presented and never verified it** — a function returning nil for any secret at all. Caught while cleaning up a leftover parameter in its signature; every test then written would have passed, because none of them presented a wrong secret. (`P1-07`)
- gosec flagged the token response for carrying `access_token`, correctly by its own logic. Suppressed with a note saying what makes the suppression true: the struct is written to one place, never logged, audited or stored — and if that changes, the rule was right. (`P1-07`)
- Both coverage floors tripped, on `CredentialsFor` and on the token package. Caught rather than noticed. (`P1-07`)

**Added** — the authorization endpoint ([record](./records/2026-09-09-P1-06-authorize.md), [spec](./specs/P1-06-authorize.md))
- `GET /oauth/authorize`: two-phase parameter validation, PKCE required for every client type, silent SSO from the session cookie, `prompt` handling, and single-use codes in Redis with a 30-second TTL. The first task that connects the previous three — `P1-02`, `P1-05` and `P1-11` were each a package with no caller. (`P1-06`)
- **The ordering of validation is the security property.** `client_id` and `redirect_uri` are settled first, and a failure there renders a page with no redirect at all; everything else is reported by redirecting to the now-validated URI. Reporting a phase-1 error by redirect *is* the open-redirect vulnerability, delivered by the code written to prevent it — so `renderError` and `redirectError` are separate methods rather than one function with a boolean, because the boolean is what gets passed wrongly.
- PKCE for confidential clients too, which `PLAN/05` demands and OAuth 2.1's baseline does not: a client secret protects the token request, not the code in transit.
- `state` is required and echoed, never checked — it is opaque to us, and mismatch is detected by the consumer. Requiring presence is real hardening; claiming to verify it would misattribute the protection.
- Discovery now advertises `authorization_endpoint` and `code_challenge_methods_supported`, because both became true. `token_endpoint` is still absent, which is also true.

**Decided**
- **One deliberate exception to ADR-013.** The generated router binds every parameter before the handler runs, which would reject a missing `state` before `redirect_uri` was validated — the wrong channel — and would silently collapse duplicated parameters, which is the parameter-pollution bypass the handler refuses. The operation is excluded from code generation and registered by hand; it stays in `openapi.yaml`, so it is still documented, still in the public API reference, and still checked by `openapi-shipped-paths.py`. A deviation from the mechanism, not the goal. (`P1-06`)

**Found**
- **A sequential test cannot see a lost race, demonstrated.** `PLAN/04` requires that "two concurrent redemptions of one code yield exactly one success". Replacing the atomic `GETDEL` with `GET`-then-`DEL` makes 32 concurrent racers *all* succeed — while the single-use test stays green. The concurrency test is the only thing standing between that and duplicate code redemption in production. (`P1-06`)
- **The capability audit declared single sign-on shipped, and it was wrong.** `P0-19`'s check mapped each landing-page capability to one roadmap task, so `P1-06` going DONE made it demand the card stop saying "Phase 1" — while a consumer still could not complete a login, having nowhere to exchange a code and no page to log in on. Marking it available would have been the exact false claim the check exists to prevent, produced by the check itself. A capability now lists every task it needs, and the audit reports which are outstanding. (`P1-06`)
- `internal/oauth/client` fell below its coverage floor when `ByClientID` arrived untested. Caught rather than noticed, on the one method that resolves a client before any tenant scope exists. (`P1-06`)

**Added** — session management and the SSO cookie ([record](./records/2026-09-09-P1-11-session-management.md), [spec](./specs/P1-11-session-management.md))
- `internal/session`: the browser session single sign-on runs on. A 256-bit cookie token, a PostgreSQL record, a Redis lookup cache, an hourly sweep, and revocation that takes effect on the next request. Unblocks `P1-06`, `P1-10` and `P1-12`. (`P1-11`)
- The cookie is `__Host-zedauth_session`. The prefix makes the **browser** enforce `Secure`, `Path=/` and the absence of `Domain`, rather than us asserting them — including against a future change of ours. `SameSite=Lax` rather than `Strict` is a requirement, not a compromise: `Strict` withholds the cookie on the top-level navigation silent SSO depends on.
- **Revocation is immediate, and that took work.** A cache in front of an authoritative store normally defers it to a TTL. Commit first then invalidate — the other order lets a concurrent reader repopulate the pre-commit state — and a tombstone plus a Lua-guarded populate closes the read-then-write-back race the ordering still leaves. The TTL remains only as a backstop for a failed delete, with a counter and an alert, because the guarantee is only as good as noticing when it fails.
- **`PG-14`: the session cookie must not carry the row's primary key.** `PLAN/04` describes it doing so, and `PLAN/05` routes `/v1/organizations/{org_id}/users/{user_id}/sessions` — an administrator listing another user's sessions would receive, per row, the exact string that authenticates as that user. `refresh_tokens.session_id` would carry one too, and so would any revocation audit event. Closed by an additive `sessions.token_hash`; the cookie carries a token, the database stores its hash, and `id` becomes a safe identifier.
- Idle timeout and absolute lifetime both apply, shorter wins, both clamped against organization configuration. `last_seen_at` is written at most once a minute — a write per authenticated request would put the database on the silent-SSO hot path.

**Found**
- **Instance scope reads no sessions**, and five failing tests were how that surfaced. `sessions_tenant_isolation` is `org_id = current_org_id()`, so with no tenant set the comparison is NULL and every row is filtered out: instance scope means "no tenant", not "every tenant". That is `P0-08` working correctly, and the fix was not to relax the policy — admitting NULL would open every session to any instance-scoped code path — but to give the bootstrap its own narrow `SECURITY DEFINER` function, on the pattern `P0-12` used for partition maintenance. (`P1-11`)
- **`session_id` was in the logger's redaction list**, correctly, back when the id *was* the cookie. After `PG-14` it is the safe identifier and `token_hash` is the credential, and continuing to redact it made the audit log unable to say *which* session had been revoked — defeating the point of the separation. (`P1-11`)
- **gosec flagged the cookie's `Secure` field, and was right for a better reason than it gave.** It was a `bool` parameter, so gosec could not prove it was set; the real problem is that a parameter is the only way the control could ever be off, and no case needs it — `__Host-` requires `Secure` and browsers treat `localhost` as a secure context. Deleted the parameter rather than suppressing the warning. (`P1-11`)
- The sweep reported "1" however many rows it deleted, because it scanned a single `RETURNING 1`. A maintenance job that cannot count cannot tell you whether it is keeping up — the same blind spot as `BL-01`. (`P1-11`)

**Added** — OIDC client registration and credentials ([record](./records/2026-09-09-P1-05-application-registration.md), [spec](./specs/P1-05-application-registration.md))
- `internal/oauth/client`: client types, grant/type consistency, redirect URI validation and matching, secret generation, verification and rotation with an overlap window, and a store that commits each lifecycle change with its audit event. No HTTP endpoints — `P1-18` exposes it, and the split is what let the rules be tested exhaustively without a router. (`P1-05`)
- **No schema change.** `P0-07` had already written both rotation columns and the CHECK that a public client holds no secret, so rotation was a feature of the data model rather than something bolted on.
- **Redirect matching is one line: compare the strings.** Every rule runs at registration, where a human is present and a rejection can be explained; the matcher does nothing clever, because that is where an attacker is present. Fifteen rejection cases each name the attack they prevent — the trailing slash, the dot segments, the `app.example.com.attacker.net` suffix confusion — and a control asserts the exact registered URI matches, since a matcher that rejects everything would pass a rejection table.
- Registration canonicalises scheme and host and touches nothing else. Path case, percent-encoding and query order are left alone, because normalising them is decoding and decoding is where `%2e%2e%2f` gets in.
- A `spa` asking for `client_credentials` is refused with an error explaining that the grant *is* the client authenticating as itself and a public client has nothing to authenticate with. `IsConfidential` is deliberately not `!IsPublic`: `saml` is neither, and a negation would hand it a secret.
- Redirect URI values are recorded in audit payloads, before and after. An entry saying "redirect_uris changed" cannot answer the question it exists for — if an attacker with admin access widens one, that log is the only place it shows.

**Decided**
- **ADR-016: client secrets are hashed with SHA-256, not Argon2id.** 256 bits from `crypto/rand` puts brute force at roughly 10^52 years behind an infinitely fast hash, so the hash's speed protects nothing the entropy did not. What a slow KDF would add is an amplification vector: secrets are verified on every `client_credentials` request, and at `P1-01`'s measured 90ms and 64 MiB, fifty requests a second is 4.5 cores and 288 MiB — paid by us while an attacker sending wrong secrets pays nothing. The decision is conditional on the secret being generated here, so `Generate` is the sole producer and a test fails if the entropy weakens. (`P1-05`)

**Found**
- **`fmt.Stringer` does not redact under every verb.** The `Secret` type returns `[REDACTED]` from `String()`, and `%d` printed the plaintext anyway: `fmt` consults `String` only for `%v`, `%s`, `%q`, `%x` and `%X`, and falls back to printing struct fields for anything else. Nobody formats a secret with `%d` deliberately — somebody formats a struct that contains one. Fixed with `fmt.Formatter`, which takes precedence for all verbs. Found because the test enumerated verbs rather than checking the one that was obviously handled. (`P1-05`)
- The loopback check was case-sensitive while hostnames are not, so `http://LOCALHOST:5173/cb` was refused as cleartext-on-a-public-host. Caught by a test asserting the canonical form is stable under re-validation. (`P1-05`)
- A nil `[]string` binds as SQL NULL against a `NOT NULL` column, and `database/sql` cannot scan `text[]` back at all. The array columns now travel out through `to_jsonb`, so Postgres does the escaping it already knows how to do instead of this package parsing array literals or taking a second SQL driver for its codec. (`P1-05`)

**Added** — password policy and breached-password rejection ([record](./records/2026-09-09-P1-02-password-policy.md), [spec](./specs/P1-02-password-policy.md))
- `authn.Evaluate`: a pure function over (password, policy), with `min_length` and `require_uppercase` read from `organizations.settings` rather than compiled in — so `P2-14`'s per-organization editor plugs into a mechanism that exists instead of replacing a hard-coded one. An integration test `UPDATE`s a live row and watches enforcement change with no restart. (`P1-02`)
- A breached-password check over a k-anonymity API: five hex characters leave the process and nothing else. The outbound-request test asserts the password, its hash suffix and the full hash are absent from the URL, the headers and the body — and a second test proves that assertion can actually fail, against a synthetic leaky request.
- **An 8-character floor no configuration can cross.** Without one, `"min_length": 1` is a valid policy and the control an administrator was given is the control they can silently remove. Every clamp is reported and logged, because a silent correction leaves the gap between the configured and the enforced policy discoverable only by experiment.
- Length counted in **runes over NFC**, not bytes: twelve characters otherwise means twelve in English and four in Japanese. Normalization touches only the count, never the string that reaches the hasher.
- `users.password_changed_at`, closing **`PG-13`** — `max_age_days` has been in the specified policy since `PLAN/08` with nothing in the schema to evaluate it against. NULL means *not* expired; the alternative turns deploying the migration into a mass lockout.
- Two audit event types, two metrics, and two alert rules (17 total, promtool-validated).

**Decided**
- **ADR-015: the breach check fails open, and says so every time.** Failing closed makes a third party a hard dependency of password changes, and the moment that matters most is the worst one for it — during an incident users are told to rotate passwords, this path spikes, and a rate-limited corpus would block the exact remediation the incident calls for. The risk accepted is bounded and identified per user in the audit log; the risk refused has no ceiling. Fail-open covers service failure only: a definitive match always rejects, and an unparseable response is a failure rather than an answer. (`P1-02`)

**Found**
- **A parser whose success condition was satisfied by a failure.** The corpus check counted response lines, and an HTML error page is one line — so a proxy answering `200` with "Access denied" produced zero matches, one line, and therefore `clean`. The password was admitted with `OutcomeClean` and nothing recorded that no check had happened: an always-open path that ADR-015's own metric would have shown as healthy. Caught by a test written before the parser was finished; it now counts well-formed 35-character hex entries, and a body with none of them has not answered the question. A new shape of the vacuous-check failure this project keeps finding — not a test that did not run, but a check that was wrong about what success means. (`P1-02`)
- `PG-13`, above.

**Added** — discovery document and JWKS endpoint ([record](./records/2026-09-09-P1-04-discovery-jwks.md))
- `GET /.well-known/openid-configuration` and `GET /.well-known/jwks.json`, both declared in the OpenAPI spec and served through the generated strict router rather than registered beside it — so ADR-013's guarantee that served paths equal documented paths covers the two endpoints whose entire purpose is discoverability. (`P1-04`)
- **The document is derived, never written out.** `internal/oidc.Capabilities` is a struct of facts about running code; an endpoint that does not exist leaves its field empty and is absent from the JSON. Advertising `/oauth/token` before `P1-07` builds it is not a discipline anyone has to remember — it is not expressible.
- `Validate()` runs at startup, so a bad issuer is a refusal to boot rather than a document that lies to every client that reads it. It rejects a trailing-slash issuer (clients compare `iss` byte for byte) and refuses `implicit` or `password` in the grant list, which `PLAN/05` rules out permanently.
- `plain` is absent from `code_challenge_methods_supported`, and PKCE is not advertised at all until there is an authorization endpoint to apply it to. Cache lifetimes: an hour for discovery, five minutes for the JWKS — matched to the service's own key-cache TTL so the stale window is bounded by the same number on both sides.
- **Two DoD items are deliberately unticked.** A client library cannot finish configuring without an authorization and a token endpoint, and nothing emits an `iss` claim yet; both become true with `P1-06`/`P1-07`. Ticking them now would be precisely the false claim this task exists to prevent. The verifiable half is tested: a client follows `jwks_uri` out of the document and reaches the current key.
- `AUTH_JWT_SIGNING_KEY_REF` is now **refused** at startup rather than ignored. Since `P1-03`, keys come from the `signing_keys` table; a deployment still setting it holds a false belief about where its key comes from, and that belief surfaces during an incident.

**Fixed**
- **Every endpoint answered 405 to HEAD.** chi matches methods exactly, so a HEAD request reached no route and got the middleware's default headers — which is why `curl -I` reported `no-store` on an endpoint whose GET response had been serving `max-age=300` correctly all along. A tool lied about the service and the service was fine. `HeadAsGet` now routes HEAD to the GET handler, cloning the request first: `net/http` decides whether to suppress the body from the *original* method, so mutating in place produces a HEAD response with a body. (`P1-04`)
- **The public site's API-reference staleness gate passed while the site build was broken by the same change.** `gen-api-docs` writes new operation and tag pages but leaves an existing `sidebar.ts` alone, so the new `Discovery` tag had no sidebar entry and its page could not render — while the gate, which runs that same generator and diffs the result, reported the reference up to date. `api:generate` now cleans before generating. A staleness check that only sees the files its generator chooses to overwrite is not a staleness check. (`P1-04`)
- `Server.Handler()` returned the bare router rather than the handler actually being served, so the first HEAD test passed against a server that was broken. The same shape as every vacuous check collected in these records: it ran, reported success, and covered nothing. (`P1-04`)

**Added** — signing keys, JWKS and rotation ([record](./records/2026-09-09-P1-03-signing-keys.md))
- `internal/signing`: a four-state key lifecycle (`next` → `current` → `previous` → `retired`), signing, verification, JWKS and a bounded cache. No schema change — `P0-07` had already written the table for this, including the partial unique index that makes two simultaneous signing keys unrepresentable. (`P1-03`)
- **The overlap window is the whole design.** A key is published before it signs, because consumers cache JWKS and would otherwise reject a valid token signed with a key they have not fetched. A key keeps verifying after it stops signing, because a token issued a second before a rotation is valid for its full lifetime. Two states would have been simpler and wrong in both directions.
- The `kid` is an RFC 7638 thumbprint rather than a generated value: stable across restarts so consumer caches survive a deploy, and impossible to collide with deliberately.
- Verification pins the algorithm from a fixed server-side list, which defeats `alg:none` and HS256-signed-with-the-RSA-public-key before a key is even looked up. Both tested with properly constructed forgeries.
- `cmd/keyctl` and a rotation runbook. Rotation is an operator command, not a timer — the failure modes want a person watching.

**Learned**
- **Token strings are malleable; token identity is not.** An RSA-2048 signature base64url-encodes with four bits that decode away, so **16 distinct token strings decode to the same signature and all verify**. Not a forgery risk — the signature must still be valid. It is a token *identity* risk: refresh-token reuse detection that hashes the presented string can be defeated by mutating one character, which would silently disable the whole mechanism. `P1-07` and `P3-02` must key on `jti` or the decoded signature. Pinned as a test so it is a constraint rather than a discovery. (`P1-03`)
- **A runbook is a hypothesis until executed.** `P1-03`'s DoD requires running it against staging, and doing so found two things reading could not: there is no Go toolchain on the VM, and `keyctl` recorded a key reference pointing at a path only *it* could see. The service mounts the same directory elsewhere, so the database held a reference that resolved for the tool and not for the service — harmless today, a refusal to start at the next restart after `P1-07`. Fixed by mounting at the service's own path, so the reference is right by construction rather than by remembering.

**Added** — Argon2id password hashing ([record](./records/2026-09-08-P1-01-password-hashing.md), [spec](./specs/P1-01-password-hashing.md))
- `internal/authn`: Argon2id with PHC encoding, so every row records the cost it was hashed at and raising parameters is a deploy rather than a migration. (`P1-01`)
- **Parameters measured, not copied**: 64 MiB / t=3 / p=4, giving 90ms per hash on the staging VM and 63ms on a development laptop. The binding constraint is concurrency rather than latency — memory cost multiplies by simultaneous logins, and the VM runs five other things. On four cores that is roughly 44 logins/second before latency climbs, which is a number `P1-13`'s rate limiting should sit below.
- **Enumeration defence**: the not-found path performs a real Argon2 computation rather than returning early, because response time would otherwise say which addresses have accounts. Measured ratio 0.88; verified by short-circuiting it and watching the test fail at 0.00. A real hash rather than a sleep — a sleep guesses a duration, gets it wrong when parameters change, and does not consume the CPU that makes timings match under load.
- Rehash-on-login, and a hash **stronger** than current is deliberately never flagged — otherwise a deploy that lowers parameters silently weakens every password that logs in afterwards, one user at a time, invisible in any diff.
- No bcrypt. `PLAN/07` names it a fallback "if compatibility is needed"; there is no legacy system, and adding it now means maintaining a path that accepts a weaker algorithm for a migration that may never happen.
- **gosec found a real gap, not a false positive.** `G115` flagged an unbounded `int -> uint32` conversion of salt and key lengths read from a stored hash — and the actual problem was that nothing bounded those lengths at all. Salt is now 8–64 bytes and key 16–64, which closes the overflow concern and an unbounded-allocation path together, with its own test across both boundaries. The first instinct on a lint finding is to silence it; reading it as "what would have to be true for this to be safe?" produced a bound the code was missing.
- 95.9% coverage. **`internal/authn` is the first package `P0-15`'s coverage floors apply to** — they had reported "not built yet" since they were written and activated on their own when the package appeared, which is the behaviour they were designed for, observed rather than assumed.

---

**Operational** — a verified restore ([record](./records/2026-09-08-P0-20-backup-verification.md))
- **A staging backup was restored and verified against the source**: 19 tables, every row count matching, into a throwaway database that was dropped afterwards. `PLAN/15` § Restore Testing — "a backup that's never been tested isn't a backup you can rely on". (`P0-20`)
- Backups are now automated: `zed-auth-backup.timer`, daily at 03:15 UTC, `Persistent=true` so a VM that was off overnight backs up at boot rather than skipping the day. A backup taken by hand is taken until the week somebody is busy.

**Fixed**
- **The backup verification was weaker than its own message.** It reported "all 14 tables restored" while the database had 19 — not missing data, but a hardcoded list of fourteen table names, so the line read like completeness and meant "all fourteen I was told to look for". A table added by a future migration would not have been checked, and the event partitions were not checked at all, in a script whose own comment calls a missing partition the likeliest way to lose the audit log. It now enumerates the source and compares tables and per-table row counts; the hardcoded list survives as a floor so that two empty databases cannot pass trivially. Verified by creating a table after the newest backup and watching the check fail by name. (`P0-20`)

**Decisions the owner needs to make**
- `OQ-11`: does GitHub Actions get a deploy path to the VM? Continuous deployment needs a credential to a machine on a private subnet, held by a system that runs code from pull requests. A trust decision, not a configuration task.
- `OQ-12`: where do backups go? They currently sit on the same disk as the database, which protects against `DROP TABLE` and nothing else.

**Added** — the test harness ([record](./records/2026-09-08-P0-15-test-harness.md))
- `internal/testsupport`: PostgreSQL and Redis started by the tests themselves through testcontainers, the embedded migrations applied, both database roles created. The integration suite now runs on a machine with nothing but Docker — no `make up`, no migrations by hand, no environment variables. (`P0-15`)
- `backend/tests/security/`: the abuse-case tests from `PLAN/11` § Security Testing, kept apart from feature tests because they are a checklist as much as a suite. The package comment carries a coverage map naming the four scenarios covered and the six that cannot be tested until the feature exists — an unwritten test nobody knows is unwritten is worse than a failing one. (`P0-15`)
- A Playwright E2E layer against the console's **production build**, not the dev server. It is the only layer that applies a real stylesheet, which is where the console's dead design tokens would have been caught — every jsdom test passed while `text-body` generated no CSS at all. (`P0-15`)
- `scripts/check-coverage.sh`: floors on `internal/authn`, `internal/authz` and `internal/oidc` rather than a repo-wide average, which is satisfied by testing whatever is easiest — and the easiest code to test is rarely the code where a bug matters. All three are pending; each floor starts applying the moment its package appears. (`P0-15`)
- A test-data factory, and `-shuffle=on` everywhere so an accidental ordering dependency fails rather than lurking as an unreproducible flake.

**Fixed**
- **The integration suite reported success without running.** It resolved a database from an environment variable with a `localhost` fallback and called `t.Skipf` when nothing answered — so a fresh clone or a misconfigured runner skipped every test touching row-level security, the audit log and the schema, and went green. Five skips across three files. That is worse than having no tests: no tests is a known gap, a green suite that skipped them is a false statement everyone acts on. (`P0-15`)
- **The new harness then broke a production guarantee, and a test caught it.** It granted `UPDATE, DELETE ON ALL TABLES` after migrating, re-granting privileges on the `events` partitions that migration 000006 had revoked — so the harness had a weaker privilege model than production and the audit log was no longer append-only inside it. Production sets `ALTER DEFAULT PRIVILEGES` *before* migrating; the harness now does the same. `TestNewPartitionsAreAppendOnly` found it, which it could only do because the skip hiding it was removed in the same change. (`P0-15`)
- `check-coverage.sh` tested for a directory, and `P0-02` had scaffolded empty ones — so it reported a coverage failure for packages containing no code. A floor that fails before the code exists is a floor everyone learns to ignore.

**Learned**
- **The same IPv4/IPv6 bug three times in one day**: Playwright waiting two minutes on a server that started instantly, the console's nginx healthcheck probing a server that was serving perfectly, and a local preview unreachable on one address and fine on the other. `localhost` means different things to the thing binding and the thing connecting, and the symptom never looks like name resolution. Name the address family on both sides.
- **A control test is the cheapest answer to the vacuous pass**, which this project has now hit five times. `TestIsolationTestsAreNotVacuous` asserts the owner sees *both* tenants' rows, because "tenant A saw one user" is only evidence of isolation if the row it did not see was there. Pointing the security suite at the owner DSN makes all four of its assertions fail — proof they measure the property rather than agreeing with themselves.

**Added** — site content and the capability audit ([record](./records/2026-09-08-P0-19-site-content.md))
- `public-site/CLAIMS.md`: every claim on every page, what it maps to, and whether it is shipped, planned-and-labelled, positioning, or a principle. The document `P0-19`'s Definition of Done asks for. (`P0-19`)
- `check-claims.mjs` — every capability on the landing page carries a phase label, and no label contradicts the roadmap board. The second half matters more than it looks: a card labelled "Phase 4" whose task is now DONE is exactly as inaccurate as an unlabelled one, and it is the version nobody notices, because the label is there. (`P0-19`)
- `check-no-internal-leak.mjs` — scans the built pages for **verbatim eight-word phrases** from the three documents `PLAN/20` forbids publishing. Phrases rather than keywords, because a keyword check on a site that legitimately discusses authorization and tokens cries wolf, and this project watched a PEM scanner get flagged for `node_modules` days ago. An eight-word run in both places is a paste, not a coincidence. (`P0-19`)
- Both audits verified by breaking them: a sentence from `SECURITY/02` pasted into `/about`, a private IP on `/contact`, and a capability card stripped of its label. Each caught and located.

**Fixed**
- The landing page had three different labels for its one primary action. `UI-UX/20` § Interaction asks for exactly one primary CTA used consistently, and its landing spec says the closing section restates the primary action rather than introducing a new one. Three reasonable-looking labels is the five-competing-CTAs failure in slower motion. (`P0-19`)
- Added the primary CTA to the navigation, which `UI-UX/20` § Above-the-fold lists alongside the logo and the Docs and About links. (`P0-19`)

**Added** — the public site ([record](./records/2026-09-08-P0-18-public-site-skeleton.md))
- The full route structure from `PLAN/20`: landing, about, contact, docs (quickstart, concepts, guides, console, API reference) and a changelog. 34 pages. `/pricing` and `/security` are deliberately absent — the first because no commercial tier exists, the second because `PLAN/20` places the trust page alongside Phase 5. (`P0-18`)
- **The API reference is generated** from `openapi/openapi.yaml`, the same file that generates the backend's server interface and the console's client — three consumers, one source, none able to disagree. This closes `P0-16`, whose last open step was exactly this. (`P0-18`)
- Docs versioning live from the first commit, with a `1.0` snapshot labelled *placeholder*: it proves the pipeline before there is a release to version. Retrofitting versioning once v1 docs exist means reorganising every file at the moment there is most content to break. (`P0-18`)
- Local search over 123 documents. `UI-UX/20` wants fuzzy matching because "a developer often doesn't know the exact terminology this project uses yet".
- Three scripts turn `PLAN/20`'s separation rules into checks: the nine shared colour values still match the console's, nothing here imports from `console/`, and every token meets AA in both themes. All three erode by convenience rather than by decision, so none is left to memory.
- `deploy/public-site/` and a CI job with its own install and cache — `PLAN/20`: "a docs typo fix shouldn't require a backend deploy pipeline".

**Decided**
- [ADR-014](./DECISIONS.md): one Docusaurus project rather than a marketing SSG plus a separate docs framework. `PLAN/20` implies two; `UI-UX/20` § Cross-Page Requirements requires a shared header and footer so Landing → Docs "never feels like a different product", and two projects make that a duplicated component. The deciding argument was that `PLAN/20` requires docs versioning and the obvious marketing-side alternative has none.

**Fixed**
- **A dark mode that would have shipped unreadable.** Carrying the console's palette to a dark surface measures accent 2.4:1, danger 2.7:1, warning 3.3:1, success 2.8:1 — all below AA — and the border 1.8:1, below the 3:1 for a component boundary. It would have looked deliberate. Each token was re-tuned: same meaning, different value for a different surface. None of it was visible by looking; it came from computing the ratios. (`P0-18`)
- **A soft 404.** The nginx config served the 404 page with a `200` status, telling every crawler that missing URLs exist — on the one surface whose job is discovery. `=404` with `error_page` returns the real status and still renders the styled page. (`P0-18`)
- **A security check that cried wolf.** `scripts/check.sh` reported "a PEM private key block is committed" for three files inside `public-site/node_modules` — gitignored, committed by no definition. It walked the working tree while its message said "committed"; it now scans `git ls-files` like the credential check beside it always did. A check that fires the first time someone installs dependencies is a check that gets commented out.

**Added** — the console shell ([record](./records/2026-09-08-P0-17-console-skeleton.md))
- React 19 + TypeScript on Vite 8 and Tailwind 4: every design token `UI-UX/05` names, routing, the navigation tree from `PLAN/06`, an error boundary, TanStack Query, and the typed API client generated from `openapi/openapi.yaml` — which closes `P0-16` step 3. (`P0-17`)
- Three local ESLint rules make the token discipline a build failure rather than a convention: a raw hex, a Tailwind arbitrary value, or an inline style all fail lint. The first run caught a real bug — `w-[--spacing-nav]` referenced a token that did not exist, so the navigation would have had no width. (`P0-17`)
- `color-danger` is un-overridable structurally: the brandable set is a union of literal token names, backed by a runtime filter because branding arrives as JSON where the type system has ended. A custom accent is contrast-checked against its surface before it is applied, which `UI-UX/13` requires at the moment an org admin sets it. (`P0-17`)
- Accessibility in the shell rather than on a list: landmarks, a skip link whose target is genuinely focusable, 44px targets at compact density, a focus ring using `color-accent`, and a `prefers-reduced-motion` fallback. Verified with real `Tab` presses in a browser, plus an axe pass on the WCAG 2.1 A/AA rule set. (`P0-17`)
- `deploy/console/` — nginx config and compose file for the staging preview at `console.zedth.my.id`, bound to `127.0.0.1:10920` so only `cloudflared` reaches it.
- Console lint, typecheck, tests and generated-client freshness are gates in `scripts/check.sh` and CI. 25 gates became 29.

**Fixed**
- **Every piece of text rendered at the browser default size, and nothing failed.** Tailwind v4 reads font sizes from `--text-*`; `--font-size-*` is not a namespace it knows, so the five type-scale tokens sat in the stylesheet as inert custom properties and generated no CSS. The same for `--line-height-*`, `--easing-*`, and `--duration-*` (not a namespace at all). Lint, typecheck, 72 tests and the build were all green — **because the tests read the source file**, which contained exactly what it should. A test that reads the input to a compiler cannot tell you what the compiler did. `utilities.test.ts` now compiles Tailwind and asserts the classes the components use produce rules; verified by reintroducing the bug, which the new test catches and the old one passes 48/48 through. (`P0-17`)
- Two `<h1>` elements: the narrow-width message overlaid the layout instead of replacing it, so below tablet width a screen-reader user would have walked past "this screen is too narrow" into the application it says is unusable. A visual overlay hides nothing from assistive technology. (`P0-17`)
- Refreshing on `/projects` returned 404 — the history-mode failure, where a static host looks up the route as a file. `deploy/console/nginx.conf` adds the fallback, never caches `index.html` (it is the file that names the current bundle), and sets the console's own security headers. (`P0-17`)
- Those headers then silently vanished: in nginx `add_header` does not accumulate across contexts, so a `location` block with any header of its own discards every server-level one. Found with `curl -I` against the deployed site; nothing in the config looked wrong. (`P0-17`)

**Added** — the API contract ([record](./records/2026-09-08-P0-16-api-contract.md))
- `openapi/openapi.yaml` is the single contract artifact, and it **generates the code rather than describing it** ([ADR-013](./DECISIONS.md)). Handlers implement a generated interface, so a signature that stops matching the contract fails to compile. `PLAN/05` accepts a spec that CI merely validates; that is now the backstop, not the mechanism. (`P0-16`)
- The shared component schemas — error envelope, pagination token, common parameters — are deliberately ahead of the endpoints. They are contract infrastructure, and an error format that changes after twenty endpoints exist is a breaking change to all twenty. (`P0-16`)
- `scripts/openapi-shipped-paths.py`: the spec documents only endpoints that exist. `/docs/api-reference` renders from it, so a documented endpoint is a public claim it exists (`UI-UX/21` governance, `CLAUDE.md`). Adding one means adding it to `SHIPPED` in the same commit. (`P0-16`)
- Three new gates in CI and `scripts/check.sh` — spec validity, generated-code freshness, and the shipped-endpoint rule. 22 gates became 25. Each was verified by deliberately breaking it.
- **ADR-012**, the audit write-semantics decision, written at last. Four code comments and a change record referenced it and it had never been written into `DECISIONS.md`, which `P0-12`'s Definition of Done required. A reference to a decision that does not exist reads as though the reasoning was recorded somewhere.

**Fixed**
- Writing the spec against the running service found that `/healthz` returns `{"status":"ok"}` while `/readyz` returns `{"status":"ready"}`. Recorded as two schemas rather than tidied into one: a consumer already parsing `ready` would break if the server were changed to match a prettier document. A test now pins both values and says what changing them would cost. (`P0-16`)
- `scripts/check.sh` defined a shell function named `head`, which shadowed the `head` command for the whole script. Every `... | head -20` in a pipeline called the function, which ignores stdin and **discards the piped output**. Two failure paths — unit-test failures and `govulncheck` findings — had been throwing their diagnostic detail away since they were written. Invisible until something failed, which is where a silent bug survives longest. Renamed to `section`.

**Added** — metrics endpoint authentication ([record](./records/2026-09-08-P0-11-metrics-endpoint-authentication.md))
- A bearer token on the metrics endpoint, resolved through the same secret indirection as everything else, compared in constant time, and rejecting with a bare `404` rather than a `401` — a `401` with a challenge header confirms to a prober that the endpoint exists and says what it wants. (`P0-11`)
- The service now refuses to boot when the metrics listener binds beyond loopback with no token configured. It caught two misconfigurations within the hour, both of them ours. (`P0-11`)
- `deploy/vm/secrets.sh` — creates and repairs the secrets directory. It exists because `chmod 400` is necessary and not sufficient, and the gap between those two costs a restart loop to find. (`P0-11`)
- The metrics port is **not published to the host**. `metrics.zedth.my.id` was live and token-gated for about an hour; the hostname and the published port are both gone. The endpoint stays reachable inside the compose network, which is where the only scraper that will ever exist here would run. A port published for a scraper that does not exist is a port open for no one. `docker-compose.metrics-port.yml` is the opt-in override, loopback-only, with a header explaining what publishing it means.

**Fixed**
- Secrets are resolved once, immediately after configuration loads, before any pool is opened or goroutine started. Previously a bad secret reference produced `sql: database is closed` on repeat — the deferred pool close racing the already-running audit goroutine — which is a consequence three steps removed from the cause, pointing at the wrong subsystem. Startup failures should be ordered so the first thing to fail is the thing that is wrong. (`P0-11`)
- The secrets directory and its contents now belong to the service's uid (`65532`), not the operator's. The runtime image is distroless `:nonroot`, so a directory owned by the operator at mode `700` cannot be traversed by the service at all and the mode on the file inside is never reached. The signing key had the same wrong ownership and would have failed identically in `P1`. (`P0-11`)
- `scripts/check.sh` supplies a synthetic `AUTH_ADMIN_TOKEN_REF` when validating the tunnel compose, since `${VAR:?}` fails static validation as well as deployment.

**Added** — audit log and toolchain patches ([record](./records/2026-09-08-P0-12-audit-writer.md))
- The audit writer. Events commit inside the transaction of the action that caused them, so a permission change and its record succeed or fail together. Redaction happens in the writer rather than at call sites, because the table is append-only and a credential written there cannot be deleted by anyone. (`P0-12`)
- Partition maintenance at startup and daily, keeping three months of runway. Without it every `INSERT` into `events` — and therefore every security-sensitive action — would have failed at a month boundary, with no deploy to correlate against. This was flagged as a time bomb two records ago. (`P0-12`)
- Partition creation via `SECURITY DEFINER` with a pinned `search_path`, rather than granting the runtime role `CREATE` on the schema. (`P0-12`)
- Instance-level events: `events.org_id` is now nullable, so the cross-tenant database path can be audited as `PLAN/08` Part B requires. (`P0-12`)
- `scripts/check.sh` — 22 gates, the same ones CI runs, before a push rather than after.

**Fixed**
- Six Go standard-library vulnerabilities at 1.26.5, one of them directly relevant: `ReadHeaderTimeout` was not applied during the unencrypted HTTP/2 check, and that timeout exists to bound slow-header attacks. Toolchain pinned to 1.26.6; `golang.org/x/text` upgraded past an infinite-loop bug. (`P0-12`)

**Added** — observability ([record](./records/2026-09-08-P0-11-metrics-and-tracing.md))
- Sixteen Prometheus instruments on a listener separate from the public one, so "not reachable from the public ingress" is a property of the socket rather than an ingress rule someone has to remember. The endpoint is unauthenticated and discloses request rates, error rates and login outcomes, so an all-interfaces bind is refused in production and compose publishes it to loopback. (`P0-11`)
- Histogram buckets sit exactly on `PLAN/12`'s latency targets, so a quantile query can answer "did we meet it" without interpolating across a wide bucket. Tested. (`P0-11`)
- Fifteen alert rules, promtool-validated, each carrying the reason it exists. Rules that must catch "stopped happening" use `absent()` rather than `== 0`, because a counter with no observations produces no time series and a zero comparison never fires when the thing is down. (`P0-11`)
- OpenTelemetry tracing with W3C propagation, off unless an OTLP endpoint is configured. Handler and query spans arrive with the endpoints in Phase 1. (`P0-11`)
- Both `P0-12` follow-ups closed: the unsupervised partition-maintenance goroutine is now visible as a gauge with two alerts, and cross-tenant database access is counted as `PLAN/08` Part B asks.

**Status**
- Phase 0: 21 of 21 tasks done. `P0-20` (staging) is the remaining exit-checklist item, largely satisfied by the VM deployment. `P0-16` is complete — its last open step, the public site's generated API reference, landed with `P0-18` — step 3 (the console's typed client) landed with `P0-17`; step 4, the public site's API reference, waits on `P0-18`.
- Open deviations: DV-01 (single-VM production vs. Multi-AZ), DV-02 (`manager_roles` has no tenant policy until `P2-05` provides a user context).
- `P0-20` is partially done: the restore is verified and automated, and TLS-only staging is live. Continuous deployment (`OQ-11`) and offsite backups (`OQ-12`) are the owner's decisions. Two further DoD items — staging keys distinct from production, and a production promotion gate — are vacuously true because no production environment exists, and are deliberately left unticked so that they are checked when one does.
- `OQ-10` is open: `P0-18`'s Definition of Done asks for Lighthouse scores against a bar `UI-UX/20` never sets. Set the number or drop the item — inventing a threshold to satisfy a checkbox is the same failure as inventing a capability to fill a section.
- New plan gap `PG-12`: `color-border` serves both input borders (WCAG 1.4.11 wants 3:1) and table dividers (which want a hairline). One token cannot do both well.
- The OIDC provider library remains undecided by design: confirming JWKS rotation with overlap and refresh-token reuse detection requires building against it, so it moves to `P1-03`.
- Open questions now number nine; `OQ-09` (audit log retention period and the erasure approach) is new and should be confirmed before `P0-07` writes the partitioning migration.

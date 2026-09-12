# Phase 1 — MVP: Core Auth + Basic SSO

| | |
|---|---|
| **Date** | 2026-09-08 → 2026-09-12 |
| **Tasks** | `P1-01` … `P1-29` (29 tasks; `P1-29` added mid-phase) |
| **Phase** | Phase 1 — MVP |
| **Surface** | all |
| **Author** | Claude Code |
| **Closing task** | `P1-28` — acceptance validation |
| **Status** | Complete — every `docs/PLAN/17` Phase 1 criterion verified against the deployed service |

---

## What shipped

An OIDC provider that two independent applications log real people into, a Management API that does everything the console does, a console that uses only that API, and a public site that describes exactly what exists.

| | |
|---|---|
| **Authentication** | Argon2id with per-hash parameters and rehash-on-login; a password policy with breached-password rejection that fails open and says so; login rate limiting keyed on the submitted address rather than a resolved account |
| **OIDC/OAuth 2.1** | `/oauth/authorize` (code + PKCE S256 only), `/oauth/token`, `/oauth/userinfo`, `/oauth/introspect`, `/oauth/revoke`, `/oidc/logout`, discovery and JWKS with key rotation |
| **Sessions** | Server-side sessions with immediate revocation, an SSO cookie that is not the row's primary key, and silent renewal that works in a third-party iframe |
| **Management API** | Organizations, projects, applications, users, audit log read — idempotency, pagination, per-client quotas |
| **Console** | Dogfoods the provider: it logs in through `/oauth/authorize` like any other client and calls only published endpoints |
| **Public site** | Landing, quickstart executed against the live service, API reference generated from the OpenAPI spec, changelog |
| **Demos** | Two applications, one confidential and one public, each verifying tokens itself |

## The eight criteria, and how each was actually checked

`docs/PLAN/17` § Phase 1, walked on 2026-09-11 against `https://auth.zedth.my.id` — **35 assertions, 0 failures**, re-run after the staging restore and green again.

| Criterion | Evidence |
|---|---|
| Two independent applications authenticate real users | Two distinct client ids, one `web` and one `spa`; a real sign-in through demo A, whose own `verify` package accepted the token |
| A user logged into A opens B and is not prompted | `/oauth/authorize` returned a code with no login page; `console/e2e/sso.spec.ts` asserts in a browser that the login page was never visited |
| `/oauth/token` and `/oauth/authorize` conform to `docs/PLAN/05` | 11 assertions: `no-store`, the four required response fields, `invalid_grant` on replay in OAuth's error shape, `password` and `implicit` refused by name, an unknown client is a 400 and never a redirect, `plain` PKCE refused, exact `redirect_uri` matching |
| Creatable via both API and console, consistently | `console/e2e/consistency.spec.ts`, 5 tests, **both** directions — plus one that watches the traffic and asserts the console calls nothing outside the published surface |
| Failed and successful logins in the audit log | Both present with actor and timestamp; and the runtime role cannot `UPDATE` the table |
| Rate limiting blocks a brute force | Blocked after 7 attempts; the lockout is audited, and names neither the account nor the address |
| Pyramid items have passing tests in CI | Eight CI jobs; `check.sh` 45 gates; six reverted security controls each turn the build red |
| The public site claims nothing unshipped | Four pages live; the "no hosted signup" qualifier and the status sentence still present; the claims audit runs in both directions on every build |

## Performance, measured rather than assumed

First load test (`scripts/loadtest/`), on the staging VM against the service's own port — through the tunnel would have measured Cloudflare.

**Every endpoint meets its `docs/PLAN/12` target in isolation. `/oauth/token` misses two in the mixed workload**: p50 72.7ms against 50, p95 209ms against 200, once authorize, userinfo and management traffic run beside it. That is exactly what the plan's "production will never see just one traffic type" clause exists to catch, and it is invisible to a per-endpoint run.

The shape said queueing rather than per-request cost, so the sweep went looking for the ceiling: `/oauth/token` gives 246 rps at 2 workers, 385 at 4, and then flat — 384 at 8, 378 at 16, 329 at 24. `/oauth/authorize` peaks at 1774 rps and holds every target to 32 workers. In both, the limit is **Postgres CPU** (~190% of four cores) rather than the Go service (~100%), on a VM shared with 27 other containers.

**Accepted, not fixed.** The miss is on a shared 4-core staging box with one replica and no read replica — the plan's own answer to this shape (`docs/PLAN/12` § Design Decisions: read replicas, horizontal scaling) is Phase 5 work. Recorded here so the next measurement has something to compare against.

## What deviated from the plan

**`P1-29` was added mid-phase.** There was no cross-origin policy anywhere in the plan, and two Phase 1 consumers needed one. It stopped being a forecast the first time a browser was pointed at the login flow: no public client could complete one, and the console could not call the API at all. Closed with a split policy — wildcard-no-credentials on public endpoints, per-application `allowed_origins` on personal-data endpoints, nothing anywhere else (ADR-020).

**The console gained a Client ID column** that `docs/UI-UX/08` had always specified and the table never had. Found by walking the acceptance criteria rather than by reading the code.

**Two PostgreSQL constraints could not be written as specified** — a CHECK cannot contain a subquery, and `CREATE OR REPLACE FUNCTION` cannot change a return type. Both are in the migrations with the reasoning.

## What Phase 1 found that no test had

The end-to-end environment earned its cost in its first hour. Four production bugs, each of which had passed every other layer:

- CSP `form-action 'self'` made browser sign-in impossible — Chrome enforces it against the **redirect chain**, and every prior test used curl.
- The console gated every screen on a `roles` claim the service does not issue, so it refused everyone.
- Preflight returned 405, because chi's method-not-allowed path skips route-level middleware.
- `127.0.0.1` and `localhost` are different sites, so the silent-renewal iframe got no cookie.

And `P1-28` itself found three more: a test suite that expired at noon because a row was built from two clocks, a security gate that passed without reading its input, and a backup that cannot restore the one secret its own recovery procedure depends on.

## What is deferred, and where it is written down

Nothing is "remembered". Every item is in `TASKS/BACKLOG.md`:

| | |
|---|---|
| `PG-19` | Per-client rate limiting has a requirement and no owner — and behind the tunnel every request shares one bucket |
| `PG-26` | A new deployment cannot be bootstrapped without database access |
| `PG-27` | No SBOM is produced |
| `PG-28` | The container image is never scanned |
| `PG-29` | The backup cannot restore the signing key |
| `docs/PLAN/12` | The mixed-workload token latency, above |

The threat-model review before Phase 2 is `MEMORY/records/2026-09-11-P1-28-threat-model-review.md`: nineteen categories, sixteen verified, three with no surface yet, two gaps inside verified rows (`PG-27`, `PG-28`).

## What to watch in Phase 2

**The `roles` claim.** The console now treats an absent claim as *unknown* and defers to the API. Phase 2 issues real roles, and the moment it does, `hasRole` starts making decisions it currently declines to make. The API must stay the only enforcement point — a hidden button is not a security control.

**Project Grants land in Phase 4, but the schema meets them in Phase 2.** `CLAUDE.md`'s standing rule — role assignment validated server-side as a subset of `granted_role_keys` on *every* request — has no code to attach to yet. The row that will carry it is being designed now.

**Postgres is the bottleneck, not the service.** Every Phase 2 feature adds queries to the request path. The load test exists and is re-runnable; run it again when `/v1/authz/check` arrives, because that endpoint is expected to carry more traffic than everything in Phase 1 combined.

**The audit log grows a writer per feature.** It is append-only to the runtime role and month-partitioned. Both properties are asserted by tests; neither is self-maintaining.

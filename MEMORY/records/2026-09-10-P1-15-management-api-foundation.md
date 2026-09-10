# P1-15 — Management API foundation

**Date**: 2026-09-10
**Branch**: `feat/P1-15-management-api`
**Spec**: [`MEMORY/specs/P1-15-management-api-foundation.md`](../specs/P1-15-management-api-foundation.md)

---

## What this is

The cross-cutting mechanics every `/v1` endpoint depends on, built once: the permission model, bearer authentication, tenant scoping, the error envelope, cursor pagination, `Idempotency-Key`, per-client rate limiting, and the audit guard.

No endpoints. `P1-16` onward add those, and adding one is now a single registration through `management.Chain.Handle` with its own `Requirement`.

`docs/PLAN/02` FR-14 is the reason this is a task of its own: every console capability must exist in the API, so there will be dozens of endpoints — and **a control implemented per endpoint is a control that will be missing from one of them**.

## The one property the whole design leans on

> **Every default is a refusal.**

`Requirement`'s zero value demands `INSTANCE_OWNER` over `ScopeUnset`, which no caller can satisfy. A route registered with a forgotten requirement is therefore unreachable rather than open — tested against an `INSTANCE_OWNER`, the most powerful role there is, who is still refused.

The failure mode of a default-open design is one endpoint nobody annotated, and it is invisible until it is exploited.

## Roles come from the database, never from the token

`docs/PLAN/08` Part A shows a token carrying `"urn:authservice:manager_roles": ["ORG_ADMIN"]`. That claim is real and `P2-04` will populate it — for a consumer application deciding whether to grey out a menu item, ten minutes of staleness is nothing.

It is **not** what this middleware reads. An access token lives ten minutes, so an administrator whose `ORG_ADMIN` was revoked one minute ago still holds a token asserting it. For "delete this organization" or "rotate this client's secret", that is the difference between revocation and a promise of revocation.

The integration test uses **the same token** for both calls and revokes the role in between. Reading the token would pass every other test in the file.

## 403 versus 404, and the gap the end-to-end test found

`errors.go` documented the rule from the start:

> `NotFound` — used for another organization's resource as well as for one that does not exist, and the two are deliberately the same answer.

`Require` did not implement it. Every authorization failure wrote `Forbidden`, so a caller could ask "is org `8f3e…` real?" one request at a time — abuse case A-3's disclosure (`docs/SECURITY/02` §2, §14). The documented control existed only in a comment, and the unit test asserted the wrong answer, which is why nothing caught it until the chain was exercised end to end against real `manager_roles`.

`Decision` now carries `Invisible`, and the rule is:

- The caller holds **nothing** over the target organization → `404`. It is the same answer an organization that does not exist gets.
- The caller holds **something** over it but not enough — an `ORG_ADMIN` asked for `ORG_OWNER` → `403`. They already know it exists; hiding it buys nothing and turns "you need a higher role" into a puzzle.
- The requirement is instance-scoped → `403`. Every caller is already on the instance; there is nothing to conceal.

## Idempotency

### PostgreSQL, not Redis

Redis holds the short-lived and reconstructible: authorization codes, the session cache, rate-limit counters. An idempotency record is none of those. The guarantee it makes is to a caller retrying **after a failure** — and the failure that prompts a retry is exactly the kind of event that also restarts things. A record that vanishes turns a safe retry into a duplicate provisioning call, which is the thing the header exists to prevent.

This closes **`PG-21`**: `docs/PLAN/05` Part B requires the header, `docs/PLAN/04` modelled no table for it, and its "What Is Deliberately Not Stored Here" section did not mention it either way — a gap rather than a decision.

### The claim is an INSERT, not a check-then-insert

Two concurrent requests with the same key race on the primary key and exactly one wins. This is the same reasoning `P1-06` gives for redeeming an authorization code with `GETDEL` rather than `GET`-then-`DEL`.

A check-then-insert passes every sequential test and runs the handler twice under concurrency, which is the precise failure the header exists to prevent. There is a test with eight racing goroutines, and a mutation that accepts any `RowsAffected` is caught by it and by nothing else.

The claim also **commits before the handler runs**. Held open inside the handler's transaction, a concurrent duplicate would block on the primary key until the first request finished — slow rather than refused, which defeats the point.

### Three answers, and being strict where leniency looks helpful

- Same key, same request, already answered → the stored response, byte for byte.
- Same key, **different** request → `409`. Returning the first result would make the caller's second, different intention disappear with no way for them to notice.
- Same key, first request **still in flight** → `409`. Waiting holds a connection for as long as the first takes; running defeats the header.

### The response column was jsonb, and that was wrong

`jsonb` is a parsed document: it reorders keys, collapses whitespace and drops duplicates. `{"id":"user-1"}` came back as `{"id": "user-1"}`.

A replay would have returned **different bytes** from the ones the first request received — equivalent JSON, and silently broken for any client comparing a hash, an ETag or a signature. It is now `text`, and `TestAReplayReturnsTheOriginalBytes` uses a body with awkward whitespace and out-of-order keys specifically so that a return to `jsonb` fails it.

The request body is hashed and never stored: a user-creation call carries a password, and this table would otherwise be a durable copy of it. Method and path are inside the hash too, so a key reused across endpoints is a conflict rather than a replay of an unrelated result.

### Failures release the claim

Only a `2xx` is stored. An error is a state the caller is expected to correct, and pinning it to their key for twenty-four hours would make the correction impossible: the retry with a **fixed** body would be refused as a conflict.

Every path out of the middleware either stores an answer or releases the claim, including a panic. `Release` carries `AND status IS NULL`, so it cannot erase a **completed** record — without that predicate, a caller able to make one request fail after a sibling succeeded could erase the sibling's stored answer.

## Rate limiting

Closes **`PG-19`** — a requirement in `docs/PLAN/05` Part B with no owner since `P1-09`.

This is a different mechanism from `P1-13`'s, not a reuse of it. `P1-13` counts **failures** and answers with a cooldown, because it exists to make guessing a credential expensive. This counts **requests** and answers with a remaining allowance, because it exists to bound how much work one client can ask the estate to do — including a client whose credentials are entirely valid and have been stolen.

600 a minute, per `client_id`. Keying on the client id is what makes abuse case A-5 fail: rotating a secret does not reset the bound, because the id is precisely the part that does not change when a secret does. There is a test that rotates the secret and asserts the 429 survives.

A **fixed** window, and the tradeoff is stated rather than discovered: a client can send `Limit` just before a boundary and `Limit` just after, so the worst case over a sliding minute is `2×Limit`. A sliding window would close that at the cost of an `X-RateLimit-Reset` that no longer names a real moment. For what this bound is for, a factor of two at a boundary does not change the answer and a client being able to reason about `Reset` does.

The increment is a Lua script, not `INCR` then `EXPIRE`. A process that dies between those two leaves a counter with **no expiry** — a client rate-limited forever by a total that never resets. `PEXPIRE` fires only when the count is 1, or a busy client would slide its own window forward and never reset while it is being hit. Both have tests, and both mutations are caught.

`X-RateLimit-*` on **every** response, not only on a refusal, and set before the handler writes. A client that learns its remaining allowance only once it has run out cannot pace itself, which is the difference between a limit and a trap.

Fail open, loudly (ADR-017), for the reason applied here: Redis already backs the session cache, so an outage is already degrading, and turning it into "nobody can administer anything" converts a cache failure into a total management outage. The unavailable verdict reports the **full** allowance rather than a fabricated remainder — a client pacing itself against a number this service made up is pacing against fiction.

## The audit guard

`Audit` writes the event inside the request's **own** transaction, so the change and its record commit together. An audit entry for a change that rolled back is a lie; a change with no entry is worse.

`AuditGuard` is the half that matters. `docs/PLAN/09` § Audit requires every security-sensitive action to leave a record, and the way that requirement fails is never a broken writer — it is one handler out of thirty that nobody remembered. That gap is invisible by nature: nothing errors, nothing is slow, and it is found during an incident, when the record is needed and absent. The guard turns it into a loud line and `auth_management_unaudited_mutations_total` on the very first request. **The counter should be permanently zero, so the alert is "greater than zero" rather than a threshold.**

Three things it deliberately does not do:

- **Not on reads.** Auditing reads here would make the log mostly reads, which is how the entries that matter become unfindable.
- **Not on refusals.** A `403` changed nothing, and demanding an event for every refusal hands a caller a way to write to an append-only table as fast as they can send requests — the reason `P1-13` audits a cooldown rather than each attempt.
- **Not on a replay.** The original request already wrote the event. This is why the guard sits **inside** the idempotency middleware, and it is the easiest thing in the chain to get backwards: outside, every replay would be reported, and an alarm that fires in normal operation is an alarm somebody turns off.

The trail is marked only **after** a successful write. Marking first would let a broken audit path report itself as healthy — precisely the reassurance the guard exists to withhold.

## The sweep that did nothing

`Sweep` ran under `WithInstanceScope`, where `current_org_id()` is `NULL` and `idempotency_tenant_isolation` matches no row. It reported success and deleted zero rows.

This is the `P0-20` shape exactly — a cleanup job that silently does nothing for days — and the only reason it was caught is that the integration test asserts the **count** rather than the absence of an error. A test that checked `err == nil` would have passed forever.

The fix is what `P0-12` (partition maintenance) and `P1-11` (session sweep) already do for the identical reason: a `SECURITY DEFINER` function with a pinned `search_path`, granted only to `auth_app`, narrowed to exactly its question. It takes no organization, returns no row contents, and touches nothing that has not already expired — so it does not become a way to erase somebody's replay protection.

## Pagination, and a DoD item I nearly ticked without evidence

Keyset, sorted by `(created_at, id)` — `created_at` alone is not unique, and two rows written in the same microsecond make a page boundary non-deterministic, which surfaces as a row that appears twice or never.

The cursor carries **only** a sort position. Every filter comes from the request and is re-applied per page, which is what makes forging one uninteresting: a crafted cursor chooses a starting position within data the caller can already see, because RLS applies to the page query regardless of what the cursor says. A cursor carrying its own filter would be a filter the *client* controls.

The DoD says "stable under concurrent inserts", and I had ticked it on the strength of a unit test that walks a slice. That test cannot show the property: stability under inserts is a property of the **query**, and a slice has no query. The integration test now inserts rows on every page boundary in the two positions that break an `OFFSET` — before the cursor, which shifts every later row back by one, and after it — and asserts that every row present when the walk began is returned exactly once.

It is paired with a **control that requires an `OFFSET` walk to duplicate rows** under the same disruption. Without that, "keyset pagination is stable" is a claim about a property nothing has been shown to lack, and a test passing against both designs is not a test about the design.

Rows inserted mid-walk may or may not appear. That is inherent to any paged read of a moving table, and promising otherwise would mean holding a snapshot across HTTP requests.

## The key is fingerprinted, not logged

The spec's NFR-2 says the `Idempotency-Key` never reaches a log line, and the first version of `logFault` wrote it verbatim — with a comment justifying it, which is the worse kind of violation because it reads as considered.

The spec is right. The key is a string the **caller** chose, and a provisioning script will cheerfully use an email address, an employee number or an internal record id. None of those belong in a log that is shipped, retained and read by people with no business seeing them.

A twelve-character SHA-256 prefix keeps the only property an operator needs — that two lines concern the same key — and gives up the one nobody needs. The test asserts the key is absent **and** the fingerprint is present, because "nothing was logged" would satisfy the first half on its own.

## Vacuous verification, three new shapes

`vacuous-verification-pattern` gained three entries this task, and two were in the **verification tooling** rather than in the code:

1. **A mutation harness with the wrong package path.** `go test <pkg> -run <name>` prints `ok` when the pattern matches nothing, so three quota mutations reported `SURVIVED` against tests that never ran. Every one of them was in fact caught. The harness now fails loudly when `-run` matches nothing.

2. **`httptest.ResponseRecorder.Header()` is live, not the snapshot.** A test asserting on it passes even when the header was written after `WriteHeader` and the real client would never receive it. `Result().Header` is the snapshot that actually goes on the wire — and with the fix, the mutation that moves `writeQuotaHeaders` after the handler is caught.

3. **A "confusable pair" that was not confusable.** `TestAdjacentComponentsCannotBeConfused` compared `POST`+`/v1/usersX` with `POSTX`+`/v1/users`, which differ concatenated too. It survived deleting the hash separators. The pairs are now genuinely identical without a delimiter.

Two test expectations were also simply wrong, and worth recording because both looked like code defects first:

- **Six concurrent duplicates all returned `201`.** That is correct: one executed, five replayed the stored answer. The test demanded exactly one `201`.
- **Two `401` messages differed.** Also correct: a request with no credential at all is told authentication is required, which leaks nothing. The uniformity that matters is among **presented** tokens, and the test now checks that separately — with a count assertion, so a future edit that drops cases turns a real check into a comparison of one string with itself.

## What is deliberately not built

- **Endpoints.** `P1-16` onward. `/v1` is mounted and empty, which answers `404` for every path — exactly what should happen while no endpoint exists, and it means the next task adds a route rather than a route *and* the chain protecting it.
- **`app.current_user_id`.** `BL` notes a policy that needs it; still absent, still deferred.
- **Project-scoped roles.** `PROJECT_OWNER` and `PROJECT_GRANT_OWNER` are named in `roles.go` so a row carrying one is recognised as "not yet" rather than silently treated as no role — but Phase 2 implements them.
- **Auditing reads.** A separate, deliberate capability if it is ever wanted.

## Deployed

Rolled out to staging as part of `9ad4660`, together with `P1-13`, `P1-14` and `P1-16` — the VM had been sitting at `c3a00cb`. The chain is verified end to end there through the organization endpoints; see [`P1-16`'s record](./2026-09-10-P1-16-organizations.md).

The `idempotency_records` migration applied cleanly, and `sweep_idempotency_records` is executable by `auth_app` and by nobody else.

## Verification

- Unit: the role hierarchy exhaustively, default refuse, the error mapping class by class, the cursor round trip and a forged cursor, `page_size` clamping, the quota arithmetic, key validation, and every middleware branch.
- Integration (real PostgreSQL, real Redis, a real signing key, tokens minted by `P1-07`'s own claim builder): every DoD item, the concurrency races for both the idempotency claim and the quota counter, cross-tenant isolation asserted from both ends, and the secret-rotation abuse case.
- **23 mutations, all caught** — after the harness was fixed so that "caught" means something.
- Two tests exist to make other tests mean something rather than to test the code: the `OFFSET` control above, and the count assertion on the token-uniformity check.

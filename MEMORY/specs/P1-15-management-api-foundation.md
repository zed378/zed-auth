# P1-15 — Management API Foundation

Feature specification, per `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`. "Spec required — authorization surface", and it is the authorization surface every later endpoint inherits.

---

## 1. Business Objective

The cross-cutting mechanics every `/v1` endpoint depends on, built once: bearer authentication, permission checks, tenant scoping, the error envelope, pagination, idempotency, per-client rate limiting and audit.

Built once is the objective, not a convenience. `docs/PLAN/02` FR-14 requires every console capability to exist in this API, so there will be dozens of endpoints; a control implemented per endpoint is a control that will be missing from one of them.

## 2. Actors

| Actor | Interaction |
|---|---|
| The console | Calls `/v1` with a user's access token |
| A service account | Calls `/v1` with a `client_credentials` token |
| An organization administrator | May act on their own organization and no other |
| An attacker with a valid token for org A | Wants to read or write org B |
| An attacker whose role was revoked a minute ago | Wants their still-valid token to keep working |

## 3. Functional Requirements

- **FR-1** Bearer authentication against the local key set, no network round trip.
- **FR-2** Permission checks from `manager_roles`: `INSTANCE_OWNER`, `ORG_OWNER`, `ORG_ADMIN` in Phase 1.
- **FR-3** Every check server-side, on every request.
- **FR-4** RLS context set for the caller's organization on every request.
- **FR-5** `docs/PLAN/05`'s exact error envelope, with a consistent class→status mapping.
- **FR-6** Opaque cursor pagination with `next_page_token`, bounded `page_size`.
- **FR-7** `Idempotency-Key` on `POST`, keyed by (client, key).
- **FR-8** Rate limiting per `client_id`, with `X-RateLimit-*` headers.
- **FR-9** Every mutating request audited.
- **FR-10** Every endpoint in the OpenAPI spec as it is built.

## 4. Non-Functional Requirements

- **NFR-1** Authentication adds one signature verification and one indexed read.
- **NFR-2** No token, and no `Idempotency-Key`, in any log line.

## 5. Dependencies

| Depends on | Why |
|---|---|
| `P0-16` | The generated server interface and the spec-drift check |
| `P1-07` | The tokens being presented |
| `P1-03` | The key set they are verified against |
| `P0-08` | The RLS context |
| `P1-13` | The limiter, extended to a per-client bound |
| `P0-12` | Audit |

## 6. Database Changes

**One additive migration**, for `PG-21` (§21). Idempotency records need somewhere to live and `docs/PLAN/04` has nowhere.

## 7. API Contract

No endpoints of its own. It defines what every `/v1` endpoint does before and after its handler:

```
Authenticate → authorize → scope → handle → audit
```

Error envelope, unchanged from `docs/PLAN/05`:

```json
{ "error": { "code": "…", "message": "…", "details": [ { "field": "…", "issue": "…" } ] } }
```

| Class | Status | Code |
|---|---|---|
| No credential, bad signature, expired | `401` | `UNAUTHENTICATED` |
| Authenticated, lacks the role | `403` | `PERMISSION_DENIED` |
| Not visible in the caller's scope | `404` | `NOT_FOUND` |
| Malformed input | `400` | `VALIDATION_ERROR` |
| Idempotency-Key reused with a different body | `409` | `CONFLICT` |
| Over the client's rate limit | `429` | `RATE_LIMITED` |
| Anything else | `500` | `INTERNAL` |

`429` here and **not** on the login page, deliberately: this caller is a machine that acts on a status and reads `Retry-After`, which is exactly what `P1-13` argued a browser is not.

## 8. Frontend Changes

None. `P1-21` is the console's first call.

## 9. Backend Changes

New package `internal/management`: the middleware chain, the error mapping, the cursor, the idempotency store, and the permission model. `internal/ratelimit` gains a per-client policy.

## 10. Authorization Rules — the decision this task turns on

**Manager roles are read from the database on every request, not taken from the token.**

`docs/PLAN/08` Part A illustrates a token carrying `"urn:authservice:manager_roles": ["ORG_ADMIN"]`, and that claim is real and will exist — `P2-04` populates it so a consumer application can hide a button without a round trip. It is **not** what this middleware reads.

A role in a token is a snapshot taken when the token was issued. An access token lives ten minutes (`P1-07`), so an administrator whose `ORG_ADMIN` was revoked one minute ago still holds a token asserting it. For a consumer application deciding whether to grey out a menu item, ten minutes of staleness is nothing. For **this** API — where the actions are "delete this organization", "assign this role", "rotate this client's secret" — it is the difference between revocation and a promise of revocation.

`docs/PLAN/08` Part C already sets the precedent in the strongest terms for the analogous case: a project grant's roles are "validated server-side as a subset of `granted_role_keys` on **every single request**, not just at grant-creation time". Manager roles govern who administers the service itself and deserve at least that.

The cost is one indexed read per request, on a request that is about to touch the database anyway.

### The hierarchy

```
INSTANCE_OWNER   → every organization
ORG_OWNER        → one organization, entirely
ORG_ADMIN        → one organization, except deleting it or changing its owner
```

Inheritance is explicit rather than implied: `ORG_OWNER` satisfies a requirement for `ORG_ADMIN`, and `INSTANCE_OWNER` satisfies both. Phase 2 adds the project-scoped roles below them.

**A permission requirement is declared per endpoint and defaults to refusing.** An endpoint that declares nothing is not open — it is unreachable, and a test asserts that, because the failure mode of a default-open design is an endpoint somebody forgot to annotate.

## 11. Tenant scoping

Every `/v1` request runs inside `WithTenant` for the organization the request addresses, after the caller's role for that organization is established. RLS then confines every query without any handler containing an `org_id` predicate.

`INSTANCE_OWNER` is the exception and is handled explicitly: it uses `WithInstanceScope` with a reason, which is logged and audited by `P0-08`'s existing mechanism. There is no path where a missing scope silently means "all tenants" — `WithTenant("")` already returns an error, which `P1-14` found the hard way.

## 12. Pagination

`page_size` bounded: default 20, maximum 100. A caller asking for more gets the maximum rather than an error, because failing a list request over a number is unhelpful when clamping is unambiguous.

**The cursor carries only the sort position** — `(created_at, id)` of the last row — and nothing else. Every filter comes from the request and is re-applied on each page.

That is what makes forging a cursor uninteresting: a caller who crafts one can choose a starting position **within data they can already see**, because RLS applies to the page query regardless of what the cursor says. A cursor carrying its own filter would be a filter the client controls, which is the shape of the bug where a crafted token pages past a permission check.

The token is base64 of a small JSON object. It is opaque **by contract**, not by encryption — the spec says so, so nobody builds on its contents.

## 13. Idempotency

`Idempotency-Key` on `POST`, keyed by **(client id, key)** so two clients cannot collide and one client cannot read another's result.

- First request with a key: the response is stored with its status and body.
- Replay with the same key **and the same request body**: the stored response is returned, and nothing runs.
- Replay with the same key and a **different** body: `409 CONFLICT`. Silently returning the first result would make a client's second, different intention disappear.
- A request still in flight: `409`, rather than running twice.

The body is compared by hash, not stored. Storing request bodies would put whatever a caller sent — including a password on a user-creation call — into a durable store, which is exactly what this codebase refuses to do everywhere else.

Records expire after 24 hours: long enough for any retry that is not a bug, short enough that the table is not a permanent log of everything anybody ever posted.

## 14. Abuse Cases

| # | Abuse case | Source | Control |
|---|---|---|---|
| A-1 | Privilege escalation by a caller whose token lacks the role | card, `docs/SECURITY/02` §3 | Roles read from the database per request |
| A-2 | A revoked role that still works because the token says so | §10 | Same — this is why |
| A-3 | IDOR: another organization's resource by id | card, `docs/SECURITY/02` §2 §14 | RLS, and a `404` rather than a `403` so the id's existence is not confirmed |
| A-4 | Mass assignment of `org_id`, `id`, or a role field | card, `docs/SECURITY/02` §11 | The generated request types carry no such field; server-set values are never read from the body |
| A-5 | Rate-limit bypass across rotated client credentials | card, `docs/SECURITY/02` §10 | The bound is per client id, which survives a secret rotation — the client id is what does not change |
| A-6 | Idempotency key reuse to replay a different action | — | The body hash must match, or `409` |
| A-7 | Reading another client's idempotent result | — | Keyed by (client, key) |
| A-8 | A forged page token paging past a filter | — | The cursor carries only a sort position; filters are re-applied |
| A-9 | An endpoint reachable because nobody annotated it | — | Default refuse, with a test that every registered route declares a requirement |

## 15. Logging / Audit

Every **mutating** request writes an event through `P0-12`, with the actor, the organization, the client and the resource. Reads are not audited, for the reason `P1-08` and `P1-09` give: volume proportional to traffic, and no signal.

Never logged: the token, the `Idempotency-Key`, or a request body.

## 16. Security Controls

- One signature verification against the local key set — no network round trip, so a JWKS outage cannot become an authentication outage.
- `aud` must be this service. A token minted for a consumer's resource server must not administer the platform.
- Roles from the database, per request.
- RLS per request, with instance scope a separate, audited, named path.
- `404` rather than `403` for a resource outside the caller's scope.
- Default-refuse permissions.
- Per-client rate limiting, which closes `PG-19`.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | The role hierarchy: who satisfies what, exhaustively |
| Unit | Default refuse: an endpoint with no declared requirement is unreachable |
| Unit | The error mapping, class by class |
| Unit | The cursor round-trips, and a forged one cannot change the filter |
| Unit | `page_size` clamping at both ends |
| Integration | No token, expired, wrong audience, wrong issuer — each with the right status |
| Integration | **A role revoked between two requests takes effect on the second** |
| Integration | An organization admin cannot reach another organization, and gets `404` |
| Integration | Pagination is stable under concurrent inserts |
| Integration | An idempotent replay returns the original and creates nothing |
| Integration | The same key with a different body is `409` |
| Integration | Rate limiting survives a secret rotation |
| Integration | Every mutating call writes an audit event |

## 18. Acceptance Criteria

The card's six, unchanged.

## 19. Implementation Sequence

1. The permission model and its hierarchy tests.
2. Authentication middleware.
3. Scoping and the error mapping.
4. Pagination.
5. Idempotency, with its migration.
6. Per-client rate limiting.
7. Audit.

## 20. Rollback Strategy

The migration is additive. Rolling back removes `/v1`, which nothing consumes yet.

## 21. Gap raised

**`PG-21` — idempotency records have nowhere to live.** `docs/PLAN/05` Part B requires `Idempotency-Key` support on `POST`; `docs/PLAN/04` has no table for it and `docs/PLAN/04` § What Is Deliberately Not Stored Here does not mention it either way.

Redis was considered and rejected. An idempotency record must outlive a Redis restart, because the guarantee it makes is to a caller retrying after a failure — and the failure that prompts a retry is exactly the kind of event that also restarts things. A record that vanishes turns a safe retry into a duplicate provisioning call, which is the thing the header exists to prevent.

So: an additive `idempotency_records` table, tenant-scoped like everything else, with an expiry and a `PG-10`-style retention note. **`docs/PLAN/04` should be amended.**

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| An endpoint ships without a permission requirement | Medium | High | Default refuse, plus a test that enumerates registered routes |
| The role check drifts back to the token claim | Medium | High | The integration test that revokes a role between two requests |
| A `403` leaks the existence of another organization's resource | Medium | Medium | `404` for anything outside scope, with a test |
| Idempotency records grow without bound | Low | Medium | An expiry and a sweep, like `P1-11`'s sessions |

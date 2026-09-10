# P1-16 — Management API: Organizations

**Date**: 2026-09-10
**Branch**: `feat/P1-16-organizations`
**Spec**: [`MEMORY/specs/P1-16-organizations.md`](../specs/P1-16-organizations.md)

---

## What this is

The first real `/v1` surface: five operations on organizations, generated from the contract and running behind `P1-15`'s chain.

Each handler is short, and that is the deliverable rather than a happy accident. Authentication, permissions, tenant scoping, the error envelope, idempotency, rate limiting and the audit guard all happened before the handler ran. What is left in each function is the thing the endpoint is actually about.

## The bug P1-15 shipped, found on the first real endpoint

> **An `INSTANCE_OWNER` acting on another organization could reach the endpoint and see nothing.**

`InScope` routed a cross-tenant action through `WithInstanceScope`, which sets `current_org_id()` to `NULL`. Every tenant policy reads `org_id = current_org_id()`, and that is false for every row when the value is `NULL`. So the most powerful role in the system got an **empty result** rather than a refusal — no error, no log line, nothing.

`P1-15`'s own test asserted a `201` from a handler that never touched a tenant-scoped table. It passed while the capability was broken, which is the same shape as the sweep that deleted nothing: **a check at the wrong layer for the claim it supports.**

The rule now is: **a request that names a target organization is scoped to that organization, whoever the caller is.** That is stricter than what it replaced, not a relaxation traded for the capability — the transaction can reach exactly one tenant's rows. `WithInstanceScope` stays for what genuinely spans organizations: listing them, and creating one before its tenant exists.

Two tests, one for each half: the cross-tenant read returns the target's rows, and it returns *only* the target's rows.

## `organizations` is the one table whose RLS is keyed on `id`

An organization row *is* the tenant, so the policy is `id = current_org_id()`. That makes two operations impossible under either scope:

- **List** spans organizations, so tenant scope is wrong by definition.
- **Create** happens before the organization exists, so there is no tenant to scope to.

And instance scope does not rescue them: `id = NULL` is false for every row, so a list would return nothing — silently — and a create would fail its `WITH CHECK`.

Same answer as `P0-12`'s partition maintenance, `P1-11`'s session sweep and `P1-15`'s idempotency sweep: a `SECURITY DEFINER` function with a pinned `search_path`, granted only to `auth_app`, narrow to exactly its question. **Neither function takes an organization id**, so neither is a way to reach a specific tenant's row by guessing one — asserted by reading `pg_get_function_arguments` rather than by trusting the comment.

Read, update and delete take no such function: they run under `WithTenant(target)`, where RLS confines them to a single row. `Store.Get` takes **no id parameter at all** — the row the transaction can see is the target, so there is no second way to name it and therefore no second thing to check.

## Deletion

Soft, via `deleted_at`. A timestamp rather than a `status` value, because `status` is a reversible lifecycle (`active` ⇄ `suspended`) and folding deletion into it immediately raises "may a deleted organization be suspended?" — a question with no useful answer. And a timestamp because **"when" answers questions "whether" cannot**, which is why `password_changed_at` is one too.

Every table referencing `organizations` is `ON DELETE RESTRICT`, so a hard delete of a populated tenant already fails at the database. Nothing here weakens a constraint to make deletion work.

Three things a delete does beyond setting the column:

- **Releases the domain.** A domain routes login traffic to a tenant; left claimed by a deleted organization it could never be reused, and reusing it is the ordinary case. A `CHECK` makes that a rule rather than a habit — and a mutation that dropped the `CHECK` left every test passing, because the handler happens to do the right thing. The constraint now has a test of its own, attempted as the owner by direct SQL, because the point is what the *database* refuses.
- **Revokes every session in the organization.** Otherwise a deleted tenant's users keep working until their tokens expire — a tenant deleted only in the console (abuse case A-8).
- **Requires the name typed back.** `docs/UI-UX/07` specifies typed confirmation in the console, and a UI affordance is not a control: a script that deletes the wrong organization never goes near the console. Case must match; "acme corp" and "Acme Corp" being interchangeable defeats the point of typing it.

**The audit history survives**, and that is `P0-12`'s decision rather than this task's: `events` carries no foreign key on `org_id`, deliberately, so the trail outlives the rows it refers to. The test asserts it by reading the rows back after the delete rather than by inspecting the schema.

## Authorization at the field level

`PATCH` requires `ORG_OWNER` at the route and **`INSTANCE_OWNER` when the body contains `status`**. Suspending an organization locks out every user in it, and an organization owner suspending their own tenant — or an investigation — is not a capability anybody asked for.

Route-level authorization cannot express "depends what is in the body", so the handler checks it. Through the same `Authorize` the middleware uses, so there is one implementation of "does this caller satisfy this role" rather than two that can drift. The test pairs the refusal with a control: the same caller *can* rename through the same endpoint, so the refusal is about the field rather than the route.

## The permission table

`Chain.Handle` takes a `Requirement` per route, which works for a route registered by hand. These come from the generated router, and `oapi-codegen`'s chi server applies its middlewares to every operation uniformly — there is nowhere to hang a per-route argument.

So the requirement is looked up at request time from `management.Policy`, keyed by method and route pattern. **A route absent from the table gets the zero `Requirement`, which no caller can satisfy**, so the default-refuse property survives the move from an argument to a lookup.

Safe is not correct, though: a route silently unreachable is a broken endpoint, and a `403` on a working endpoint is a confusing thing to debug. So a test walks what the router **actually registers** — not a list somebody maintains — and fails on any `/v1` route with no entry. It also fails when the router registers no `/v1` routes at all, which is exactly what would have made it vacuous between `P1-15` and this task.

The reverse is checked too: an entry naming a route nobody serves makes the table "the permission surface plus some history".

## Three gaps in the contract, all made visible

### `PG-23` — prefixed identifiers, resolved toward UUIDs

`openapi.yaml` specified `org_01HQZX3M8K4N7P2R5T9V6W8Y0B`-style ids and argued the case well: an id pasted into a support ticket is self-describing, and passing a project id where a user id belongs is visible on sight.

What that cannot survive is being applied to part of the surface. `docs/PLAN/04` makes every primary key a UUID, the access token's `org_id` claim is a UUID, and OpenID Connect's `sub` — already shipped by `P1-08` — is a UUID that integrators store as a user's permanent key. Prefixing only the Management API gives one user two identifiers and makes every consumer convert; prefixing everything means changing a protocol field people have already stored.

**UUIDs everywhere**, and if prefixed identifiers are wanted later they arrive everywhere at once or not at all. The reasoning is in the schema description, not only in the backlog, because the next person to read `ResourceId` is the one who needs it.

### The `Forbidden` response contradicted itself

Its description said a resource in another organization returns `403` — and then argued, in the same paragraph, that distinguishing forbidden from not-found across a tenant boundary confirms the resource's existence. `P1-15` implemented the `404`. The contract now says what the code does.

### `additionalProperties: false` is documentation, not enforcement

`oapi-codegen` renders it as a struct with named fields, and `json.Unmarshal` silently discards anything else. So `{"settings": {"mfa_requried": true}}` would have been accepted, the response would have echoed back a policy without it, and an administrator would have believed MFA was on.

`management.BufferBody` keeps the raw bytes so the handler can compare the keys the caller **actually sent** against the schema. Every unknown key is reported, not just the first — a caller fixing three typos should learn about three, each as a field the console can highlight.

Buffering once in the chain also removed a double read: the idempotency middleware needs the same bytes to hash, and now three views — the hash, the decoder and the key check — see one array.

## Three error paths, and the one that was missed

The generated router has three places an error can be written, and setting two of them looks complete:

1. `StrictHTTPServerOptions.RequestErrorHandlerFunc` — the body failed to decode.
2. `StrictHTTPServerOptions.ResponseErrorHandlerFunc` — the handler returned an error.
3. **`ChiServerOptions.ErrorHandlerFunc`** — a path or query parameter failed to bind, which happens *before* the strict wrapper runs.

With only the first two set, `GET /v1/organizations/not-a-uuid` answered `text/plain` with `error unmarshaling 'not-a-uuid' text as *uuid.UUID`. The integration test caught it. The message is now fixed rather than the library's: the caller's own input is not a disclosure, but the internal type it failed to parse into is, and neither belongs in a response a consumer branches on.

## The generated router moved

It was mounted on the bare `health` sub-router, which skips the access log and the metrics — correct while the spec held only probes and discovery documents, and wrong the moment it grew `/v1`. **A management request that is neither logged nor timed is a management request nobody can investigate.**

It now sits on the main mux, and `AccessLog` skips the two probe paths by name. Skipped by path rather than by router, so the exclusion is two named endpoints a reader can see rather than a property of where something happens to be mounted.

`guardV1` engages the chain by **path prefix**, not by a list of operations, and that is the safer direction of default: a `/v1` endpoint added to `openapi.yaml` is guarded the moment it exists, without anybody remembering to add it. What it still needs is a declared permission, and `Policy`'s missing-key case refuses it until it has one.

## What is deliberately not built

- **Hard delete, or a purge of soft-deleted rows.** A purge is a separate, auditable operation and not a Phase 1 need.
- **Domain verification.** The column and its uniqueness exist; proving ownership is a later phase, and the contract says so.
- **`passkey` and `social` login methods.** Named as planned and rejected with a message that says so, because an API accepting a method nothing implements would silently disable every method that works.
- **Instance management.** `/v1/instances/{instance_id}` is in `docs/PLAN/05`'s endpoint list and has no task in Phase 1.

## Deployed and verified on staging

`9ad4660` rolled out on `10.1.200.13`. Backup first (97KB), then both migrations as the owner while the previous version still served, then the application — `migrated up: version 20260910000016`.

This deploy carried four tasks' worth of change: `P1-13`, `P1-14`, `P1-15` and `P1-16`. The VM had been sitting at `c3a00cb` (`P1-10`).

The smoke test drives the whole chain the way a consumer does — authorization code with PKCE through the public TLS endpoint, then the Management API with the resulting access token. Everything it creates is deleted afterwards, because nothing in the product can create a user or an application yet.

| Check | Result |
|---|---|
| Login flow through TLS | `/oauth/authorize` → `/login?request=…` → `302` to the callback with a code → a 970-character access token |
| `GET /v1/organizations` | `200`, `X-RateLimit-Limit: 600`, `Remaining: 599`, `Reset` a real timestamp, `Cache-Control: no-store` |
| `POST /v1/organizations` | `201`, and `settings` shows `mfa_required: true` **merged onto** the instance defaults rather than replacing them |
| The same `Idempotency-Key` again | `Idempotency-Replayed: true`, byte-identical body, **one** row named `Smoke P1-16` |
| The same key, a different body | `409 CONFLICT` |
| `{"settings": {"mfa_requried": true}}` | `400`, `details[0].field = settings.mfa_requried` — the misspelling is refused rather than dropped |
| `PATCH` naming one setting | `200`, `session_lifetime_hours` changed to 6 and `mfa_required` still `true` |
| `GET /v1/organizations/not-a-uuid` | `400` in the envelope, not the wrapper's `text/plain` |
| `DELETE` with the wrong confirmation | `400`, `details[0].field = confirm_name`, nothing deleted |
| `DELETE` with the right confirmation | `204` |
| `GET` the deleted organization | `404`, as an `INSTANCE_OWNER` |
| Audit | `organization.created`, `organization.updated`, `organization.deleted` |
| No token / a junk token | `401` with `WWW-Authenticate: Bearer realm="…", error="invalid_token"` |

**There are no `X-RateLimit-*` headers on a `401`**, and that is correct rather than a gap: the quota is per client, `RateLimit` sits inside `Require`, and an unauthenticated request has no client to charge.

The fixture's rows were deleted afterwards; the **audit events were not**. The `events` table is append-only and has no foreign key to `organizations`, so the smoke test's trail survives its own tenant — which is the property `P1-16`'s Definition of Done is about, demonstrated by accident.

Two things went wrong before it worked, both in the test rather than the service. `psql -Atc` prints the command tag on stdout alongside a `RETURNING` value, so the first run concatenated `INSERT 0 1` into a UUID. The cleanup then ran with empty ids and deleted nothing, which is why the second run collided with the first run's leftovers — a cleanup that cannot fail loudly is a cleanup that leaves state behind.

## Verification

- Unit: settings validation key by key, including every unknown key being reported and the password floor being named in the refusal; the route policy table against the routes the router registers, in both directions.
- Integration (real PostgreSQL, real Redis, a real signing key, tokens from `P1-07`'s own claim builder, the real generated router): every permission boundary, the field-level `status` rule with its control, `404` for another tenant, mass assignment having nowhere to land, the domain conflict naming nobody, the confirmation, the audit history surviving a delete, and a replayed `POST` creating one organization.
- **11 mutations, all caught**, including removing `BufferBody` from the chain and dropping the `CHECK` that keeps a deleted organization from holding a domain.

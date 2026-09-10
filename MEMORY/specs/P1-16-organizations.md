# P1-16 — Management API: Organizations

**Task**: `TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-16
**Depends on**: `P1-15`
**Plan refs**: `docs/PLAN/05-API-CONTRACT.md` § Endpoint Structure, `docs/PLAN/04-DATA-MODEL.md` § `organizations`, `docs/PLAN/08-AUTHORIZATION.md` Part B/C, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`

---

## 1. Business Objective

The first real endpoints on `/v1`. Organizations are the tenant boundary, so this is also the first place where the whole of `P1-15` — permissions, scoping, idempotency, quotas, audit — has to hold against a real resource rather than a test handler.

The MVP runs one organization. This is built for the multi-organization world anyway, because `docs/PLAN/16` puts Phase 2's activation immediately after and a tenant boundary retrofitted is a tenant boundary with holes.

## 2. Actors

| Actor | What they do here |
|---|---|
| `INSTANCE_OWNER` | Creates, lists, suspends, reactivates and deletes organizations |
| `ORG_OWNER` | Reads and edits their own organization's name, domain and settings |
| `ORG_ADMIN` | Reads their own organization |
| A service account | Anything its client's grants allow — this API is not console-only |

## 3. Functional Requirements

- **FR-1** `GET /v1/organizations` — a page of organizations, cursor-paginated.
- **FR-2** `POST /v1/organizations` — create one.
- **FR-3** `GET /v1/organizations/{org_id}` — read one.
- **FR-4** `PATCH /v1/organizations/{org_id}` — change name, domain, settings, or status.
- **FR-5** `DELETE /v1/organizations/{org_id}` — soft-delete one.
- **FR-6** `settings` is validated against a schema, and unknown keys are **refused** rather than stored.
- **FR-7** `domain` is unique across the instance, so tenant resolution stays unambiguous.
- **FR-8** Destructive operations require an explicit confirmation of what is being destroyed.
- **FR-9** Every lifecycle change writes an audit event.

## 4. Non-Functional Requirements

- **NFR-1** Every endpoint goes through `P1-15`'s generated router, so the served paths are exactly the documented ones (ADR-013).
- **NFR-2** A list page is one query. No N+1 per organization.
- **NFR-3** Deleting an organization never destroys its audit history.

## 5. Dependencies

| Needs | Provided by |
|---|---|
| The `/v1` chain | `P1-15` |
| `manager_roles` and the hierarchy | `P1-15`, `docs/PLAN/08` Part C |
| The generated server interface | `P0-16` |
| Audit events | `P0-12` |
| Session and refresh revocation on delete | `P1-11`, `P1-07` |

## 6. Database Changes

**One additive migration**, for `PG-22` (§21).

`organizations.deleted_at timestamptz` — nullable, indexed for the list query's partial predicate.

**Why a timestamp and not a `status` value.** `status` is a lifecycle the operator drives: `active` ⇄ `suspended`, reversible, and a suspended organization is one somebody intends to bring back. Deletion is a different axis, and folding it into `status` immediately raises "may a deleted organization be suspended?" — a question with no useful answer. A separate column keeps the two orthogonal.

It is also a timestamp rather than a boolean for the reason `password_changed_at` is: **"when" answers questions "whether" cannot**, and the first question asked about a deleted tenant is when it happened.

**Why not a hard delete.** Every table referencing `organizations` is `ON DELETE RESTRICT` (`users`, `projects`, `applications`, `roles`, `sessions`, `refresh_tokens`, `user_tokens`, `project_grants`), so a hard delete of a populated organization fails at the database with a foreign-key violation. That is a good property and this design keeps it: nothing here weakens a constraint to make deletion work.

`events` deliberately has **no** foreign key on `org_id` (`P0-12`), so audit history already survives whatever happens to the organization row. NFR-3 is satisfied by a decision made two phases ago, and the test asserts it rather than assuming it.

## 7. API Contract

Added to `openapi/openapi.yaml`, generated into the server interface. Nothing here is hand-registered — the `/oauth/*` exception in `oapi-codegen.yaml` is about protocol endpoints, and these are REST.

| Method | Path | Requires | Answers |
|---|---|---|---|
| `GET` | `/v1/organizations` | `INSTANCE_OWNER` | `200` a page |
| `POST` | `/v1/organizations` | `INSTANCE_OWNER` | `201` the organization |
| `GET` | `/v1/organizations/{org_id}` | `ORG_ADMIN` over it | `200` the organization |
| `PATCH` | `/v1/organizations/{org_id}` | `ORG_OWNER` over it, **and `INSTANCE_OWNER` to change `status`** | `200` the organization |
| `DELETE` | `/v1/organizations/{org_id}` | `INSTANCE_OWNER` | `204` |

`POST` and `DELETE` honour `Idempotency-Key`. Every response carries `X-RateLimit-*`.

### The organization resource

```json
{
  "id": "uuid",
  "name": "Acme Corp",
  "domain": "acme.example",
  "status": "active",
  "settings": {
    "password_policy": {"min_length": 12, "require_uppercase": true, "max_age_days": 90},
    "mfa_required": false,
    "session_lifetime_hours": 12,
    "allowed_login_methods": ["password"]
  },
  "created_at": "2026-09-10T12:00:00Z",
  "updated_at": "2026-09-10T12:00:00Z"
}
```

`instance_id` is **not** exposed. It is an internal grouping with one row in Phase 1, and a field a client can see is a field a client will send back.

## 8. Frontend Changes

None. `PF-25` builds the Organizations screen against these endpoints.

## 9. Backend Changes

New `internal/organization`: the store and the handlers. `internal/management` gains a route policy table (§10) and the `Guard` middleware that reads it.

## 10. Authorization Rules — the decision this task turns on

`P1-15`'s `Chain.Handle` takes a `Requirement` per route, which works when a route is registered by hand. These routes come from the **generated** router, and `oapi-codegen`'s chi server applies `ChiServerOptions.Middlewares` to every operation uniformly — there is nowhere to hang a per-route argument.

So the requirement is looked up at request time, from a table keyed by method and chi route pattern:

```go
var Policy = map[string]Requirement{
    "GET /v1/organizations":            {Role: InstanceOwner, Scope: ScopeInstance},
    "POST /v1/organizations":           {Role: InstanceOwner, Scope: ScopeInstance},
    "GET /v1/organizations/{org_id}":   {Role: OrgAdmin,      Scope: ScopeOrganization},
    "PATCH /v1/organizations/{org_id}": {Role: OrgOwner,      Scope: ScopeOrganization},
    "DELETE /v1/organizations/{org_id}":{Role: InstanceOwner, Scope: ScopeInstance},
}
```

**A route absent from this table gets the zero `Requirement`, which no caller can satisfy.** The default-refuse property `P1-15` built survives the move from an argument to a lookup — that is the whole reason the lookup is allowed to be a map with a missing-key case.

Safe is not the same as correct, though: a route silently unreachable is a broken endpoint. So a test walks every route the generated router registers under `/v1` and fails if any has no policy entry. The failure mode is a red test on the first run, not a 403 in production.

### The field-level rule on PATCH

`PATCH` requires `ORG_OWNER` at the route, and **`INSTANCE_OWNER` additionally when the body contains `status`**. Suspending an organization locks out every user in it; the card puts that with create and delete, and an `ORG_OWNER` suspending their own tenant is not a capability anybody asked for.

Route-level authorization cannot express "depends what is in the body", so the handler checks it — and that is the normal place for it, not a workaround. `docs/PLAN/08` Part C's hierarchy is evaluated by the same `Authorize`, so there is one implementation of "does this caller satisfy this role", called twice.

### Reading a deleted organization

`404`, for everybody including an `INSTANCE_OWNER`. A deleted tenant is not a suspended one, and an endpoint that keeps serving it makes "deleted" a label rather than a state. Its audit history is reachable through `P1-20`, which is where a deleted organization's history should be read from.

## 11. Tenant scoping

`GET`, `PATCH` and `DELETE` on a single organization run through `Middleware.InScope`, so RLS confines them.

`GET /v1/organizations` and `POST` cannot: listing organizations is inherently cross-tenant and creating one happens before the tenant exists. Both are `ScopeInstance`, both take `WithInstanceScope`, which is named, logged and audited — `docs/PLAN/08` Part B's requirement that the cross-tenant path be "explicit, documented, and auditable, not the normal path with the filter omitted".

`organizations` is under RLS with `org_id = current_org_id()` — where for this table the row's own `id` is the tenant. The list query therefore genuinely cannot run tenant-scoped, which is worth stating rather than discovering.

## 12. Validation

### `settings`

Validated field by field, and **unknown keys are refused**. Storing them silently is how a typo (`mfa_requried`) becomes a policy that is not in force and looks like it is.

| Key | Rule |
|---|---|
| `password_policy.min_length` | 8–128, and never below the instance floor |
| `password_policy.require_uppercase` | boolean |
| `password_policy.max_age_days` | 0 (never expires) or 1–3650 |
| `mfa_required` | boolean |
| `session_lifetime_hours` | 1–720 |
| `allowed_login_methods` | non-empty, each of `password`, `passkey`, `social`, and only `password` is implemented in Phase 1 |

`allowed_login_methods` accepting only `password` today is deliberate: `docs/UI-UX/21`'s governance rule says nothing may describe a capability that is not shipped, and an API that accepts `"passkey"` is describing one.

`min_length` never below the instance floor, because a per-organization policy that can weaken the platform's own minimum is a per-organization way to disable a control.

### `domain`

Lowercased, and unique across the instance — the schema already enforces uniqueness on `lower(domain)`. A conflict is `409`, and the message says the domain is taken **without** saying by whom: which organization owns a domain is not information a caller who does not administer it should get.

### `name`

Non-blank after trimming, bounded at 200 characters. Not unique: two customers may legitimately be called the same thing, and a uniqueness constraint on a display name is a support ticket generator.

## 13. Confirmation on destructive operations

`DELETE` requires `?confirm_name=` matching the organization's current name exactly.

`docs/PLAN/08` § Least Privilege asks for an extra step; `docs/UI-UX/07` specifies typed confirmation in the console. **The check belongs on the server**, because a UI affordance is not a control — a script that deletes the wrong organization does not go through the console at all.

A mismatch is `400` with a field error, never `404`. The caller has already passed the permission check for this organization, so telling them the name did not match discloses nothing they cannot already read.

## 14. Abuse Cases

| # | Abuse | Source | Defence |
|---|---|---|---|
| A-1 | An `ORG_ADMIN` creates or deletes an organization | card, `docs/SECURITY/02` §3 | `INSTANCE_OWNER` at the route |
| A-2 | An `ORG_OWNER` suspends their own organization to lock out an investigation | `docs/SECURITY/02` §3 | `status` needs `INSTANCE_OWNER`, checked on the body |
| A-3 | Reading another organization by id | `docs/SECURITY/02` §2 §14 | `404`, not `403` (`P1-15`) |
| A-4 | Mass assignment: `id`, `instance_id`, `created_at`, `status` via `POST` | `docs/SECURITY/02` §11 | The generated request types carry no such field; server-set values are never read from the body |
| A-5 | Weakening `password_policy.min_length` below the platform floor | `docs/SECURITY/02` §11 | Validated against the floor, not merely against a range |
| A-6 | Claiming another tenant's `domain` to capture its login traffic | `docs/PLAN/08` Part B | Unique index, and `409` without naming the holder |
| A-7 | Deleting an organization to destroy its audit trail | `docs/SECURITY/02` §19 | `events` has no FK on `org_id`; delete is soft; a test asserts the history survives |
| A-8 | A deleted organization's users keep working until their tokens expire | — | Delete revokes every session and refresh token in the organization |
| A-9 | An unannotated `/v1` route ships without a permission | card, `P1-15` | Default refuse, plus a test that every registered route has a policy entry |

## 15. Logging / Audit

| Event | When |
|---|---|
| `organization.created` | `POST` succeeds |
| `organization.updated` | `PATCH` changes anything |
| `organization.suspended` | `status` → `suspended` |
| `organization.reactivated` | `status` → `active` |
| `organization.deleted` | `DELETE` succeeds |

`organization.updated` names the fields that changed and, for `settings`, **what they changed to** — the same exception `EventApplicationUpdated` makes for redirect URIs, and for the same reason: "settings changed" cannot answer the question the log exists for. If somebody with admin access turns `mfa_required` off, this is the only place that shows it.

`organization.reactivated` needs an event type that does not exist yet; `organization.deleted` likewise.

Never logged: the confirmation string, or a request body.

## 16. Security Controls

- Server-side permission checks on every request, at the route and — for `status` — on the field.
- `404` for anything outside the caller's scope.
- Instance-scoped reads only where the operation is genuinely cross-tenant, on the named and audited path.
- Unknown settings keys refused, so a policy is never silently absent.
- Delete revokes sessions and tokens rather than trusting expiry.
- Audit history outlives the organization.

## 17. Testing Strategy

| Layer | Coverage |
|---|---|
| Unit | Settings validation, key by key, including unknown keys and the instance floor |
| Unit | The route policy table: every generated `/v1` route has an entry |
| Unit | Field-level `status` authorization, both directions |
| Integration | Each endpoint's permission boundary, with the roles that should and should not pass |
| Integration | An `ORG_OWNER` may change the name and may **not** change the status |
| Integration | Another organization's id answers `404`, not `403` |
| Integration | A duplicate `domain` is `409`, and the message names no organization |
| Integration | `DELETE` without a matching `confirm_name` changes nothing |
| Integration | **After `DELETE`, the audit history is still readable and the sessions are gone** |
| Integration | A deleted organization is `404` for an `INSTANCE_OWNER` |
| Integration | Mass assignment: `id`, `instance_id`, `status` in a `POST` body are ignored |
| Integration | A replayed `POST` creates one organization, not two |

## 18. Acceptance Criteria

The card's five, unchanged.

## 19. Implementation Sequence

1. The route policy table and `Chain.Guard`, with the "every route is annotated" test.
2. The migration for `deleted_at`, and the two new audit event types.
3. Settings validation.
4. The store.
5. The handlers, one at a time, `GET` first.
6. The OpenAPI spec, and the generated client.
7. Integration tests, then mutation testing.

## 20. Rollback Strategy

The migration is additive; the previous version ignores `deleted_at` and would show a deleted organization again, which is the correct behaviour for code that does not know about deletion. Removing the endpoints removes a capability nothing depends on yet — `PF-25` is not built.

## 21. Gap raised

**`PG-22` — organizations have no soft-delete, and the card requires one.** `docs/PLAN/04` models `status` as `active | suspended` and no deletion state at all, while `P1-16` step 4 says "prefer soft-delete or suspension over hard delete" and its Definition of Done requires deletion to preserve audit history. There is no column for it.

`docs/PLAN/04` should be amended to describe `deleted_at`, through the deliberate plan-change process (`AGENTS.md` rule 9).

## 22. Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| A `/v1` route ships with no policy entry and is silently unreachable | Medium | Medium | A test enumerating registered routes |
| The list query grows an N+1 when Phase 2 adds counts | Medium | Low | One query now; a count is a join, not a loop |
| `deleted_at` is forgotten in a later query and deleted tenants reappear | Medium | Medium | The store is the only place that reads `organizations`, and its methods filter |
| Soft delete accumulates rows forever | Low | Low | Deferred deliberately: a purge is a separate, auditable operation and not a Phase 1 need |

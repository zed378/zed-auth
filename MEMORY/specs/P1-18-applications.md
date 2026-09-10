# P1-18 — Management API: Applications

**Task**: [`TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-18](../../TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md)
**Spec required**: Yes — credential handling
**Template**: `docs/PLAN/19-FEATURE-SPECIFICATION-TEMPLATE.md`

---

## 1. Business Objective

Register and manage OIDC clients through the REST API, so a consumer application can be onboarded without touching the database — and so `P1-22`'s Applications tab has an API to be built on rather than a special case.

The credential rules are the point of this task. `P1-05` already implemented them; what is new here is exposing them over HTTP without any of them leaking on the way out.

## 2. Actors

| Actor | What they may do |
|---|---|
| `ORG_ADMIN` over the organization | List, read, create, update applications; rotate a secret |
| `ORG_OWNER` over the organization | All of the above, plus delete |
| `INSTANCE_OWNER` | Everything, in any organization, scoped to the one in the path |
| `PROJECT_OWNER` | Nothing yet — reserved (`P1-17`), activated in Phase 2 |
| A consumer application holding a client-credentials token | Same rules; the token's roles decide, not its subject type |

Delete is raised to `ORG_OWNER` for the same reason as `P1-17`'s project delete: it is the operation that cannot be undone, and deleting a registration stops every login through that client immediately.

## 3. Functional Requirements

| # | Requirement |
|---|---|
| FR-1 | `GET /v1/organizations/{org_id}/projects/{project_id}/applications` — a keyset-paginated list |
| FR-2 | `POST` the same path — register a client, returning the plaintext secret **once** for a confidential type |
| FR-3 | `GET .../applications/{application_id}` — read, never carrying a secret |
| FR-4 | `PATCH .../applications/{application_id}` — name, redirect URIs, post-logout URIs, grant types |
| FR-5 | `DELETE .../applications/{application_id}` — remove the registration |
| FR-6 | `POST .../applications/{application_id}/rotate-secret` — issue a new secret, returned once, with an overlap window |
| FR-7 | Redirect URI validation runs identically on create and update |
| FR-8 | `type` is immutable; a request that tries to change it is refused, not ignored |
| FR-9 | A public client (`spa`, `native`) has no secret: creating one returns none, and rotating one is refused |

## 4. Non-Functional Requirements

- Every response inherits `P1-15`'s envelope, pagination, rate limiting, idempotency and audit guard. This task adds no cross-cutting mechanism.
- List is a keyset page over `(created_at, id)`, the same shape as `P1-16` and `P1-17`, so a client walks one paginator across the whole API.
- Secret generation is 32 bytes of CSPRNG (`P1-05`), and hashing stays where `P1-05` put it.

## 5. Dependencies

- **`P1-05`** — the entire credential model: `client.Store`, `Secret`, `Credentials`, redirect URI canonicalisation, grant-type rules, and the audit events. This task adds an HTTP surface and one `List` method; it re-implements none of it.
- **`P1-15`** — the chain.
- **`P1-17`** — a project must exist for an application to hang from, and the path says which.
- **`P0-16`** — the contract generates the handler interface.

## 6. Database Changes

**None.** `applications` shipped in `P0-07` with its type `CHECK`, its public-clients-have-no-secret `CHECK`, its rotation-window `CHECK`, and its RLS policy. `P1-05` has been writing to it since.

One store method is added — `List` — with no schema behind it: `applications_project_idx` already covers the query.

## 7. API Contract

### The application resource

```json
{
  "id": "3f6b...",
  "project_id": "c5a4...",
  "name": "Billing portal",
  "type": "web",
  "redirect_uris": ["https://billing.example.com/callback"],
  "post_logout_redirect_uris": [],
  "grant_types": ["authorization_code", "refresh_token"],
  "has_secret": true,
  "previous_secret_expires_at": null,
  "created_at": "...",
  "updated_at": "..."
}
```

`id` **is** the OIDC `client_id` (`docs/PLAN/04` § applications). There is no separate field, because two identifiers for one thing is how a console shows the wrong one.

`has_secret` is a boolean, never the secret and never its hash. It answers the only question a reader legitimately has — "is this client configured with a credential?" — without answering "what is it?" or "how long is it?".

### The create response, and only it

```json
{ "...": "the application resource", "client_secret": "…" }
```

`client_secret` appears in the `201` body of create and in the `200` body of rotate-secret. **Nowhere else, ever.** It is not in the list, not in the read, not in the update response, and not in the audit payload.

### Rotation

```
POST .../applications/{application_id}/rotate-secret
{ "overlap_hours": 24 }     ← optional; default 24, 0 means retire the old one now
→ 200 { "client_secret": "…", "previous_secret_expires_at": "..." }
```

A verb-shaped sub-resource rather than a `PATCH` field, because rotation is an event with a side effect on a live system, not a property being set. `docs/PLAN/05` uses the same shape for `:deactivate` on a user.

`overlap_hours: 0` is the compromised-secret path: the previous secret stops working immediately, and the operator accepts the outage that follows for anything still using it.

## 8. Frontend Changes

None in this task. `P1-22` builds the Applications tab against this contract; `docs/UI-UX/08` already specifies the show-once dialog.

## 9. Backend Changes

- `internal/oauth/client`: add `List`; widen `NewStore` to take a `Recorder` interface rather than `*audit.Writer` concretely (see §15).
- `internal/application`: the five handlers plus rotation — the HTTP surface only.
- `internal/management/policy.go`: six route entries.
- `internal/httpserver`: a third sub-interface and its guard, as `P1-17` added the second.
- `openapi/openapi.yaml`, and the generated backend interface, console client and public API reference.

## 10. Authorization Rules

| Route | Role | Scope |
|---|---|---|
| `GET .../applications` | `ORG_ADMIN` | organization |
| `POST .../applications` | `ORG_ADMIN` | organization |
| `GET .../applications/{id}` | `ORG_ADMIN` | organization |
| `PATCH .../applications/{id}` | `ORG_ADMIN` | organization |
| `POST .../applications/{id}/rotate-secret` | `ORG_ADMIN` | organization |
| `DELETE .../applications/{id}` | `ORG_OWNER` | organization |

Rotation stays at `ORG_ADMIN` deliberately: it is the response to a suspected leak, and a control that requires waking the organization owner is a control that gets skipped at 3am. It is loud in the audit log instead.

## 11. Tenant and project scoping

Two containers in the path, and both must hold.

- The **organization** is enforced the way `P1-17` enforces it: the transaction is scoped to it and RLS confines every query. No handler compares an `org_id`.
- The **project** is not covered by RLS — `applications.project_id` is not a tenant column — so it is an explicit predicate: every read and write is `WHERE id = $1 AND project_id = $2`. An application id from another project in the same organization is `404`.

That difference is worth stating plainly: **RLS is the tenant boundary, not a general-purpose filter.** Assuming otherwise is how a project boundary quietly stops existing.

## 12. Validation

- **Redirect URIs** — `client.ValidateRedirectURI` per `P1-05`: exact strings, no wildcards, HTTPS except loopback for `native`, no fragment. Applied on create and update through the same function, which is what makes FR-7 true by construction rather than by discipline.
- **Grant types** — `client.ValidateGrantTypes`: `implicit` and `password` are refused for every type, and each type has its own allowed set.
- **`type`** — one of `web`, `native`, `spa`, `api`, `saml` on create; **absent from the update schema entirely**, and a body carrying it is rejected as an unknown field (`P1-16`'s raw-body check, which is why that exists).
- **`name`** — required, trimmed, non-blank, bounded.

## 13. Abuse Cases

Cross-referenced with `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`.

| # | Abuse case | Control | Test |
|---|---|---|---|
| A-1 | Read another tenant's application by id | RLS, and the project predicate | `404`, with a control proving the same read works for the owner |
| A-2 | Smuggle a permissive redirect URI through `PATCH` | Same validator on both paths | An update with `https://evil.example` appended is accepted only if it passes; a wildcard is refused on update exactly as on create |
| A-3 | Recover a secret by reading the resource | The secret is not in the read schema at all | Create, then `GET`, and assert the response contains no substring of the secret |
| A-4 | Strand a credential by flipping `type` | `type` is not updatable, and the store validates against the stored type | An update naming `type` is a `400`; the stored type is unchanged |
| A-5 | Give a public client a secret | Database `CHECK` plus the store | Rotating a `spa` client's secret is refused |
| A-6 | Keep a leaked secret alive by never rotating | Not a control this task can supply — rotation is exposed, and nothing records when a secret was last issued — `applications` has no such column, and deriving it from `updated_at` would be wrong, since any edit moves that | Out of scope; noted |
| A-7 | Read the secret out of the audit log | The audit payload carries the application id and never the secret or its hash | Assert no event payload contains the plaintext |
| A-8 | Delete an application to break logins, quietly | `ORG_OWNER` plus an audit event | The refusal for `ORG_ADMIN`, and the event for the owner |

## 14. Logging / Audit

`P1-05`'s events, unchanged: `application.created`, `application.updated`, `application.secret_rotated`, `application.deleted`.

**The update event records redirect URIs before and after, by value.** They are not secret, and a widened redirect URI is the single highest-value change anybody can make to a registration — it is the open-redirect and token-theft path. This is the only place it becomes visible after the fact.

Nothing logs a secret. `client.Secret` cannot be printed or marshalled — `String()`, `GoString()`, `Format` and `MarshalJSON` all return `[REDACTED]` — so this is a property of the type rather than a rule the handler follows.

## 15. Security Controls

- **The secret leaves the process exactly twice**, at create and at rotate, both times as an explicit `Reveal()` call in a response builder. Every other path holds a `Secret` that renders as `[REDACTED]`.
- **The audit guard must see these mutations.** `P1-05`'s store writes its own events through `audit.Writer`, which does not mark `P1-15`'s per-request trail — so every successful application mutation would be reported as unaudited and increment a metric that is supposed to be permanently zero. `NewStore` therefore takes an interface, and the handler passes an adapter that routes through `management.Audit`. **The alarm would have been false, which is worse than a missing one**: an alert that cries wolf on every normal request is an alert that gets muted.
- RLS for the tenant, an explicit predicate for the project (§11).
- `Idempotency-Key` on create and on rotate. Rotation is exactly the operation where a retried request must not issue a second secret.

## 16. Testing Strategy

- **Unit** — nothing new; `P1-05`'s validators already carry their tables.
- **Integration** (real PostgreSQL and Redis, real router, real tokens): every route's permission and its control; the secret appearing once and never again; the same redirect validation on both paths; rotation with an overlap where the old secret still authenticates and after a zero overlap where it does not; the type refusal; cross-tenant and cross-project `404`s; the four audit events; a replayed create producing one application.
- **Mutation** — at minimum: remove the project predicate, remove `type` from the rejected-fields set, and make the read schema carry the secret. Each must break a test.

## 17. Acceptance Criteria

The task's four DoD items, plus the global DoD. Specifically:

1. Create returns the secret; a subsequent `GET` of the same application contains no part of it.
2. A redirect URI refused on create is refused on update, asserted through the API rather than by reading the code.
3. Rotation returns a new secret, the previous one still authenticates until its expiry, and the event is written.
4. An update naming `type` is refused and changes nothing.

## 18. Implementation Sequence

1. OpenAPI first — the contract generates the interface (ADR-013).
2. `client.List` and the `Recorder` widening.
3. Handlers.
4. Policy entries and the server wiring.
5. Integration tests.
6. Regenerate the console client and public API reference; add the two paths to `SHIPPED`.

## 19. Rollback Strategy

No migration, so rollback is redeploying the previous image. Nothing here changes a column, and the previous version serves every route this one does except the new ones, which it 404s.

## 20. Technical Risks

| Risk | Mitigation |
|---|---|
| The generated update struct silently accepts `type` | The raw-body unknown-key check from `P1-16`, which exists because `additionalProperties: false` is documentation |
| Rotation retried without idempotency issues two secrets, the first of which nobody has | `Idempotency-Key` accepted on the route, and the overlap means the previous one keeps working regardless |
| The project predicate forgotten on one of six queries | One `Get` used by every other method, so there is a single place it can be wrong |

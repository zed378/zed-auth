# 04 - Permission Evaluation Engine

> Category: **Authorization** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P2-04, P2-06, P2-07 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how a permission question is answered: from a token claim, or live through `/v1/authz/check`, and what each answer is worth.

## Scope

The application-role decision path. Administrative access to `/v1` is `01-MULTI-TENANT-RBAC.md`.

## As Built

### Two ways to answer, with different freshness

1. **Token claims.** `backend/internal/grant/claims.go` reads the subject's `user_grants` row for the client's project inside a tenant transaction, plus their `manager_roles` by user id, and puts both in the access and ID tokens. A token is a snapshot: it stays true until it expires, which is why the plan tells consumers to prefer the live check for anything sensitive.
2. **The live check.** `POST /v1/authz/check` (`backend/internal/authz/handler.go`) resolves the question against current data.

### The decision

```
permission := resource.type + ":" + action
allowed    := the subject holds a role, in the caller's project,
              whose permission_keys contain that permission
```

`resource.id` and `resource.attributes` are accepted and unused; they are Phase 4b's inputs (`03-ATTRIBUTE-BASED-ACCESS-CONTROL.md`).

There is no organization or project field in the request type (`authz.Request`): both come from the caller's token, "partly so that there is nowhere to put one". The handler runs in the caller's tenant transaction, so RLS bounds every read.

The response carries `allowed`, `matched_policy` (the role key that decided, under RBAC) and `reasons`. A subject who does not exist and a subject with no matching role are answered identically, so the endpoint cannot be used to test whether a user exists.

### Caching

`backend/internal/authz/cache.go` caches two things in Redis: a user's role keys per (organization, user, project), and a project's role definitions per (organization, project). Entries expire after `DefaultTTL` = 30 seconds and are invalidated proactively when a grant or a role changes (`InvalidateUser`, `InvalidateProjectRoles`), which is what makes the TTL a backstop rather than the revocation window. Invalidation is issued after the transaction commits, so a rollback cannot drop an entry that was still correct and a concurrent read cannot re-cache a stale one. If Redis is unavailable the check still answers from the database, and the unavailability is counted.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Cache TTL | 30 seconds | `backend/internal/authz/cache.go` (`DefaultTTL`) |
| Invalidation | Proactive, per user+project and per project | `grant.Handler.invalidate`, `role` handlers |
| Endpoint requirement | `MEMBER` at organization scope | `backend/internal/management/policy.go` |
| Unknown subject | Same answer as "no matching role" | `backend/internal/authz/decide.go` |
| Resource attributes | Accepted, unused, never logged | `decide.go`, log-hygiene gate in `scripts/check.sh` |

## Interfaces

- `POST /v1/authz/check` — request: `subject.user_id`, `action`, `resource.type` (plus optional `resource.id`, `resource.attributes`); response: `allowed`, `matched_policy`, `reasons`. Schema in `openapi/openapi.yaml`; published reference under `public-site/docs/api-reference/`.
- Token claims: the role claim shape is described in `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`.

## Security Considerations

- **Why the endpoint exists**: a revoked role stays in an unexpired token. The live check plus proactive invalidation closes that gap to the cache TTL.
- **Never a general-purpose oracle**: the caller may only ask about their own organization's project, and a refusal never distinguishes a missing user.
- **Delegated roles are absent.** Until `P4-04`, a delegated `user_grants` row is invisible to both readers; treating the current behaviour as "delegation works" would be wrong (threat review T4-2).

## Verification

- `backend/internal/authz/authz_integration_test.go`, `cache_integration_test.go`, `tampering_integration_test.go`.
- `backend/internal/authz/decide_test.go` — the pure decision.
- Load results for the endpoint are recorded with `P2-17` in `MEMORY/records/`.

## Not Yet Built / Open Questions

- Delegated grants in the decision path, and invalidation keyed by grant rather than by user (`P4-04`; threat review T4-3 notes that revoking a grant held by 400 users would otherwise mean 400 invalidations).
- Attribute evaluation (Phase 4b).

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md` § Full Permission Check Flow; `docs/PLAN/12-PERFORMANCE.md`.
- `docs/API/00-API-OVERVIEW.md`, `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`.

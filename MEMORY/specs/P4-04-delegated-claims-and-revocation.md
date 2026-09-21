# P4-04 — Delegated Role Claims and Revocation Propagation

**Task**: `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § P4-04
**Plan refs**: `docs/PLAN/08` Part C § Token Claim Format and § Full Permission Check Flow, `docs/PLAN/12` (latency), `docs/PLAN/13` (metrics)
**Threat review**: T4-2 (readers ignore the subset), T4-3 (invalidation by grant, not by user), T4-13 (publish the real window)
**Decisions**: ADR-025 (stricter of both policies — applies to sign-in, not to this card)

---

## 0. The problem this closes

`P4-02` writes delegated `user_grants` rows. **Nothing reads them as access.** Both readers
select `role_keys` straight from the row:

- `grant.TokenClaims.ForToken` (`backend/internal/grant/claims.go`) for the token claim;
- `authz.readRoleKeys` (`backend/internal/authz/handler.go`) for `/v1/authz/check`.

Neither joins `project_grants`, so neither can see that a grant was revoked, and RLS hides
delegated rows from the granting tenant entirely. Until this card, a delegated role is
inert — which is why `P4-02` deliberately did not open visibility (its record says so).

Opening visibility and adding the join must happen **in the same change**. That ordering is
the whole of T4-2.

## 1. Scope

In: the reader-side join, the granting-side read policy, claim shape for delegated roles,
invalidation keyed by grant, the published revocation window, and grant-rate metrics.

Out: **cross-organization sign-in**. A session from another organization is still refused at
`/oauth/authorize`, so no live path issues a token carrying a delegated role. ADR-025 decided
the policy question (stricter of both organizations); building it is its own card, and it is
recorded in §8. This card makes `/v1/authz/check` correct for delegated access, and makes the
claim code correct for the day sign-in lands.

## 2. Functional requirements

| # | Requirement |
|---|---|
| F-1 | Every reader of `user_grants` resolves a delegated row against its grant: the effective keys are `role_keys ∩ granted_role_keys`, and **nothing at all** unless `status = 'active'` |
| F-2 | The granting organization can read delegated rows **of its own grants**, and nothing else, so `/v1/authz/check` in the granting tenant sees a partner user's delegated roles |
| F-3 | A third organization still sees nothing |
| F-4 | In the token claim, a delegated role's nested `org_id` is the organization that owns the project (the granting side); a direct role's stays the caller's organization |
| F-5 | Revoking a grant invalidates every cached decision that depended on it, in one write, without scanning users |
| F-6 | The revocation window per path is measured, documented in `docs/` and published on the public site |
| F-7 | Project Grant creation and revocation are counted as metrics (`docs/PLAN/13` names an unusual spike as a misuse signal) |

## 3. Non-functional

- `/v1/authz/check` p95 stays within `docs/PLAN/12`'s target. The join adds one `LEFT JOIN`
  on an indexed column to a query that already runs; the cache check adds at most one extra
  Redis `GET`, and only for a delegated entry.
- No `SECURITY DEFINER` and no tenant switching (T4-3's ask, and the project's standing rule).

## 4. Database changes (migration 038)

**One new policy**, additive:

```sql
CREATE POLICY user_grants_granting_side_read ON user_grants
    FOR SELECT
    USING (
        project_grant_id IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM project_grants g
             WHERE g.id = user_grants.project_grant_id
               AND g.granting_org_id = current_org_id()
        )
    );
```

Permissive policies are OR-ed, so the existing tenant policy is unchanged: the receiving
organization still reads its own rows through `org_id = current_org_id()`. The new one adds
exactly the rows a granting organization must see to decide access in its own project, and
only through a grant it made. Read-only: writes remain the receiving side's.

**Consequences accepted deliberately:** a role's `grant_count`, and the guard that refuses
deleting a role some grant references, now also count delegated holders in the granting
tenant. That is correct — a role delegated to a partner *is* held by their users — and the
guard's message already speaks in counts.

## 5. Reader change

Both readers use the same shape:

```sql
SELECT CASE
         WHEN ug.project_grant_id IS NULL THEN ug.role_keys
         WHEN pg.status <> 'active' THEN '{}'::text[]
         ELSE (SELECT coalesce(array_agg(k ORDER BY k), '{}')
                 FROM unnest(ug.role_keys) AS k
                WHERE k = ANY (pg.granted_role_keys))
       END,
       coalesce(ug.project_grant_id::text, ''),
       coalesce(pg.granting_org_id::text, '')
  FROM user_grants ug
  LEFT JOIN project_grants pg ON pg.id = ug.project_grant_id
 WHERE ug.user_id = $1 AND ug.project_id = $2
```

The intersection is computed in SQL rather than in Go **on purpose**: a grant narrowed by a
direct `UPDATE` (which the trigger allows only through revoke-and-regrant, but which an
operator could do from the owner connection) is still answered correctly, because nothing
between the row and the answer caches the grant's contents.

## 6. Cache change

Today an entry is keyed `authz:grants:{org}:{user}:{project}` and invalidated per user.
Revoking a grant held by 400 users would be 400 invalidations — the "scan or a guess" the
cache's own comment rules out.

Instead, each grant carries a **generation** in Redis (`authz:grantgen:{grant_id}`,
incremented on revocation). A cached entry records the grant it depended on and the
generation it was written at. On lookup:

- an entry with no grant id is served as today;
- an entry with a grant id is served only if the grant's current generation matches.

Revocation is therefore one `INCR`, whatever the number of holders. A missing generation key
is treated as generation 0, so a cold Redis does not invalidate everything, and the 30-second
TTL remains the backstop.

## 7. API and claim shape

No new endpoints. `POST /v1/authz/check` starts answering `allowed: true` for a delegated
role, in the granting organization's project.

The claim (`docs/PLAN/08` Part C) is unchanged in shape:

```json
"urn:authservice:iam:org:project:<project_id>:roles": {
  "cashier": { "org_id": "<granting organization>" }
}
```

For a direct role `org_id` is the caller's organization, as today. The nested field finally
carries the meaning the plan gave it: which organizational context the role came from.

## 8. Abuse cases (to test)

| # | Scenario | Control |
|---|---|---|
| A-1 | A revoked grant keeps granting access through a token or a check | F-1 reader join; test asserts both readers return nothing after revocation |
| A-2 | A role removed from the delegation (revoke + re-grant narrower) still resolves | Intersection in SQL; test narrows and re-checks |
| A-3 | A third organization reads delegated rows | Tenant policy; isolation test with organization C |
| A-4 | The granting organization reads delegated rows of a grant it did **not** make | New policy's `granting_org_id = current_org_id()` predicate |
| A-5 | A stale cache entry serves a revoked grant beyond the documented window | Generation check; test revokes and asserts the next read misses |
| A-6 | A delegated role is claimed with the receiving organization's `org_id`, so a consumer reads it as a role in their own tenant | F-4; claim test asserts the granting organization's id |
| A-7 | Delegated rows leak into a direct-grant listing for the wrong tenant | Existing handler filters plus the read-only policy |

## 9. Verification

- Integration tests in `internal/authz` and `internal/grant` covering F-1…F-4 and A-1…A-6.
- The RLS test in `backend/tests/security/` gains the granting-side visibility case and the
  third-organization case.
- Cache tests for generation invalidation, including "Redis unavailable ⇒ database answer".
- Mutations: remove the join, remove the `status` check, remove the intersection, remove the
  generation check, widen the policy predicate to any grant. Each must turn a named test red.
- The revocation window is measured on staging and recorded, not estimated.

## 10. What this card does not finish

- Cross-organization sign-in (ADR-025). Until it lands, delegated roles cannot appear in a
  live token — only in `/v1/authz/check`. The claim path is implemented and tested directly.
- `P4-06`'s console for the receiving side.
- ABAC (Phase 4b) still ignores attributes.

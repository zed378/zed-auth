# 03 - Indexing and Query Optimization

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-07, P1-06, P1-11, P1-29, P2-01, P2-16, P2-17 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Catalogue the indexes actually defined in the schema, explain why each one is shaped the way it is (unique vs. non-unique, partial, GIN), and document the automated test suite that proves row-level security does not degrade query plans as the number of tenants grows — the specific failure mode this document exists to rule out.

## Scope

Source: `backend/migrations/*.up.sql` (index definitions) and `backend/tests/security/rls_plans_test.go` (plan-shape verification). Column-level detail for each table is in [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md), which this document does not repeat — it lists indexes by *purpose* instead. Target latencies are set in `docs/PLAN/12-PERFORMANCE.md` (design intent, not restated here); this document cites the one measured data point available against those targets and says plainly where no other measurement exists yet.

## As Built

### Uniqueness indexes that double as lookup indexes

The most common index in this schema does two jobs at once: enforcing a per-tenant uniqueness constraint and serving the exact query the login/token path issues.

| Index | Table | Columns | Purpose |
|---|---|---|---|
| `users_org_email_key` | `users` | `(org_id, lower(email))` | Email unique **per organization**, not globally — the same lookup the login path performs |
| `users_org_username_key` | `users` | `(org_id, lower(username))` WHERE `username IS NOT NULL` | Partial: rows with no username never compete for uniqueness |
| `organizations_domain_key` | `organizations` | `lower(domain)` WHERE `domain IS NOT NULL` | Two tenants cannot claim the same domain — see `docs/MULTI-TENANCY/03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md` for why this is currently unused for routing |
| `projects_org_name_key` | `projects` | `(org_id, lower(name))` | Project names unique per organization |
| `roles_project_key_key` | `roles` | `(project_id, key)` | Role keys unique **per project** — "admin" in Project A and "admin" in Project B are unrelated |
| `user_grants_user_project_key` | `user_grants` | `(user_id, project_id)` | One grant row per user per project (direct or delegated) |
| `refresh_tokens_hash_key` | `refresh_tokens` | `token_hash` | The token-lookup index; a collision would mean one token authenticating as another |
| `sessions_token_hash_key` | `sessions` | `token_hash` WHERE NOT NULL | Same shape, for session cookies |
| `user_tokens_hash_key` | `user_tokens` | `token_hash` | Same shape, for invite/reset/verification links |
| `signing_keys_kid_key` | `signing_keys` | `kid` | JWKS `kid` lookup |
| `user_mfa_factors_credential_unique` | `user_mfa_factors` | `credential_id` WHERE NOT NULL | One WebAuthn credential belongs to one account (`docs/SECURITY/02` §2) |
| `user_recovery_codes_unique` | `user_recovery_codes` | `(user_id, code_hash)` | A CSPRNG collision becomes a failed `INSERT` rather than a code that works twice |

### Partial indexes for a state that dominates the query pattern

| Index | Table | Predicate | Why partial |
|---|---|---|---|
| `organizations_live_idx` | `organizations` | WHERE `deleted_at IS NULL` | The live set is what almost every query wants, and it stays small relative to the table as soft-deletions accumulate |
| `sessions_active_expiry_idx` | `sessions` | WHERE `revoked_at IS NULL` | The expiry sweep only ever wants rows that could still be live |
| `user_tokens_expiry_idx` | `user_tokens` | WHERE `used_at IS NULL` | Same shape, for the token sweep |
| `project_grants_project_granted_org_key` | `project_grants` | WHERE `status = 'active'` | Uniqueness applies only to the currently-active grant; a revoked-then-regranted pair is not a conflict |
| `signing_keys_one_current_per_purpose` | `signing_keys` | WHERE `status = 'current'` | Exactly one signing key may be `current` per purpose at a time |
| `user_mfa_factors_one_totp` | `user_mfa_factors` | WHERE `type = 'totp'` | One TOTP secret per user; WebAuthn credentials are intentionally unrestricted in count |
| `events_instance_level_idx` | `events` | WHERE `org_id IS NULL` | Instance-level audit rows are rare and read together — they would otherwise be a NULL-key minority inside the tenant-scoped index |

### GIN indexes on array columns

| Index | Table | Column | Query it serves |
|---|---|---|---|
| `user_grants_role_keys_idx` | `user_grants` | `role_keys` | "Is this role in use by any grant" — asked on every role deletion, since `role_keys` is a `text[]` with no foreign key (permission and role keys are the consumer application's own vocabulary, not rows to join — `docs/PLAN/04`) |
| `applications_allowed_origins_idx` | `applications` | `allowed_origins` | The per-application CORS check; small today, made not-a-scan at the cheapest possible moment (`021`) |

No GIN index exists on `roles.permission_keys` or on any `jsonb` column (`organizations.settings`, `events.payload`, `user_mfa_factors.data`) — nothing in the current query set filters *by* the contents of those columns; they are read whole by primary key or by the tenant index instead.

### Plain B-tree tenant/lookup indexes

Every tenant-scoped table also carries a plain `org_id` (or, for child tables, `project_id`/`user_id`) index to serve the ordinary listing query — enumerated per table in [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md): `organizations_instance_id_idx`, `projects_org_idx`, `applications_{project,org}_idx`, `roles_org_idx`, `user_grants_{user,project,org,project_grant}_idx`, `project_grants_{granting_org,granted_org}_idx`, `sessions_{user,org}_idx`, `refresh_tokens_{family,user,session}_idx`, `user_tokens_user_purpose_idx`, `events_{org_created_at,type,actor}_idx`, `manager_roles_{user,scope}_idx`, `user_mfa_factors_{user,org}_idx`, `user_recovery_codes_{user,org}_idx`.

### Keyset pagination over `OFFSET`

The `SECURITY DEFINER` listing functions that must page across tenants — `organizations_page(after_created, after_id, max_rows)` and `organizations_administered_by(who, after_created, after_id, max_rows)` (`backend/migrations/20260910000016_organizations_soft_delete.up.sql`, `backend/migrations/20260912000025_organizations_administered_by.up.sql`) — use keyset pagination (`WHERE (created_at, id) > (after_created, after_id) ORDER BY created_at, id`) rather than `OFFSET`. This avoids the re-scan-from-zero cost `OFFSET` has on a growing table, at the cost of the caller carrying an opaque cursor instead of a page number.

### Why row-level security does not force a sequential scan

The tenant predicate every policy adds (`org_id = current_org_id()`) is itself indexed by the plain B-tree indexes above, so the planner can push it into an index scan rather than reading every tenant's rows and filtering afterward. `backend/tests/security/rls_plans_test.go` asserts this holds **at scale**, not just at small table sizes where Postgres might reasonably choose a sequential scan anyway:

- `TestRowLevelSecurityDoesNotForceSequentialScansAtScale` seeds 400 organizations (each with a project, role, user and grant) and asserts `EXPLAIN` for a tenant-scoped `projects`/`roles`/`user_grants` query contains no `Seq Scan`.
- `TestTheTenantPredicateReachesTheIndex` asserts the plan shows `Index Cond` rather than a `Filter` applied after a broader read — the distinguishing signal that the predicate actually steered which rows were read, not merely which rows were kept.
- `TestTheScaleTestsActuallyHaveScale` and `TestTheSequentialScanDetectorWorks` are controls: the first guards against the seed silently inserting nothing, the second proves the sequential-scan detector itself can fire, by running an unindexed `count(*)` over `manager_roles` (which has no tenant column and no policy) and asserting that one *does* show `Seq Scan`.

This test file's own commentary states the failure mode precisely: "Correctness is untouched — the wrong rows are filtered out, just after they have been read. The symptom is a service that is fine with three organizations, fine with thirty, and unusable at three hundred, and by then the cause is invisible because every test still passes." (`backend/tests/security/rls_plans_test.go`)

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Every foreign key is indexed | Yes, for every `org_id`/`project_id`/`user_id` foreign key in the schema | see per-table index lists in [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md) |
| Array membership queries use GIN, never a sequential scan of the array | `user_grants.role_keys`, `applications.allowed_origins` | `022`, `021` |
| Live/active subsets get partial indexes rather than the full table | see partial-index table above | multiple migrations |
| Cross-tenant listing uses keyset pagination, not `OFFSET` | `organizations_page`, `organizations_administered_by` | `016`, `025` |
| RLS predicate must remain an `Index Cond`, not a post-read `Filter`, at 400-tenant scale | asserted mechanically | `backend/tests/security/rls_plans_test.go` |

## Interfaces

Not applicable — no additional configuration surface beyond the index definitions above.

## Security Considerations

A query plan that degrades into a sequential scan under RLS is a performance problem with a security-adjacent consequence: correctness is not lost (the wrong tenant's rows are still filtered before being returned), but a service that becomes unusable as tenant count grows is itself an availability failure, and one that is invisible to a functional test suite. `TestRowLevelSecurityDoesNotForceSequentialScansAtScale` exists specifically because this class of regression would otherwise ship silently.

## Verification

- `TestRowLevelSecurityDoesNotForceSequentialScansAtScale`, `TestTheTenantPredicateReachesTheIndex`, `TestTheScaleTestsActuallyHaveScale`, `TestTheSequentialScanDetectorWorks` — `backend/tests/security/rls_plans_test.go`.
- One measured data point against `docs/PLAN/12`'s targets: `MEMORY/records/2026-09-12-P2-17-acceptance.md` records a local-stack load test of `/v1/authz/check` (RBAC path) at `p50 6.3ms / p95 22.0ms / p99 30.4ms / max 74.5ms` against targets of `p50 20ms / p95 80ms / p99 150ms` — within target, but this is a single endpoint on a local stack, not a production or staging measurement, and no equivalent load-test record exists yet for the Management API's list/pagination endpoints this document's indexes primarily serve.

## Not Yet Built / Open Questions

- No load test record exists yet for the Management API listing endpoints (organizations, projects, roles, grants) under the keyset-pagination functions described above, so their behavior at high tenant/row counts is verified only by the RLS plan-shape tests, not by a latency measurement.
- `docs/PLAN/12-PERFORMANCE.md`'s load-testing plan (mixed workload, degraded-dependency scenarios, horizontal-scale verification) is design intent; only the single RBAC-path measurement above has a recorded result.

## Related Documents

- [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md), [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md), [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md)
- `docs/PLAN/12-PERFORMANCE.md` (latency targets, design intent)
- `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`
- `scripts/loadtest/` (load-test harness), `MEMORY/records/2026-09-12-P2-17-acceptance.md`

# 01 - Relational Schema Definitions

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-07, P1-05…P1-29, P2-01…P2-14, P3-01…P3-13, P4-01…P4-05 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Give the exact, current shape of every table in the schema — columns, constraints, indexes, foreign keys and their `ON DELETE` behaviour — and name the Go package that owns each one, so this document rather than the migration history is the reference for "what does this table actually look like today".

## Scope

Source: all 37 files in `backend/migrations/*.up.sql`, read in numeric order, taking the **last** definition of every column, constraint, index and trigger (a later migration silently redefining an earlier object is the normal pattern here — e.g. `roles_org_must_match_project()` from migration `022` is replaced by the generic `org_must_match_project()` in `024`, which is itself replaced again in `037`). Row-level security policies, triggers and `SECURITY DEFINER` functions are catalogued in full in [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md) — this document lists which trigger fires on which table but does not repeat each trigger's body. Index rationale lives in [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md).

All 17 tables are listed. `events` is partitioned and documented in more depth in [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md); it is included here too for completeness.

## As Built — Conventions

From `backend/migrations/20260908000001_baseline.up.sql`, applied throughout:

- `timestamptz`, never `timestamp` — a naive timestamp on an audit row is ambiguous across timezones.
- Status/lifecycle columns are `text` + `CHECK`, never a native `enum` — adding a value to a Postgres enum cannot run inside a transaction on older versions, and removing one is effectively impossible, which conflicts with the expand/contract discipline (see [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md)).
- Every tenant-scoped table carries `org_id` directly (denormalized, even where reachable through a join), because row-level security filters on the column rather than a join path.
- All primary keys are UUIDs (`gen_random_uuid()`), not prefixed identifiers — a deliberate, recorded choice (`TASKS/BACKLOG.md` PG-23) over the OpenAPI contract's original prefixed-id sketch, because every other identifier in the system (JWT `sub`, `org_id` claims) is already a UUID and a half-applied convention was judged worse than none.

---

## `instances`

Deployment root. `backend/migrations/20260908000001_baseline.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `name` | `text` | no | — |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |

- **Constraints**: `instances_name_not_blank` — `length(btrim(name)) > 0`.
- **Indexes**: primary key only.
- **Foreign keys**: none (root of the hierarchy).
- **Trigger**: `instances_set_updated_at` → `set_updated_at()`.
- **Row-level security**: none — deployment-level, not tenant data (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: no dedicated package. Read only inside `organization_create()` (a `SECURITY DEFINER` SQL function, see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)); written only by deploy-time seeding and `backend/internal/testsupport/factory.go` in tests.

---

## `organizations`

The tenant. `backend/migrations/20260908000002_organizations_and_users.up.sql`, extended by `20260910000016_organizations_soft_delete.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `instance_id` | `uuid` | no | — |
| `name` | `text` | no | — |
| `domain` | `text` | yes | — |
| `settings` | `jsonb` | no | `{"password_policy": {"min_length": 12, "require_uppercase": true, "max_age_days": 90}, "mfa_required": false, "session_lifetime_hours": 12, "allowed_login_methods": ["password"]}` |
| `status` | `text` | no | `'active'` |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |
| `deleted_at` | `timestamptz` | yes | — (added `016`) |

- **Constraints**: `organizations_name_not_blank`; `organizations_status_valid` (`status IN ('active','suspended')`); `organizations_settings_object` (`jsonb_typeof(settings) = 'object'`); `organizations_deleted_has_no_domain` (`deleted_at IS NULL OR domain IS NULL`, `016` — a deleted organization must release its domain so the address can be reused).
- **Indexes**: `organizations_domain_key` UNIQUE on `lower(domain)` WHERE `domain IS NOT NULL`; `organizations_instance_id_idx (instance_id)`; `organizations_live_idx (created_at, id)` WHERE `deleted_at IS NULL` (`016`).
- **Foreign keys**: `instance_id → instances(id)` ON DELETE RESTRICT.
- **Trigger**: `organizations_set_updated_at` → `set_updated_at()`.
- **Row-level security**: `organizations_tenant_isolation`, keyed on `id` (an organization row **is** the tenant), not `org_id`. See [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md) for why list/create need `SECURITY DEFINER` functions (`organizations_page`, `organization_create`, `organizations_administered_by`) instead.
- **Go**: `backend/internal/organization` (`store.go`, `handler.go`, `mfaimpact.go`) is the writer. Read by `backend/internal/authn/{loginpolicy.go,store.go}`, `backend/internal/login/{branding.go,notme.go,password.go}`, `backend/internal/account/handler.go`, `backend/internal/user/handler.go`.
- **Note on `domain`**: stored and editable through the organization CRUD API, but no authentication or tenant-resolution code path reads it. See `docs/MULTI-TENANCY/03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`.

---

## `users`

`backend/migrations/20260908000002_organizations_and_users.up.sql`, extended by `20260909000010`, `20260910000017`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `org_id` | `uuid` | no | — |
| `email` | `text` | no | — |
| `username` | `text` | yes | — |
| `password_hash` | `text` | yes | — |
| `status` | `text` | no | `'invited'` |
| `mfa_enabled` | `boolean` | no | `false` |
| `display_name` | `text` | yes | — |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |
| `password_changed_at` | `timestamptz` | yes | — (added `010`; `NULL` means never recorded, treated as **not** expired) |
| `email_verified_at` | `timestamptz` | yes | — (added `017`) |

- **Constraints**: `users_status_valid` (`status IN ('active','locked','invited','deactivated')`); `users_email_not_blank`; `users_email_shape` (`email LIKE '%_@_%'`, a structural check only — real validation is application-layer); `users_username_not_blank`.
- **Indexes**: `users_org_email_key` UNIQUE on `(org_id, lower(email))` — email is unique **per organization**, not globally; `users_org_username_key` UNIQUE on `(org_id, lower(username))` WHERE `username IS NOT NULL`; `users_org_status_idx (org_id, status)`; `users_org_created_at_idx (org_id, created_at DESC)`.
- **Foreign keys**: `org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: `users_set_updated_at` → `set_updated_at()`. `mfa_enabled` is also kept in sync by `user_mfa_factors_sync_flag`, a trigger on `user_mfa_factors` (see below).
- **Row-level security**: `users_tenant_isolation` (`org_id = current_org_id()`).
- **Go**: `backend/internal/user` (`store.go`, `handler.go`, `tokens.go`) is the primary owner. Also read/written by `backend/internal/authn/{user.go,store.go}` (login), `backend/internal/authz/handler.go`, `backend/internal/grant/handler.go`, `backend/internal/oauth/userinfo/store.go`, `backend/internal/organization/mfaimpact.go`, `backend/internal/projectgrant/{delegated.go,owners.go}` (membership checks for delegation).

---

## `projects`

`backend/migrations/20260908000003_projects_applications_roles.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `org_id` | `uuid` | no | — |
| `name` | `text` | no | — |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |

- **Constraints**: `projects_name_not_blank`.
- **Indexes**: `projects_org_name_key` UNIQUE on `(org_id, lower(name))`; `projects_org_idx (org_id)`.
- **Foreign keys**: `org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: `projects_set_updated_at` → `set_updated_at()`.
- **Row-level security**: `projects_tenant_isolation` (`org_id = current_org_id()`).
- **Go**: `backend/internal/project/store.go` is the writer; read by `backend/internal/application/handler.go`, `backend/internal/projectgrant/handler.go`, `backend/internal/role/handler.go`. `projects` is also the table every `org_must_match_project()` trigger call looks up (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).

---

## `applications`

OIDC/SAML clients. `backend/migrations/20260908000003_projects_applications_roles.up.sql`, extended by `20260911000020_application_allowed_origins.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` — doubles as the OIDC `client_id` |
| `project_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — (denormalized from `projects` for RLS) |
| `name` | `text` | no | — |
| `type` | `text` | no | — |
| `client_secret_hash` | `text` | yes | — (NULL for public clients) |
| `previous_client_secret_hash` | `text` | yes | — |
| `previous_client_secret_expires_at` | `timestamptz` | yes | — |
| `redirect_uris` | `text[]` | no | `'{}'` |
| `post_logout_redirect_uris` | `text[]` | no | `'{}'` |
| `grant_types` | `text[]` | no | `'{authorization_code,refresh_token}'` |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |
| `allowed_origins` | `text[]` | no | `'{}'` (added `020`; CORS origins, distinct from `redirect_uris`) |

- **Constraints**: `applications_name_not_blank`; `applications_type_valid` (`type IN ('web','native','spa','api','saml')`); `applications_public_clients_have_no_secret` (`type NOT IN ('spa','native') OR client_secret_hash IS NULL`); `applications_previous_secret_has_expiry` (hash and expiry are set together or not at all); `applications_allowed_origins_shape` (`origins_are_well_formed(allowed_origins)`, an `IMMUTABLE` SQL function checking each origin against `^https?://[^/?#*[:space:]]+$`, `020`).
- **Indexes**: `applications_project_idx (project_id)`; `applications_org_idx (org_id)`; `applications_allowed_origins_idx` GIN on `allowed_origins` (`021`).
- **Foreign keys**: `project_id → projects(id)` ON DELETE RESTRICT; `org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: `applications_set_updated_at` → `set_updated_at()`; `applications_org_matches_project` → `org_must_match_project()` (added `024` — writable since Phase 0 with nothing checking this until then).
- **Row-level security**: `applications_tenant_isolation` (`org_id = current_org_id()`). Bootstrap reads bypass this via `application_by_client_id()` and `origin_is_registered()` — both `SECURITY DEFINER` (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: `backend/internal/application/handler.go` is the writer for the Management API; `backend/internal/oauth/client/store.go` resolves clients for the OAuth/OIDC flows (including the pre-tenant bootstrap read) and is one of the named exceptions in `TestOnlyNamedPlacesBypassTheTenantScope`; also read by `backend/internal/authz/handler.go`, `backend/internal/project/store.go`.

---

## `roles`

`backend/migrations/20260908000003_projects_applications_roles.up.sql`, rules added by `20260912000022_role_rules.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `project_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `key` | `text` | no | — |
| `display_name` | `text` | no | — |
| `permission_keys` | `text[]` | no | `'{}'` |
| `is_builtin` | `boolean` | no | `false` |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |

- **Constraints**: `roles_key_not_blank`; `roles_key_shape` (`key ~ '^[a-z0-9][a-z0-9_-]{0,62}$'` — role keys appear inside JWT claim keys, so the character set is restricted); `roles_display_name_not_blank`; `roles_permission_keys_well_formed` (`022`, each key matches `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$` — `resource:action`, dotted namespacing allowed, **no wildcards**); `roles_permission_keys_bounded` (`022`, ≤ 256 keys); `roles_permission_keys_distinct` (`022`, no duplicate keys).
- **Indexes**: `roles_project_key_key` UNIQUE on `(project_id, key)` — roles are scoped per project; `roles_org_idx (org_id)`.
- **Foreign keys**: `project_id → projects(id)` ON DELETE RESTRICT; `org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: `roles_set_updated_at`; `roles_builtin_immutable` → `roles_builtin_is_immutable()` (`022` — a built-in role cannot be renamed, deleted, or have its `is_builtin` flag cleared; `display_name` stays editable); `roles_org_matches_project` → `org_must_match_project()` (unified onto the shared function in `024`).
- **Row-level security**: `roles_tenant_isolation` (`org_id = current_org_id()`).
- **Go**: `backend/internal/role/store.go` is the writer; read by `backend/internal/authz/handler.go`, `backend/internal/project/store.go`, `backend/internal/projectgrant/store.go`.

---

## `project_grants`

Cross-organization delegation contract. `backend/migrations/20260908000004_grants_and_manager_roles.up.sql`, extended by `20260915000035_project_grant_lifecycle.up.sql`, `20260915000036_project_grant_holder_count.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `project_id` | `uuid` | no | — |
| `granting_org_id` | `uuid` | no | — |
| `granted_org_id` | `uuid` | no | — |
| `granted_role_keys` | `text[]` | no | `'{}'` |
| `status` | `text` | no | `'active'` |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |
| `revoked_at` | `timestamptz` | yes | — |

- **Constraints**: `project_grants_status_valid` (`status IN ('active','revoked')`); `project_grants_no_self_grant` (`granting_org_id <> granted_org_id`); `project_grants_revoked_has_timestamp` (`(status = 'revoked') = (revoked_at IS NOT NULL)`).
- **Indexes**: `project_grants_project_granted_org_key` UNIQUE on `(project_id, granted_org_id)` WHERE `status = 'active'` — one active grant per project per receiving organization; `project_grants_granting_org_idx (granting_org_id)`; `project_grants_granted_org_idx (granted_org_id, status)`.
- **Foreign keys**: `project_id → projects(id)` ON DELETE RESTRICT; `granting_org_id → organizations(id)` ON DELETE RESTRICT; `granted_org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: `project_grants_set_updated_at`; `project_grants_org_matches_project` → `org_must_match_project()` (checks `granting_org_id`, `024`); `project_grants_only_narrow` (`035`) — refuses any `UPDATE` that changes `project_id`, either organization, or `granted_role_keys`, and refuses reactivating a `revoked` grant, **for every writer including the owner connection**.
- **Row-level security**: `project_grants_tenant_isolation` — the one two-sided policy in the schema. See [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md) for the exact `USING`/`WITH CHECK` text.
- **Go**: `backend/internal/projectgrant` (`store.go`, `handler.go`, `delegated.go`, `owners.go`); read by `backend/internal/role/store.go`.

---

## `user_grants`

Direct or delegated role assignments. `backend/migrations/20260908000004_grants_and_manager_roles.up.sql`, rules added by `20260912000023_user_grant_rules.up.sql`, delegation implemented by `20260917000037_delegated_user_grants.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `project_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `project_grant_id` | `uuid` | yes | — (NULL = direct grant; set = delegated) |
| `role_keys` | `text[]` | no | `'{}'` |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |

- **Constraints**: `user_grants_role_keys_not_empty` (`cardinality(role_keys) > 0` — a grant with no roles should not exist rather than sit as a confusing empty row).
- **Indexes**: `user_grants_user_project_key` UNIQUE on `(user_id, project_id)`; `user_grants_user_idx (user_id)`; `user_grants_project_idx (project_id)`; `user_grants_org_idx (org_id)`; `user_grants_project_grant_idx (project_grant_id)` WHERE NOT NULL; `user_grants_role_keys_idx` GIN on `role_keys` (`022`).
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE; `project_id → projects(id)` ON DELETE RESTRICT; `org_id → organizations(id)` ON DELETE RESTRICT; `project_grant_id → project_grants(id)` ON DELETE CASCADE.
- **Trigger**: `user_grants_set_updated_at`; `user_grants_org_matches_project` → `org_must_match_project()` (skips rows carrying `project_grant_id`, `037`); `user_grants_roles_exist` → `user_grants_roles_must_exist()` (skips delegated rows — the granting project's roles are invisible under the receiving tenant, `037`); `user_grants_delegation_closed` → `user_grants_delegation_is_not_yet_implemented()` — despite the name, this is the **live** delegation-subset-validation trigger since `037` (the name was kept from the `P2-03` placeholder deliberately, so the call site never has a moment of absence — see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Row-level security**: `user_grants_tenant_isolation` (`org_id = current_org_id()`). For a delegated row, `org_id` is the **receiving** organization — the row is that organization's own statement about its own user, so it is invisible to the granting organization under this policy. A granting-side read policy is designed but not built (`P4-04`; see Not Yet Built below).
- **Go**: `backend/internal/grant` (`store.go`, `handler.go`, `claims.go`) is the direct-grant path; `backend/internal/projectgrant/delegated.go` is the delegated-grant path; read by `backend/internal/authz/handler.go`, `backend/internal/role/store.go`.

---

## `manager_roles`

Administrative roles over the Auth Service itself (not application roles). `backend/migrations/20260908000004_grants_and_manager_roles.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `role` | `text` | no | — |
| `scope_id` | `uuid` | no | — (an instance, organization, project, or project-grant id, depending on `role`) |
| `created_at` | `timestamptz` | no | `now()` |

- **Constraints**: `manager_roles_valid` (`role IN ('INSTANCE_OWNER','ORG_OWNER','ORG_ADMIN','PROJECT_OWNER','PROJECT_GRANT_OWNER')`).
- **Indexes**: `manager_roles_user_role_scope_key` UNIQUE on `(user_id, role, scope_id)`; `manager_roles_user_idx (user_id)`; `manager_roles_scope_idx (scope_id, role)`.
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE. `scope_id` has **no** foreign key — it names different tables depending on `role`.
- **Trigger**: none.
- **Row-level security**: **none** — a deliberate, recorded gap. See [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md) for the full explanation and its status.
- **Go**: `backend/internal/management/store.go` reads it during permission resolution; `backend/internal/grant/claims.go` reads it for token claims; `backend/internal/projectgrant/owners.go` is the **only** write path in the codebase, and it writes only `PROJECT_GRANT_OWNER` rows (`P4-03`, the first and so-far only manager-role write API — see `TASKS/BACKLOG.md` PG-31). `INSTANCE_OWNER`, `ORG_OWNER` and `ORG_ADMIN` rows exist only through direct `INSERT` (bootstrap/seed scripts); no endpoint grants or revokes them yet.

---

## `sessions`

`backend/migrations/20260908000005_sessions_and_tokens.up.sql`, extended by `20260909000011_session_token_hash.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `auth_methods` | `text[]` | no | `'{}'` |
| `ip` | `inet` | yes | — |
| `user_agent` | `text` | yes | — |
| `created_at` | `timestamptz` | no | `now()` |
| `last_seen_at` | `timestamptz` | no | `now()` |
| `expires_at` | `timestamptz` | no | — |
| `revoked_at` | `timestamptz` | yes | — |
| `token_hash` | `text` | yes | — (added `011`; the cookie carries a fresh 256-bit token, this column holds only `sha256(token)` — `id` is **not** the credential) |

- **Constraints**: `sessions_expires_after_creation` (`expires_at > created_at`).
- **Indexes**: `sessions_user_idx (user_id, created_at DESC)`; `sessions_org_idx (org_id)`; `sessions_active_expiry_idx (expires_at)` WHERE `revoked_at IS NULL`; `sessions_token_hash_key` UNIQUE on `token_hash` WHERE NOT NULL (`011`).
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE; `org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: none directly (`updated_at` does not exist on this table; `last_seen_at` and `revoked_at` are written explicitly by the application).
- **Row-level security**: `sessions_tenant_isolation` (`org_id = current_org_id()`). The pre-tenant cookie lookup uses `session_by_token_hash()`, a `SECURITY DEFINER` function (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: `backend/internal/session` (`store.go` writer, `manager.go`, `owned.go`); also read by `backend/internal/mfaapi/handler.go`, `backend/internal/oauth/userinfo/store.go`, `backend/internal/anomaly/postgres.go`.

---

## `refresh_tokens`

`backend/migrations/20260908000005_sessions_and_tokens.up.sql`, extended by `20260909000014_refresh_token_scope.up.sql`, reuse detection added by `20260913000032_refresh_reuse_detection.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `client_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `session_id` | `uuid` | yes | — |
| `family_id` | `uuid` | no | — |
| `replaced_by` | `uuid` | yes | — (self-referencing) |
| `token_hash` | `text` | no | — |
| `expires_at` | `timestamptz` | no | — |
| `family_expires_at` | `timestamptz` | no | — |
| `revoked` | `boolean` | no | `false` |
| `created_at` | `timestamptz` | no | `now()` |
| `scope` | `text[]` | no | `'{}'` (added `014`) |

- **Constraints**: `refresh_tokens_hash_not_blank`; `refresh_tokens_family_outlives_token` (`family_expires_at >= expires_at`).
- **Indexes**: `refresh_tokens_hash_key` UNIQUE on `token_hash`; `refresh_tokens_family_idx (family_id)`; `refresh_tokens_user_idx (user_id)`; `refresh_tokens_session_idx (session_id)` WHERE NOT NULL.
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE; `client_id → applications(id)` ON DELETE CASCADE; `org_id → organizations(id)` ON DELETE RESTRICT; `session_id → sessions(id)` ON DELETE SET NULL; `replaced_by → refresh_tokens(id)` ON DELETE SET NULL.
- **Trigger**: none.
- **Row-level security**: `refresh_tokens_tenant_isolation` (`org_id = current_org_id()`). Pre-tenant lookups use two `SECURITY DEFINER` functions: `refresh_token_by_hash()` (returns only live tokens) and `refresh_token_lineage()` (`032`, returns dead tokens too, for reuse detection — see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: `backend/internal/oauth/token` (`refresh.go` writer, `rotation.go` reader/writer for rotation and reuse detection — a named exception in `TestOnlyNamedPlacesBypassTheTenantScope`).

---

## `signing_keys`

`backend/migrations/20260908000005_sessions_and_tokens.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `kid` | `text` | no | — |
| `purpose` | `text` | no | `'oidc'` |
| `algorithm` | `text` | no | — |
| `public_key` | `text` | no | — |
| `private_key_ref` | `text` | no | — (a secret-manager reference, never key material) |
| `status` | `text` | no | `'next'` |
| `created_at` | `timestamptz` | no | `now()` |
| `activated_at` | `timestamptz` | yes | — |
| `retired_at` | `timestamptz` | yes | — |

- **Constraints**: `signing_keys_purpose_valid` (`purpose IN ('oidc','saml')`); `signing_keys_status_valid` (`status IN ('next','current','previous','retired')`); `signing_keys_algorithm_valid` (`algorithm IN ('RS256','ES256')` — asymmetric only, never `HS256`); `signing_keys_kid_not_blank`; `signing_keys_private_key_is_a_reference` (`private_key_ref NOT LIKE '%BEGIN%PRIVATE KEY%'` — refuses anything that looks like PEM key material).
- **Indexes**: `signing_keys_kid_key` UNIQUE on `kid`; `signing_keys_one_current_per_purpose` UNIQUE on `purpose` WHERE `status = 'current'`; `signing_keys_status_idx (purpose, status)`.
- **Foreign keys**: none.
- **Trigger**: none.
- **Row-level security**: none — instance-wide by design; every organization's tokens are signed by the same key set (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: `backend/internal/signing/store.go`; rotation driven by `backend/cmd/keyctl`.

---

## `user_tokens`

Single-use invite / password-reset / email-verification / "this wasn't me" tokens. `backend/migrations/20260908000005_sessions_and_tokens.up.sql`, purpose widened by `20260913000033_report_not_me_token.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `purpose` | `text` | no | — |
| `token_hash` | `text` | no | — |
| `expires_at` | `timestamptz` | no | — |
| `used_at` | `timestamptz` | yes | — |
| `created_at` | `timestamptz` | no | `now()` |

- **Constraints**: `user_tokens_purpose_valid` (`purpose IN ('invite','password_reset','email_verification','report_not_me')` — the fourth value added `033`); `user_tokens_hash_not_blank`; `user_tokens_expires_after_creation`.
- **Indexes**: `user_tokens_hash_key` UNIQUE on `token_hash`; `user_tokens_user_purpose_idx (user_id, purpose)`; `user_tokens_expiry_idx (expires_at)` WHERE `used_at IS NULL`.
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE; `org_id → organizations(id)` ON DELETE RESTRICT.
- **Trigger**: none.
- **Row-level security**: `user_tokens_tenant_isolation` (`org_id = current_org_id()`). Pre-tenant lookup is `user_token_by_hash()`, `SECURITY DEFINER`, read-only — consumption (`used_at`) happens as a separate tenant-scoped `UPDATE` (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: `backend/internal/user/tokens.go` (invite/reset/verification); `backend/internal/login/notme.go` (the `report_not_me` flow, `P3-08`).

---

## `events`

Append-only, month-partitioned audit log. Full detail in [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md). `backend/migrations/20260908000006_events_audit_log.up.sql`, `org_id` made nullable by `20260908000008_audit_instance_events_and_partitions.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `bigint GENERATED ALWAYS AS IDENTITY` | no | — |
| `org_id` | `uuid` | yes (since `008`) | — |
| `actor_user_id` | `uuid` | yes | — |
| `event_type` | `text` | no | — |
| `payload` | `jsonb` | no | `'{}'` |
| `ip` | `inet` | yes | — |
| `request_id` | `text` | yes | — |
| `created_at` | `timestamptz` | no | `now()` |

- **Primary key**: `(id, created_at)` — the partition key must be part of the primary key on a partitioned table.
- **Constraints**: `events_type_not_blank`; `events_payload_object`.
- **Indexes**: `events_org_created_at_idx (org_id, created_at DESC)`; `events_type_idx (event_type, created_at DESC)`; `events_actor_idx (actor_user_id, created_at DESC)` WHERE NOT NULL; `events_instance_level_idx (created_at DESC)` WHERE `org_id IS NULL` (`008`).
- **Foreign keys**: **none, deliberately**, on `org_id` or `actor_user_id` — so the audit trail can outlive the rows it refers to (pseudonymization design intent; see [`05`](./05-AUDIT-TRAIL-STORAGE.md)).
- **Partitioning**: `PARTITION BY RANGE (created_at)`, monthly partitions named `events_YYYY_MM`, created and privilege-hardened by `ensure_events_partition()` / `ensure_events_partitions_ahead()` (both `SECURITY DEFINER`).
- **Row-level security**: `events_read` / `events_insert` (org-scoped **or** both-NULL for instance-level rows). No policy governs `UPDATE`/`DELETE` because the privilege itself is revoked from `auth_app` — see [`05`](./05-AUDIT-TRAIL-STORAGE.md) for the append-only enforcement and its staging drift incident.
- **Go**: `backend/internal/audit/audit.go` is the sole writer; `backend/internal/auditlog/handler.go` is the Management API reader.

---

## `idempotency_records`

Replay protection for `POST /v1/...` with an `Idempotency-Key`. `backend/migrations/20260910000015_idempotency_records.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `org_id` | `uuid` | no (part of PK) | — |
| `client_id` | `uuid` | no (part of PK) | — |
| `key` | `text` | no (part of PK) | — |
| `request_hash` | `text` | no | — |
| `method` | `text` | no | — |
| `path` | `text` | no | — |
| `status` | `integer` | yes | — |
| `response` | `text` | yes | — (raw response bytes, not `jsonb` — a replay must return the exact bytes the first request received) |
| `created_at` | `timestamptz` | no | `now()` |
| `expires_at` | `timestamptz` | no | — |

- **Primary key**: `(org_id, client_id, key)` — the pair is the identity, so one client cannot read another's stored response by guessing a key.
- **Constraints**: `idempotency_expires_after_creation`; `idempotency_response_complete` (`status` and `response` are set together or not at all).
- **Indexes**: `idempotency_expires_idx (expires_at)`.
- **Foreign keys**: `org_id → organizations(id)` ON DELETE CASCADE; `client_id → applications(id)` ON DELETE CASCADE.
- **Trigger**: none.
- **Row-level security**: `idempotency_tenant_isolation`, and this table additionally has `FORCE ROW LEVEL SECURITY` — the only table in the schema with `FORCE` — so the policy applies even to the table owner.
- **Go**: `backend/internal/management/idempotency.go`.

---

## `user_mfa_factors`

Enrolled TOTP and WebAuthn factors. Created as `user_factors` by `backend/migrations/20260912000027_user_factors.up.sql`; renamed and reshaped to match `docs/PLAN/04` by `20260912000028_align_factors_with_plan.up.sql`; `credential_id` uniqueness added by `20260913000031_webauthn_credential_unique.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `type` | `text` | no | — |
| `label` | `text` | yes | — |
| `secret_encrypted` | `bytea` | yes | — (renamed from `secret` in `028`; NULL for WebAuthn, whose public key is not a secret) |
| `data` | `jsonb` | no | `'{}'` |
| `last_used_at` | `timestamptz` | yes | — |
| `created_at` | `timestamptz` | no | `now()` |
| `updated_at` | `timestamptz` | no | `now()` |
| `status` | `text` | no | `'pending'` (replaced `confirmed_at` in `028`) |
| `last_used_counter` | `bigint` | yes | — (TOTP step counter, monotonic — prevents replay of a code within its 30-second window) |
| `credential_id` | `text` | yes | — (WebAuthn) |
| `public_key` | `text` | yes | — (WebAuthn) |
| `sign_count` | `bigint` | yes | — (WebAuthn) |

- **Constraints**: `user_mfa_factors_type_valid` (`type IN ('totp','webauthn')`); `user_mfa_factors_label_bounded` (`length(label) <= 64`); `user_mfa_factors_secret_not_empty`; `user_mfa_factors_data_is_object`; `user_mfa_factors_status_valid` (`status IN ('pending','active')`).
- **Indexes**: `user_mfa_factors_user_idx (user_id, status)`; `user_mfa_factors_org_idx (org_id)`; `user_mfa_factors_one_totp` UNIQUE on `user_id` WHERE `type = 'totp'` (one TOTP secret per user; several WebAuthn credentials are allowed); `user_mfa_factors_credential_unique` UNIQUE on `credential_id` WHERE NOT NULL (`031` — one WebAuthn credential belongs to one account, `docs/SECURITY/02` §2).
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE; `org_id → organizations(id)` ON DELETE CASCADE (note: `CASCADE`, not `RESTRICT` — distinct from most `org_id` foreign keys in this schema).
- **Trigger**: `user_mfa_factors_org_matches_user` (a factor's `org_id` must equal its user's `org_id`); `user_mfa_factors_sync_flag` AFTER INSERT/UPDATE/DELETE → `sync_user_mfa_enabled()`, which keeps `users.mfa_enabled` equal to "this user has at least one `active` factor".
- **Row-level security**: `user_mfa_factors_tenant_isolation` (`org_id = current_org_id()`). Pre-tenant lookup by factor id is `mfa_factor_org()`, `SECURITY DEFINER`, returning only the org id (see [`02`](./02-ROW-LEVEL-SECURITY-POLICIES.md)).
- **Go**: `backend/internal/mfa` (`store.go`, `webauthnstore.go`); `backend/internal/mfaapi/handler.go`; read by `backend/internal/authn/mandatecheck.go`, `backend/internal/organization/mfaimpact.go`.

---

## `user_recovery_codes`

Single-use MFA recovery codes. `backend/migrations/20260913000030_user_recovery_codes.up.sql`.

| Column | Type | Nullable | Default |
|---|---|---|---|
| `id` | `uuid` | no (PK) | `gen_random_uuid()` |
| `user_id` | `uuid` | no | — |
| `org_id` | `uuid` | no | — |
| `code_hash` | `bytea` | no | — (SHA-256 of an 80-bit code — not Argon2; see `TASKS/BACKLOG.md` PG-39 for the recorded reasoning) |
| `used_at` | `timestamptz` | yes | — |
| `batch_id` | `uuid` | no | — |
| `created_at` | `timestamptz` | no | `now()` |

- **Constraints**: `user_recovery_codes_hash_length` (`length(code_hash) = 32`).
- **Indexes**: `user_recovery_codes_user_idx (user_id, used_at)`; `user_recovery_codes_org_idx (org_id)`; `user_recovery_codes_unique` UNIQUE on `(user_id, code_hash)`.
- **Foreign keys**: `user_id → users(id)` ON DELETE CASCADE; `org_id → organizations(id)` ON DELETE CASCADE.
- **Trigger**: `user_recovery_codes_org_matches_user`.
- **Row-level security**: `user_recovery_codes_tenant_isolation` (`org_id = current_org_id()`).
- **Go**: `backend/internal/mfa/recoverystore.go`.

## Verification

- `backend/internal/storage/postgres/schema_integration_test.go` and `rls_integration_test.go` exercise the schema and constraints described above against a real PostgreSQL instance (`//go:build integration`).
- `TestNoRowCanReferenceAnotherOrganizationsProject` and `TestASecondOrganizationNeedsNoMigration` (`backend/tests/security/tenancy_test.go`) exercise the `org_id`/`project_id` agreement triggers and foreign keys listed above.
- CI applies every migration and round-trips `down`/`up` (`.github/workflows/ci.yml`, "Apply migrations" / "Migrations roll back and re-apply cleanly").

## Not Yet Built / Open Questions

- The granting organization cannot yet read delegated `user_grants` rows through a dedicated policy (`P4-04`); it currently sees only the aggregate holder count via `project_grant_holder_counts()`.
- No API writes `manager_roles` rows of type `INSTANCE_OWNER`, `ORG_OWNER` or `ORG_ADMIN` (`TASKS/BACKLOG.md` PG-31); only `PROJECT_GRANT_OWNER` has a write path (`P4-03`).

## Related Documents

- [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md), [`02-ROW-LEVEL-SECURITY-POLICIES.md`](./02-ROW-LEVEL-SECURITY-POLICIES.md), [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md), [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md), [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md)
- `docs/MULTI-TENANCY/01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`
- `docs/PLAN/04-DATA-MODEL.md` (design intent), `TASKS/BACKLOG.md` (PG-23, PG-31, PG-39), `MEMORY/specs/P4-01-project-grants.md`, `MEMORY/specs/P4-02-delegated-user-grants.md`, `MEMORY/specs/P4-03-project-grant-owner.md`

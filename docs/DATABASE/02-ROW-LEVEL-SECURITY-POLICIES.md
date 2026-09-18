# 02 - Row-Level Security Policies

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-08, P1-06, P1-07, P1-11, P1-15, P1-16, P1-19, P1-29, P2-01, P2-03, P2-08, P2-13, P2-14, P3-01…P3-06, P4-01, P4-02, P4-05, DV-02 (open) &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

List every row-level security (RLS) policy in the schema with its exact `USING`/`WITH CHECK` expression, explain `current_org_id()` and the owner-vs-application role split that makes RLS meaningful, document the one deliberately unprotected table (`manager_roles`), and catalogue the triggers and `SECURITY DEFINER` functions that carry the parts of tenant isolation RLS cannot express on its own.

## Scope

Source: every `backend/migrations/*.up.sql` file that touches `ENABLE ROW LEVEL SECURITY`, `CREATE POLICY`, `CREATE TRIGGER` or `SECURITY DEFINER`, read in order so that a later redefinition is recorded as the current behaviour. This document does not repeat column/index/foreign-key detail — see [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md) — and does not repeat the application-side `WithTenant`/`WithInstanceScope` mechanism in depth — see [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md). The migration/CI mechanics that keep this surface honest over time (expand/contract, the destructive-migration gate) are in [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md). The tenancy-guarantee narrative and its own verification suite are in `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`; this document is the source of the exact SQL that narrative points back to.

## As Built

### `current_org_id()`

```sql
CREATE OR REPLACE FUNCTION current_org_id()
RETURNS uuid AS $$
  SELECT NULLIF(current_setting('app.current_org_id', true), '')::uuid;
$$ LANGUAGE sql STABLE;
```

(`backend/migrations/20260908000007_row_level_security.up.sql`)

Every policy compares against this function rather than inlining `current_setting` in each one, so the `NULLIF` guard is written once. Two properties follow:

- **Fail-closed.** `current_setting(..., true)` returns `NULL` when `app.current_org_id` is unset. `org_id = NULL` evaluates to `NULL`, which Postgres treats as not-true, so a query with no tenant context set returns **zero rows** rather than every row.
- **Instance scope means "no tenant", not "every tenant".** `WithInstanceScope` sets the tenant to the empty string, which `NULLIF` turns into `NULL`. Every ordinary tenant policy then evaluates false for every row — instance-scoped code cannot read a policy-protected table by accident; it has to query something not under RLS, or run as the owner.

The application sets `app.current_org_id` with `SET LOCAL` inside `postgres.DB.withScope` (see [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md)) — transaction-scoped, so a pooled connection cannot leak one request's tenant into the next.

### Owner versus application role

RLS policies apply to `auth_app` (the runtime role) but **not** to `auth_owner` (the migration/schema-owner role), because none of the tables are created with `FORCE ROW LEVEL SECURITY` — with one exception, `idempotency_records` (see below). This is deliberate, not an oversight: `FORCE ROW LEVEL SECURITY` would apply the policies to the table owner too, and the owner runs migrations and administrative queries that legitimately span tenants. Forcing it would break the migration tool.

Safety instead comes from the application never connecting as the owner: `auth_app` is `NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS NOINHERIT` and owns no tables (`deploy/postgres/init/01-roles.sh`), verified at boot by `DB.AssertRoleIsNotPrivileged` and mechanically by the CI job "Every tenant-scoped table has row-level security policy" (`.github/workflows/ci.yml`). See [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md) for the full role-split rationale.

### Every policy, verbatim

Unless noted, `USING` and `WITH CHECK` are identical.

| Table | Policy | `USING` | `WITH CHECK` | Migration |
|---|---|---|---|---|
| `organizations` | `organizations_tenant_isolation` | `id = current_org_id()` | same | `007` |
| `users` | `users_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `projects` | `projects_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `applications` | `applications_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `roles` | `roles_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `user_grants` | `user_grants_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `sessions` | `sessions_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `refresh_tokens` | `refresh_tokens_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `user_tokens` | `user_tokens_tenant_isolation` | `org_id = current_org_id()` | same | `007` |
| `events` | `events_read` (SELECT) | `org_id = current_org_id() OR (org_id IS NULL AND current_org_id() IS NULL)` | — | `008` (replaces `007`'s `events_tenant_read`) |
| `events` | `events_insert` (INSERT) | — | `org_id = current_org_id() OR (org_id IS NULL AND current_org_id() IS NULL)` | `008` (replaces `007`'s `events_tenant_insert`) |
| `project_grants` | `project_grants_tenant_isolation` | `granting_org_id = current_org_id() OR granted_org_id = current_org_id()` | `granting_org_id = current_org_id()` | `007` |
| `idempotency_records` | `idempotency_tenant_isolation` | `org_id = current_org_id()` | same | `015`. Table also has `FORCE ROW LEVEL SECURITY` |
| `user_mfa_factors` | `user_mfa_factors_tenant_isolation` (created as `user_factors_tenant_isolation`) | `org_id = current_org_id()` | same | `027`, renamed `028` |
| `user_recovery_codes` | `user_recovery_codes_tenant_isolation` | `org_id = current_org_id()` | same | `030` |

**`organizations` is keyed on `id`, not `org_id`** — an organization row *is* the tenant it protects. This makes two operations impossible under any ordinary scope: `LIST` (which must span organizations) and `CREATE` (which happens before the row, and therefore its tenant, exists). Both are answered by `SECURITY DEFINER` functions instead of a relaxed policy — see below.

**`events` has no policy at all governing `UPDATE`/`DELETE`.** Those are not filtered by RLS; the privilege itself is revoked from `auth_app`. See [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md) for the append-only mechanism and a staging incident where that privilege drifted.

### The two-sided `project_grants` policy

```sql
CREATE POLICY project_grants_tenant_isolation ON project_grants
    USING (
        granting_org_id = current_org_id()
        OR granted_org_id = current_org_id()
    )
    WITH CHECK (granting_org_id = current_org_id());
```

(`backend/migrations/20260908000007_row_level_security.up.sql`)

`project_grants` is the one table visible to two tenants by design: a Project Grant is the delegation contract between a granting and a receiving organization (`docs/PLAN/08` Part C), and each side must see it — the granting organization to manage and revoke it, the receiving organization to know which roles it may assign. The `USING` clause reflects that: either organization ID matching the caller's tenant makes the row visible.

The `WITH CHECK` clause is asymmetric: only the **granting** side may write. If the receiving organization could `INSERT` or `UPDATE` this table, it could widen its own delegation — the privilege escalation `docs/PLAN/09` § "Delegation abuse" names. Visibility and authority are different questions here: RLS answers "what can be seen", and it deliberately does **not** answer "what roles may be claimed through it" — that is a same-request comparison against `granted_role_keys`, enforced by the `user_grants` delegation trigger below, not by row visibility.

### `manager_roles` has no tenant RLS — the deliberate gap

From `backend/migrations/20260908000007_row_level_security.up.sql`, verbatim:

> `manager_roles` — This one is a real gap and is recorded as such, not overlooked.
>
> It has no `org_id`, only `user_id` and `scope_id`, and it is read **during permission resolution** — that is, before a tenant context exists, since what the caller may access is exactly what is being determined. An org-scoped policy would make the table unreadable at the only moment it is needed.
>
> The correct policy keys on the current **USER**, not the current org, which needs an `app.current_user_id` setting that does not exist until the bearer-authentication middleware lands in `P1-15`/`P2-05`. Adding a half-working policy now would give the appearance of isolation without the substance.
>
> Until then `manager_roles` is protected by the application's own scoping and by the fact that it holds no personal data — only `(user, role, scope)` triples. Tracked as `DV-02` in `TASKS/BACKLOG.md` so it cannot quietly persist past `P2-05`.

**Current status — a contradiction worth flagging.** `TASKS/BACKLOG.md` DV-02 and `TASKS/PROGRESS.md` both state "P2-05 cannot be marked done while this is open." `TASKS/PROGRESS.md` also marks `P2-05` **DONE**. No later migration adds a policy to `manager_roles` (the table is untouched after `004` except for being read by `organizations_administered_by()`, `035`/`037`'s triggers, etc.), and no `app.current_user_id` session setting exists anywhere in `backend/internal/storage/postgres`. So the gap the plan says should have closed at `P2-05` is, as of this codebase, still open. The CI job "Every tenant-scoped table has row-level security policy" (`.github/workflows/ci.yml`) does not flag `manager_roles` because the table carries no `org_id`/`granting_org_id` column, which is exactly the shape the gap's own reasoning describes — the table cannot be tenant-scoped by column at all. This is recorded here as an unresolved discrepancy between `TASKS/PROGRESS.md`'s "done" mark and the schema; see "Not Yet Built / Open Questions" below.

## Triggers and the Rule Each One Enforces

`set_updated_at()` (baseline, `001`) is omitted below — it only maintains `updated_at` and carries no security rule. All others:

| Trigger | On | Function | Rule enforced | Introduced / redefined |
|---|---|---|---|---|
| `roles_builtin_immutable` | `roles` | `roles_builtin_is_immutable()` | A built-in role cannot be deleted, re-keyed, or have `is_builtin` cleared. `display_name` stays editable. | `022` |
| `roles_org_matches_project` | `roles` | originally `roles_org_must_match_project()`; unified onto `org_must_match_project()` | `roles.org_id` must equal the organization owning `roles.project_id`. Runs as the caller under RLS, so a project in another tenant is simply "not found" rather than compared and refused — the refusal follows from invisibility, not from a value comparison. | `022`, unified `024` |
| `user_grants_org_matches_project` | `user_grants` | originally `user_grants_org_must_match_project()`; unified onto `org_must_match_project()`; skips delegated rows | Same rule as above, for `user_grants`. Since `037`, a row carrying `project_grant_id` is exempted — the delegation trigger (below) enforces the stricter, cross-organization version of this check instead. | `023`, unified `024`, exempted `037` |
| `applications_org_matches_project` | `applications` | `org_must_match_project()` | Same rule, for `applications` — writable since Phase 0 with nothing checking this until `024`. | `024` |
| `project_grants_org_matches_project` | `project_grants` | `org_must_match_project()` (reads `granting_org_id`) | The granting organization named on a grant must actually own the project being delegated. | `024` |
| `user_grants_delegation_closed` | `user_grants` | `user_grants_delegation_is_not_yet_implemented()` | **The name is historical; the body is live since `037`.** `P2-03` created this trigger to refuse any non-`NULL` `project_grant_id` outright (the "designed slot" for delegation), and named it for the placeholder rather than the feature so that filling it in `P4-02` would not require dropping and recreating the trigger — a constraint dropped even briefly is a window for the invariant to lapse. Since `037` it: refuses changing `project_grant_id` on `UPDATE` in either direction; passes direct rows (`project_grant_id IS NULL`) through untouched; and for a delegated row, locks the referenced `project_grants` row `FOR SHARE` and refuses unless **all** of: the grant is visible under the caller's RLS, its `status` is `'active'`, its `granted_org_id` equals `NEW.org_id`, its `project_id` equals `NEW.project_id`, every key in `NEW.role_keys` is in `granted_role_keys` (the unmatched key is named in the error), and `NEW.user_id` belongs to `NEW.org_id`. Fires on `INSERT` and on `UPDATE OF project_grant_id, role_keys, org_id, project_id, user_id` — widened from `project_grant_id` alone in `037`, because until then an `UPDATE` of `role_keys` on a delegated row was not re-checked at all. | Placeholder `023`; implemented `037` |
| `user_grants_roles_exist` | `user_grants` | `user_grants_roles_must_exist()` | Every key in `role_keys` must name a role that exists in `project_id`. Skips delegated rows since `037` — the granting project's roles are invisible under the receiving tenant, and the grant's keys were already validated against those roles when the grant itself was created (`P4-01`). | `023`, exempted `037` |
| `project_grants_only_narrow` | `project_grants` | `project_grants_only_narrow()` | A grant's `project_id` and both organization IDs can never change; `granted_role_keys` can never change in either direction (narrowing in place would make the audit trail's "roles delegated at creation" no longer true of the row); a `revoked` grant can never become anything else. Refused **for every writer, including the owner connection** — there is no `UPDATE` path for a grant at all, by design. | `035` |
| `user_mfa_factors_org_matches_user` | `user_mfa_factors` | same name (recreated on rename) | A factor's `org_id` must equal its user's `org_id`. | `027`, recreated `028` |
| `user_mfa_factors_sync_flag` | `user_mfa_factors` (AFTER INSERT/UPDATE/DELETE) | `sync_user_mfa_enabled()` | Keeps `users.mfa_enabled` equal to "this user has at least one `active` factor" — a denormalized flag maintained by the database rather than by whichever application code path happens to remember. | `028` |
| `user_recovery_codes_org_matches_user` | `user_recovery_codes` | same name | A recovery code's `org_id` must equal its user's `org_id`. | `030` |

### The P4-02 delegation trigger, in context

The trigger most relevant to cross-organization delegation is `user_grants_delegation_closed`, described in full above. Its design is deliberately staged across three migrations rather than built once:

1. **`023` (`P2-03`)** closes the slot before delegation exists: any non-`NULL` `project_grant_id` is refused outright, with an error naming `P4-01` as the future implementer.
2. **`037` (`P4-02`)** replaces the function body with the real subset/active/grantee/project checks, and widens the trigger's column list so `role_keys` on an already-delegated row cannot be silently broadened by an `UPDATE`.
3. Two related triggers (`org_must_match_project`, `user_grants_roles_must_exist`) are taught in the same migration to **exempt** delegated rows, because the delegation trigger's checks are strictly stronger for that case — a delegated row's `org_id` is deliberately **not** the project's owning organization (it is the receiving organization), so the generic "org owns project" check would be wrong for it.

### `org_must_match_project()` — the generalized function

`022` and `023` each grew a near-identical trigger function for the table they were adding. `024` replaced both with one shared function, reading `NEW` as JSON so it can serve every table whose relevant columns are named `org_id`/`project_id` (or, for `project_grants`, `granting_org_id`/`project_id`):

```sql
CREATE OR REPLACE FUNCTION org_must_match_project()
RETURNS trigger AS $$
DECLARE
    row_json    jsonb := to_jsonb(NEW);
    row_org     uuid  := coalesce(row_json ->> 'org_id', row_json ->> 'granting_org_id')::uuid;
    row_project uuid  := (row_json ->> 'project_id')::uuid;
    owning_org  uuid;
BEGIN
    IF row_project IS NULL THEN RETURN NEW; END IF;
    -- (037 adds:) IF TG_TABLE_NAME = 'user_grants' AND NEW.project_grant_id IS NOT NULL THEN RETURN NEW; END IF;
    IF row_org IS NULL THEN
        RAISE EXCEPTION '% carries project_id but no org_id or granting_org_id; ...', TG_TABLE_NAME;
    END IF;
    SELECT org_id INTO owning_org FROM projects WHERE id = row_project;
    IF owning_org IS NULL THEN
        RAISE EXCEPTION '% references project % which is not visible in this tenant', TG_TABLE_NAME, row_project;
    END IF;
    IF owning_org <> row_org THEN
        RAISE EXCEPTION '% org_id % does not match project %''s organization %', TG_TABLE_NAME, row_org, row_project, owning_org;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

It is attached to `roles`, `user_grants`, `applications` and `project_grants`. It deliberately runs **without** `SECURITY DEFINER`: the `SELECT ... FROM projects` lookup executes as the caller, under that caller's own RLS, so a project belonging to another organization is simply not found — the refusal follows from ordinary tenant isolation rather than from comparing two values the function was handed.

## `SECURITY DEFINER` Functions and the Bound Each One Respects

Every function below runs `SET search_path = public, pg_temp` (or `pg_catalog, public`), which closes the classic `SECURITY DEFINER` hazard: without pinning `search_path`, a caller who can influence it could point an unqualified name at their own object and have it execute with the owner's privileges. Every one is `REVOKE ALL ... FROM PUBLIC` then `GRANT EXECUTE ... TO auth_app` only.

| Function | Purpose | The bound it is written to respect | Migration |
|---|---|---|---|
| `ensure_events_partition(target date)` | Creates the month partition covering `target`, if missing | The only caller-supplied value is a date; the statement is always `CREATE TABLE ... PARTITION OF events`, never an arbitrary table. Serializes via `pg_advisory_xact_lock` so concurrent instances don't race the existence check. | `009` (supersedes non-definer `006`) |
| `ensure_events_partitions_ahead(months_ahead int DEFAULT 3)` | Creates the current partition through `months_ahead` out | Bounds `months_ahead` to `0..24`, refusing anything else. | `009` |
| `session_by_token_hash(hash text, at timestamptz)` | Resolves a session cookie to its tenant, before one is known | Authorization is possession: the argument is a SHA-256 of a 256-bit secret, and a miss returns no rows. Liveness (`revoked_at IS NULL AND expires_at > at`) is filtered **inside** the function so a dead session can never be returned and later acted on by a caller that forgot to check. | `012` |
| `sweep_expired_sessions(before timestamptz, max_rows int)` | Deletes sessions that are already expired or long-revoked | Instance-wide maintenance with no tenant to scope to. Reaches only rows already unusable (`expires_at < before` or `revoked_at < before`); a live session is not reachable through it at all. Bounded by `max_rows`. | `012` |
| `application_by_client_id(client uuid)` | Resolves an OIDC `client_id` to its application, before a tenant is known | `client_id` is public by construction (it travels in every authorization URL). The function returns **no secret columns** — `client_secret_hash` is absent from its result at any privilege level — extended in `021` to also return `allowed_origins`, itself no more secret than `redirect_uris`. | `013`, redefined `021` |
| `origin_is_registered(origin text)` | Answers whether *any* application registered a browser origin, for CORS preflight | Preflight requests arrive with no credential at all (no `Authorization` header, by the CORS specification), so there is no per-application check to make yet. Discloses one bit about a value the asker already supplies. The actual cross-origin read is still checked against the specific application's own `allowed_origins` afterward. | `021` |
| `refresh_token_by_hash(hash text, at timestamptz)` | Resolves a refresh token to its tenant, before one is known | Same possession argument as the session lookup. Returns only tokens that are not revoked and not expired (token or family). | `014` |
| `session_live(session uuid, at timestamptz)` | Whether a session id is still usable | Returns liveness and nothing else — no columns, only rows. Used by the refresh grant to check the session that authorized a refresh token, before any tenant is known. | `014` |
| `user_token_by_hash(hash text, at timestamptz)` | Resolves an invite/reset/verification token to its tenant | Possession argument again. **Read-only, deliberately** — consuming the token (`used_at`) happens as a separate tenant-scoped `UPDATE` in the transaction that also writes the password change, so a privileged mutation is not smuggled into the bootstrap function. | `018` |
| `sweep_idempotency_records(before timestamptz, max_rows int)` | Deletes expired idempotency records | Instance-wide maintenance; takes no organization, returns no row contents, and reaches only rows already past `expires_at` — a live replay-protection record is not reachable through it. | `015` |
| `organizations_page(after_created, after_id, max_rows)` | Keyset-paginated list of live organizations | `organizations`' own policy is keyed on `id`, so no ordinary scope can list more than one row. Takes no organization id — nothing a caller could supply reaches a specific tenant through it; only enumerates all of them, for a caller the Go layer has already confirmed holds `INSTANCE_OWNER`. | `016` |
| `organization_create(new_name, new_domain, new_settings)` | Creates an organization | Runs before its own tenant exists, so no scope could apply. Does **not** take an `instance_id` parameter — it looks up the single `instances` row and raises an exception if there is not exactly one, so a future multi-instance deployment fails loudly instead of silently picking one. `new_settings` is deep-merged onto the column default rather than substituted, so a caller supplying only one setting cannot accidentally delete the rest of the default policy. | `016` |
| `organizations_administered_by(who, after_created, after_id, max_rows)` | Lists the organizations one user administers, for the console's organization switcher | Takes a **user id**, not an organization id — there is no argument that reaches an organization the caller does not administer, because the function derives the list from that user's own `manager_roles` rows (unioning in `INSTANCE_OWNER` separately, since that role has no organization-scoped row). `PROJECT_OWNER` is deliberately excluded — its `scope_id` is a project, and including it would offer a switcher entry that leads to a `403` on every organization-level screen. | `025` |
| `jsonb_deep_merge(base, patch)` | Recursive merge of two `jsonb` documents | Not a tenant-boundary function, but load-bearing for a security-relevant defect: a shallow `jsonb ||` merge on organization `settings` PATCH silently discarded sibling policy fields (e.g. raising `min_length` cleared `require_uppercase`) — an administrator's tightening producing an unintended loosening, with nothing reporting it. Objects recurse; **arrays and scalars are replaced wholesale**, which matters because `allowed_login_methods` is a permission *set* and a concatenating merge could never remove a method. | `026` |
| `organization_accepts_grants(candidate uuid)` | Whether an organization id names a live, active organization | Returns a boolean only. Requires already knowing a 122-bit random id — not an enumeration a caller can drive by guessing. | `035` |
| `granted_organization_names(ids uuid[])` | Names of organizations holding a grant from the caller's own organization | Filters to `granting_org_id = current_org_id()` — an id with no such grant returns nothing. Used so a granting administrator revoking a grant can see a name instead of a bare UUID for a tenant otherwise invisible under RLS. | `035` |
| `project_grant_holder_counts(grant_ids uuid[])` | Count of distinct users holding a role through each grant | Filters to `granting_org_id = current_org_id()`. Returns **counts only, never identities** — the number is the blast radius a revoking administrator needs; the people behind it are the receiving organization's business. Returns zero until `P4-02` creates delegated rows, which is the truth rather than a placeholder. | `036` |
| `refresh_token_lineage(hash)` | What happened to a presented refresh token, including if it is already dead | A **second**, narrower function rather than a relaxation of `refresh_token_by_hash` — reuse detection needs to see a dead token, but must not resurrect one for ordinary use. Returns no user, client or scope — only the family id, tenant, revocation state and whether the token's replacement has itself been used, which is exactly what distinguishes a legitimate retry from token theft. | `032` |
| `mfa_factor_org(p_factor_id uuid)` | Resolves an MFA factor id to its organization, before a tenant is known | Unlike a `client_id`, a factor id is **not** public — a caller has no ordinary way to come by one. Returns the organization id and nothing else: not the secret, type, status or user. No reverse direction exists (it takes one id, returns one column), so it cannot be used to enumerate a user's or an organization's factors. | `029` |

## Expand/Contract and the CI Gate

Documented in full in [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md); summarized here because several of the objects above (`org_must_match_project`, `application_by_client_id`, `ensure_events_partition`) were redefined in place across migrations under that rule.

## The Partitioned `events` Table and Append-Only Privileges

Documented in full in [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md), including the staging incident where the append-only privilege drifted (`20260910000019_reassert_events_append_only.up.sql`) and the CI check that now guards it.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Tenant predicate | `org_id = current_org_id()` (or `id = current_org_id()` for `organizations`) | every `*_tenant_isolation` policy, `007`–`030` |
| No tenant set ⇒ zero rows | `current_org_id()` returns `NULL`; `NULL = NULL` is not true | `current_org_id()`, `007` |
| Owner not forced | No table uses `FORCE ROW LEVEL SECURITY` except `idempotency_records` | `007`, `015` |
| `SECURITY DEFINER` search path | Always pinned (`SET search_path = public, pg_temp` or `pg_catalog, public`) | every function listed above |
| `SECURITY DEFINER` execute grant | `REVOKE ALL ... FROM PUBLIC` then `GRANT EXECUTE ... TO auth_app` only | every function listed above |
| `manager_roles` tenant policy | None — open gap, `DV-02` | `007`; see above |

## Interfaces

Not applicable in the endpoint/table sense — this document's "interface" is the SQL surface catalogued above.

## Security Considerations

- **Confused deputy across organizations** (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` — delegation abuse) is the abuse case the two-sided `project_grants` policy and the `user_grants_delegation_closed` trigger jointly close: visibility for both sides, write authority for the granting side only, and subset/active/grantee validation on every write regardless of which code path or connection performs it.
- **A misscoped query** (a forgotten `WHERE org_id = ...`) is the abuse case ordinary RLS closes, verified by `TestUnfilteredQueryCannotSeeAnotherTenant` and `TestCrossTenantReadsAreEmpty` (`backend/tests/security/isolation_test.go`), which deliberately issue unfiltered/direct-id queries.
- **A privileged connection accidentally used at runtime** is the abuse case the owner/app role split and `AssertRoleIsNotPrivileged` close — see [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md).
- **A `SECURITY DEFINER` function as a general-purpose privilege escalation** is the abuse case every function above is written narrowly to avoid — each is documented with the specific caller input it accepts and the specific, minimal output it returns.

## Verification

- `TestUnfilteredQueryCannotSeeAnotherTenant`, `TestIsolationTestsAreNotVacuous`, `TestCrossTenantReadsAreEmpty`, `TestRuntimeRoleCannotBypassRLS`, `TestAuditLogCannotBeAltered` — `backend/tests/security/isolation_test.go`.
- `TestASecondOrganizationNeedsNoMigration`, `TestNoRowCanReferenceAnotherOrganizationsProject`, `TestEveryTenantScopedTableIsInvisibleAcrossOrganizations` — `backend/tests/security/multiorg_test.go`.
- `TestNoRequestInputCanNameTheTenant`, `TestTrustingProxyHeadersDoesNotExtendToTheTenant` — `backend/tests/security/tenant_resolution_test.go`.
- `TestOnlyNamedPlacesBypassTheTenantScope` — `backend/tests/security/tenancy_test.go`.
- `TestRowLevelSecurityDoesNotForceSequentialScansAtScale`, `TestTheTenantPredicateReachesTheIndex` — `backend/tests/security/rls_plans_test.go` (see [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md)).
- Phase 4 delegation abuse cases (`A-1` through `A-9`) — `backend/internal/projectgrant` tests, catalogued by name in `backend/tests/security/isolation_test.go`'s coverage map.
- CI: "Every tenant-scoped table has row-level security policy" (`.github/workflows/ci.yml`).

## Not Yet Built / Open Questions

- **`manager_roles` has no tenant RLS.** Recorded as `DV-02`, and `TASKS/PROGRESS.md` states `P2-05` "cannot be marked done while it is open" — yet `P2-05` is separately marked **DONE** in the same file, and no migration after `007` adds a policy. This is a genuine, unresolved contradiction between the task tracker and the schema, surfaced here rather than silently resolved either way.
- **Granting-side read policy on delegated `user_grants` rows** (T4-3) is designed but deferred to `P4-04`, together with the reader join that would let tokens and `/v1/authz/check` honor delegated roles at all (see `docs/MULTI-TENANCY/01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`).

## Related Documents

- [`00-DATABASE-ARCHITECTURE.md`](./00-DATABASE-ARCHITECTURE.md), [`01-SCHEMA-DEFINITIONS.md`](./01-SCHEMA-DEFINITIONS.md), [`03-INDEXING-AND-QUERY-OPTIMIZATION.md`](./03-INDEXING-AND-QUERY-OPTIMIZATION.md), [`04-MIGRATIONS-STRATEGY.md`](./04-MIGRATIONS-STRATEGY.md), [`05-AUDIT-TRAIL-STORAGE.md`](./05-AUDIT-TRAIL-STORAGE.md)
- `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`, `docs/MULTI-TENANCY/01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`
- `docs/PLAN/08-AUTHORIZATION.md` Part B and Part C, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
- `TASKS/BACKLOG.md` DV-02, PG-31, PG-32, PG-44; `MEMORY/specs/P4-02-delegated-user-grants.md` §6, §14

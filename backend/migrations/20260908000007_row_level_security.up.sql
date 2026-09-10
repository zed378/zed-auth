-- Row-level security: cross-tenant isolation as a database property.
--
-- docs/PLAN/08-AUTHORIZATION.md Part B is explicit about why this exists:
-- "so a misscoped query can't leak data across organizations". The application
-- layer also filters by org_id, and will keep doing so — but application
-- filtering is a code-review outcome, and this is a guarantee. One forgotten
-- WHERE clause in one repository method is all it takes, and that is a bug
-- class RLS removes rather than mitigates.
--
-- HOW THE TENANT CONTEXT IS SET
--
-- Every policy compares against `app.current_org_id`, a session-local setting
-- the application sets with SET LOCAL at the start of each transaction. SET
-- LOCAL is transaction-scoped, which matters more than it looks: with a
-- connection pool, a plain SET would leak one request's tenant into the next
-- request that happened to reuse the connection. That is the exact failure
-- this whole mechanism exists to prevent, so the storage layer only ever sets
-- it inside a transaction (internal/storage/postgres).
--
-- FAIL-CLOSED BY CONSTRUCTION
--
-- current_setting(..., true) returns NULL when unset. `org_id = NULL` is NULL,
-- which is not true, so a query with no tenant context returns ZERO rows
-- rather than every row. The NULLIF guards the other case: an empty string
-- would raise a cast error rather than filtering, and an error is a worse
-- failure mode than an empty result for a health check or a background job.
--
-- WHY THE OWNER IS NOT FORCED
--
-- FORCE ROW LEVEL SECURITY would apply these policies to the table owner too.
-- The owner runs migrations and administrative queries that legitimately span
-- tenants, so forcing it would break the migration tool. Safety comes from the
-- application connecting as auth_app, which owns nothing and has no BYPASSRLS
-- (deploy/postgres/init/01-roles.sh, asserted by an integration test).

-- --- Helper -----------------------------------------------------------------

-- Returns the current tenant, or NULL when none is set.
--
-- A function rather than inlining current_setting in eleven policies: the
-- NULLIF guard has to be identical everywhere, and eleven copies of a subtle
-- expression is eleven chances for one of them to drift.
CREATE OR REPLACE FUNCTION current_org_id()
RETURNS uuid AS $$
  SELECT NULLIF(current_setting('app.current_org_id', true), '')::uuid;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION current_org_id() IS
  'The tenant of the current transaction, or NULL. NULL makes every policy evaluate false, so a query without tenant context returns nothing (P0-08).';


-- --- organizations ----------------------------------------------------------
--
-- Keyed on `id` rather than `org_id`: an organization row IS the tenant.

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;

CREATE POLICY organizations_tenant_isolation ON organizations
    USING (id = current_org_id())
    WITH CHECK (id = current_org_id());


-- --- Straightforwardly org-scoped tables ------------------------------------

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_tenant_isolation ON users
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
CREATE POLICY projects_tenant_isolation ON projects
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE applications ENABLE ROW LEVEL SECURITY;
CREATE POLICY applications_tenant_isolation ON applications
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY roles_tenant_isolation ON roles
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE user_grants ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_grants_tenant_isolation ON user_grants
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY sessions_tenant_isolation ON sessions
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY refresh_tokens_tenant_isolation ON refresh_tokens
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

ALTER TABLE user_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_tokens_tenant_isolation ON user_tokens
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());


-- --- events -----------------------------------------------------------------
--
-- RLS on a partitioned table applies to every partition, including ones
-- created later by ensure_events_partition(). That matters: a partition added
-- next month must not silently arrive without isolation.
--
-- Read and insert only. auth_app has no UPDATE or DELETE privilege at all
-- (docs/SECURITY/02 §19), so those need no policy — the privilege is the control.

ALTER TABLE events ENABLE ROW LEVEL SECURITY;

CREATE POLICY events_tenant_read ON events
    FOR SELECT
    USING (org_id = current_org_id());

CREATE POLICY events_tenant_insert ON events
    FOR INSERT
    WITH CHECK (org_id = current_org_id());


-- --- project_grants ---------------------------------------------------------
--
-- The one table visible to TWO tenants, and deliberately so: a Project Grant
-- is the delegation contract between a granting and a receiving organization
-- (docs/PLAN/08 Part C). Each side must see it — the granting org to manage and
-- revoke it, the receiving org to know which roles it may assign.
--
-- Note what this policy does NOT do. It makes the grant row visible to the
-- receiving organization; it does not let that organization assign roles
-- beyond granted_role_keys. That check is application logic validated on every
-- request (P4-02), because it is a comparison between a request and a row
-- rather than a question of row visibility. RLS bounds what can be seen, not
-- what can be claimed.

ALTER TABLE project_grants ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_grants_tenant_isolation ON project_grants
    USING (
        granting_org_id = current_org_id()
        OR granted_org_id = current_org_id()
    )
    -- Writes are restricted to the GRANTING side. A receiving organization
    -- that could insert or alter a grant row could widen its own delegation,
    -- which is the privilege escalation docs/PLAN/09 § Delegation abuse names.
    WITH CHECK (granting_org_id = current_org_id());


-- --- Tables deliberately left without tenant RLS ----------------------------
--
-- instances     Deployment-level, not tenant data. Reached only through the
--               instance-scoped path.
--
-- signing_keys  Instance-wide: every organization's tokens are signed by the
--               same key set. Scoping it per tenant would be meaningless and
--               would break token issuance for any request without a tenant.
--
-- manager_roles This one is a real gap and is recorded as such, not overlooked.
--
--               It has no org_id, only user_id and scope_id, and it is read
--               DURING permission resolution — that is, before a tenant
--               context exists, since what the caller may access is exactly
--               what is being determined. An org-scoped policy would make the
--               table unreadable at the only moment it is needed.
--
--               The correct policy keys on the current USER, not the current
--               org, which needs an `app.current_user_id` setting that does
--               not exist until the bearer-authentication middleware lands in
--               P1-15/P2-05. Adding a half-working policy now would give the
--               appearance of isolation without the substance.
--
--               Until then manager_roles is protected by the application's own
--               scoping and by the fact that it holds no personal data — only
--               (user, role, scope) triples. Tracked as DV-02 in
--               TASKS/BACKLOG.md so it cannot quietly persist past P2-05.

COMMENT ON TABLE manager_roles IS
  'No tenant RLS yet: read during permission resolution, before a tenant context exists. Needs a user-scoped policy once app.current_user_id exists (P2-05). Tracked as DV-02.';

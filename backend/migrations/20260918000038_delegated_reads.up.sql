-- P4-04: the granting organization reads the delegated rows of its own grants.
--
-- `P4-02` writes delegated `user_grants` rows under the RECEIVING organization's
-- tenant and deliberately opened no visibility to the granting side, because no
-- reader joined `project_grants` yet: a row the granting tenant could see would
-- have kept granting access after revocation (threat review T4-2).
--
-- That join lands in the same change as this policy — `grant.TokenClaims.ForToken`
-- and `authz.readRoleKeys` now resolve a delegated row against its grant and
-- return `role_keys ∩ granted_role_keys`, and nothing at all unless the grant is
-- active. With both halves in place, the granting organization can answer
-- "may this partner user do this in MY project?" — which is the whole point of a
-- delegation, and is asked through /v1/authz/check in the granting tenant.
--
-- Permissive policies are OR-ed, so `user_grants_tenant_isolation` is untouched:
-- the receiving organization still reads and writes its own rows through
-- `org_id = current_org_id()`. This adds exactly the rows a granting organization
-- must see, and only through a grant it made itself. It is FOR SELECT: writing a
-- delegated row stays the receiving side's, as `P4-02` established.
--
-- A third organization sees nothing under either policy.
--
-- EXPAND/CONTRACT: additive. One SELECT policy. A previous-version instance
-- reads strictly less and keeps working.

BEGIN;

-- Written as `IN (SELECT ...)` rather than `EXISTS (...)` deliberately.
--
-- Permissive policies are OR-ed together, so every read of this table is
-- planned against `org_id = current_org_id() OR <this>`. With a correlated
-- EXISTS on the right-hand side the planner cannot index either branch and
-- falls back to a sequential scan across every tenant — which is correct and
-- slow, and invisible until the service is slow for a reason nobody can find.
-- `tests/security/rls_plans_test.go` caught exactly that.
--
-- `project_grant_id IN (...)` is an uncorrelated subquery: Postgres hashes it
-- once, and the `IS NOT NULL` guard lets the partial index
-- `user_grants_project_grant_idx` carry this branch, so the two policies can be
-- combined with a bitmap OR over two indexes.
--
-- Note where the isolation actually comes from. The subquery reads
-- `project_grants` as the caller, under ITS row-level security, which already
-- shows a grant only to its two parties. So `granting_org_id = current_org_id()`
-- cannot be made to leak by removing it — a third organization's grants are not
-- in the subquery's result to begin with, and a receiving organization's own
-- rows are visible to it through the tenant policy anyway. It is kept because
-- it states the rule this policy exists for, and because a future change to
-- `project_grants` visibility would otherwise widen this policy silently.
CREATE POLICY user_grants_granting_side_read ON user_grants
    FOR SELECT
    USING (
        project_grant_id IS NOT NULL
        AND project_grant_id IN (
            SELECT g.id FROM project_grants g
             WHERE g.granting_org_id = current_org_id()
        )
    );

COMMENT ON POLICY user_grants_granting_side_read ON user_grants IS
    'P4-04: the granting organization reads delegated rows of ITS OWN grants, read-only, so it can decide access in its own project. Revocation is honoured by the readers'' join, not by this policy.';

COMMIT;

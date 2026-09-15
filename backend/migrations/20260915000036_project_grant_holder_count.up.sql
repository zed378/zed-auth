-- How many users hold a role through each grant, for the granting organization
-- deciding whether to revoke (P4-05, docs/UI-UX/04 Flow 2: "This will remove
-- access for N users currently holding a role through this grant").
--
-- SECURITY DEFINER because a delegated assignment lives under the RECEIVING
-- organization's tenant (threat review T4-3, built in P4-02), which the
-- granting organization's RLS cannot see. Bounded the way P4-01's name lookup
-- is: counts only, and only for grants FROM the transaction's own organization.
-- No user ids, no names — the number is the blast radius, and the people behind
-- it are the other organization's business.
--
-- Returns zero for every grant until P4-02 creates delegated assignments, which
-- is the truth rather than a placeholder.
--
-- EXPAND/CONTRACT: one additive function.

CREATE OR REPLACE FUNCTION project_grant_holder_counts(grant_ids uuid[])
RETURNS TABLE (grant_id uuid, holders bigint)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT g.id, count(DISTINCT ug.user_id)
      FROM project_grants g
      LEFT JOIN user_grants ug ON ug.project_grant_id = g.id
     WHERE g.id = ANY(grant_ids)
       AND current_org_id() IS NOT NULL
       AND g.granting_org_id = current_org_id()
     GROUP BY g.id;
$$;

REVOKE ALL ON FUNCTION project_grant_holder_counts(uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION project_grant_holder_counts(uuid[]) TO auth_app;

COMMENT ON FUNCTION project_grant_holder_counts(uuid[]) IS
    'P4-05: distinct users holding a role through each grant, for grants from the current tenant only. Counts, never identities.';

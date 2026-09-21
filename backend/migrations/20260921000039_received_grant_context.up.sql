-- P4-06: the receiving organization can name the project it was given, and who
-- gave it.
--
-- A grant row is visible to both parties (`project_grants_tenant_isolation`),
-- but the PROJECT it names and the ORGANIZATION that made it are the granting
-- side's rows, which the receiving tenant's RLS hides. So a partner
-- administrator opening "Granted Projects" would see two UUIDs and no way to
-- tell which vendor sent them.
--
-- This is the mirror of `P4-01`'s `granted_organization_names()`, and it is
-- bounded the same way: a row comes back only for a grant whose
-- `granted_org_id` is the transaction's own organization. It reveals the name
-- of a project this organization was given and of the organization that gave
-- it — which is the delegation it already holds — and nothing else. No listing,
-- no lookup by name, no other column.
--
-- EXPAND/CONTRACT: additive. One function. Nothing reads it in the previous
-- version.

BEGIN;

CREATE OR REPLACE FUNCTION received_grant_context(grant_ids uuid[])
RETURNS TABLE (grant_id uuid, project_name text, granting_org_name text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT g.id, p.name, o.name
      FROM project_grants g
      JOIN projects p      ON p.id = g.project_id
      JOIN organizations o ON o.id = g.granting_org_id
     WHERE g.id = ANY (grant_ids)
       AND current_org_id() IS NOT NULL
       AND g.granted_org_id = current_org_id();
$$;

REVOKE ALL ON FUNCTION received_grant_context(uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION received_grant_context(uuid[]) TO auth_app;

COMMENT ON FUNCTION received_grant_context(uuid[]) IS
    'P4-06: the project and granting organization names for grants made TO the current tenant, and no others.';

COMMIT;

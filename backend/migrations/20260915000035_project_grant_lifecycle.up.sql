-- Project Grants: the lifecycle the data must enforce, not only the handler (P4-01).
--
-- `project_grants` has existed since P0-07 with its constraints (no self-grant,
-- revocation carries a timestamp, one active grant per project and receiving
-- organization), its two-sided RLS policy, and — since P2-08 — the trigger that
-- the granting organization owns the project. What it lacks is the rule that a
-- delegation only ever narrows: once created, a grant's project, parties and
-- role keys never change, and a revoked grant never comes back.
--
-- The API has no update path, and that is the first control (spec F-9). This
-- makes it a property of the data, because "widen a delegation after the fact"
-- is the privilege escalation docs/PLAN/18 R-04 names, and a future handler — or
-- a hand-run UPDATE from the owner connection during an incident — should meet
-- a refusal rather than a convention.
--
-- EXPAND/CONTRACT: additive. A trigger and two functions; no previous-version
-- instance writes project_grants at all.

CREATE OR REPLACE FUNCTION project_grants_only_narrow()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id <> OLD.project_id
       OR NEW.granting_org_id <> OLD.granting_org_id
       OR NEW.granted_org_id <> OLD.granted_org_id THEN
        RAISE EXCEPTION 'project_grants: a grant''s project and parties cannot change; revoke it and create another'
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.granted_role_keys IS DISTINCT FROM OLD.granted_role_keys THEN
        -- Refused in BOTH directions. Narrowing in place is harmless on its own,
        -- but it would make the audit trail's "roles delegated" at creation a
        -- statement that is no longer true of the row, and P4-04's revocation
        -- work is keyed on the grant rather than on its contents.
        RAISE EXCEPTION 'project_grants: granted_role_keys cannot change; revoke the grant and create another'
            USING ERRCODE = 'check_violation';
    END IF;

    IF OLD.status = 'revoked' AND NEW.status <> 'revoked' THEN
        RAISE EXCEPTION 'project_grants: a revoked grant cannot be reactivated; create a new one'
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER project_grants_only_narrow
    BEFORE UPDATE ON project_grants
    FOR EACH ROW EXECUTE FUNCTION project_grants_only_narrow();

COMMENT ON FUNCTION project_grants_only_narrow() IS
    'P4-01: a grant never changes project, parties or role keys, and a revoked grant never reactivates. Refused for every writer, the owner included.';


-- Whether an organization may receive a grant, answered for a caller whose RLS
-- cannot see other organizations at all.
--
-- SECURITY DEFINER, and deliberately as narrow as that makes necessary: one
-- boolean about one id the caller already holds. It does not return a name or
-- any other column. It reveals that a UUID names a live organization, which
-- requires already knowing a 122-bit random identifier — not an enumeration a
-- caller can perform by guessing.
CREATE OR REPLACE FUNCTION organization_accepts_grants(candidate uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM organizations
         WHERE id = candidate
           AND deleted_at IS NULL
           AND status = 'active'
    );
$$;

REVOKE ALL ON FUNCTION organization_accepts_grants(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION organization_accepts_grants(uuid) TO auth_app;

COMMENT ON FUNCTION organization_accepts_grants(uuid) IS
    'P4-01: whether an organization id names a live organization, for grant creation under a tenant that cannot see it. Returns a boolean only.';


-- The names of the organizations the CALLER'S organization has granted to.
--
-- A granting administrator revoking a grant needs to read who it is to; a UUID
-- is not something a person can check. The receiving organization's row is
-- invisible under the granting tenant's RLS, so this is the one place that name
-- crosses the boundary — and only for an organization that already holds a
-- grant, active or revoked, FROM the organization the transaction is scoped to.
-- An id with no such grant returns nothing, which is what stops this being a
-- lookup of arbitrary organizations' names.
CREATE OR REPLACE FUNCTION granted_organization_names(ids uuid[])
RETURNS TABLE (id uuid, name text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT o.id, o.name
      FROM organizations o
     WHERE o.id = ANY(ids)
       AND current_org_id() IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM project_grants g
            WHERE g.granted_org_id = o.id
              AND g.granting_org_id = current_org_id()
       );
$$;

REVOKE ALL ON FUNCTION granted_organization_names(uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION granted_organization_names(uuid[]) TO auth_app;

COMMENT ON FUNCTION granted_organization_names(uuid[]) IS
    'P4-01: names of organizations holding a grant from the current tenant, and no others.';

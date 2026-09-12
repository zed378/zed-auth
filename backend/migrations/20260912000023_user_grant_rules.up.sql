-- P2-03: the rules on `user_grants`, which is the row that actually grants
-- access to anything.
--
-- The table is from `P0-07`: one row per (user, project), a non-empty
-- `role_keys`, a nullable `project_grant_id` for Phase 4's delegation, and
-- row-level security on org_id. What it has never had is any statement that
-- the roles it names exist.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Every role key must name a role in that project
-- ---------------------------------------------------------------------------

-- Without this, a grant can name `billing-admin` in a project that has no such
-- role. The row looks like access. The token carries a key nothing defines.
-- Every consumer checking for it denies, quietly, and the administrator who
-- created the grant has no way to see why — there is no error anywhere, just a
-- permission that does not work.
--
-- A trigger because it spans two tables, and one that names the offending key:
-- "one of your role keys is invalid" sends somebody to guess which.
CREATE OR REPLACE FUNCTION user_grants_roles_must_exist()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    missing text;
BEGIN
    SELECT k INTO missing
      FROM unnest(NEW.role_keys) AS k
     WHERE NOT EXISTS (
         SELECT 1 FROM roles r WHERE r.project_id = NEW.project_id AND r.key = k
     )
     LIMIT 1;

    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'role key % does not exist in project %', missing, NEW.project_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER user_grants_roles_exist
    BEFORE INSERT OR UPDATE OF role_keys, project_id ON user_grants
    FOR EACH ROW EXECUTE FUNCTION user_grants_roles_must_exist();

-- ---------------------------------------------------------------------------
-- 2. org_id must match the project's
-- ---------------------------------------------------------------------------

-- The same invariant and the same reasoning as `P2-01`'s roles trigger: a
-- mismatch files the grant under a tenant that row-level security then hides
-- it from, so the organization that owns the project cannot see the access
-- somebody was given inside it.
CREATE OR REPLACE FUNCTION user_grants_org_must_match_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owning_org uuid;
BEGIN
    -- Runs as the caller, under RLS, so a project in another organization is
    -- simply not found and the refusal follows from that rather than from
    -- comparing two values we were handed.
    SELECT org_id INTO owning_org FROM projects WHERE id = NEW.project_id;

    IF owning_org IS NULL THEN
        RAISE EXCEPTION 'grant references project % which is not visible in this tenant', NEW.project_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF owning_org <> NEW.org_id THEN
        RAISE EXCEPTION 'grant org_id % does not match project %''s organization %',
            NEW.org_id, NEW.project_id, owning_org
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER user_grants_org_matches_project
    BEFORE INSERT OR UPDATE OF org_id, project_id ON user_grants
    FOR EACH ROW EXECUTE FUNCTION user_grants_org_must_match_project();

-- ---------------------------------------------------------------------------
-- 3. The Phase 4 slot, closed
-- ---------------------------------------------------------------------------

-- `project_grant_id` distinguishes a delegated grant from a direct one, and
-- `docs/PLAN/08` Part C requires a delegated grant's role_keys to be a SUBSET
-- of the delegation's granted_role_keys, revalidated on every request. That is
-- the most security-critical check in the system and it belongs to `P4-01`.
--
-- Until then the column is refused rather than ignored. A trigger rather than
-- `CHECK (project_grant_id IS NULL)` deliberately: Phase 4 replaces this
-- FUNCTION BODY with the subset validation, so the call site, the error path
-- and the tests are already where they need to be. A CHECK would have to be
-- dropped, and dropping a constraint is how a window opens between removing
-- the refusal and adding the real check.
CREATE OR REPLACE FUNCTION user_grants_delegation_is_not_yet_implemented()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_grant_id IS NOT NULL THEN
        RAISE EXCEPTION
            'delegated grants are not implemented: project_grant_id must be NULL until P4-01 adds granted_role_keys subset validation'
            USING ERRCODE = 'feature_not_supported';
    END IF;
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION user_grants_delegation_is_not_yet_implemented() IS
    'P2-03 step 5: the designed slot for P4-01. Replace this body with the subset check; do not drop the trigger.';

CREATE TRIGGER user_grants_delegation_closed
    BEFORE INSERT OR UPDATE OF project_grant_id ON user_grants
    FOR EACH ROW EXECUTE FUNCTION user_grants_delegation_is_not_yet_implemented();

COMMIT;

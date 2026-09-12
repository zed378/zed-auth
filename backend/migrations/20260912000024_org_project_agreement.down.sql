-- Reverses P2-08's consolidation, restoring the two per-table functions that
-- P2-01 and P2-03 created and dropping the two triggers this migration added.

BEGIN;

DROP TRIGGER IF EXISTS project_grants_org_matches_project ON project_grants;
DROP TRIGGER IF EXISTS applications_org_matches_project ON applications;

DROP TRIGGER IF EXISTS user_grants_org_matches_project ON user_grants;
DROP TRIGGER IF EXISTS roles_org_matches_project ON roles;

DROP FUNCTION IF EXISTS org_must_match_project();

CREATE OR REPLACE FUNCTION roles_org_must_match_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owning_org uuid;
BEGIN
    SELECT org_id INTO owning_org FROM projects WHERE id = NEW.project_id;
    IF owning_org IS NULL THEN
        RAISE EXCEPTION 'role references project % which is not visible in this tenant', NEW.project_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF owning_org <> NEW.org_id THEN
        RAISE EXCEPTION 'role org_id % does not match project %''s organization %',
            NEW.org_id, NEW.project_id, owning_org
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER roles_org_matches_project
    BEFORE INSERT OR UPDATE OF org_id, project_id ON roles
    FOR EACH ROW EXECUTE FUNCTION roles_org_must_match_project();

CREATE OR REPLACE FUNCTION user_grants_org_must_match_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owning_org uuid;
BEGIN
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

COMMIT;

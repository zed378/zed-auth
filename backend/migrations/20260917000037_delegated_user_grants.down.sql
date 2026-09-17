-- Restores P2-03's refusal and the pre-P4-02 bodies of the two checks that now
-- skip delegated rows. Delegated rows written in between remain as inert data;
-- any UPDATE to one is refused again, as every such row was before P4-02.

BEGIN;

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

DROP TRIGGER user_grants_delegation_closed ON user_grants;

CREATE TRIGGER user_grants_delegation_closed
    BEFORE INSERT OR UPDATE OF project_grant_id ON user_grants
    FOR EACH ROW EXECUTE FUNCTION user_grants_delegation_is_not_yet_implemented();

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

CREATE OR REPLACE FUNCTION org_must_match_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row_json    jsonb := to_jsonb(NEW);
    row_org     uuid  := coalesce(row_json ->> 'org_id', row_json ->> 'granting_org_id')::uuid;
    row_project uuid  := (row_json ->> 'project_id')::uuid;
    owning_org  uuid;
BEGIN
    IF row_project IS NULL THEN
        RETURN NEW;
    END IF;

    IF row_org IS NULL THEN
        RAISE EXCEPTION '% carries project_id but no org_id or granting_org_id; org_must_match_project() cannot check it',
            TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END IF;

    SELECT org_id INTO owning_org FROM projects WHERE id = row_project;

    IF owning_org IS NULL THEN
        RAISE EXCEPTION '% references project % which is not visible in this tenant',
            TG_TABLE_NAME, row_project
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF owning_org <> row_org THEN
        RAISE EXCEPTION '% org_id % does not match project %''s organization %',
            TG_TABLE_NAME, row_org, row_project, owning_org
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

COMMIT;

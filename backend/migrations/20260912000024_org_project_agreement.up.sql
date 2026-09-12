-- P2-08 step 4: a project in one organization can never be referenced by a row
-- filed under another.
--
-- `P2-01` and `P2-03` each added this invariant for the table they were
-- building, with a near-identical plpgsql function. Two copies is a
-- coincidence; four would be a pattern that drifts. This replaces them with
-- ONE function and attaches it to every table that carries both columns.
--
-- The failure it prevents is the quiet one. A row with organization A's
-- `org_id` and organization B's `project_id` is not rejected by anything —
-- both ids are real — and row-level security then HIDES it from B, the
-- organization that actually owns the project. An application nobody can see,
-- a grant nobody can audit.
--
-- `applications` is the one that matters today: it holds every OIDC client, it
-- has been writable since Phase 0, and a misfiled one is a login nobody can
-- account for.

BEGIN;

-- The generic form. Reads NEW through `to_jsonb` so one function serves every
-- table whose columns are named `org_id` and `project_id` — which is all of
-- them, because `docs/PLAN/04` uses those names consistently.
CREATE OR REPLACE FUNCTION org_must_match_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row_json    jsonb := to_jsonb(NEW);
    -- `org_id` on three tables; `granting_org_id` on `project_grants`, which
    -- has two organizations and where the one that must own the project is the
    -- granting side.
    --
    -- Both names are read because the first version read only `org_id` and
    -- would therefore have attached to `project_grants` and enforced NOTHING —
    -- a trigger that looks present and is a no-op, which is worse than an
    -- absent one because it answers the question "is this checked?" wrongly.
    row_org     uuid  := coalesce(row_json ->> 'org_id', row_json ->> 'granting_org_id')::uuid;
    row_project uuid  := (row_json ->> 'project_id')::uuid;
    owning_org  uuid;
BEGIN
    IF row_project IS NULL THEN
        -- A nullable project_id is legitimate on tables that have one.
        RETURN NEW;
    END IF;

    IF row_org IS NULL THEN
        -- The table carries a project and no organization column this function
        -- recognises. Refused rather than skipped: silence here is how the
        -- no-op above would have gone unnoticed.
        RAISE EXCEPTION '% carries project_id but no org_id or granting_org_id; org_must_match_project() cannot check it',
            TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END IF;

    -- SECURITY DEFINER is deliberately NOT used. This runs as the caller,
    -- under row-level security, so the lookup can only see projects in the
    -- caller's own tenant: a project in another organization is simply not
    -- found, and the refusal follows from that rather than from comparing two
    -- values we were handed.
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

COMMENT ON FUNCTION org_must_match_project() IS
    'P2-08: org_id must name the organization that owns project_id. Attached to every table carrying both.';

-- --- the two tables that had it, now sharing one function -------------------

DROP TRIGGER IF EXISTS roles_org_matches_project ON roles;
DROP FUNCTION IF EXISTS roles_org_must_match_project();

CREATE TRIGGER roles_org_matches_project
    BEFORE INSERT OR UPDATE OF org_id, project_id ON roles
    FOR EACH ROW EXECUTE FUNCTION org_must_match_project();

DROP TRIGGER IF EXISTS user_grants_org_matches_project ON user_grants;
DROP FUNCTION IF EXISTS user_grants_org_must_match_project();

CREATE TRIGGER user_grants_org_matches_project
    BEFORE INSERT OR UPDATE OF org_id, project_id ON user_grants
    FOR EACH ROW EXECUTE FUNCTION org_must_match_project();

-- --- the two that never had it ---------------------------------------------

-- Every OIDC client. Writable since Phase 0 with nothing checking this.
CREATE TRIGGER applications_org_matches_project
    BEFORE INSERT OR UPDATE OF org_id, project_id ON applications
    FOR EACH ROW EXECUTE FUNCTION org_must_match_project();

-- Empty until Phase 4, and given the rule now so `P4-01` inherits it rather
-- than having to remember. A delegation filed under the wrong organization
-- would be the worst instance of this bug: the whole point of the row is to
-- cross an organization boundary deliberately, so a mistake looks like the
-- feature working.
CREATE TRIGGER project_grants_org_matches_project
    BEFORE INSERT OR UPDATE OF granting_org_id, project_id ON project_grants
    FOR EACH ROW EXECUTE FUNCTION org_must_match_project();

COMMIT;

-- P4-02: delegated user grants — the slot P2-03 closed, filled.
--
-- A delegated grant is a `user_grants` row with `project_grant_id` set. Its
-- `org_id` is the RECEIVING organization: the row is that organization's
-- statement about its own user, lives under its tenant, and is administered
-- there (spec §6, threat review T4-3). Its `project_id` is the granting
-- organization's project, which the receiving tenant cannot see.
--
-- The rule `docs/PLAN/08` Part C, CLAUDE.md and docs/PLAN/09 all state: the
-- roles must be a subset of the grant's `granted_role_keys`, on every write.
-- The handler checks it first, for a clear error. This checks it for EVERY
-- writer — a future handler, the direct-grant PATCH, or a hand-run statement.
--
-- What this migration deliberately does NOT do is make delegated rows readable
-- to the granting tenant. The token and authz readers do not join the grant
-- yet, so a row they could see would keep serving roles after revocation
-- (T4-2). That policy arrives with the join, in P4-04.
--
-- EXPAND/CONTRACT: function bodies replaced and one trigger recreated, in one
-- transaction. A previous-version instance never wrote a non-null
-- project_grant_id — the old body refused it — so nothing it writes changes
-- meaning.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. The delegation check (P2-03's designed slot)
-- ---------------------------------------------------------------------------
--
-- The name stays. P2-03 wrote it to have its body replaced rather than be
-- dropped, so the call site and the trigger never have a moment of absence.
CREATE OR REPLACE FUNCTION user_grants_delegation_is_not_yet_implemented()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    g           record;
    not_granted text;
BEGIN
    -- Which grant a row belongs to never changes, in either direction. A
    -- direct grant that acquired a project_grant_id would borrow a
    -- delegation's scope; a delegated one that shed it would become a direct
    -- grant nobody issued (spec A-5).
    IF TG_OP = 'UPDATE' AND NEW.project_grant_id IS DISTINCT FROM OLD.project_grant_id THEN
        RAISE EXCEPTION 'user_grants: project_grant_id cannot change; delete the grant and create another'
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.project_grant_id IS NULL THEN
        RETURN NEW;
    END IF;

    -- FOR SHARE: a revocation takes FOR UPDATE on this row, so the two
    -- serialize. An assignment that waits behind a revocation re-reads the row
    -- and sees `revoked` (spec A-4). Runs as the caller, under RLS: a grant the
    -- caller's tenant cannot see is not found.
    SELECT project_id, granted_org_id, granted_role_keys, status
      INTO g
      FROM project_grants
     WHERE id = NEW.project_grant_id
       FOR SHARE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'delegated grant: project grant % is not visible in this tenant', NEW.project_grant_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF g.status <> 'active' THEN
        RAISE EXCEPTION 'delegated grant: project grant % is revoked', NEW.project_grant_id
            USING ERRCODE = 'check_violation';
    END IF;

    IF g.granted_org_id <> NEW.org_id THEN
        RAISE EXCEPTION 'delegated grant: org_id % is not the organization project grant % was granted to',
            NEW.org_id, NEW.project_grant_id
            USING ERRCODE = 'check_violation';
    END IF;

    IF g.project_id <> NEW.project_id THEN
        RAISE EXCEPTION 'delegated grant: project % is not the project of project grant %',
            NEW.project_id, NEW.project_grant_id
            USING ERRCODE = 'check_violation';
    END IF;

    SELECT k INTO not_granted
      FROM unnest(NEW.role_keys) AS k
     WHERE NOT (k = ANY (g.granted_role_keys))
     LIMIT 1;

    IF not_granted IS NOT NULL THEN
        RAISE EXCEPTION 'delegated grant: role key % is not delegated by project grant %',
            not_granted, NEW.project_grant_id
            USING ERRCODE = 'check_violation';
    END IF;

    -- The user must be the receiving organization's own. Assigning a
    -- delegated role to anyone else is a cross-tenant write (spec A-3).
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = NEW.user_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'delegated grant: user % is not a member of organization %',
            NEW.user_id, NEW.org_id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION user_grants_delegation_is_not_yet_implemented() IS
    'P4-02 (the body P2-03 reserved): a delegated grant names an active project grant to its own organization, for that grant''s project, with a subset of its granted_role_keys, for one of that organization''s users. project_grant_id never changes.';

-- Recreated to fire on every column the rules above read. It fired on
-- project_grant_id alone, so an UPDATE of role_keys on a delegated row would
-- have widened it unchecked (spec A-6).
DROP TRIGGER user_grants_delegation_closed ON user_grants;

CREATE TRIGGER user_grants_delegation_closed
    BEFORE INSERT OR UPDATE OF project_grant_id, role_keys, org_id, project_id, user_id ON user_grants
    FOR EACH ROW EXECUTE FUNCTION user_grants_delegation_is_not_yet_implemented();

-- ---------------------------------------------------------------------------
-- 2. Role existence: the delegation check stands in for delegated rows
-- ---------------------------------------------------------------------------
--
-- The granting project's roles are invisible under the receiving tenant, so
-- this lookup would refuse every delegated row. The grant's keys were checked
-- against the project's roles when it was created (P4-01), and a role an active
-- grant carries cannot be deleted (P4-01 F-10).
CREATE OR REPLACE FUNCTION user_grants_roles_must_exist()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    missing text;
BEGIN
    IF NEW.project_grant_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

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

-- ---------------------------------------------------------------------------
-- 3. Organization and project agreement: likewise
-- ---------------------------------------------------------------------------
--
-- A delegated row's org_id is deliberately NOT the project's owner. The
-- delegation check requires it to be the grant's receiving organization and the
-- project to be the grant's, which is the stricter statement.
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

    IF TG_TABLE_NAME = 'user_grants' AND (row_json ->> 'project_grant_id') IS NOT NULL THEN
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

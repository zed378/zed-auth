-- P2-01: the rules on `roles`, which the table has never had.
--
-- The table itself is from `P0-07` and is not touched here: id, project_id,
-- org_id, key, display_name, permission_keys, is_builtin, the unique index on
-- (project_id, key), the updated_at trigger and row-level security all exist.
-- What it has never had is any statement about what may go IN it.
--
-- Nothing writes roles yet — there is no API until `P2-02` — so every
-- constraint below is added validated rather than NOT VALID. There is no data
-- to be incompatible with, and a constraint carried as NOT VALID is one nobody
-- remembers to validate later.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Permission keys have a shape
-- ---------------------------------------------------------------------------

-- `resource:action`, from docs/PLAN/08 Part A's own examples (user:read,
-- billing:write), with a dotted resource permitted so a consumer application
-- can namespace (billing.invoice:read) instead of inventing a separator we did
-- not anticipate.
--
-- `*` is deliberately NOT permitted. A wildcard in a permission key is an
-- authorization decision hiding inside a string: every consumer would have to
-- reimplement the same match, and they would not agree. If wildcards are
-- wanted they belong in the decision engine (`P2-06`), where the matching is
-- written once and can be reasoned about. Widening this pattern later is
-- harmless; narrowing it breaks deployed consumer code we cannot see, so it
-- starts narrow.
--
-- IMMUTABLE because a CHECK may not call a volatile function, and a plain
-- subquery is not allowed in a CHECK at all (`P1-29` hit both).
CREATE OR REPLACE FUNCTION permission_keys_are_well_formed(keys text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT keys IS NULL
        OR NOT EXISTS (
            SELECT 1
              FROM unnest(keys) AS k
             WHERE k !~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$'
        );
$$;

COMMENT ON FUNCTION permission_keys_are_well_formed(text[]) IS
    'P2-01: every element matches resource:action, optionally dotted. No wildcards.';

ALTER TABLE roles
    ADD CONSTRAINT roles_permission_keys_well_formed
    CHECK (permission_keys_are_well_formed(permission_keys));

-- A role with 4,000 permission keys is not a role, it is a denial of service
-- against every token it lands in (`P2-04` embeds them) and every decision that
-- scans them (`P2-06`). The bound is generous and its absence is not.
ALTER TABLE roles
    ADD CONSTRAINT roles_permission_keys_bounded
    CHECK (array_length(permission_keys, 1) IS NULL OR array_length(permission_keys, 1) <= 256);

-- Duplicates are refused rather than folded. Silently de-duplicating means the
-- row does not match what the caller sent, and the caller finds out from a
-- later read instead of from the write that was wrong.
--
-- A function again, for the reason stated above and which this migration got
-- wrong on its first run: the natural spelling — `array_length(...) = (SELECT
-- count(DISTINCT k) FROM unnest(...))` — is a subquery in a CHECK, which
-- PostgreSQL refuses outright. `P1-29` hit the identical wall on
-- `applications.allowed_origins`.
CREATE OR REPLACE FUNCTION permission_keys_are_distinct(keys text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT keys IS NULL
        OR array_length(keys, 1) IS NULL
        OR array_length(keys, 1) = (SELECT count(DISTINCT k) FROM unnest(keys) AS k);
$$;

COMMENT ON FUNCTION permission_keys_are_distinct(text[]) IS
    'P2-01: a role does not carry the same permission key twice.';

ALTER TABLE roles
    ADD CONSTRAINT roles_permission_keys_distinct
    CHECK (permission_keys_are_distinct(permission_keys));

-- ---------------------------------------------------------------------------
-- 2. org_id and project_id must agree
-- ---------------------------------------------------------------------------

-- `roles` carries both, and nothing has ever made them consistent. A caller
-- supplying its own valid org_id together with another organization's
-- project_id writes a role into the wrong tenant — and row-level security then
-- HIDES that row from the organization that actually owns the project. A
-- silently misfiled permission is worse than a rejected write.
--
-- A trigger rather than a CHECK because the invariant spans two tables and a
-- CHECK cannot see another row.
CREATE OR REPLACE FUNCTION roles_org_must_match_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    owning_org uuid;
BEGIN
    -- SECURITY DEFINER is deliberately NOT used. This runs as the caller, and
    -- the caller is the runtime role under RLS, so the lookup can only see
    -- projects in the caller's own tenant. A project in another organization
    -- is therefore not found, and the mismatch is refused for that reason
    -- rather than by comparing two values we were handed.
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

-- ---------------------------------------------------------------------------
-- 3. Built-in roles cannot be re-keyed or deleted
-- ---------------------------------------------------------------------------

-- docs/PLAN/04 § roles: "built-in roles cannot be renamed or deleted".
--
-- In the database rather than only in the store, because the rule has to hold
-- against writers the store does not mediate — a migration, a support script,
-- a future service. `P1-20` already found `events` writable on staging while
-- the code that was supposed to prevent it read correctly.
--
-- `display_name` is deliberately still editable. What a grant references is
-- `key`; the display name is presentation, and freezing it would mean a typo
-- in a built-in role's label could never be corrected.
CREATE OR REPLACE FUNCTION roles_builtin_is_immutable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.is_builtin THEN
            RAISE EXCEPTION 'built-in role % cannot be deleted', OLD.key
                USING ERRCODE = 'restrict_violation';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.is_builtin AND NEW.key IS DISTINCT FROM OLD.key THEN
        RAISE EXCEPTION 'built-in role % cannot be re-keyed', OLD.key
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- Clearing the flag would be a two-step deletion: unset, then delete.
    IF OLD.is_builtin AND NOT NEW.is_builtin THEN
        RAISE EXCEPTION 'role % cannot stop being built-in', OLD.key
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER roles_builtin_immutable
    BEFORE UPDATE OR DELETE ON roles
    FOR EACH ROW EXECUTE FUNCTION roles_builtin_is_immutable();

-- ---------------------------------------------------------------------------
-- 4. Finding the grants that reference a role
-- ---------------------------------------------------------------------------

-- `user_grants.role_keys` is a text[] of role keys scoped to a project — there
-- is no foreign key, by design (docs/PLAN/04: permission and role keys are the
-- consumer's vocabulary, not rows to join). So "is this role in use" is an
-- array containment search, and refusing to delete a referenced role (`P2-01`
-- FR-6) does it on every delete.
--
-- GIN on role_keys, with project_id, because the question is always asked
-- within one project.
CREATE INDEX IF NOT EXISTS user_grants_role_keys_idx ON user_grants USING GIN (role_keys);

COMMIT;

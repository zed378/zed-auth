-- Reverses P2-01's rules, leaving `P0-07`'s table exactly as it was.
--
-- Dropping these makes the table permissive again; it does not destroy data.
-- Any role written while they were in force still satisfies them afterwards,
-- so re-applying the migration cannot fail on rows created in between.

BEGIN;

DROP INDEX IF EXISTS user_grants_role_keys_idx;

DROP TRIGGER IF EXISTS roles_builtin_immutable ON roles;
DROP FUNCTION IF EXISTS roles_builtin_is_immutable();

DROP TRIGGER IF EXISTS roles_org_matches_project ON roles;
DROP FUNCTION IF EXISTS roles_org_must_match_project();

ALTER TABLE roles DROP CONSTRAINT IF EXISTS roles_permission_keys_distinct;
ALTER TABLE roles DROP CONSTRAINT IF EXISTS roles_permission_keys_bounded;
ALTER TABLE roles DROP CONSTRAINT IF EXISTS roles_permission_keys_well_formed;

DROP FUNCTION IF EXISTS permission_keys_are_distinct(text[]);
DROP FUNCTION IF EXISTS permission_keys_are_well_formed(text[]);

COMMIT;

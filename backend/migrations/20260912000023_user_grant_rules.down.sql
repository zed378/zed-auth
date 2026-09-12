-- Reverses P2-03's rules, leaving `P0-07`'s table as it was.
--
-- Dropping these makes the table permissive again; it destroys no data. A
-- grant written while they were in force still satisfies them afterwards, so
-- re-applying cannot fail on rows created in between.

BEGIN;

DROP TRIGGER IF EXISTS user_grants_delegation_closed ON user_grants;
DROP FUNCTION IF EXISTS user_grants_delegation_is_not_yet_implemented();

DROP TRIGGER IF EXISTS user_grants_org_matches_project ON user_grants;
DROP FUNCTION IF EXISTS user_grants_org_must_match_project();

DROP TRIGGER IF EXISTS user_grants_roles_exist ON user_grants;
DROP FUNCTION IF EXISTS user_grants_roles_must_exist();

COMMIT;

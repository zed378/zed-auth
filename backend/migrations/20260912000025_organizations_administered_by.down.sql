-- Reverses 20260912000025.
--
-- Dropping the function is safe in the expand/contract sense: nothing depends
-- on it but `GET /v1/me/organizations`, and a rollback that removes the
-- function is rolling back to a version that does not serve that route.

DROP FUNCTION IF EXISTS organizations_administered_by(uuid, timestamptz, uuid, int);

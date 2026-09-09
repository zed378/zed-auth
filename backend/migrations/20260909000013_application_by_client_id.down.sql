-- Removes the bootstrap lookup. /oauth/authorize can no longer resolve a
-- client_id, so authorization requests fail; no data is lost.
DROP FUNCTION IF EXISTS application_by_client_id(uuid);

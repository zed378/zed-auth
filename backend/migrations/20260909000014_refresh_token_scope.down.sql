-- Drops the bootstrap lookup and the scope column. Existing refresh tokens
-- become unusable, which is a forced re-login rather than data loss.
DROP FUNCTION IF EXISTS refresh_token_by_hash(text, timestamptz);
ALTER TABLE refresh_tokens DROP COLUMN scope;
DROP FUNCTION IF EXISTS session_live(uuid, timestamptz);

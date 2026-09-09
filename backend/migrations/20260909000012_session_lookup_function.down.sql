-- Removes the bootstrap lookup. Sessions become unresolvable, which is a mass
-- logout rather than data loss.
DROP FUNCTION IF EXISTS session_by_token_hash(text, timestamptz);
DROP FUNCTION IF EXISTS sweep_expired_sessions(timestamptz, int);

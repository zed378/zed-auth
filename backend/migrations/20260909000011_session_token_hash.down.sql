-- Drops the lookup key, which makes every existing session unusable: the
-- service can no longer resolve a cookie to a row. That is a mass logout, not
-- data loss, and it is recoverable by logging in again.
DROP INDEX IF EXISTS sessions_token_hash_key;
ALTER TABLE sessions DROP COLUMN token_hash;

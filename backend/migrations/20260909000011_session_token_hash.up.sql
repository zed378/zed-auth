-- Separates the session credential from the session identifier.
--
-- PG-14 (TASKS/BACKLOG.md): PLAN/04 § sessions describes the cookie as
-- carrying the row's id. That makes the primary key a bearer credential, and
-- the data model already contains the places it leaks:
--
--   * PLAN/05 routes /v1/organizations/{org_id}/users/{user_id}/sessions, so
--     an admin listing another user's sessions would receive, per row, the
--     exact string that authenticates as that user.
--   * refresh_tokens.session_id is a foreign key, so a token row would carry a
--     live session credential.
--   * A revocation audit event naming its session_id would write one into an
--     append-only table with 24-month retention.
--
-- So the cookie carries a fresh 256-bit token and this column holds only
-- sha256(token). `id` stays an internal identifier: safe to display, join on
-- and audit.
--
-- Additive and backward-compatible per PLAN/14's expand/contract rule.
-- Nullable with no backfill because no sessions exist yet; P1-11 writes it on
-- every insert.
ALTER TABLE sessions ADD COLUMN token_hash text;

-- Unique because it is the lookup key, and a collision would be one session
-- authenticating as another. Partial, so rows predating the column (there are
-- none, and there should stay none) do not collide on NULL.
CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash)
    WHERE token_hash IS NOT NULL;

COMMENT ON COLUMN sessions.token_hash IS
  'sha256 of the cookie token. The token itself is never stored. The id is NOT the credential (P1-11, PG-14).';

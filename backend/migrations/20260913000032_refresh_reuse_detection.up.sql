-- Refresh-token reuse detection (P3-06).
--
-- `docs/PLAN/17`'s Phase 3 criterion is that a rotated refresh token cannot be
-- reused. The columns for it have existed since `20260908000005` — `family_id`,
-- `replaced_by`, `family_expires_at` — because Phase 1 decided this phase should
-- change behaviour rather than storage shape. This adds the one read that
-- behaviour needs.
--
-- EXPAND/CONTRACT: additive. One new function and one new index; no column
-- touched, no existing function altered. `refresh_token_by_hash` keeps its
-- signature and its liveness filter, so a previous-version instance running
-- mid-rollout is unaffected.

-- Resolves a presented token REGARDLESS of whether it is still usable.
--
-- `refresh_token_by_hash` deliberately returns only live tokens, so a dead one
-- is not merely reported dead — it cannot be acted on by a caller that forgot
-- to check. That is right for the happy path and useless for this one: reuse
-- detection is precisely the question "what happened to this token", and a
-- function that hides dead tokens cannot answer it.
--
-- So this is a SECOND function rather than a relaxation of the first. The
-- liveness filter stays where the happy path depends on it, and the narrow,
-- deliberately-unhelpful read below is the only thing that can see past it.
--
-- It returns NO user, NO client and NO scope: a caller asking this question is
-- deciding whether to kill a family, and needs the family id and the tenant to
-- do it. Everything else would be information leaked to a caller holding a
-- token that may well be stolen.
CREATE OR REPLACE FUNCTION refresh_token_lineage(hash text)
RETURNS TABLE (
    id           uuid,
    org_id       uuid,
    family_id    uuid,
    revoked      boolean,
    replaced_by  uuid,
    -- When the replacement was created, which is when rotation happened. Read
    -- from the replacement's own row rather than stored on this one, so there
    -- is no second column that can disagree with the link it describes.
    replaced_at  timestamptz,
    -- Whether the replacement has itself been used or killed. This is what
    -- separates a client retrying after a network timeout from a thief: a
    -- legitimate retry means the replacement never arrived, so it is untouched.
    successor_spent boolean
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT t.id,
           t.org_id,
           t.family_id,
           t.revoked,
           t.replaced_by,
           r.created_at,
           COALESCE(r.revoked OR r.replaced_by IS NOT NULL, false)
      FROM refresh_tokens t
      LEFT JOIN refresh_tokens r ON r.id = t.replaced_by
     WHERE t.token_hash = hash;
$$;

REVOKE ALL ON FUNCTION refresh_token_lineage(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION refresh_token_lineage(text) TO auth_app;

COMMENT ON FUNCTION refresh_token_lineage(text) IS
  'Bootstrap only: what happened to a presented refresh token, for reuse detection. Returns dead tokens too (P3-06).';


-- Revoking a family reads by family_id, and reuse detection is on the hot path
-- of every refresh.
CREATE INDEX IF NOT EXISTS refresh_tokens_family_idx ON refresh_tokens (family_id);

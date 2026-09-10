-- Resolves an invite or reset token to its user, before a tenant is known.
--
-- The same bootstrap problem sessions have (P1-11's session_by_token_hash), for
-- the same reason and answered the same way. A person clicking a link in an
-- email is anonymous and names no organization; the TOKEN names it. So the read
-- that resolves the token cannot itself be tenant-scoped.
--
-- Instance scope is not the answer. `user_tokens_tenant_isolation` is
-- `org_id = current_org_id()`, so with no tenant set the comparison is NULL and
-- every row is filtered out — instance scope means "no tenant", not "every
-- tenant" (P0-08). Relaxing the policy to admit NULL would open every token to
-- any instance-scoped code path, which is a large hole for one narrow need.
--
-- The authorization argument is possession. The argument is a SHA-256 of a
-- 256-bit secret that exists only in one mailbox; a caller who can supply a
-- matching hash is by definition the party the token was issued to, and a miss
-- returns no rows and tells them nothing.
--
-- READ ONLY, deliberately. Consumption is an UPDATE and it happens in the
-- tenant-scoped transaction that follows — where RLS applies, alongside the
-- password write it must commit with. Putting the write in here too would move
-- a privileged mutation outside the transaction that owns it.
CREATE OR REPLACE FUNCTION user_token_by_hash(hash text, at timestamptz)
RETURNS TABLE (
    user_id uuid,
    org_id  uuid,
    purpose text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT t.user_id, t.org_id, t.purpose
      FROM user_tokens t
     WHERE t.token_hash = hash
       AND t.used_at IS NULL
       AND t.expires_at > at;
$$;

-- Liveness is filtered inside the function rather than by the caller, so a used
-- or expired token cannot travel back to a caller that might then act on it.

REVOKE ALL ON FUNCTION user_token_by_hash(text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION user_token_by_hash(text, timestamptz) TO auth_app;

COMMENT ON FUNCTION user_token_by_hash(text, timestamptz) IS
  'Bootstrap only: resolves an invite or reset link to its tenant before one is known. Consumption is a tenant-scoped UPDATE (P1-19).';

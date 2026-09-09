-- Resolves a session cookie to its session, before a tenant is known.
--
-- This is the bootstrap problem sessions have and nothing else does. Every
-- other table is read inside a tenant-scoped transaction, and `sessions` is
-- what ESTABLISHES which tenant a request belongs to — so the read that
-- resolves a cookie cannot itself be tenant-scoped.
--
-- Instance scope is not the answer. `sessions_tenant_isolation` is
-- `org_id = current_org_id()`, so with no tenant set the comparison is NULL
-- and the row is filtered out: instance scope means "no tenant", not "every
-- tenant", which is P0-08 working as designed. Relaxing the policy to admit
-- NULL would open every session to any instance-scoped code path, which is a
-- large hole opened for one narrow need.
--
-- So the bootstrap gets its own door, on the same pattern as P0-12's partition
-- maintenance: SECURITY DEFINER with a pinned search_path, granted to auth_app,
-- doing exactly one thing.
--
-- The authorization argument is that the caller has already presented the
-- token. Possession of the token IS the credential this row exists to check,
-- so a caller who can supply a matching hash is by definition entitled to the
-- session it names. An attacker without the token learns nothing: the argument
-- is a 64-character hash of a 256-bit secret, and a miss returns no rows.
CREATE OR REPLACE FUNCTION session_by_token_hash(hash text, at timestamptz)
RETURNS TABLE (
    id           uuid,
    user_id      uuid,
    org_id       uuid,
    auth_methods jsonb,
    ip           text,
    user_agent   text,
    created_at   timestamptz,
    last_seen_at timestamptz,
    expires_at   timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT s.id, s.user_id, s.org_id, to_jsonb(s.auth_methods),
           COALESCE(host(s.ip), ''), COALESCE(s.user_agent, ''),
           s.created_at, s.last_seen_at, s.expires_at
      FROM sessions s
     WHERE s.token_hash = hash
       AND s.revoked_at IS NULL
       AND s.expires_at > at;
$$;

-- Liveness is filtered inside the function rather than by the caller, so a
-- revoked or expired row cannot travel back through the cache layer where a
-- later read might use it. The safest way to guarantee that is for it never to
-- be returned.

REVOKE ALL ON FUNCTION session_by_token_hash(text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION session_by_token_hash(text, timestamptz) TO auth_app;

COMMENT ON FUNCTION session_by_token_hash(text, timestamptz) IS
  'Bootstrap only: resolves a session cookie to its tenant before one is known. Every other session access is tenant-scoped (P1-11).';


-- Sweeping expired sessions has the same problem for the same reason.
--
-- The sweep is instance-wide maintenance, not a request, so there is no tenant
-- to scope it to — and `sessions_tenant_isolation` means auth_app deletes
-- nothing with no tenant set. P0-12 solved the identical problem for partition
-- maintenance the identical way.
--
-- Bounded by `max_rows` so a long-neglected table is caught up over several
-- runs rather than in one enormous transaction, and restricted to rows that
-- are already unusable: expired, or revoked before the cutoff. A live session
-- is not reachable through this function at all.
CREATE OR REPLACE FUNCTION sweep_expired_sessions(before timestamptz, max_rows int)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    removed integer;
BEGIN
    WITH doomed AS (
        SELECT id FROM sessions
         WHERE (expires_at < before)
            OR (revoked_at IS NOT NULL AND revoked_at < before)
         LIMIT max_rows
    ), gone AS (
        DELETE FROM sessions WHERE id IN (SELECT id FROM doomed) RETURNING 1
    )
    SELECT count(*) INTO removed FROM gone;

    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION sweep_expired_sessions(timestamptz, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sweep_expired_sessions(timestamptz, int) TO auth_app;

COMMENT ON FUNCTION sweep_expired_sessions(timestamptz, int) IS
  'Deletes sessions that are already unusable. Instance-wide maintenance, bounded per call (P1-11).';

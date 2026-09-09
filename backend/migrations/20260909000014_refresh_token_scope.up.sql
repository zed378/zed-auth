-- Records what a refresh token was granted, and gives it a bootstrap lookup.
--
-- PG-15 (TASKS/BACKLOG.md): PLAN/04 § refresh_tokens lists eleven columns and
-- none of them is the scope the token was issued for. Without it a refresh can
-- only guess: it would either carry no scope at all, or re-derive one from the
-- client's registration — which is not the same thing, because scope is what
-- the USER consented to at the authorization endpoint, not what the client is
-- permitted to ask for.
--
-- A refresh that widens scope would make the refresh token more powerful than
-- the consent that created it, which is precisely what a refresh token must
-- not be. Narrowing needs the original to narrow FROM.
--
-- Additive and backward-compatible: default '{}' so a previous-version
-- instance neither reads nor writes it.
ALTER TABLE refresh_tokens ADD COLUMN scope text[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN refresh_tokens.scope IS
  'What the user consented to when this lineage started. A refresh may narrow it, never widen (P1-07, PG-15).';


-- Resolves a refresh token to its row, before a tenant is known.
--
-- The third instance of the same bootstrap problem, after sessions (P1-11) and
-- client_id (P1-06), and it has the same shape: `refresh_tokens_tenant_isolation`
-- is `org_id = current_org_id()`, so with no tenant set nothing matches. The
-- token endpoint receives a bare refresh token and has to discover which tenant
-- it belongs to before it can scope anything.
--
-- The authorization argument is the strongest of the three: the caller has
-- presented the token itself, and possession of it is the credential this row
-- exists to check. An attacker without it learns nothing — the argument is a
-- SHA-256 of a 256-bit secret, and a miss returns no rows.
--
-- Liveness is filtered inside the function, so a revoked, expired or
-- family-expired token is not merely reported as dead — it is not returned at
-- all, and cannot be acted on by a caller that forgot to check.
CREATE OR REPLACE FUNCTION refresh_token_by_hash(hash text, at timestamptz)
RETURNS TABLE (
    id         uuid,
    user_id    uuid,
    client_id  uuid,
    org_id     uuid,
    session_id uuid,
    family_id  uuid,
    scope      jsonb,
    expires_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT t.id, t.user_id, t.client_id, t.org_id, t.session_id, t.family_id,
           to_jsonb(t.scope), t.expires_at
      FROM refresh_tokens t
     WHERE t.token_hash = hash
       AND NOT t.revoked
       AND t.expires_at > at
       AND t.family_expires_at > at;
$$;

REVOKE ALL ON FUNCTION refresh_token_by_hash(text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION refresh_token_by_hash(text, timestamptz) TO auth_app;

COMMENT ON FUNCTION refresh_token_by_hash(text, timestamptz) IS
  'Bootstrap only: resolves a refresh token to its tenant before one is known. Returns only live tokens (P1-07).';
-- Answers whether one session is still usable, by its id.
--
-- The fourth bootstrap read, and the narrowest: P1-07's refresh grant holds a
-- refresh token that records which session authorised it, and must ask whether
-- that session is still live before honouring the token — before any tenant is
-- known, because the refresh token's own lookup is what establishes one.
--
-- Returns no columns at all, only rows: a caller learns whether the session is
-- live and nothing else about it. That is the whole question being asked, and
-- narrowing a SECURITY DEFINER function to exactly its question is what keeps
-- the exception small.
CREATE OR REPLACE FUNCTION session_live(session uuid, at timestamptz)
RETURNS TABLE (live boolean)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT true
      FROM sessions s
     WHERE s.id = session
       AND s.revoked_at IS NULL
       AND s.expires_at > at;
$$;

REVOKE ALL ON FUNCTION session_live(uuid, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION session_live(uuid, timestamptz) TO auth_app;

COMMENT ON FUNCTION session_live(uuid, timestamptz) IS
  'Bootstrap only: whether a session is usable, for P1-07 refresh grants. Returns liveness and nothing else.';

-- Resolves a factor id to its organization, before a tenant is set (P3-03).
--
-- EXPAND/CONTRACT: purely additive. One new function; no table, column or
-- constraint is touched, so a previous-version instance running alongside this
-- migration is unaffected — it simply never calls it.
--
-- THE BOOTSTRAP PROBLEM, ONCE MORE
--
-- `mfa.Verifier` is `Verify(ctx, factorID, code)`. A factor id alone does not
-- name a tenant, every write goes through `WithTenant`, and
-- `user_mfa_factors_tenant_isolation` is `org_id = current_org_id()` — which
-- matches nothing with no tenant set. Same shape as `application_by_client_id`
-- and `session_by_token_hash`, and the same answer: SECURITY DEFINER, pinned
-- search_path, granted to auth_app, doing exactly one thing.
--
-- WHY THIS ONE IS NARROWER THAN IT LOOKS
--
-- A factor id is NOT public — unlike a client_id, it never travels in a URL
-- and a caller has no way to come by one. So the disclosure argument cannot be
-- "the input was already public", and is instead about the OUTPUT:
--
--   * It returns an organization id and nothing else. Not the secret, not the
--     status, not the type, not the user. A caller who somehow held a factor id
--     learns which tenant it belongs to and cannot learn whose it is.
--   * There is no reverse direction. It cannot be used to enumerate a user's
--     factors, or an organization's, because it takes exactly one id and
--     returns exactly one column.
--   * A guessed id returns NULL, which is what a non-existent id returns, so it
--     is not an existence oracle for anything more than "this uuid is a factor",
--     and the caller had to guess a v4 uuid to ask.
--
-- The alternative considered was threading the org id through
-- `mfa.Verifier`, which would remove the need for this entirely. It was
-- rejected for `Verify` because the interface is `P3-02`'s and already shipped,
-- and widening a four-method interface across three implementations to avoid
-- one narrow function is the larger change. `EnrolledFactors.Confirmed` DID get
-- the org threaded through it in this task, because that one had a single
-- caller and no such cost — so this function is the residue of a seam that is
-- genuinely load-bearing rather than a default reach for privilege.
CREATE OR REPLACE FUNCTION mfa_factor_org(p_factor_id uuid)
RETURNS uuid
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT f.org_id
      FROM user_mfa_factors f
     WHERE f.id = p_factor_id;
$$;

REVOKE ALL ON FUNCTION mfa_factor_org(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION mfa_factor_org(uuid) TO auth_app;

COMMENT ON FUNCTION mfa_factor_org(uuid) IS
  'Bootstrap only: resolves a factor id to its organization before a tenant is set. Returns the org id and nothing else (P3-03).';

-- BL-08: the tables whose cleanup code nothing could call.
--
-- Four tables are written on a request path and had no reachable sweeper:
--
--   saml_authn_requests   one row per SAML login; Consume only sets consumed_at
--   saml_assertion_ids    one row per issued assertion
--   user_tokens           one row per invite, reset and verification link
--   refresh_tokens        one row per issued refresh token, and one more per
--                         rotation, because rotation inserts rather than updates
--
-- `idempotency_records` had a function and no caller. `sessions` is the one
-- table of this shape that was already swept, which is why its function is the
-- model for these.
--
-- WHY FUNCTIONS AND NOT A DELETE
--
-- Every one of these tables is under row-level security. A sweeper is
-- instance-wide maintenance with no tenant to be, and `postgres.withScope`
-- sets `app.current_org_id` to the empty string for instance scope — so
-- `current_org_id()` is NULL, every policy evaluates false, and a plain DELETE
-- reports success having removed nothing. No error anywhere. The table grows
-- forever while a green log line says it was swept.
--
-- That is not hypothetical: `saml.Requests.Prune` and `saml.Replay.Prune` were
-- written as plain DELETEs, and the same mistake in a read shipped twice in
-- P4-08 before being caught on staging.
--
-- So: SECURITY DEFINER, a pinned search_path, EXECUTE granted only to
-- auth_app, and each one narrowed to exactly its question. Note what they
-- cannot do — none takes an organization, none returns row contents, and none
-- touches a row that is still live. A row somebody could still use is not
-- reachable through any of them, so none becomes a way to erase a credential,
-- a pending login, or the evidence of one.
--
-- WHY EACH WINDOW IS WHAT IT IS
--
-- `saml_authn_requests` and `saml_assertion_ids` are deleted at expiry with no
-- margin. Both are refused by two independent checks once expired — the window
-- in the assertion and the row itself — so a kept row protects nothing and
-- costs a page of memory on the login path.
--
-- `user_tokens` keeps a margin. The row is what tells an operator that an
-- invitation was sent and used, and "the link I was sent does not work" is a
-- question asked days later.
--
-- `refresh_tokens` keeps the longest margin, because the row is the lineage
-- P3-06's reuse detection walks. An EXPIRED token needs none — it is refused
-- for expiry before lineage is consulted — but a family's history is what an
-- incident investigation reads, and deleting it at expiry would remove the
-- record at the moment a stolen token surfaces. The caller passes the cutoff;
-- these functions only agree to delete what is already past it.
--
-- EXPAND/CONTRACT: additive. Four functions. The previous version calls none
-- of them, and no table, column or constraint changes.

BEGIN;

-- --- SAML pending requests ---------------------------------------------------

CREATE OR REPLACE FUNCTION sweep_saml_authn_requests(before timestamptz, max_rows int)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    removed integer;
BEGIN
    WITH doomed AS (
        SELECT id
          FROM saml_authn_requests
         WHERE expires_at < before
         LIMIT max_rows
    ), gone AS (
        DELETE FROM saml_authn_requests r
              USING doomed d
              WHERE r.id = d.id
          RETURNING 1
    )
    SELECT count(*) INTO removed FROM gone;

    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION sweep_saml_authn_requests(timestamptz, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sweep_saml_authn_requests(timestamptz, int) TO auth_app;

COMMENT ON FUNCTION sweep_saml_authn_requests(timestamptz, int) IS
  'BL-08: deletes AuthnRequests past expires_at. Instance-wide, bounded per call.';

-- --- SAML assertion ids ------------------------------------------------------

CREATE OR REPLACE FUNCTION sweep_saml_assertion_ids(before timestamptz, max_rows int)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    removed integer;
BEGIN
    WITH doomed AS (
        SELECT assertion_id
          FROM saml_assertion_ids
         WHERE expires_at < before
         LIMIT max_rows
    ), gone AS (
        DELETE FROM saml_assertion_ids a
              USING doomed d
              WHERE a.assertion_id = d.assertion_id
          RETURNING 1
    )
    SELECT count(*) INTO removed FROM gone;

    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION sweep_saml_assertion_ids(timestamptz, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sweep_saml_assertion_ids(timestamptz, int) TO auth_app;

COMMENT ON FUNCTION sweep_saml_assertion_ids(timestamptz, int) IS
  'BL-08: deletes assertion ids past expires_at. An expired assertion is refused by its own window regardless.';

-- --- invite, reset and verification links ------------------------------------

CREATE OR REPLACE FUNCTION sweep_user_tokens(before timestamptz, max_rows int)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    removed integer;
BEGIN
    WITH doomed AS (
        SELECT id
          FROM user_tokens
         WHERE expires_at < before
         LIMIT max_rows
    ), gone AS (
        DELETE FROM user_tokens t
              USING doomed d
              WHERE t.id = d.id
          RETURNING 1
    )
    SELECT count(*) INTO removed FROM gone;

    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION sweep_user_tokens(timestamptz, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sweep_user_tokens(timestamptz, int) TO auth_app;

COMMENT ON FUNCTION sweep_user_tokens(timestamptz, int) IS
  'BL-08: deletes invite and reset links past expires_at. The caller keeps a margin so a support question still has a row to read.';

-- --- refresh tokens ----------------------------------------------------------
--
-- Bounded by expires_at only. A revoked-but-unexpired token is NOT deletable
-- here, deliberately: it is exactly the row reuse detection needs in order to
-- tell a stolen token from a new one.

CREATE OR REPLACE FUNCTION sweep_refresh_tokens(before timestamptz, max_rows int)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    removed integer;
BEGIN
    WITH doomed AS (
        SELECT id
          FROM refresh_tokens
         WHERE expires_at < before
         LIMIT max_rows
    ), gone AS (
        DELETE FROM refresh_tokens t
              USING doomed d
              WHERE t.id = d.id
          RETURNING 1
    )
    SELECT count(*) INTO removed FROM gone;

    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION sweep_refresh_tokens(timestamptz, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sweep_refresh_tokens(timestamptz, int) TO auth_app;

COMMENT ON FUNCTION sweep_refresh_tokens(timestamptz, int) IS
  'BL-08: deletes refresh tokens past expires_at. A revoked but unexpired token is kept: it is the lineage reuse detection walks.';

COMMIT;

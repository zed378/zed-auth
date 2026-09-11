-- CORS lookups that must work before a tenant is known (P1-29).
--
-- Two of them, and they answer deliberately different questions.
--
-- `application_by_client_id` gains `allowed_origins`. It is the same
-- bootstrap read P1-06 added, extended with one more public column: an
-- application's registered origins are no more secret than its redirect URIs,
-- and the function still returns no secret columns at any privilege level.
--
-- `origin_is_registered` exists for the CORS PREFLIGHT, which is the one
-- request in the system that arrives with no credential at all. A browser
-- sends `OPTIONS` with an `Origin` header and no `Authorization` header —
-- that is the specification, not an omission — so there is no token to resolve
-- an application from, and the per-application check cannot be made yet.
--
-- What this function discloses, to a caller who can already reach the service,
-- is whether SOME application somewhere registered a given origin. That is a
-- single bit about a value the asker already knows, and the actual request
-- that follows is still checked against the specific application's own list.
-- The alternative — refusing every preflight — means no browser client works
-- at all, which is the state PG-17 recorded.
-- DROP first: `CREATE OR REPLACE` cannot change a function's return type, and
-- this adds a column to the RETURNS TABLE. Dropping and recreating inside one
-- migration is atomic — the transaction either has the new signature or the
-- old one, never neither.
DROP FUNCTION IF EXISTS application_by_client_id(uuid);

CREATE OR REPLACE FUNCTION application_by_client_id(client uuid)
RETURNS TABLE (
    id                        uuid,
    org_id                    uuid,
    project_id                uuid,
    name                      text,
    type                      text,
    redirect_uris             jsonb,
    post_logout_redirect_uris jsonb,
    grant_types               jsonb,
    allowed_origins           jsonb
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT a.id, a.org_id, a.project_id, a.name, a.type,
           to_jsonb(a.redirect_uris), to_jsonb(a.post_logout_redirect_uris),
           to_jsonb(a.grant_types), to_jsonb(a.allowed_origins)
      FROM applications a
     WHERE a.id = client;
$$;

REVOKE ALL ON FUNCTION application_by_client_id(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION application_by_client_id(uuid) TO auth_app;

COMMENT ON FUNCTION application_by_client_id(uuid) IS
  'Bootstrap only: resolves a public client_id before a tenant is known. Returns no secret columns (P1-06, P1-29).';

CREATE OR REPLACE FUNCTION origin_is_registered(origin text)
RETURNS boolean
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM applications a WHERE origin = ANY (a.allowed_origins)
    );
$$;

REVOKE ALL ON FUNCTION origin_is_registered(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION origin_is_registered(text) TO auth_app;

COMMENT ON FUNCTION origin_is_registered(text) IS
  'Preflight only: whether any application registers this origin. The actual request is still checked per application (P1-29).';

-- The preflight lookup is a scan of every application without it. Small today,
-- and this is the cheapest moment to make it not matter.
CREATE INDEX IF NOT EXISTS applications_allowed_origins_idx
    ON applications USING gin (allowed_origins);

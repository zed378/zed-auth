-- Resolves a client_id to its application, before a tenant is known.
--
-- The same bootstrap problem sessions have (P1-11), reached from the other
-- direction. /oauth/authorize receives a client_id in a URL and must resolve it
-- to decide which organization the request belongs to — so the read cannot be
-- tenant-scoped, and `applications_tenant_isolation` is
-- `org_id = current_org_id()`, which matches nothing with no tenant set.
--
-- Same answer, same pattern as P0-12's partition maintenance: SECURITY DEFINER,
-- pinned search_path, granted to auth_app, doing exactly one thing.
--
-- The disclosure argument is different from the session one and worth stating.
-- A client_id is public by construction: it travels in the query string of
-- every authorization request, in browser history, and in the consumer
-- application's own source. Resolving one is not a privileged act. What this
-- function must not do is return anything that ISN'T public, so the secret
-- columns are absent from its result — a caller cannot obtain
-- client_secret_hash through it at any privilege level.
CREATE OR REPLACE FUNCTION application_by_client_id(client uuid)
RETURNS TABLE (
    id                        uuid,
    org_id                    uuid,
    project_id                uuid,
    name                      text,
    type                      text,
    redirect_uris             jsonb,
    post_logout_redirect_uris jsonb,
    grant_types               jsonb
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
STABLE
AS $$
    SELECT a.id, a.org_id, a.project_id, a.name, a.type,
           to_jsonb(a.redirect_uris), to_jsonb(a.post_logout_redirect_uris),
           to_jsonb(a.grant_types)
      FROM applications a
     WHERE a.id = client;
$$;

REVOKE ALL ON FUNCTION application_by_client_id(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION application_by_client_id(uuid) TO auth_app;

COMMENT ON FUNCTION application_by_client_id(uuid) IS
  'Bootstrap only: resolves a public client_id before a tenant is known. Returns no secret columns (P1-06).';

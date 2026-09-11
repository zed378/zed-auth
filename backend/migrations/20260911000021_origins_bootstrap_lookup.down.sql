DROP INDEX IF EXISTS applications_allowed_origins_idx;
DROP FUNCTION IF EXISTS origin_is_registered(text);

-- Back to the P1-06 signature, without allowed_origins.
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

-- P4-08: resolve a SAML service provider before the tenant is known.
--
-- Same problem OIDC has, and the same answer. An `AuthnRequest` arrives naming
-- an `Issuer`; that entity ID belongs to a service provider, which belongs to
-- an application, which belongs to an organization — so the tenant is known
-- FROM the request, but not until this lookup has run. A tenant-scoped read
-- cannot perform it, because there is no tenant to scope to yet.
--
-- ADR-023 settled the shape for OIDC: `application_by_client_id()`, a
-- `SECURITY DEFINER` function bounded to exactly the columns tenant resolution
-- needs. This is the SAML twin, and it is deliberately NOT a general "read any
-- service provider" function:
--
--   It matches one entity ID, exactly, and returns one row. No listing, no
--   pattern, no enumeration.
--
--   It returns only what the flow needs: the organization, the application,
--   the ACS URL an assertion may be posted to, the attributes that service
--   provider is registered to receive, and whether its requests must be
--   signed. Not the whole row.
--
--   It ignores revoked registrations. A decommissioned service provider keeps
--   its row for the history; it does not keep its ability to start a login.
--
-- The alternative — `WithInstanceScope` on every SAML login — would be both
-- broader (the whole table, under a role that bypasses RLS) and noisier (that
-- path writes an audit event per use, by design, because it is meant to be
-- uncomfortable). Uncomfortable is right for an operator listing tenants and
-- wrong for a login.
--
-- EXPAND/CONTRACT: additive. One function. Nothing reads it in the previous
-- version.

BEGIN;

CREATE OR REPLACE FUNCTION service_provider_by_entity_id(lookup_entity_id text)
RETURNS TABLE (
    sp_id             uuid,
    org_id            uuid,
    application_id    uuid,
    entity_id         text,
    acs_url           text,
    attribute_release text[],
    want_signed_requests boolean,
    certificate       text
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT sp.id, sp.org_id, sp.application_id, sp.entity_id, sp.acs_url,
           sp.attribute_release, sp.want_signed_requests, sp.certificate
      FROM saml_service_providers sp
     WHERE sp.entity_id = lookup_entity_id
       AND sp.revoked_at IS NULL;
$$;

REVOKE ALL ON FUNCTION service_provider_by_entity_id(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION service_provider_by_entity_id(text) TO auth_app;

-- The same, by the registration's own id.
--
-- Used when a pending AuthnRequest is answered. The pending row recorded WHICH
-- registration the login was started against, not merely which entity id, and
-- that distinction matters: a service provider revoked and re-registered under
-- the same entity id is a different row, and a login started against the old
-- one must not be completed against the new. An entity id is a name; the row is
-- the thing.
--
-- It needs to be a function for the same reason as the lookup above — the
-- caller has no tenant scope at the moment it runs — and is bounded the same
-- way: one id, live registrations only, the columns the flow needs.
CREATE OR REPLACE FUNCTION service_provider_by_id(lookup_id uuid)
RETURNS TABLE (
    sp_id             uuid,
    org_id            uuid,
    application_id    uuid,
    entity_id         text,
    acs_url           text,
    attribute_release text[],
    want_signed_requests boolean,
    certificate       text
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT sp.id, sp.org_id, sp.application_id, sp.entity_id, sp.acs_url,
           sp.attribute_release, sp.want_signed_requests, sp.certificate
      FROM saml_service_providers sp
     WHERE sp.id = lookup_id
       AND sp.revoked_at IS NULL;
$$;

REVOKE ALL ON FUNCTION service_provider_by_id(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION service_provider_by_id(uuid) TO auth_app;

COMMENT ON FUNCTION service_provider_by_id(uuid) IS
    'P4-08: resolve the registration a pending AuthnRequest was recorded against. A re-registered entity id is a different row.';

COMMENT ON FUNCTION service_provider_by_entity_id(text) IS
    'P4-08: tenant resolution for SAML, the twin of application_by_client_id. One exact entity ID, live registrations only, and only the columns the flow needs.';

COMMIT;

-- P4-08 follow-up: let the login page read the AuthnRequest it is rendering for.
--
-- `Requests.Peek` read `saml_authn_requests` directly, under `WithInstanceScope`
-- because the login page has no tenant yet — the browser arrives with a request
-- id and no session. Instance scope sets `current_org_id()` to NULL, under which
-- `saml_authn_requests_tenant_isolation` matches no row at all, so the read
-- returned nothing and the page answered "Nothing to sign in to".
--
-- The same shape `ByID` had and migration 043 fixed. It was missed here because
-- no test drives the login PAGE for a SAML request: the integration suite calls
-- `Resume` directly with a session, and the acceptance suite — which goes
-- through the real page — is what found it, on staging, after the feature was
-- already merged.
--
-- Bounded the way 043's functions are, and narrower than they are, because the
-- login page needs less:
--
--   One id, exactly. No listing, no pattern, no enumeration.
--
--   It returns nothing a rendered page should not show. In particular NOT
--   `relay_state`, which is the service provider's own opaque state and has no
--   business in HTML — `Peek`'s doc comment already said so and the query
--   already honoured it; the function keeps that property rather than trusting
--   the caller to.
--
--   It does not filter on `consumed_at` or `expires_at`. The caller decides
--   what a consumed or expired request means, and answers differently for each
--   — a function that silently returned no row would collapse "already
--   answered" and "never existed" into one message.
--
-- EXPAND/CONTRACT: additive. One function; the previous version does not call it.

BEGIN;

CREATE OR REPLACE FUNCTION saml_authn_request_for_login(lookup_id text)
RETURNS TABLE (
    id                      text,
    sp_id                   uuid,
    requested_authn_context text,
    idp_initiated           boolean,
    created_at              timestamptz,
    expires_at              timestamptz,
    consumed_at             timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT r.id, r.sp_id, r.requested_authn_context, r.idp_initiated,
           r.created_at, r.expires_at, r.consumed_at
      FROM saml_authn_requests r
     WHERE r.id = lookup_id;
$$;

REVOKE ALL ON FUNCTION saml_authn_request_for_login(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION saml_authn_request_for_login(text) TO auth_app;

COMMENT ON FUNCTION saml_authn_request_for_login(text) IS
    'P4-08: read a pending AuthnRequest for the login page, which has no tenant yet. No relay_state: it is the SP''s opaque state and never belongs in a page.';

COMMIT;

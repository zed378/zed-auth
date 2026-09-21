-- P4-07: the SAML service providers this instance issues assertions to, and the
-- assertion IDs it has already issued.
--
-- Two tables, and they answer two different questions.
--
-- `saml_service_providers` is the registration: which application a service
-- provider belongs to, where its assertions may be POSTed, what it is allowed to
-- receive, and the certificate its own signed requests are verified against. The
-- ACS URL is held to the same exact-match discipline as an OIDC redirect URI,
-- because it is the same open-redirect class of bug in a different protocol.
--
-- `saml_assertion_ids` is replay protection. A SAML assertion is a bearer
-- credential for the duration of its validity window, and a consumer that does
-- not remember which ones it has seen accepts the same one twice — which is the
-- whole attack, and needs nothing more than a copy of a legitimate login.
--
-- EXPAND/CONTRACT: additive. Two new tables and nothing else; a previous-version
-- instance neither reads nor writes them.

BEGIN;

CREATE TABLE saml_service_providers (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    org_id         uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    -- The service provider's own identifier, opaque and compared exactly. It is
    -- unique across the instance rather than per organization: an entity ID is
    -- a global name in SAML, and two organizations claiming the same one would
    -- make "which service provider is this assertion for" ambiguous at exactly
    -- the moment it matters.
    entity_id      text NOT NULL,

    -- Where an assertion may be sent. Exact match, never a prefix: a prefix
    -- match on a URL is how an open redirect is written by accident.
    acs_url        text NOT NULL,

    -- What this service provider receives. Empty means the NameID and nothing
    -- else, which is the correct default for a party that has not asked for
    -- anything — the same discipline /oauth/userinfo applies to scopes.
    attribute_release text[] NOT NULL DEFAULT '{}',

    -- Whether this service provider's AuthnRequests must be signed. Per
    -- provider because it is a property of what the other side can do, not a
    -- policy this service gets to set alone.
    want_signed_requests boolean NOT NULL DEFAULT false,

    -- The service provider's certificate, for verifying those requests. PEM.
    certificate    text,

    -- Whether this service provider accepts an IdP-initiated login (P4-08).
    --
    -- Off by default, and that default is the point. An IdP-initiated
    -- assertion answers no request, so there is nothing for the service
    -- provider to correlate it against — which is the same shape as a CSRF:
    -- an attacker who can make a browser visit this service's initiation URL
    -- can have an assertion delivered to a service provider that never asked
    -- for one. Some service providers handle that; many log the user in.
    --
    -- So it is a decision somebody makes per integration, in the open, rather
    -- than a capability every registration quietly has.
    allow_idp_initiated boolean NOT NULL DEFAULT false,

    created_at     timestamptz NOT NULL DEFAULT now(),
    revoked_at     timestamptz,

    CONSTRAINT saml_sp_entity_id_present CHECK (length(trim(entity_id)) > 0),
    CONSTRAINT saml_sp_acs_url_https CHECK (acs_url LIKE 'https://%'),
    CONSTRAINT saml_sp_signed_needs_cert
        CHECK (NOT want_signed_requests OR certificate IS NOT NULL)
);

-- One live registration per entity ID. A revoked one keeps its row, so the
-- history of a decommissioned integration survives, and the partial index lets
-- the same entity ID be registered again afterwards.
CREATE UNIQUE INDEX saml_service_providers_entity_id_live
    ON saml_service_providers (entity_id)
    WHERE revoked_at IS NULL;

CREATE INDEX saml_service_providers_application_idx
    ON saml_service_providers (application_id);

COMMENT ON TABLE saml_service_providers IS
    'P4-07: registered SAML service providers. acs_url is matched exactly, like an OIDC redirect URI.';

ALTER TABLE saml_service_providers ENABLE ROW LEVEL SECURITY;

CREATE POLICY saml_service_providers_tenant_isolation ON saml_service_providers
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON saml_service_providers TO auth_app;


CREATE TABLE saml_assertion_ids (
    -- The assertion's own ID, as issued. The primary key IS the protection: a
    -- second insert of the same ID violates it, which is the refusal, and it
    -- cannot race because the database serialises it. A SELECT-then-INSERT
    -- would have a window between the two, and that window is the replay.
    assertion_id text PRIMARY KEY,

    org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    -- Written by the application, from the same clock that computes expires_at.
    -- A CHECK comparing two timestamps only means what it says when one clock
    -- wrote both — scripts/check.sh enforces that, after this repository spent
    -- three days on a constraint that was really asserting something about
    -- clock skew.
    issued_at  timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,

    CONSTRAINT saml_assertion_ids_expiry_after_issue CHECK (expires_at > issued_at)
);

-- Pruning reads this, and only this.
CREATE INDEX saml_assertion_ids_expires_at_idx ON saml_assertion_ids (expires_at);

COMMENT ON TABLE saml_assertion_ids IS
    'P4-07: assertion IDs already issued or consumed. The primary key is the replay defence; rows are pruned after expiry.';

ALTER TABLE saml_assertion_ids ENABLE ROW LEVEL SECURITY;

CREATE POLICY saml_assertion_ids_tenant_isolation ON saml_assertion_ids
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- No UPDATE. An assertion ID is written once and read until it is pruned;
-- rewriting one would be a way to make a consumed assertion look fresh.
GRANT SELECT, INSERT, DELETE ON saml_assertion_ids TO auth_app;

COMMIT;

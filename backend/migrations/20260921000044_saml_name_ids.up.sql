-- P4-08: a persistent, per-service-provider identifier for a user.
--
-- The threat review's C-5 is blunt: `NameID` must never default to email. Two
-- separate failures follow from an email NameID, and both are permanent by the
-- time anybody notices.
--
--   A service provider keyed on an address hands a deactivated user's account
--   to whoever is issued that address next. `budi@company.test` leaves, the
--   address is reassigned, and the new holder inherits everything the old one
--   had at every service provider that trusted the address as an identity.
--
--   The same address across two service providers lets them correlate a user
--   who never agreed to be correlated. That is the property `persistent`
--   NameID format exists to avoid, and the reason it is per-SP rather than
--   global.
--
-- So: a random identifier per (user, service provider), stored.
--
-- # Why stored rather than derived
--
-- The obvious alternative is `HMAC(secret, user_id || entity_id)` — stable, no
-- table, no writes on the login path. It was rejected because of what the
-- secret becomes: a value that can never be rotated. Rotating it changes every
-- NameID this service has ever issued, which silently re-identifies every user
-- at every service provider as a stranger. A secret that cannot be rotated is
-- not a secret that is safe to hold for years, and "we can never change this
-- key" is a worse property than a table.
--
-- Stored also makes the linkage visible: an operator can see that a user is
-- known to a service provider, and a future card can offer to break the link
-- deliberately. A derived identifier offers neither.
--
-- EXPAND/CONTRACT: additive. One table, written on first sign-in to each
-- service provider. A previous-version instance neither reads nor writes it.

BEGIN;

CREATE TABLE saml_name_ids (
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    sp_id     uuid NOT NULL REFERENCES saml_service_providers (id) ON DELETE CASCADE,
    org_id    uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    -- The identifier the service provider knows this user by. Opaque, random,
    -- and meaningless outside the pair it belongs to.
    name_id   text NOT NULL,

    created_at timestamptz NOT NULL,

    -- One identifier per pair. The primary key is what makes the value stable:
    -- a second sign-in finds the row rather than minting a new identity, and a
    -- race between two sign-ins collides here rather than issuing two.
    PRIMARY KEY (user_id, sp_id),

    -- Unique per service provider, so two users are never the same subject to
    -- one of them. Across service providers the values are unrelated, which is
    -- the whole point.
    CONSTRAINT saml_name_ids_unique_per_sp UNIQUE (sp_id, name_id),

    CONSTRAINT saml_name_ids_opaque CHECK (length(name_id) BETWEEN 16 AND 255),

    -- Never an address. A CHECK rather than a comment, because the failure this
    -- prevents is silent and permanent: nothing downstream can tell an email
    -- NameID from a pseudonymous one, and a service provider that keyed on it
    -- has already stored it by the time anyone looks.
    CONSTRAINT saml_name_ids_not_an_address CHECK (name_id NOT LIKE '%@%')
);

CREATE INDEX saml_name_ids_org_idx ON saml_name_ids (org_id);

COMMENT ON TABLE saml_name_ids IS
    'P4-08: the persistent, per-service-provider identifier a user is known by. Never an email address (threat review C-5).';

ALTER TABLE saml_name_ids ENABLE ROW LEVEL SECURITY;

CREATE POLICY saml_name_ids_tenant_isolation ON saml_name_ids
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- No UPDATE. An identifier is minted once and read afterwards; changing one in
-- place would re-identify a user at a service provider with nothing recording
-- that it happened. Breaking a linkage deliberately is a DELETE, which leaves
-- the next sign-in to mint a new one.
GRANT SELECT, INSERT, DELETE ON saml_name_ids TO auth_app;

COMMIT;

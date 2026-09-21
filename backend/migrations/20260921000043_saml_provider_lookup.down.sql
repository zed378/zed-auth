-- Drops the SAML tenant-resolution function.
--
-- Registrations are untouched; without this function nothing can resolve one
-- from an AuthnRequest, so SAML logins stop. That is the intended effect of
-- removing it.

DROP FUNCTION IF EXISTS service_provider_by_id(uuid);
DROP FUNCTION IF EXISTS service_provider_by_entity_id(text);

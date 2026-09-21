-- Removes the SAML certificate column.
--
-- EXPAND/CONTRACT: this DROPs a column, so it is destructive in the one
-- direction that matters — any SAML key's certificate is lost, and a service
-- provider that pinned it can no longer verify anything this service signs.
-- That is acceptable only because the column exists solely for SAML, and
-- running this down migration means SAML is being removed.
--
-- The CHECK constraints go first so the column can leave without them.

ALTER TABLE signing_keys DROP CONSTRAINT IF EXISTS signing_keys_certificate_is_not_private;
ALTER TABLE signing_keys DROP CONSTRAINT IF EXISTS signing_keys_saml_needs_certificate;
ALTER TABLE signing_keys DROP COLUMN IF EXISTS certificate;

-- Drops the per-service-provider identifiers.
--
-- Destructive in the way that matters: every service provider knows its users
-- by these values, and re-creating the table mints new ones, so every user
-- appears as a stranger at every SAML integration. Running this down migration
-- means SAML is being removed, and the accounts on the other side go with it.

DROP TABLE IF EXISTS saml_name_ids;

-- Drops the SAML registration and replay tables.
--
-- The assertion IDs go with them, which means a replay that was refused before
-- this ran would be accepted afterwards. That is the correct behaviour for a
-- down migration — the feature is being removed, and nothing issues assertions
-- to replay — and it is stated here so nobody discovers it during an incident.

DROP TABLE IF EXISTS saml_assertion_ids;
DROP TABLE IF EXISTS saml_service_providers;

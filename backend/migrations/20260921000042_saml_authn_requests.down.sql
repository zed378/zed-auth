-- Drops the AuthnRequest table.
--
-- Any in-flight SP-initiated login loses its correlation and fails, which is
-- the correct outcome: the feature is being removed, and an assertion answering
-- a request nobody remembers is exactly what the table exists to prevent.

DROP TABLE IF EXISTS saml_authn_requests;

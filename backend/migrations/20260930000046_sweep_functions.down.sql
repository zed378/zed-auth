BEGIN;

DROP FUNCTION IF EXISTS sweep_saml_authn_requests(timestamptz, int);
DROP FUNCTION IF EXISTS sweep_saml_assertion_ids(timestamptz, int);
DROP FUNCTION IF EXISTS sweep_user_tokens(timestamptz, int);
DROP FUNCTION IF EXISTS sweep_refresh_tokens(timestamptz, int);

COMMIT;

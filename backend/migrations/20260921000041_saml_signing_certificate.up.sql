-- P4-07: a SAML signing key also needs a certificate.
--
-- OIDC publishes a JWKS: a consumer fetches the public key by `kid` and that is
-- the whole trust story. SAML publishes metadata containing an X.509
-- certificate, and a service provider pins it — `<ds:KeyDescriptor>` carries a
-- `<ds:X509Certificate>`, and the signature on every assertion carries the same
-- one in `KeyInfo`. There is no SAML equivalent of "look up the key by id".
--
-- So a SAML key is a key AND a certificate, and the certificate has to be
-- stable: a service provider that pinned one yesterday must see the same one
-- today. Deriving it on the fly at startup would be stable only for as long as
-- nobody edited the template, and a certificate that changes when a struct
-- literal is reformatted is not a certificate anybody can pin.
--
-- Nullable, because an OIDC key has no certificate and never will. The CHECK
-- makes it required exactly where it is needed, so a SAML key cannot be
-- inserted without one and then fail at the moment somebody tries to log in.
--
-- EXPAND/CONTRACT: additive. A nullable column and a CHECK that no existing row
-- can violate — there are no `saml` rows yet. A previous-version instance does
-- not read the column and keeps working.

BEGIN;

ALTER TABLE signing_keys
    ADD COLUMN IF NOT EXISTS certificate text;

ALTER TABLE signing_keys
    ADD CONSTRAINT signing_keys_saml_needs_certificate
    CHECK (purpose <> 'saml' OR certificate IS NOT NULL);

-- The same refusal the public key already carries. A certificate is public by
-- definition, so this is not about the certificate itself: it is about somebody
-- pasting a combined PEM bundle — key and certificate in one blob, which is how
-- openssl often emits them — into a column that is read into metadata and
-- published.
ALTER TABLE signing_keys
    ADD CONSTRAINT signing_keys_certificate_is_not_private
    CHECK (certificate IS NULL OR certificate NOT LIKE '%PRIVATE KEY%');

COMMENT ON COLUMN signing_keys.certificate IS
    'P4-07: the X.509 certificate published in SAML metadata, PEM. Required for purpose = saml, absent for oidc.';

COMMIT;

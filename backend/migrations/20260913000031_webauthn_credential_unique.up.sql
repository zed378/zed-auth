-- One credential belongs to one account (P3-05).
--
-- `docs/SECURITY/02` §2 names "registering a credential to another user's
-- account" as an attack, and the registration ceremony already refuses it: the
-- credential is bound to the authenticated user server-side and the request
-- names no user. This makes it a property of the DATABASE as well, so the
-- guarantee does not rest on that one code path staying correct.
--
-- It is safe for a legitimate user with several accounts: a credential id is
-- generated per (relying party, account), so one authenticator enrolled twice
-- produces two different ids. A collision is either the same credential being
-- filed twice — which is the bug this catches — or an authenticator violating
-- the specification.
--
-- Scoped to WebAuthn rows. TOTP factors have a null `credential_id` and a
-- partial index leaves them alone; a plain unique index would have been
-- satisfied by nulls in Postgres anyway, but saying which rows this is about
-- makes the intent readable.
--
-- EXPAND/CONTRACT: additive. A new index, no column touched and no constraint
-- tightened on existing data — nothing has written to `credential_id` yet, so
-- the index cannot fail to build on a live table.

CREATE UNIQUE INDEX user_mfa_factors_credential_unique
    ON user_mfa_factors (credential_id)
    WHERE credential_id IS NOT NULL;

COMMENT ON INDEX user_mfa_factors_credential_unique IS
  'One WebAuthn credential belongs to one account — docs/SECURITY/02 §2 (P3-05).';

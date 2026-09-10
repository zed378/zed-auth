-- users.email_verified_at, closing PG-18.
--
-- OpenID Connect Core defines the `email` scope as covering two claims, `email`
-- and `email_verified`, and docs/PLAN/04 § users has no column behind the
-- second. P1-08 omits the claim rather than returning `false`, which is the
-- honest answer — absent means "not asserted", `false` means "we checked and it
-- is not", and we had not checked.
--
-- A timestamp rather than a boolean, for the reason password_changed_at is one:
-- "when" answers questions "whether" cannot, and an audit needs it.
--
-- Set by P1-19.5 when an invitation is accepted through a link sent to that
-- address, which is exactly what verification means. Cleared when the address
-- changes, because a verification is a statement about one address and not
-- about the user.
--
-- Additive and nullable: the previous version of the application neither reads
-- nor writes it, so a rollback mid-deploy leaves a working schema
-- (docs/PLAN/14 § Rollback Strategy).
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;

COMMENT ON COLUMN users.email_verified_at IS
  'When this address was proven reachable, by accepting an invitation sent to it (P1-19.5, PG-18). NULL means not asserted, which is what OIDC omits the email_verified claim for.';

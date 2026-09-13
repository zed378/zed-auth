-- Reverses 20260913000031.
--
-- Dropping the index removes a guarantee rather than data: the registration
-- ceremony still binds a credential to the authenticated user, so a rollback
-- returns to that check being the only thing enforcing it.

DROP INDEX IF EXISTS user_mfa_factors_credential_unique;

-- Removes the factor-to-organization resolver (P3-03).
--
-- Safe to run against a previous-version instance: nothing but the MFA
-- challenge step calls it, and a build without that step never looks for it.
DROP FUNCTION IF EXISTS mfa_factor_org(uuid);

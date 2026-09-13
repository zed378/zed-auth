-- Reverses 20260913000030.
--
-- Dropping the table destroys every outstanding recovery code. That is a
-- rollback to a version with no recovery path rather than a routine operation:
-- a user whose device is already lost, and who was holding a code they had not
-- yet used, has nothing left but the administrator-assisted path.

DROP TRIGGER IF EXISTS user_recovery_codes_org_matches_user ON user_recovery_codes;
DROP FUNCTION IF EXISTS user_recovery_codes_org_matches_user();
DROP TABLE IF EXISTS user_recovery_codes;

-- Reverses 20260912000027.
--
-- Dropping the table takes every enrolled factor with it, which is why this is
-- a rollback to a version that had no multi-factor authentication rather than
-- a routine operation. A deployment that rolls back past this and forward
-- again leaves every user re-enrolling.

DROP TRIGGER IF EXISTS user_factors_org_matches_user ON user_factors;
DROP FUNCTION IF EXISTS user_factors_org_matches_user();
DROP TABLE IF EXISTS user_factors;

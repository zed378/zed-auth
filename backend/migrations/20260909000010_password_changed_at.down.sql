-- Drops the column. Loses the recorded change times, so max_age_days becomes
-- unenforceable again; no user is locked out and no login breaks.
ALTER TABLE users DROP COLUMN password_changed_at;

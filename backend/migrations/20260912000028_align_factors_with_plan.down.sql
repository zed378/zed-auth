-- Reverses 20260912000028.
--
-- Back to P3-01's names. The WebAuthn columns are dropped with the rest: they
-- held nothing, because nothing wrote them before P3-05.

DROP TRIGGER IF EXISTS user_mfa_factors_sync_flag ON user_mfa_factors;
DROP FUNCTION IF EXISTS sync_user_mfa_enabled();

DROP TRIGGER IF EXISTS user_mfa_factors_org_matches_user ON user_mfa_factors;
DROP FUNCTION IF EXISTS user_mfa_factors_org_matches_user();

ALTER TABLE user_mfa_factors DROP COLUMN IF EXISTS last_used_counter;
ALTER TABLE user_mfa_factors DROP COLUMN IF EXISTS sign_count;
ALTER TABLE user_mfa_factors DROP COLUMN IF EXISTS public_key;
ALTER TABLE user_mfa_factors DROP COLUMN IF EXISTS credential_id;

ALTER TABLE user_mfa_factors ADD COLUMN confirmed_at timestamptz;
UPDATE user_mfa_factors SET confirmed_at = now() WHERE status = 'active';
ALTER TABLE user_mfa_factors DROP CONSTRAINT IF EXISTS user_mfa_factors_status_valid;
ALTER TABLE user_mfa_factors DROP COLUMN status;

ALTER TABLE user_mfa_factors RENAME COLUMN secret_encrypted TO secret;
ALTER TABLE user_mfa_factors RENAME TO user_factors;

DROP INDEX IF EXISTS user_mfa_factors_user_idx;
CREATE INDEX user_factors_user_idx ON user_factors (user_id, confirmed_at);
ALTER INDEX IF EXISTS user_mfa_factors_org_idx  RENAME TO user_factors_org_idx;
ALTER INDEX IF EXISTS user_mfa_factors_one_totp RENAME TO user_factors_one_totp;

ALTER TABLE user_factors RENAME CONSTRAINT user_mfa_factors_type_valid TO user_factors_type_valid;
ALTER TABLE user_factors RENAME CONSTRAINT user_mfa_factors_label_bounded TO user_factors_label_bounded;
ALTER TABLE user_factors RENAME CONSTRAINT user_mfa_factors_secret_not_empty TO user_factors_secret_not_empty;
ALTER TABLE user_factors RENAME CONSTRAINT user_mfa_factors_data_is_object TO user_factors_data_is_object;

ALTER POLICY user_mfa_factors_tenant_isolation ON user_factors RENAME TO user_factors_tenant_isolation;

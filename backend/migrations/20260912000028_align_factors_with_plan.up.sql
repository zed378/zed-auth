-- Rename the factor table to the one `docs/PLAN/04` specifies (P3-02).
--
-- `P3-01` created `user_factors` with a `secret` column and a `confirmed_at`
-- timestamp. `docs/PLAN/04-DATA-MODEL.md` § user_mfa_factors specifies
-- `user_mfa_factors`, `secret_encrypted`, and a `status` enum of `pending` /
-- `active`. Neither shape is better; the plan's is the one written down, and
-- `CLAUDE.md` is explicit that a deviation should be a visible decision rather
-- than a silent one. This was silent, so it is corrected rather than defended.
--
-- EXPAND/CONTRACT: this migration is deliberately NOT expand/contract, and the
-- justification is that the rule has nothing to protect here.
--
-- `docs/PLAN/14`'s expand/contract pattern exists so a running previous-version
-- instance keeps working mid-rollout. The previous version of this table was
-- created in the immediately preceding commit; **no deployed instance has ever
-- read or written it**, because nothing calls the code that would. There is no
-- running reader to break.
--
-- The alternative — add the new names, dual-write, backfill, drop the old ones
-- in a later release — would spend three migrations and a window where both
-- names exist, to protect a reader that does not exist. And every day it is
-- deferred makes it harder: the moment `P3-02` stores a real secret, a rename
-- stops being free.
--
-- This is the one moment where correcting the name costs nothing. Taken.

ALTER TABLE user_factors RENAME TO user_mfa_factors;

ALTER TABLE user_mfa_factors RENAME COLUMN secret TO secret_encrypted;

-- `status`, replacing `confirmed_at`.
--
-- The plan's enum says the same thing the timestamp did — an enrolment that has
-- not been proven is not a factor — and says it in a word rather than in the
-- absence of a value. `last_used_at` already carries "when", which is what a
-- timestamp is for; `confirmed_at` was carrying a boolean in a timestamp's
-- clothes.
ALTER TABLE user_mfa_factors
    ADD COLUMN status text NOT NULL DEFAULT 'pending';

UPDATE user_mfa_factors SET status = 'active' WHERE confirmed_at IS NOT NULL;

ALTER TABLE user_mfa_factors DROP COLUMN confirmed_at;

ALTER TABLE user_mfa_factors
    ADD CONSTRAINT user_mfa_factors_status_valid CHECK (status IN ('pending', 'active'));

-- The last counter step a verification consumed (P3-02 step 6).
--
-- A TOTP code is valid for its whole 30-second step, so without this it is
-- accepted as many times as it is presented — which turns a shoulder-surfed
-- code into a login for the rest of its window.
--
-- The COUNTER, never the code. The code is a live credential until its step
-- ends, and storing it to prevent the reuse of a credential would be storing a
-- credential. The counter is a small integer that reveals only when somebody
-- last authenticated, which `last_used_at` already says.
--
-- Monotonic: a verification is recorded only when its step is strictly greater
-- than the last one, so an OLD code from a step already passed is refused too
-- — not only the exact code just used.
ALTER TABLE user_mfa_factors ADD COLUMN last_used_counter bigint;

-- WebAuthn's own columns, from the plan. Null for TOTP, and `P3-05` fills them.
--
-- Added now rather than by `P3-05` because the plan names them here and a table
-- that gains a column per factor type is the shape this design exists to avoid
-- — better to have three nullable columns visible and unused than to discover
-- the table needs widening the week WebAuthn lands.
ALTER TABLE user_mfa_factors ADD COLUMN credential_id text;
ALTER TABLE user_mfa_factors ADD COLUMN public_key    text;
ALTER TABLE user_mfa_factors ADD COLUMN sign_count    bigint;

-- The indexes and constraints follow the rename automatically; only the ones
-- naming the dropped column need rebuilding.
DROP INDEX IF EXISTS user_factors_user_idx;
CREATE INDEX user_mfa_factors_user_idx ON user_mfa_factors (user_id, status);

ALTER INDEX IF EXISTS user_factors_org_idx  RENAME TO user_mfa_factors_org_idx;
ALTER INDEX IF EXISTS user_factors_one_totp RENAME TO user_mfa_factors_one_totp;

ALTER TABLE user_mfa_factors
    RENAME CONSTRAINT user_factors_type_valid TO user_mfa_factors_type_valid;
ALTER TABLE user_mfa_factors
    RENAME CONSTRAINT user_factors_label_bounded TO user_mfa_factors_label_bounded;
ALTER TABLE user_mfa_factors
    RENAME CONSTRAINT user_factors_secret_not_empty TO user_mfa_factors_secret_not_empty;
ALTER TABLE user_mfa_factors
    RENAME CONSTRAINT user_factors_data_is_object TO user_mfa_factors_data_is_object;

ALTER POLICY user_factors_tenant_isolation ON user_mfa_factors
    RENAME TO user_mfa_factors_tenant_isolation;

-- The trigger and its function.
DROP TRIGGER IF EXISTS user_factors_org_matches_user ON user_mfa_factors;
DROP FUNCTION IF EXISTS user_factors_org_matches_user();

CREATE OR REPLACE FUNCTION user_mfa_factors_org_matches_user()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
DECLARE
    owner_org uuid;
BEGIN
    SELECT u.org_id INTO owner_org FROM users u WHERE u.id = NEW.user_id;

    IF owner_org IS NULL THEN
        RAISE EXCEPTION 'user_mfa_factors: user % does not exist', NEW.user_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF NEW.org_id <> owner_org THEN
        RAISE EXCEPTION 'user_mfa_factors: org_id % does not own user % (which belongs to %)',
            NEW.org_id, NEW.user_id, owner_org
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER user_mfa_factors_org_matches_user
    BEFORE INSERT OR UPDATE ON user_mfa_factors
    FOR EACH ROW EXECUTE FUNCTION user_mfa_factors_org_matches_user();

COMMENT ON TABLE user_mfa_factors IS
  'Enrolled authentication factors, per docs/PLAN/04. status is pending until the user has proven they can use it (P3-01, renamed P3-02).';


-- `users.mfa_enabled` is the plan's denormalized flag, and it must not drift.
--
-- `docs/PLAN/04` calls it "a fast denormalized flag for the login path" and
-- this table "the source of truth". A denormalized flag that can disagree with
-- its source is worse than no flag: the login path would take a fast decision
-- from a value nobody maintains, and the disagreement is invisible until
-- somebody with MFA enrolled is let in without it.
--
-- So it is maintained by the database rather than by whichever code path
-- happens to remember. An ACTIVE factor sets it; losing the last one clears it.
CREATE OR REPLACE FUNCTION sync_user_mfa_enabled()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
DECLARE
    subject uuid;
BEGIN
    subject := COALESCE(NEW.user_id, OLD.user_id);

    UPDATE users u
       SET mfa_enabled = EXISTS (
               SELECT 1 FROM user_mfa_factors f
                WHERE f.user_id = subject AND f.status = 'active'
           ),
           updated_at = now()
     WHERE u.id = subject;

    RETURN NULL;
END;
$$;

CREATE TRIGGER user_mfa_factors_sync_flag
    AFTER INSERT OR UPDATE OR DELETE ON user_mfa_factors
    FOR EACH ROW EXECUTE FUNCTION sync_user_mfa_enabled();

COMMENT ON FUNCTION sync_user_mfa_enabled() IS
  'Keeps users.mfa_enabled equal to "this user has at least one active factor". A denormalized flag that can disagree with its source is worse than no flag (P3-02).';

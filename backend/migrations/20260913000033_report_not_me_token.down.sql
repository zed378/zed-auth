-- Reverses 20260913000033.
--
-- Outstanding "this wasn't me" links are deleted before the constraint is
-- narrowed, because a row the restored constraint forbids would make the ADD
-- fail and leave the rollback half-applied. The cost is that anybody holding an
-- unused link from a recent anomaly notice loses it — they can still reset their
-- password through the ordinary forgotten-password page.

DELETE FROM user_tokens WHERE purpose = 'report_not_me';

ALTER TABLE user_tokens DROP CONSTRAINT user_tokens_purpose_valid;

ALTER TABLE user_tokens ADD CONSTRAINT user_tokens_purpose_valid
    CHECK (purpose IN ('invite', 'password_reset', 'email_verification'));

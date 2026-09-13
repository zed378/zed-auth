-- A token purpose for "this wasn't me" (P3-08).
--
-- A login-anomaly notice carries a link that lets the account's owner sign out
-- everywhere and start a password reset. That link is a `user_tokens` row, for
-- the reason invitations and resets already are: single-use, short-lived, stored
-- hashed, one set of rules to get right rather than two.
--
-- EXPAND/CONTRACT: this WIDENS a CHECK constraint by one allowed value, which is
-- backward-compatible in both directions. A previous-version instance never
-- writes `report_not_me`, so it is unaffected; and every row that satisfied the
-- old constraint satisfies the new one, so the ADD cannot fail against existing
-- data. The DROP and ADD are in one transaction (golang-migrate runs each file
-- in one), so there is no moment with no constraint at all.
--
-- Rolling back past this is the one direction with a cost: any unused
-- `report_not_me` rows would violate the restored constraint, so the down
-- migration deletes them first — see that file.

ALTER TABLE user_tokens DROP CONSTRAINT user_tokens_purpose_valid;

ALTER TABLE user_tokens ADD CONSTRAINT user_tokens_purpose_valid
    CHECK (purpose IN ('invite', 'password_reset', 'email_verification', 'report_not_me'));

-- Stamp every MFA mandate that is on and has no activation time (P3-13).
--
-- `P3-07` measures the fourteen-day grace from `settings.mfa_required_since`,
-- stamped when a PATCH turns the mandate on. Two ways to have the mandate on
-- never went through that transition:
--
--   * an organization whose `mfa_required` was set during Phase 2, when the
--     console labelled it "not enforced yet", and
--   * an organization CREATED with `mfa_required: true` — the create path did
--     not stamp until this release.
--
-- `authn.RequireMFA` reads a missing stamp as "inside the grace", which was
-- meant to give an upgrading organization a grace starting now. With nothing
-- ever writing the stamp afterwards, the grace never ended: the setting said
-- MFA was required and nobody was ever asked for it.
--
-- The grace starts at this migration, which is the first moment the deadline is
-- a real one. Not backdated to when the flag was set: nobody was told there was
-- a deadline, and a deadline nobody could have known about is not a deadline.
--
-- EXPAND/CONTRACT: data only, and a key a previous-version instance does not
-- read — `P3-06`'s build ignores it and `P3-07`'s build reads it the same way
-- this one does. Idempotent: a stamped row is not touched again.
--
-- The format is RFC 3339 with an explicit `Z`, because Go's time.Time refuses a
-- timestamp without a zone — and a stamp the service cannot parse is read as
-- no stamp at all, which is the bug being fixed.

UPDATE organizations
   SET settings = settings || jsonb_build_object(
           'mfa_required_since',
           to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
 WHERE settings->'mfa_required' = 'true'::jsonb
   AND NOT settings ? 'mfa_required_since';

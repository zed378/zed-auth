-- Records when a password was last set, so max_age_days can be enforced.
--
-- PG-13 (TASKS/BACKLOG.md): docs/PLAN/08 Part B specifies max_age_days as part of
-- the password policy and P0-07 writes it as the default for every
-- organization, but docs/PLAN/04 § users has no column recording when a password
-- was set. As specified, the policy field is unenforceable — a value an
-- administrator can set and the system can never act on, which reads as a
-- control and is not.
--
-- Added here rather than with the login work that enforces it (P1-11), because
-- the column accumulates data. Every password set before it exists is a row
-- where "never recorded" and "set long ago" are the same NULL.
--
-- Additive and backward-compatible per docs/PLAN/14's expand/contract rule: a
-- previous-version instance neither reads nor writes it.
ALTER TABLE users ADD COLUMN password_changed_at timestamptz;

-- Nullable with no backfill, and NULL means NOT EXPIRED (authn.Expired).
--
-- The alternative — unknown means infinitely old — makes deploying this
-- migration a mass lockout of every existing user, which is an outage wearing
-- a security control's clothes.
COMMENT ON COLUMN users.password_changed_at IS
  'When the password was last set. NULL means never recorded, which is not expired (P1-02, PG-13).';

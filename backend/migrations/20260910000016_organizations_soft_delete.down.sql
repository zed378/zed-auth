-- Reverses 20260910000016_organizations_soft_delete.
--
-- Dropping the column makes every soft-deleted organization live again, which
-- is the correct behaviour for code that does not know about deletion but is
-- not a no-op: a tenant somebody deleted becomes reachable. That is the cost
-- of removing the feature, and it is why the up migration is additive —
-- rolling the APPLICATION back does not require this.
ALTER TABLE organizations DROP CONSTRAINT IF EXISTS organizations_deleted_has_no_domain;
DROP INDEX IF EXISTS organizations_live_idx;
ALTER TABLE organizations DROP COLUMN IF EXISTS deleted_at;

-- Reverts to org-only events and the un-hardened partition function.
--
-- Dropping NOT NULL is not automatically reversible: any instance-level row
-- written since the up migration has a NULL org_id and would block restoring
-- the constraint. Those rows are deleted here rather than left to fail the
-- ALTER, which is the only honest option — but it means this down migration
-- DESTROYS AUDIT DATA, and that is why it is here rather than run casually.

DROP INDEX IF EXISTS events_instance_level_idx;

DROP POLICY IF EXISTS events_insert ON events;
DROP POLICY IF EXISTS events_read ON events;

CREATE POLICY events_tenant_read ON events
    FOR SELECT
    USING (org_id = current_org_id());

CREATE POLICY events_tenant_insert ON events
    FOR INSERT
    WITH CHECK (org_id = current_org_id());

DELETE FROM events WHERE org_id IS NULL;
ALTER TABLE events ALTER COLUMN org_id SET NOT NULL;

DROP FUNCTION IF EXISTS ensure_events_partitions_ahead(int);

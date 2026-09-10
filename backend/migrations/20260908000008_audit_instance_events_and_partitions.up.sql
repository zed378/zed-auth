-- Instance-level audit events, and partition maintenance that does not depend
-- on someone remembering.
--
-- EXPAND/CONTRACT: this migration only widens. org_id becomes nullable, which
-- the previous application version tolerates because it never writes NULL and
-- never reads a row it did not write. No column is dropped, renamed, or
-- narrowed, so an application rollback needs no database rollback
-- (docs/PLAN/14 § Rollback Strategy).

-- --- Instance-level events --------------------------------------------------
--
-- Not every auditable event belongs to a tenant. P0-08 added an explicit
-- instance-scoped database path (WithInstanceScope) whose whole purpose is to
-- span organizations, and docs/PLAN/08 Part B requires that path to be auditable.
-- An event recording its use cannot itself carry an org_id, because the point
-- of the event is that no single organization owns the action.
--
-- Signing key rotation (P1-03) is the next one, and the instance audit log
-- screen (docs/UI-UX/08) reads exactly these rows.
--
-- This is a real need rather than speculation, which is the bar docs/PLAN/00 sets.

ALTER TABLE events ALTER COLUMN org_id DROP NOT NULL;

COMMENT ON COLUMN events.org_id IS
  'The owning tenant, or NULL for an instance-level event that belongs to no single organization (instance-scoped access, key rotation). See the RLS policies below.';

-- Partial index: instance-level events are rare and read together, so they get
-- their own index rather than sharing the org-scoped one where they would
-- always be a NULL-key minority.
CREATE INDEX events_instance_level_idx ON events (created_at DESC)
    WHERE org_id IS NULL;


-- --- Policies, extended for instance-level rows -----------------------------
--
-- The org-scoped branch is unchanged. The added branch makes an instance-level
-- event visible exactly when there is no tenant context — that is, only from
-- the instance-scoped path.
--
-- Note what this deliberately does NOT do: it does not let the instance-scoped
-- path read every organization's events. current_org_id() is NULL there, so
-- `org_id = current_org_id()` is still false for every tenant row. Reading the
-- cross-organization audit log is a separate capability that needs a
-- deliberate design (docs/UI-UX/08's Instance audit log screen, P1-20), and
-- granting it accidentally here would be exactly the "normal path with the
-- filter omitted" that docs/PLAN/08 Part B warns against.

DROP POLICY IF EXISTS events_tenant_read ON events;
DROP POLICY IF EXISTS events_tenant_insert ON events;

CREATE POLICY events_read ON events
    FOR SELECT
    USING (
        org_id = current_org_id()
        OR (org_id IS NULL AND current_org_id() IS NULL)
    );

CREATE POLICY events_insert ON events
    FOR INSERT
    WITH CHECK (
        org_id = current_org_id()
        OR (org_id IS NULL AND current_org_id() IS NULL)
    );


-- --- Partition maintenance --------------------------------------------------
--
-- The version in migration 000006 seeded the current and next month and then
-- relied on something calling it again. Nothing did.
--
-- That is a scheduled outage, not an untidiness: when the last partition's
-- range ends, every INSERT into events fails, and because every
-- security-sensitive action writes an audit event (docs/PLAN/09 § Audit), every
-- such action fails with it. At midnight on the 1st, with no deploy and no
-- code change to point at.
--
-- Two fixes here. The service calls ensure_events_partitions_ahead() at
-- startup and daily (internal/audit), so a running deployment maintains
-- itself. And the function below is concurrency-safe, because several
-- instances will call it simultaneously.

-- Replaces the 000006 version, which had two problems:
--
--   1. The IF NOT EXISTS check was a race. Two instances could both find the
--      partition missing and both attempt CREATE TABLE; one would fail with a
--      duplicate error at exactly the moment it mattered.
--   2. It granted privileges to a hard-coded role name, which is fine here but
--      silently wrong in any deployment using a different role.
CREATE OR REPLACE FUNCTION ensure_events_partition(target date)
RETURNS text AS $$
DECLARE
    start_date date := date_trunc('month', target)::date;
    end_date   date := (date_trunc('month', target) + interval '1 month')::date;
    part_name  text := 'events_' || to_char(start_date, 'YYYY_MM');
BEGIN
    -- A transaction-scoped advisory lock keyed on the partition name. Several
    -- service instances start at once and all call this; without the lock they
    -- race between the existence check and the CREATE.
    PERFORM pg_advisory_xact_lock(hashtext('ensure_events_partition:' || part_name));

    IF EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = part_name AND n.nspname = current_schema()
    ) THEN
        RETURN part_name;
    END IF;

    EXECUTE format(
        'CREATE TABLE %I PARTITION OF events FOR VALUES FROM (%L) TO (%L)',
        part_name, start_date, end_date
    );

    -- A new partition must inherit the append-only rule. Without this a
    -- partition created next month would silently accept UPDATE and DELETE,
    -- and the audit log would stop being append-only for recent events only —
    -- the ones an attacker would want to alter (docs/SECURITY/02 §19).
    EXECUTE format('GRANT SELECT, INSERT ON %I TO auth_app', part_name);
    EXECUTE format('REVOKE UPDATE, DELETE ON %I FROM auth_app', part_name);

    RETURN part_name;
END;
$$ LANGUAGE plpgsql;

-- Creates every partition from this month through `months_ahead` months out.
--
-- Called at startup and daily. Running ahead rather than just-in-time means a
-- service that is down for a few days, or a maintenance job that fails
-- silently, still has runway rather than failing at a month boundary.
CREATE OR REPLACE FUNCTION ensure_events_partitions_ahead(months_ahead int DEFAULT 3)
RETURNS TABLE(partition_name text) AS $$
DECLARE
    i int;
BEGIN
    FOR i IN 0..months_ahead LOOP
        partition_name := ensure_events_partition((CURRENT_DATE + (i || ' month')::interval)::date);
        RETURN NEXT;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION ensure_events_partitions_ahead(int) IS
  'Called by the service at startup and daily. Without it, every audited action fails at the month boundary after the last partition ends (P0-12).';

-- Catch up now, so the runway exists from this migration rather than from the
-- next service start.
SELECT * FROM ensure_events_partitions_ahead(3);

-- auth_app must be able to run the maintenance function.
GRANT EXECUTE ON FUNCTION ensure_events_partition(date) TO auth_app;
GRANT EXECUTE ON FUNCTION ensure_events_partitions_ahead(int) TO auth_app;

-- Lets the application maintain events partitions without giving it CREATE on
-- the schema.
--
-- THE PROBLEM
--
-- ensure_events_partition() issues CREATE TABLE, and auth_app has no CREATE
-- privilege on schema public — deliberately. The service needs to run this at
-- startup and daily (P0-12), otherwise every INSERT into events fails at the
-- month boundary after the last partition ends, and with it every action that
-- writes an audit event.
--
-- THE EASY WRONG FIX
--
-- GRANT CREATE ON SCHEMA public TO auth_app would work in one line. It would
-- also let the runtime role create arbitrary tables, which is a permanent
-- privilege expansion to solve a narrow, scheduled maintenance need. The
-- two-role split exists so that compromising the service yields as little as
-- possible (PLAN/08 Part B); handing it DDL rights undoes a good part of that.
--
-- THE FIX
--
-- SECURITY DEFINER. The function runs with the OWNER's privileges regardless
-- of who calls it, which is exactly the PostgreSQL mechanism for exposing one
-- narrowly-scoped privileged operation to a less-privileged role. auth_app
-- gains the ability to create an events partition, and nothing else.
--
-- SECURITY DEFINER'S OWN HAZARD, AND WHY IT IS CLOSED HERE
--
-- A SECURITY DEFINER function runs privileged code with the CALLER's
-- search_path unless told otherwise. A caller who can set search_path can
-- point an unqualified name at their own object and have the owner execute it.
-- `SET search_path` on the function pins it at definition time and closes that.
--
-- The function is also not a general CREATE TABLE primitive: the table name is
-- computed from a date, the statement is always PARTITION OF events, and the
-- only caller-supplied value is a date. There is no input that makes it create
-- something else.

CREATE OR REPLACE FUNCTION ensure_events_partition(target date)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    start_date date := date_trunc('month', target)::date;
    end_date   date := (date_trunc('month', target) + interval '1 month')::date;
    part_name  text := 'events_' || to_char(start_date, 'YYYY_MM');
BEGIN
    -- Several service instances start at once and all call this. Without the
    -- lock they race between the existence check and the CREATE, and one fails
    -- at exactly the moment partition maintenance matters.
    PERFORM pg_advisory_xact_lock(hashtext('ensure_events_partition:' || part_name));

    IF EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = part_name AND n.nspname = 'public'
    ) THEN
        RETURN part_name;
    END IF;

    EXECUTE format(
        'CREATE TABLE public.%I PARTITION OF public.events FOR VALUES FROM (%L) TO (%L)',
        part_name, start_date, end_date
    );

    -- A new partition must inherit the append-only rule. Without this, a
    -- partition created next month would accept UPDATE and DELETE, and the
    -- audit log would stop being append-only for exactly the recent events an
    -- attacker would want to alter (SECURITY/02 §19).
    EXECUTE format('GRANT SELECT, INSERT ON public.%I TO auth_app', part_name);
    EXECUTE format('REVOKE UPDATE, DELETE ON public.%I FROM auth_app', part_name);

    RETURN part_name;
END;
$$;

CREATE OR REPLACE FUNCTION ensure_events_partitions_ahead(months_ahead int DEFAULT 3)
RETURNS TABLE(partition_name text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    i int;
BEGIN
    IF months_ahead < 0 OR months_ahead > 24 THEN
        RAISE EXCEPTION 'months_ahead must be between 0 and 24, got %', months_ahead;
    END IF;

    FOR i IN 0..months_ahead LOOP
        partition_name := ensure_events_partition((CURRENT_DATE + (i || ' month')::interval)::date);
        RETURN NEXT;
    END LOOP;
END;
$$;

-- A SECURITY DEFINER function is executable by PUBLIC unless told otherwise,
-- which would let any role run privileged DDL. Revoke first, then grant to the
-- one role that needs it.
REVOKE ALL ON FUNCTION ensure_events_partition(date) FROM PUBLIC;
REVOKE ALL ON FUNCTION ensure_events_partitions_ahead(int) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION ensure_events_partition(date) TO auth_app;
GRANT EXECUTE ON FUNCTION ensure_events_partitions_ahead(int) TO auth_app;

COMMENT ON FUNCTION ensure_events_partition(date) IS
  'SECURITY DEFINER so auth_app can maintain partitions without CREATE on the schema. search_path is pinned; the only caller input is a date (P0-12).';

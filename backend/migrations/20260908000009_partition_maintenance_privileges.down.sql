-- Returns the functions to SECURITY INVOKER, which means auth_app can no
-- longer maintain partitions. The schema still works; partition creation
-- becomes an owner-only operation again.
REVOKE EXECUTE ON FUNCTION ensure_events_partitions_ahead(int) FROM auth_app;
REVOKE EXECUTE ON FUNCTION ensure_events_partition(date) FROM auth_app;

CREATE OR REPLACE FUNCTION ensure_events_partition(target date)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    start_date date := date_trunc('month', target)::date;
    end_date   date := (date_trunc('month', target) + interval '1 month')::date;
    part_name  text := 'events_' || to_char(start_date, 'YYYY_MM');
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('ensure_events_partition:' || part_name));
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = part_name AND n.nspname = 'public') THEN
        RETURN part_name;
    END IF;
    EXECUTE format('CREATE TABLE public.%I PARTITION OF public.events FOR VALUES FROM (%L) TO (%L)',
                   part_name, start_date, end_date);
    EXECUTE format('GRANT SELECT, INSERT ON public.%I TO auth_app', part_name);
    EXECUTE format('REVOKE UPDATE, DELETE ON public.%I FROM auth_app', part_name);
    RETURN part_name;
END;
$$;

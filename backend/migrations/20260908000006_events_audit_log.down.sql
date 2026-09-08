-- Dropping the parent drops every month partition with it.
DROP TABLE IF EXISTS events;
DROP FUNCTION IF EXISTS ensure_events_partition(date);

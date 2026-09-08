DROP TRIGGER IF EXISTS instances_set_updated_at ON instances;
DROP TABLE IF EXISTS instances;
DROP FUNCTION IF EXISTS set_updated_at();
-- pgcrypto is left in place: it is created by the database's init scripts too,
-- and dropping an extension another migration may depend on is not a safe undo.

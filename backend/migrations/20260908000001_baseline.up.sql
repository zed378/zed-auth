-- Baseline: extensions and the instance root.
--
-- PLAN/04-DATA-MODEL.md § Entity Hierarchy:
--   Instance (deployment) -> Organization (tenant) -> Project -> Application
--
-- Conventions used throughout every migration in this directory:
--
--   * timestamptz everywhere, never timestamp. A naive timestamp on an audit
--     row is ambiguous the moment anyone reads it from another timezone, and
--     incident timelines are always reconstructed across timezones.
--   * Status columns are text + CHECK, not native enum types. Adding a value
--     to a native enum cannot run inside a transaction on older PostgreSQL and
--     removing one is effectively impossible, which fights the expand/contract
--     discipline PLAN/14 requires.
--   * Every tenant-scoped table carries org_id directly, even where it is
--     reachable through a join, because row-level security (P0-08) filters on
--     the column rather than on a join path.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Shared trigger to maintain updated_at without every writer remembering to.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE instances (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT instances_name_not_blank CHECK (length(btrim(name)) > 0)
);

CREATE TRIGGER instances_set_updated_at
    BEFORE UPDATE ON instances
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE instances IS
  'Deployment root. One row per deployment; organizations hang off it (PLAN/04).';

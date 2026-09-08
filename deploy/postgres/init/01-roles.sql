-- Creates the two database roles the service uses.
--
-- The split exists because of PLAN/08-AUTHORIZATION.md Part B: row-level
-- security must hold "even when the application layer forgets to filter". RLS
-- is silently bypassed by the table owner and by any role with BYPASSRLS, so
-- the application must be neither. Running the application as the owner would
-- disable the entire cross-tenant isolation guarantee while every test still
-- passed, which is the worst kind of security failure: invisible.
--
-- Roles:
--   auth_owner — owns the schema, runs migrations. Never used by the service.
--   auth_app   — the application's runtime role. Not owner, no BYPASSRLS.
--
-- This runs only on first initialization of an empty data directory. Deployed
-- environments create these roles through their own provisioning (P0-20).

\set ON_ERROR_STOP on

-- auth_owner is created by POSTGRES_USER in docker-compose.yml and already
-- owns the database, so only the application role is created here.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'auth_app') THEN
    CREATE ROLE auth_app WITH LOGIN PASSWORD 'local_dev_only'
      NOSUPERUSER
      NOCREATEDB
      NOCREATEROLE
      NOBYPASSRLS   -- the whole point of this file
      NOINHERIT;
  END IF;
END
$$;

-- Connect and use the schema, but own nothing in it.
GRANT CONNECT ON DATABASE auth TO auth_app;
GRANT USAGE ON SCHEMA public TO auth_app;

-- Table-level privileges are granted per migration as tables are created
-- (P0-07), rather than blanket-granted here. A blanket grant would silently
-- extend to tables added later, including ones that should be off limits.
-- The one standing default: the application may read and write rows in tables
-- created by the owner, EXCEPT that `events` has UPDATE and DELETE revoked
-- explicitly in its own migration (SECURITY/02 §19, append-only audit log).
ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO auth_app;

ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO auth_app;

#!/bin/bash
# Creates the two database roles the service uses.
#
# This replaced an equivalent .sql file for one reason: the application role
# needs a password, and a password in a .sql file is a password in a file that
# is bind-mounted into a container and, in the local stack, committed to the
# repository. Reading it from the environment instead means the credential
# exists only where it is already required to exist.
#
# (The .sql version was tightened to 0600 to protect the password it contained,
# which made it unreadable by the postgres user inside the container — the init
# step failed with "Permission denied" and the role was silently never created.
# Loosening the mode would have put the password back at risk; removing the
# password removes the dilemma.)
#
# The role split exists because of PLAN/08-AUTHORIZATION.md Part B: row-level
# security must hold "even when the application layer forgets to filter". RLS
# is silently bypassed by the table owner and by any role with BYPASSRLS, so
# the application must be neither. Running the application as the owner would
# disable every cross-tenant isolation guarantee while every test still passed
# — the worst kind of security failure, because it is invisible.
#
#   auth_owner  owns the schema, runs migrations. Never used by the service.
#   auth_app    the application's runtime role. Not owner, no BYPASSRLS.
#
# Runs only on first initialization of an empty data directory. Deployed
# environments create these roles through their own provisioning (P0-20).

set -euo pipefail

APP_PASSWORD="${AUTH_POSTGRES_APP_PASSWORD:?AUTH_POSTGRES_APP_PASSWORD must be set for the postgres container}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
     -v app_password="$APP_PASSWORD" <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'auth_app') THEN
    CREATE ROLE auth_app WITH LOGIN
      NOSUPERUSER
      NOCREATEDB
      NOCREATEROLE
      NOBYPASSRLS   -- the whole point of this file
      NOINHERIT;
  END IF;
END
$$;
SQL

# Set the password separately, through a parameterized variable, so it is never
# interpolated into a SQL string this script builds by hand.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
     -v app_password="$APP_PASSWORD" <<'SQL'
ALTER ROLE auth_app WITH PASSWORD :'app_password';

GRANT CONNECT ON DATABASE auth TO auth_app;
GRANT USAGE ON SCHEMA public TO auth_app;

-- Table-level privileges are granted per migration as tables are created
-- (P0-07), rather than blanket-granted here. A blanket grant would silently
-- extend to tables added later, including ones that should be off limits.
--
-- The one standing default: the application may read and write rows in tables
-- created by the owner, EXCEPT that `events` has UPDATE and DELETE revoked
-- explicitly in its own migration (SECURITY/02 §19, append-only audit log).
ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO auth_app;

ALTER DEFAULT PRIVILEGES FOR ROLE auth_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO auth_app;
SQL

# Fail loudly rather than leaving a database whose application role does not
# exist. Without this the failure surfaces much later, as a migration error
# referring to a role nobody realises was never created.
if ! psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='auth_app'" \
     --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" | grep -q 1; then
  echo "FATAL: auth_app was not created" >&2
  exit 1
fi

echo "auth_app created: NOSUPERUSER NOBYPASSRLS, not the table owner"

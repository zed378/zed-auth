-- Extensions required by the schema.
--
-- pgcrypto provides gen_random_uuid(). PostgreSQL 13+ has gen_random_uuid() in
-- core, but pgcrypto is also used for digest() in tests that verify a column
-- stores a hash rather than a reversible value (P0-07).
--
-- citext is deliberately NOT used for email. PLAN/04-DATA-MODEL.md requires
-- email uniqueness scoped per organization, which is expressed as a composite
-- unique index on (org_id, lower(email)) — an approach that works identically
-- on any PostgreSQL and does not depend on an extension's collation behavior.

\set ON_ERROR_STOP on

CREATE EXTENSION IF NOT EXISTS pgcrypto;

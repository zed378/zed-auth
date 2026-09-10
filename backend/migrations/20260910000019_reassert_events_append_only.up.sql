-- Re-assert the append-only privileges on `events` and every partition of it.
--
-- WHY THIS EXISTS
--
-- `20260908000006` revokes UPDATE and DELETE on `events` from auth_app, and
-- `20260908000009`'s partition function does the same for every partition it
-- creates. Both are correct, and a partition created today gets exactly
-- SELECT and INSERT.
--
-- The staging database did not. `events` and its four live partitions carried
-- `auth_app=arwd` — read, insert, UPDATE and DELETE — so the audit log was not
-- append-only there at all, and the check that would have noticed did not
-- exist.
--
-- The cause is not a bug in either migration. It is that **a migration whose
-- effect is corrected later leaves already-migrated databases in the weaker
-- state**: golang-migrate records a version as applied and never runs it
-- again, so a database migrated before the REVOKE was written keeps the
-- privileges `ALTER DEFAULT PRIVILEGES` handed out at CREATE TABLE. The
-- repository described one thing and the deployment was another, silently, for
-- the property that says an attacker cannot edit the record of what they did.
--
-- Found by a P1-19 smoke test that asked `has_table_privilege` on staging.
--
-- WHAT THIS DOES
--
-- Revokes UPDATE and DELETE from auth_app on the parent and on every existing
-- partition, and grants SELECT and INSERT so a database that somehow has
-- neither ends up correct rather than merely less wrong. Idempotent: on a
-- database that was already right it changes nothing.
--
-- Safe mid-rollout. The application only ever SELECTs and INSERTs `events`;
-- removing privileges it does not use cannot break the running version
-- (docs/PLAN/14 § Rollback Strategy).
DO $$
DECLARE
    part record;
BEGIN
    EXECUTE 'GRANT SELECT, INSERT ON public.events TO auth_app';
    EXECUTE 'REVOKE UPDATE, DELETE ON public.events FROM auth_app';

    FOR part IN
        SELECT c.relname
          FROM pg_class c
          JOIN pg_inherits i ON i.inhrelid = c.oid
          JOIN pg_class p ON p.oid = i.inhparent
         WHERE p.relname = 'events'
    LOOP
        EXECUTE format('GRANT SELECT, INSERT ON public.%I TO auth_app', part.relname);
        EXECUTE format('REVOKE UPDATE, DELETE ON public.%I FROM auth_app', part.relname);
    END LOOP;
END;
$$;

-- Refuse to finish in a state that is still wrong.
--
-- A migration that silently does nothing is how this gap opened in the first
-- place, so this one fails loudly rather than reporting success against a
-- database it did not actually fix.
DO $$
DECLARE
    leaky int;
BEGIN
    SELECT count(*) INTO leaky
      FROM pg_class c
     WHERE (c.relname = 'events'
            OR c.oid IN (SELECT i.inhrelid FROM pg_inherits i
                          JOIN pg_class p ON p.oid = i.inhparent
                         WHERE p.relname = 'events'))
       AND (has_table_privilege('auth_app', c.oid, 'UPDATE')
         OR has_table_privilege('auth_app', c.oid, 'DELETE'));

    IF leaky > 0 THEN
        RAISE EXCEPTION
          'events is still writable by auth_app on % relation(s); the audit log is not append-only', leaky;
    END IF;
END;
$$;

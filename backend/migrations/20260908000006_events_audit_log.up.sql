-- The append-only audit log.
--
-- docs/PLAN/04-DATA-MODEL.md § events and § Retention and Growth.
-- docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md §19 (Logging / Audit Integrity).
--
-- Two properties make this table different from every other one:
--
--   1. It is APPEND-ONLY, enforced at the database level rather than by
--      convention. The application role has no UPDATE or DELETE privilege on
--      it. A convention holds until someone writes an UPDATE; a revoked
--      privilege holds regardless.
--
--   2. It is PARTITIONED BY MONTH from the start. Retrofitting partitioning
--      onto a large table is far more disruptive than starting with it, and
--      this table grows without bound while sitting on the read path of the
--      console's Audit Log screen (docs/PLAN/12).

CREATE TABLE events (
    id             bigint GENERATED ALWAYS AS IDENTITY,

    org_id         uuid NOT NULL,

    -- Nullable: a failed login against an account that does not exist has no
    -- authenticated actor, and neither do system-initiated events
    -- (docs/PLAN/04, P1-14).
    actor_user_id  uuid,

    -- noun.verb.outcome, e.g. user.login.success, role.assigned,
    -- project_grant.revoked (P0-12).
    event_type     text NOT NULL,

    -- Redacted before storage. An audit log that records a password attempt is
    -- a credential store (P0-12).
    payload        jsonb NOT NULL DEFAULT '{}'::jsonb,

    ip             inet,
    request_id     text,

    created_at     timestamptz NOT NULL DEFAULT now(),

    -- The partition key must be part of the primary key on a partitioned
    -- table, so the key is (id, created_at) rather than id alone.
    PRIMARY KEY (id, created_at),

    CONSTRAINT events_type_not_blank CHECK (length(btrim(event_type)) > 0),
    CONSTRAINT events_payload_object CHECK (jsonb_typeof(payload) = 'object')
) PARTITION BY RANGE (created_at);

-- No foreign keys on org_id or actor_user_id, deliberately.
--
-- docs/PLAN/04 § Retention and Growth resolves the GDPR-versus-immutable-audit-log
-- tension by PSEUDONYMIZING rather than deleting: personal data in `users` is
-- erased while actor_user_id is retained as an opaque identifier that no longer
-- resolves to a person. A foreign key would force a cascade or block the
-- erasure entirely, defeating that design. The audit trail must outlive the
-- rows it refers to.

-- Query patterns from docs/UI-UX/08 (Audit Log) and P1-20: recent-first within an
-- organization, filtered by type and actor.
CREATE INDEX events_org_created_at_idx ON events (org_id, created_at DESC);
CREATE INDEX events_type_idx           ON events (event_type, created_at DESC);
CREATE INDEX events_actor_idx          ON events (actor_user_id, created_at DESC)
    WHERE actor_user_id IS NOT NULL;

COMMENT ON TABLE events IS
  'Append-only audit log, month-partitioned. UPDATE/DELETE revoked from the application role (docs/SECURITY/02 §19). Retention: 24 months hot, then archived; erasure requests pseudonymize rather than delete (docs/PLAN/04 § Retention and Growth, OQ-09).';


-- Creates the partition covering a given month, if it does not already exist.
--
-- Called by the migration below to seed the current and next months, and
-- intended to be called by a scheduled job thereafter. A missing partition
-- means an INSERT fails, which would take down every audited action at once —
-- so partition creation must never depend on someone remembering.
CREATE OR REPLACE FUNCTION ensure_events_partition(target date)
RETURNS text AS $$
DECLARE
    start_date date := date_trunc('month', target)::date;
    end_date   date := (date_trunc('month', target) + interval '1 month')::date;
    part_name  text := 'events_' || to_char(start_date, 'YYYY_MM');
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = part_name
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF events FOR VALUES FROM (%L) TO (%L)',
            part_name, start_date, end_date
        );
        -- Each partition inherits the parent's privileges for new grants, but
        -- an explicitly created partition needs the append-only rule applied
        -- to it too.
        EXECUTE format('GRANT SELECT, INSERT ON %I TO auth_app', part_name);
        EXECUTE format('REVOKE UPDATE, DELETE ON %I FROM auth_app', part_name);
    END IF;
    RETURN part_name;
END;
$$ LANGUAGE plpgsql;

-- Seed the current month and the next one. Seeding ahead matters: without the
-- next month's partition, every audited action fails at midnight on the 1st.
SELECT ensure_events_partition(CURRENT_DATE);
SELECT ensure_events_partition((CURRENT_DATE + interval '1 month')::date);


-- --- Append-only enforcement ------------------------------------------------
--
-- The application role may read and insert, and may not update or delete.
-- ALTER DEFAULT PRIVILEGES in the database init script granted UPDATE and
-- DELETE on new tables, so they are explicitly revoked here.

GRANT SELECT, INSERT ON events TO auth_app;
REVOKE UPDATE, DELETE ON events FROM auth_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO auth_app;

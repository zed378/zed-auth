-- P1-15 / PG-21: idempotency records have nowhere to live.
--
-- docs/PLAN/05 Part B requires `Idempotency-Key` support on POST so that an
-- automated provisioning retry is safe. docs/PLAN/04 models no table for it and
-- its "What Is Deliberately Not Stored Here" section does not mention it
-- either way, so this is a gap rather than a decision.
--
-- WHY POSTGRESQL AND NOT REDIS
--
-- Redis holds the short-lived, reconstructible things: authorization codes,
-- the session cache, rate-limit counters. An idempotency record is none of
-- those. The guarantee it makes is to a caller retrying after a failure — and
-- the failure that prompts a retry is exactly the kind of event that also
-- restarts things. A record that vanishes turns a safe retry into a duplicate
-- provisioning call, which is the thing the header exists to prevent.
--
-- docs/PLAN/04 should be amended to describe this table (AGENTS.md rule 9).

CREATE TABLE idempotency_records (
    -- The tenant. Scoped like everything else, so RLS confines a lookup
    -- without the query carrying an org_id predicate somebody could forget.
    org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- The client that sent the key, and the key itself.
    --
    -- The PAIR is the identity, not the key alone. Two clients choosing the
    -- same key must not collide, and — more importantly — one client must not
    -- be able to read another's stored response by guessing a key.
    client_id    uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    key          text NOT NULL,

    -- The request, as a hash rather than as itself.
    --
    -- Storing bodies would put whatever a caller sent — including a password
    -- on a user-creation call — into a durable table, which is what this
    -- codebase refuses to do everywhere else. The hash answers the only
    -- question that matters: is this the SAME request, or a different one
    -- reusing a key?
    request_hash text NOT NULL,

    -- The method and path, so a key reused across endpoints is a conflict
    -- rather than a replay of an unrelated result.
    method       text NOT NULL,
    path         text NOT NULL,

    -- The stored response. NULL while the first request is still in flight,
    -- which is what lets a concurrent duplicate be refused rather than run.
    --
    -- TEXT, NOT jsonb, and the difference is the whole point of a replay. jsonb
    -- is a parsed document: it reorders keys, collapses whitespace and drops
    -- duplicates, so `{"id":"x"}` comes back as `{"id": "x"}`. The retry would
    -- then receive different bytes from the ones the first request received,
    -- which breaks any client comparing a hash, an ETag or a signature — and
    -- silently, since the JSON is still equivalent. A replay returns what was
    -- sent, byte for byte.
    status       integer,
    response     text,

    created_at   timestamptz NOT NULL DEFAULT now(),

    -- When this record stops being honoured.
    --
    -- Twenty-four hours: long enough for any retry that is not a bug, short
    -- enough that the table is not a permanent log of everything anybody ever
    -- posted. Swept rather than trusted to a TTL, because PostgreSQL has none
    -- — see the note below.
    expires_at   timestamptz NOT NULL,

    PRIMARY KEY (org_id, client_id, key),

    CONSTRAINT idempotency_expires_after_creation CHECK (expires_at > created_at),

    -- A completed record has both or neither. A status with no body, or a body
    -- with no status, is a record that cannot be replayed and would be worse
    -- than none.
    CONSTRAINT idempotency_response_complete CHECK (
        (status IS NULL AND response IS NULL) OR
        (status IS NOT NULL AND response IS NOT NULL)
    )
);

COMMENT ON TABLE idempotency_records IS
    'P1-15: replay protection for POST /v1. Swept by expires_at; see PG-21.';

-- The sweep index. Without it the cleanup is a sequential scan over a table
-- that grows with every write in the estate.
CREATE INDEX idempotency_expires_idx ON idempotency_records (expires_at);

-- RLS, like every tenant-scoped table (P0-08).
ALTER TABLE idempotency_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_records FORCE ROW LEVEL SECURITY;

CREATE POLICY idempotency_tenant_isolation ON idempotency_records
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON idempotency_records TO auth_app;


-- --- The sweep ---------------------------------------------------------------
--
-- PostgreSQL has no TTL, so `expires_at` is only real if something deletes.
--
-- And the sweep cannot simply run as auth_app: it is instance-wide maintenance
-- with no tenant to scope it to, and `idempotency_tenant_isolation` means a
-- DELETE with no tenant set matches nothing. It would report success, delete
-- zero rows, and the table would grow forever — the P0-20 shape exactly, a
-- cleanup job that silently does nothing.
--
-- Same answer as P0-12's partition maintenance and P1-11's session sweep:
-- SECURITY DEFINER with a pinned search_path, granted only to auth_app, and
-- narrowed to exactly its question. Note what it CANNOT do — it takes no
-- organization, returns no row contents, and touches nothing that has not
-- already expired. A live record is not reachable through it at all, so it
-- does not become a way to erase somebody's replay protection.
CREATE OR REPLACE FUNCTION sweep_idempotency_records(before timestamptz, max_rows int)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    removed integer;
BEGIN
    WITH doomed AS (
        SELECT org_id, client_id, key
          FROM idempotency_records
         WHERE expires_at < before
         LIMIT max_rows
    ), gone AS (
        DELETE FROM idempotency_records r
              USING doomed d
              WHERE r.org_id = d.org_id AND r.client_id = d.client_id AND r.key = d.key
          RETURNING 1
    )
    SELECT count(*) INTO removed FROM gone;

    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION sweep_idempotency_records(timestamptz, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sweep_idempotency_records(timestamptz, int) TO auth_app;

COMMENT ON FUNCTION sweep_idempotency_records(timestamptz, int) IS
  'Deletes idempotency records past expires_at. Instance-wide maintenance, bounded per call (P1-15).';

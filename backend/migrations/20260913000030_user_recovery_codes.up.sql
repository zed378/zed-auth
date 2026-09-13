-- Single-use recovery codes (P3-04).
--
-- The path back for a user whose factor is gone — a phone lost, wiped, or in a
-- drawer at the office. A factor with no recovery path produces either
-- permanently locked-out users or an ad-hoc support process invented under
-- pressure, and the second is the dangerous one: an undocumented reset path IS
-- an authentication mechanism, with no threat model and nobody accountable for
-- its rules.
--
-- Specification: MEMORY/specs/P3-04-recovery-codes.md.
--
-- EXPAND/CONTRACT: purely additive. A new table, no column touched, no
-- constraint tightened. A previous-version instance running mid-rollout does
-- not read it and is unaffected, and codes issued before a rollback stay valid
-- after one.

CREATE TABLE user_recovery_codes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The tenant, as on `user_factors` and for the same reason. `docs/PLAN/04`
    -- does not name this column; every other tenant-scoped table has one and
    -- RLS is keyed on it, so without it this would be the single table in the
    -- estate where a credential could be read across tenants.
    org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- SHA-256 of the normalised code. **bytea, not the `text` docs/PLAN/04
    -- names, and not Argon2 despite "hashed with the same rigor as a password".**
    --
    -- A deliberate, recorded deviation (PG-39), for two reasons:
    --
    --   * A slow KDF exists because passwords are low-entropy and human-chosen,
    --     so an offline attacker can enumerate the plausible ones. A recovery
    --     code is 80 bits from the CSPRNG — there is no candidate list, and
    --     SHA-256 and Argon2 are equally uncrackable against such an input. The
    --     rigor the plan asks for is delivered by the ENTROPY OF THE CODE.
    --   * A user holds ten codes and a submitted code is compared against all
    --     of them. At Argon2id's tuned parameters that is ten memory-hard
    --     computations per attempt — hundreds of megabytes touched — on a path
    --     an attacker holding the password can drive. The stronger-sounding
    --     primitive would have bought nothing and cost a denial of service.
    code_hash  bytea NOT NULL,

    -- Null until spent. Single-use is enforced by `WHERE used_at IS NULL` in
    -- the consuming UPDATE rather than by a read-then-write, so two browsers
    -- presenting the same code at the same moment cannot both win.
    used_at    timestamptz,

    -- Which batch this code was issued in.
    --
    -- Regeneration invalidates every outstanding code, which is one statement
    -- with this column and a list of ids without it. It also answers "which
    -- batch did this code come from" in an incident, which is the question
    -- asked when a code turns up somewhere it should not be.
    batch_id   uuid NOT NULL,

    created_at timestamptz NOT NULL DEFAULT now(),

    -- SHA-256 is 32 bytes. A row of another length is not a hash this service
    -- wrote, and a lookup against one would silently never match.
    CONSTRAINT user_recovery_codes_hash_length CHECK (length(code_hash) = 32)
);

-- Verification reads "the unspent codes for this user" and compares each in
-- constant time. Ten rows, so the index is about not scanning the table rather
-- than about the comparison.
CREATE INDEX user_recovery_codes_user_idx ON user_recovery_codes (user_id, used_at);
CREATE INDEX user_recovery_codes_org_idx  ON user_recovery_codes (org_id);

-- One row per hash per user. Two identical codes for one user would be a
-- CSPRNG failure, and a unique index turns that from a code that works twice
-- into an insert that fails loudly.
CREATE UNIQUE INDEX user_recovery_codes_unique ON user_recovery_codes (user_id, code_hash);

COMMENT ON TABLE user_recovery_codes IS
  'Single-use MFA recovery codes. SHA-256 of an 80-bit code; see PG-39 for why not Argon2 (P3-04).';


-- Row level security, on the same terms as every tenant-scoped table.
ALTER TABLE user_recovery_codes ENABLE ROW LEVEL SECURITY;

CREATE POLICY user_recovery_codes_tenant_isolation ON user_recovery_codes
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON user_recovery_codes TO auth_app;


-- A code's organization is its user's.
--
-- The same agreement `user_factors` enforces, and missing it would have the
-- same effect: a credential filed under a tenant that does not own the user it
-- belongs to, then invisible under RLS to the tenant that does.
CREATE OR REPLACE FUNCTION user_recovery_codes_org_matches_user()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
DECLARE
    owner_org uuid;
BEGIN
    SELECT u.org_id INTO owner_org FROM users u WHERE u.id = NEW.user_id;

    IF owner_org IS NULL THEN
        RAISE EXCEPTION 'user_recovery_codes: user % does not exist', NEW.user_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF NEW.org_id <> owner_org THEN
        RAISE EXCEPTION 'user_recovery_codes: org_id % does not own user % (which belongs to %)',
            NEW.org_id, NEW.user_id, owner_org
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER user_recovery_codes_org_matches_user
    BEFORE INSERT OR UPDATE ON user_recovery_codes
    FOR EACH ROW EXECUTE FUNCTION user_recovery_codes_org_matches_user();

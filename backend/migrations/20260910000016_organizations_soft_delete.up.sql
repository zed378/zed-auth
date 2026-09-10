-- P1-16 / PG-22: organizations have no soft-delete, and the card requires one.
--
-- docs/PLAN/04 models `status` as active | suspended and no deletion state at
-- all, while P1-16 step 4 says "prefer soft-delete or suspension over hard
-- delete" and its Definition of Done requires deletion to preserve audit
-- history. There is no column for it.
--
-- WHY A TIMESTAMP AND NOT A STATUS VALUE
--
-- `status` is a lifecycle the operator drives: active to suspended and back,
-- reversible, and a suspended organization is one somebody intends to return
-- to. Deletion is a different axis, and folding it into `status` immediately
-- raises "may a deleted organization be suspended?" — a question with no
-- useful answer.
--
-- A timestamp rather than a boolean for the reason `password_changed_at` is
-- one: "when" answers questions "whether" cannot, and the first question asked
-- about a deleted tenant is when it happened.
--
-- WHY NOT A HARD DELETE
--
-- Every table referencing organizations is ON DELETE RESTRICT — users,
-- projects, applications, roles, sessions, refresh_tokens, user_tokens,
-- project_grants — so a hard delete of a populated organization already fails
-- at the database. That is a good property, and nothing here weakens a
-- constraint to make deletion work.
--
-- `events` deliberately has NO foreign key on org_id (P0-12), so audit history
-- outlives whatever happens to this row. The card's "deleting an organization
-- never destroys its audit history" is satisfied by a decision made two phases
-- ago; P1-16 asserts it rather than assuming it.
--
-- docs/PLAN/04 should be amended to describe this column (AGENTS.md rule 9).

ALTER TABLE organizations
    ADD COLUMN deleted_at timestamptz;

COMMENT ON COLUMN organizations.deleted_at IS
    'P1-16: soft delete. NULL means live. A deleted organization is 404 everywhere, including to an INSTANCE_OWNER; its audit history is read through P1-20.';

-- Partial, because the live set is what almost every query wants and it stays
-- small relative to the table as deletions accumulate.
CREATE INDEX organizations_live_idx
    ON organizations (created_at, id)
    WHERE deleted_at IS NULL;

-- A deleted organization must not still hold a domain.
--
-- The domain routes login traffic to a tenant. Leaving it claimed by a deleted
-- organization would mean the address can never be reused, and reusing it is
-- the ordinary case: a customer leaves and comes back, or a domain changes
-- hands. The DELETE handler clears it; this constraint is what makes that a
-- rule rather than a habit.
ALTER TABLE organizations
    ADD CONSTRAINT organizations_deleted_has_no_domain
    CHECK (deleted_at IS NULL OR domain IS NULL);


-- --- Listing and creating ----------------------------------------------------
--
-- `organizations` is the one table whose RLS policy is keyed on `id` rather
-- than `org_id`, because an organization row IS the tenant. That makes two
-- operations impossible under either scope:
--
--   * LIST spans organizations by definition, so tenant scope is wrong.
--   * CREATE happens before the organization exists, so there is no tenant to
--     scope to.
--
-- And instance scope does not help: it sets current_org_id() to NULL, under
-- which `id = current_org_id()` is false for every row. A list would return
-- nothing and a create would fail its WITH CHECK — silently, in the list's
-- case, which is the P0-20 shape again.
--
-- Same answer as P0-12's partition maintenance, P1-11's session sweep and
-- P1-15's idempotency sweep: SECURITY DEFINER, pinned search_path, granted only
-- to auth_app, narrow to exactly its question.
--
-- Note what NEITHER of these can do: neither takes an organization id, so
-- neither is a way to reach a specific tenant's row by guessing one. Reading,
-- updating and deleting a named organization all run under WithTenant(target),
-- where RLS confines them to that single row.

CREATE OR REPLACE FUNCTION organizations_page(
    after_created timestamptz,
    after_id      uuid,
    max_rows      int
)
RETURNS TABLE (
    id         uuid,
    name       text,
    domain     text,
    status     text,
    settings   jsonb,
    created_at timestamptz,
    updated_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    SELECT o.id, o.name, o.domain, o.status, o.settings, o.created_at, o.updated_at
      FROM organizations o
     WHERE o.deleted_at IS NULL
       AND (after_created IS NULL OR (o.created_at, o.id) > (after_created, after_id))
     ORDER BY o.created_at, o.id
     LIMIT max_rows;
$$;

REVOKE ALL ON FUNCTION organizations_page(timestamptz, uuid, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION organizations_page(timestamptz, uuid, int) TO auth_app;

COMMENT ON FUNCTION organizations_page(timestamptz, uuid, int) IS
  'Keyset page of live organizations. Instance-wide by definition; the caller must already hold INSTANCE_OWNER (P1-16).';


-- The instance is not a parameter.
--
-- Phase 1 runs one instance, and taking an instance_id would be a value a
-- caller could supply — which is a tenant selector on a function that runs with
-- the owner's privileges. The function reads the single instance instead, and
-- refuses if there is more than one, so the day Phase 2 makes instances plural
-- this fails loudly rather than silently picking one.
CREATE OR REPLACE FUNCTION organization_create(
    new_name     text,
    new_domain   text,
    new_settings jsonb
)
RETURNS TABLE (
    id         uuid,
    name       text,
    domain     text,
    status     text,
    settings   jsonb,
    created_at timestamptz,
    updated_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
    target_instance uuid;
    instance_count  int;
    created_id      uuid;
BEGIN
    -- The instance is NOT a parameter.
    --
    -- Phase 1 runs one instance, and taking an instance_id would be a tenant
    -- selector supplied by the caller on a function that runs with the owner's
    -- privileges. The function finds the single instance instead, and refuses
    -- if there is more than one — so the day Phase 2 makes instances plural,
    -- this fails loudly rather than silently picking one.
    SELECT count(*) INTO instance_count FROM instances;
    IF instance_count <> 1 THEN
        RAISE EXCEPTION 'organization_create: expected exactly one instance, found %', instance_count;
    END IF;
    SELECT i.id INTO target_instance FROM instances i;

    -- Inserted WITHOUT settings, so the column default applies.
    --
    -- "Use the DEFAULT" is not a value that can be COALESCEd to; omitting the
    -- column is the only way to get it. And the default is what a new tenant
    -- must get: an organization created with the caller's document substituted
    -- for it would have no password policy at all unless the caller happened
    -- to send one.
    INSERT INTO organizations (instance_id, name, domain)
    VALUES (target_instance, new_name, new_domain)
    RETURNING organizations.id INTO created_id;

    IF new_settings IS NULL THEN
        RETURN QUERY
        SELECT o.id, o.name, o.domain, o.status, o.settings, o.created_at, o.updated_at
          FROM organizations o WHERE o.id = created_id;
    ELSE
        -- Merged ONTO the default, not substituted for it. A caller who sets
        -- only mfa_required must not thereby delete the password policy — the
        -- same rule the PATCH merge follows, applied at birth.
        RETURN QUERY
        UPDATE organizations o
           SET settings = o.settings || new_settings
         WHERE o.id = created_id
        RETURNING o.id, o.name, o.domain, o.status, o.settings, o.created_at, o.updated_at;
    END IF;
END;
$$;

REVOKE ALL ON FUNCTION organization_create(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION organization_create(text, text, jsonb) TO auth_app;

COMMENT ON FUNCTION organization_create(text, text, jsonb) IS
  'Creates an organization. Runs before its tenant exists, so no scope can apply; the caller must already hold INSTANCE_OWNER (P1-16).';

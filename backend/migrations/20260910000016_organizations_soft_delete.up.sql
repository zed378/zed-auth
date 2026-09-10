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

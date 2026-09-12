-- The organizations one caller administers (P2-13).
--
-- The console's organization switcher must offer exactly the organizations the
-- caller may act in, and `docs/UI-UX/08` § Cross-Screen Requirements is
-- explicit that the switcher is UI rather than a control — so what it offers
-- has to come from the same place the API's refusals do.
--
-- **Why a SECURITY DEFINER function rather than a query.**
--
-- The same obstacle `organizations_page` hit. `organizations` is the one table
-- whose RLS policy keys on `id` rather than `org_id`, because an organization
-- row IS the tenant. Under instance scope `current_org_id()` is NULL, so
-- `id = current_org_id()` is false for every row and a plain SELECT returns
-- nothing. Under tenant scope it returns exactly one row — the caller's own —
-- which is the opposite of the question.
--
-- **What this function cannot do**, which is the part that makes it safe: it
-- takes no organization id. It takes a user, and derives the organizations
-- from that user's own `manager_roles` rows. There is no argument a caller
-- could supply to reach an organization they do not administer, so running it
-- with the owner's privileges gives away nothing.

CREATE OR REPLACE FUNCTION organizations_administered_by(
    who           uuid,
    after_created timestamptz,
    after_id      uuid,
    max_rows      int
)
RETURNS TABLE (
    id         uuid,
    name       text,
    roles      text[],
    created_at timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
    WITH owner_of_instance AS (
        SELECT EXISTS (
            SELECT 1 FROM manager_roles m
             WHERE m.user_id = who AND m.role = 'INSTANCE_OWNER'
        ) AS yes
    )
    SELECT o.id,
           o.name,
           -- Every role this caller holds over this organization.
           --
           -- INSTANCE_OWNER is unioned in rather than joined, because its
           -- scope is the instance: it has no `manager_roles` row naming any
           -- particular organization, and a join would therefore drop it.
           --
           -- Unordered here. The API orders it highest-first, which is the
           -- role hierarchy rather than the alphabet — and the hierarchy is
           -- defined in Go, in one place (`management.Role`).
           (
               SELECT array_agg(DISTINCT r)
                 FROM (
                     SELECT m.role AS r
                       FROM manager_roles m
                      WHERE m.user_id = who
                        AND m.scope_id = o.id
                        AND m.role IN ('ORG_OWNER', 'ORG_ADMIN')
                     UNION
                     SELECT 'INSTANCE_OWNER'
                      WHERE (SELECT yes FROM owner_of_instance)
                 ) held
           ) AS roles,
           o.created_at
      FROM organizations o
     WHERE o.deleted_at IS NULL
       AND (
             (SELECT yes FROM owner_of_instance)
             OR EXISTS (
                 SELECT 1 FROM manager_roles m
                  WHERE m.user_id = who
                    AND m.scope_id = o.id
                    AND m.role IN ('ORG_OWNER', 'ORG_ADMIN')
             )
           )
       -- PROJECT_OWNER is deliberately NOT here.
       --
       -- Its `scope_id` is a project, not an organization, so including it
       -- would mean resolving project → organization and then offering a
       -- switch into a context where `GET /v1/organizations/{org_id}` and
       -- every screen under it answers 403 (`management.Policy`). A switcher
       -- whose entries lead to refusals is worse than one that omits them.
       AND (after_created IS NULL OR (o.created_at, o.id) > (after_created, after_id))
     ORDER BY o.created_at, o.id
     LIMIT max_rows;
$$;

REVOKE ALL ON FUNCTION organizations_administered_by(uuid, timestamptz, uuid, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION organizations_administered_by(uuid, timestamptz, uuid, int) TO auth_app;

COMMENT ON FUNCTION organizations_administered_by(uuid, timestamptz, uuid, int) IS
  'Keyset page of the organizations one user holds an organization-scoped manager role over, plus every organization if they are INSTANCE_OWNER. Takes no organization id, so it cannot be steered (P2-13).';

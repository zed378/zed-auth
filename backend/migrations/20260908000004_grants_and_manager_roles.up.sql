-- Grants (user x project x roles), cross-organization delegation, and the
-- tiered administrative roles.
--
-- docs/PLAN/04-DATA-MODEL.md § user_grants, § project_grants, § manager_roles.
-- docs/PLAN/08-AUTHORIZATION.md Part C is the source of truth for delegation.

CREATE TABLE project_grants (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    granting_org_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    granted_org_id    uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    -- The subset of the project's roles the receiving organization may assign.
    -- Every delegated assignment is validated against this list ON EVERY
    -- REQUEST, not only at creation (CLAUDE.md, AGENTS.md rule 3,
    -- docs/PLAN/08 Part C, docs/PLAN/18 R-04). A grant narrowed or revoked after
    -- creation must stop working immediately.
    granted_role_keys text[] NOT NULL DEFAULT '{}',

    status            text NOT NULL DEFAULT 'active',

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    revoked_at        timestamptz,

    CONSTRAINT project_grants_status_valid CHECK (status IN ('active', 'revoked')),

    -- Delegating a project to its own owner creates a second, confusing path
    -- to access the organization already has (P4-01).
    CONSTRAINT project_grants_no_self_grant CHECK (granting_org_id <> granted_org_id),

    -- Revocation is a status transition that preserves history, so the audit
    -- trail of a past delegation survives (P4-01).
    CONSTRAINT project_grants_revoked_has_timestamp
        CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX project_grants_project_granted_org_key
    ON project_grants (project_id, granted_org_id)
    WHERE status = 'active';

CREATE INDEX project_grants_granting_org_idx ON project_grants (granting_org_id);
CREATE INDEX project_grants_granted_org_idx  ON project_grants (granted_org_id, status);

CREATE TRIGGER project_grants_set_updated_at
    BEFORE UPDATE ON project_grants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE user_grants (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id        uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    org_id            uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    -- Set when this grant came through a Project Grant delegation. NULL means
    -- a direct grant by the project's owning organization. This column is what
    -- distinguishes delegated from direct everywhere downstream, including the
    -- role-source badge every role-displaying screen must render
    -- (docs/PLAN/04 § user_grants, docs/UI-UX/08 § Cross-Screen Requirements).
    project_grant_id  uuid REFERENCES project_grants(id) ON DELETE CASCADE,

    role_keys         text[] NOT NULL DEFAULT '{}',

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    -- Least privilege (docs/PLAN/08 § Least Privilege): a grant with no roles grants
    -- nothing, so it should not exist at all rather than sit as a confusing
    -- empty row that looks like access.
    CONSTRAINT user_grants_role_keys_not_empty CHECK (cardinality(role_keys) > 0)
);

CREATE UNIQUE INDEX user_grants_user_project_key ON user_grants (user_id, project_id);
CREATE INDEX user_grants_user_idx          ON user_grants (user_id);
CREATE INDEX user_grants_project_idx       ON user_grants (project_id);
CREATE INDEX user_grants_org_idx           ON user_grants (org_id);
CREATE INDEX user_grants_project_grant_idx ON user_grants (project_grant_id)
    WHERE project_grant_id IS NOT NULL;

CREATE TRIGGER user_grants_set_updated_at
    BEFORE UPDATE ON user_grants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON COLUMN user_grants.project_grant_id IS
  'NULL = direct grant. Non-NULL = delegated; role_keys must be a subset of the grant''s granted_role_keys, revalidated on every request (docs/PLAN/08 Part C).';


CREATE TABLE manager_roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Administrative roles governing who administers the Auth Service ITSELF,
    -- distinct from application roles governing access within consumer
    -- applications (docs/PLAN/08 Part C).
    role        text NOT NULL,

    -- The instance / organization / project / project_grant this role applies
    -- to. A PROJECT_OWNER on project X is not one on project Y.
    scope_id    uuid NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT manager_roles_valid CHECK (role IN (
        'INSTANCE_OWNER',
        'ORG_OWNER',
        'ORG_ADMIN',
        'PROJECT_OWNER',
        'PROJECT_GRANT_OWNER'
    ))
);

CREATE UNIQUE INDEX manager_roles_user_role_scope_key
    ON manager_roles (user_id, role, scope_id);
CREATE INDEX manager_roles_user_idx  ON manager_roles (user_id);
CREATE INDEX manager_roles_scope_idx ON manager_roles (scope_id, role);

COMMENT ON TABLE manager_roles IS
  'Tiered administrative roles. Permissions flow strictly downward: INSTANCE_OWNER holds every ORG_OWNER right, never the reverse (docs/PLAN/08 Part C).';

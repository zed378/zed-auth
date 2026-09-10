-- Projects, applications (OIDC/SAML clients), and roles.
--
-- docs/PLAN/04-DATA-MODEL.md § projects, § applications, § roles.
-- docs/PLAN/08-AUTHORIZATION.md Part A for the role model.

CREATE TABLE projects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT projects_name_not_blank CHECK (length(btrim(name)) > 0)
);

CREATE UNIQUE INDEX projects_org_name_key ON projects (org_id, lower(name));
CREATE INDEX projects_org_idx ON projects (org_id);

CREATE TRIGGER projects_set_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE applications (
    -- The id doubles as the OIDC client_id (docs/PLAN/04 § applications).
    id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id                 uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,

    -- Denormalized from projects for row-level security, which filters on the
    -- column rather than following a join (P0-08).
    org_id                     uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    name                       text NOT NULL,
    type                       text NOT NULL,

    -- NULL for public clients (spa, native), which have no secret to protect
    -- and must use PKCE instead (docs/PLAN/04, docs/PLAN/05 Part A).
    -- Only ever a hash: the plaintext is returned once at creation and never
    -- again (docs/UI-UX/08 § Applications tab).
    client_secret_hash         text,

    -- Rotation overlap: a newly issued secret coexists with the previous one
    -- until the old one is retired, so rotating does not require a
    -- simultaneous redeploy of the consumer application (P1-05).
    previous_client_secret_hash        text,
    previous_client_secret_expires_at  timestamptz,

    -- Matched by EXACT STRING COMPARISON at authorization time, never by
    -- prefix or pattern. Prefix matching is the open-redirect vulnerability
    -- (docs/PLAN/09 § Protection Against Common Attacks).
    redirect_uris              text[] NOT NULL DEFAULT '{}',
    post_logout_redirect_uris  text[] NOT NULL DEFAULT '{}',
    grant_types                text[] NOT NULL DEFAULT '{authorization_code,refresh_token}',

    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT applications_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT applications_type_valid CHECK (type IN ('web', 'native', 'spa', 'api', 'saml')),

    -- A public client with a secret is a configuration error worth refusing at
    -- the database level: the secret cannot be kept confidential in a browser
    -- or a shipped mobile binary, so its presence implies a false sense of
    -- security (docs/PLAN/05 Part A, P1-05).
    CONSTRAINT applications_public_clients_have_no_secret
        CHECK (type NOT IN ('spa', 'native') OR client_secret_hash IS NULL),

    -- A rotation window without an expiry never closes.
    CONSTRAINT applications_previous_secret_has_expiry
        CHECK ((previous_client_secret_hash IS NULL) = (previous_client_secret_expires_at IS NULL))
);

CREATE INDEX applications_project_idx ON applications (project_id);
CREATE INDEX applications_org_idx ON applications (org_id);

CREATE TRIGGER applications_set_updated_at
    BEFORE UPDATE ON applications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON COLUMN applications.redirect_uris IS
  'Matched by exact string comparison only. Prefix matching is the open-redirect bug (docs/PLAN/09).';


CREATE TABLE roles (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    key              text NOT NULL,
    display_name     text NOT NULL,

    -- The permission keys this role carries, e.g. {"user:read","billing:write"}
    -- (docs/PLAN/04 § roles, docs/PLAN/08 Part A). An array rather than a join table:
    -- permission keys are the consumer application's own vocabulary, not rows
    -- this service needs referential integrity over.
    permission_keys  text[] NOT NULL DEFAULT '{}',

    -- Built-in roles (org_owner, org_admin) cannot be renamed or deleted
    -- (docs/PLAN/08 Part A).
    is_builtin       boolean NOT NULL DEFAULT false,

    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT roles_key_not_blank CHECK (length(btrim(key)) > 0),
    -- Role keys appear inside JWT claim keys (docs/PLAN/08 Part A § How Role Claims
    -- Get Into the Token), so restricting the character set here prevents a
    -- role name from being able to alter the shape of a token claim.
    CONSTRAINT roles_key_shape CHECK (key ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    CONSTRAINT roles_display_name_not_blank CHECK (length(btrim(display_name)) > 0)
);

-- Roles are scoped per project: "admin" in Project A must never imply "admin"
-- in Project B (docs/PLAN/08 Part A).
CREATE UNIQUE INDEX roles_project_key_key ON roles (project_id, key);
CREATE INDEX roles_org_idx ON roles (org_id);

CREATE TRIGGER roles_set_updated_at
    BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

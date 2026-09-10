-- Organizations (tenants) and users.
--
-- docs/PLAN/04-DATA-MODEL.md § organizations, § users.
-- docs/PLAN/08-AUTHORIZATION.md Part B for the settings shape.

CREATE TABLE organizations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id  uuid NOT NULL REFERENCES instances(id) ON DELETE RESTRICT,
    name         text NOT NULL,

    -- For domain-based tenant resolution and email-domain routing
    -- (docs/PLAN/08 Part B § Tenant Resolution). Verification itself is a later
    -- phase; the column exists now so activating it needs no migration.
    domain       text,

    -- Password policy, mandatory MFA, session lifetime, allowed login methods.
    -- Shape documented in docs/PLAN/08 Part B § Policies per Organization.
    -- Defaults here are the instance defaults the MVP runs with; P2-10 makes
    -- them editable per organization without a schema change.
    settings     jsonb NOT NULL DEFAULT '{
        "password_policy": {"min_length": 12, "require_uppercase": true, "max_age_days": 90},
        "mfa_required": false,
        "session_lifetime_hours": 12,
        "allowed_login_methods": ["password"]
    }'::jsonb,

    status       text NOT NULL DEFAULT 'active',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT organizations_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT organizations_status_valid   CHECK (status IN ('active', 'suspended')),
    CONSTRAINT organizations_settings_object CHECK (jsonb_typeof(settings) = 'object')
);

-- A domain routes login traffic to a tenant, so two tenants claiming the same
-- domain would make tenant resolution ambiguous — and an ambiguous tenant
-- resolution is a cross-tenant access bug waiting to happen (P2-09).
CREATE UNIQUE INDEX organizations_domain_key
    ON organizations (lower(domain))
    WHERE domain IS NOT NULL;

CREATE INDEX organizations_instance_id_idx ON organizations (instance_id);

CREATE TRIGGER organizations_set_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON COLUMN organizations.settings IS
  'Per-org policy. Shape in docs/PLAN/08-AUTHORIZATION.md Part B. Enforced at login by P2-10.';


CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    email          text NOT NULL,
    username       text,

    -- Nullable because login can also happen via social or passwordless only
    -- (docs/PLAN/04 § users). A NOT NULL here would force a fake hash for every
    -- federated user, which is worse than an honest NULL.
    password_hash  text,

    -- Argon2id parameters are encoded into the hash string itself (PHC format),
    -- so a future parameter increase is detectable per row and rehash-on-login
    -- can upgrade it (P1-01).

    status         text NOT NULL DEFAULT 'invited',

    -- Denormalized fast flag for the login path. user_mfa_factors is the source
    -- of truth once Phase 3 creates it (docs/PLAN/04 § user_mfa_factors).
    mfa_enabled    boolean NOT NULL DEFAULT false,

    display_name   text,

    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_status_valid CHECK (status IN ('active', 'locked', 'invited', 'deactivated')),
    CONSTRAINT users_email_not_blank CHECK (length(btrim(email)) > 0),
    -- Not full RFC 5322 validation, which is a well-known rabbit hole. This
    -- catches the structurally impossible; real validation happens in the
    -- application layer where it can return a useful field-level error
    -- (docs/PLAN/05 § Standard Error Format).
    CONSTRAINT users_email_shape CHECK (email LIKE '%_@_%'),
    CONSTRAINT users_username_not_blank CHECK (username IS NULL OR length(btrim(username)) > 0)
);

-- Email is unique PER ORGANIZATION, not globally (docs/PLAN/04 § users).
-- Two different companies may legitimately both employ the same person, and a
-- global constraint would let the first tenant to register an address block
-- every other tenant from ever inviting it.
CREATE UNIQUE INDEX users_org_email_key ON users (org_id, lower(email));

CREATE UNIQUE INDEX users_org_username_key
    ON users (org_id, lower(username))
    WHERE username IS NOT NULL;

-- The login lookup path: resolve a user by tenant and email
-- (docs/PLAN/12-PERFORMANCE.md — /oauth/authorize p95 < 150ms).
-- Covered by users_org_email_key above; this index serves listing and search.
CREATE INDEX users_org_status_idx ON users (org_id, status);
CREATE INDEX users_org_created_at_idx ON users (org_id, created_at DESC);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON COLUMN users.password_hash IS
  'Argon2id, PHC-encoded so parameters are per-row and upgradable (P1-01). NULL for federated/passwordless users.';

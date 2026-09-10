-- Sessions, refresh tokens, signing keys, and single-use user tokens.
--
-- docs/PLAN/04-DATA-MODEL.md § sessions, § refresh_tokens, § signing_keys,
-- § user_tokens.

CREATE TABLE sessions (
    -- Stored as an opaque cookie ID in the browser. The cookie carries only
    -- this id, never user data (P1-11).
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    -- Which factors were actually used. Phase 3 step-up authentication reads
    -- this, and a value that was never trustworthy cannot be made trustworthy
    -- later — so it is populated accurately from Phase 1 (docs/PLAN/04, P1-11).
    auth_methods   text[] NOT NULL DEFAULT '{}',

    -- For the "active sessions" screen and Phase 3 anomaly detection.
    -- inet rather than text: it validates the value and indexes properly.
    ip             inet,
    user_agent     text,

    created_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at   timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,

    -- Revocation is a status transition, not a delete: the record is needed for
    -- audit, and docs/PLAN/17 Phase 3 requires a revoked session to be immediately
    -- unusable rather than TTL-bound.
    revoked_at     timestamptz,

    CONSTRAINT sessions_expires_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_idx ON sessions (user_id, created_at DESC);
CREATE INDEX sessions_org_idx  ON sessions (org_id);
-- Expiry sweep: only rows that could still be live.
CREATE INDEX sessions_active_expiry_idx ON sessions (expires_at)
    WHERE revoked_at IS NULL;

COMMENT ON TABLE sessions IS
  'PostgreSQL is authoritative; Redis holds a short-TTL lookup copy for the silent-SSO path. A revocation writes revoked_at AND invalidates the cache entry in the same operation (docs/PLAN/04, ADR-003).';


CREATE TABLE refresh_tokens (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id          uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    org_id             uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    -- Revoking a session revokes the tokens issued through it (P3-09).
    session_id         uuid REFERENCES sessions(id) ON DELETE SET NULL,

    -- Rotation lineage. All tokens descended from one original issuance share
    -- a family_id; presenting a token that already has replaced_by set is the
    -- reuse signal, and reuse revokes the entire family (docs/PLAN/09, P3-06).
    --
    -- These columns exist from Phase 1 even though rotation ships in Phase 3,
    -- so that phase changes behavior rather than storage shape.
    family_id          uuid NOT NULL,
    replaced_by        uuid REFERENCES refresh_tokens(id) ON DELETE SET NULL,

    -- Never the raw token (docs/PLAN/04's explicit note).
    token_hash         text NOT NULL,

    expires_at         timestamptz NOT NULL,
    -- Absolute lifetime of the whole family, so continuous refreshing cannot
    -- extend a session forever (P3-06).
    family_expires_at  timestamptz NOT NULL,

    revoked            boolean NOT NULL DEFAULT false,
    created_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT refresh_tokens_hash_not_blank CHECK (length(btrim(token_hash)) > 0),
    CONSTRAINT refresh_tokens_family_outlives_token CHECK (family_expires_at >= expires_at)
);

CREATE UNIQUE INDEX refresh_tokens_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_family_idx  ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_user_idx    ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_session_idx ON refresh_tokens (session_id) WHERE session_id IS NOT NULL;


CREATE TABLE signing_keys (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Published in JWKS and carried in the JWT header.
    kid               text NOT NULL,

    -- SAML assertion signing uses a distinct key set from OIDC tokens (P4-07).
    purpose           text NOT NULL DEFAULT 'oidc',

    -- Asymmetric only. Never HS256: consumer services must verify with a public
    -- key and no shared secret (docs/PLAN/07 § Cryptography).
    algorithm         text NOT NULL,

    public_key        text NOT NULL,

    -- A REFERENCE into the secret manager, never the key material itself.
    -- docs/PLAN/02 § Constraints is absolute: no third-party dependency holds the
    -- private signing key outside this service's own infrastructure.
    private_key_ref   text NOT NULL,

    -- next     published in JWKS, not yet signing
    -- current  signing
    -- previous still verifying, no longer signing
    -- retired  removed from JWKS
    --
    -- Several keys are active at once to give rotation the overlap window
    -- docs/PLAN/09 requires, and so that an application rollback never invalidates
    -- tokens signed under a newer key (docs/PLAN/14 § Rollback Strategy).
    status            text NOT NULL DEFAULT 'next',

    created_at        timestamptz NOT NULL DEFAULT now(),
    activated_at      timestamptz,
    retired_at        timestamptz,

    CONSTRAINT signing_keys_purpose_valid   CHECK (purpose IN ('oidc', 'saml')),
    CONSTRAINT signing_keys_status_valid    CHECK (status IN ('next', 'current', 'previous', 'retired')),
    CONSTRAINT signing_keys_algorithm_valid CHECK (algorithm IN ('RS256', 'ES256')),
    CONSTRAINT signing_keys_kid_not_blank   CHECK (length(btrim(kid)) > 0),

    -- The private key column holds a reference such as a Vault path or a
    -- secret-manager ARN. This refuses anything that looks like PEM key
    -- material, which is the "simplification" that would silently violate
    -- docs/PLAN/02's constraint while every test still passed.
    CONSTRAINT signing_keys_private_key_is_a_reference
        CHECK (private_key_ref NOT LIKE '%BEGIN%PRIVATE KEY%')
);

CREATE UNIQUE INDEX signing_keys_kid_key ON signing_keys (kid);
-- Exactly one signing key per purpose at a time: two "current" keys would make
-- which key signed a given token nondeterministic.
CREATE UNIQUE INDEX signing_keys_one_current_per_purpose
    ON signing_keys (purpose)
    WHERE status = 'current';
CREATE INDEX signing_keys_status_idx ON signing_keys (purpose, status);

COMMENT ON COLUMN signing_keys.private_key_ref IS
  'A secret-manager reference, NEVER key material. docs/PLAN/02 § Constraints: no third party holds the private signing key.';


CREATE TABLE user_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    -- One table with a discriminator rather than three near-identical tables:
    -- all three flows share the same security properties (single-use,
    -- short-lived, stored hashed) and the same abuse surface
    -- (docs/SECURITY/02 §10, §12).
    purpose     text NOT NULL,

    -- The raw token exists only in the email that carried it.
    token_hash  text NOT NULL,

    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT user_tokens_purpose_valid
        CHECK (purpose IN ('invite', 'password_reset', 'email_verification')),
    CONSTRAINT user_tokens_hash_not_blank CHECK (length(btrim(token_hash)) > 0),
    CONSTRAINT user_tokens_expires_after_creation CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX user_tokens_hash_key ON user_tokens (token_hash);
CREATE INDEX user_tokens_user_purpose_idx ON user_tokens (user_id, purpose);
CREATE INDEX user_tokens_expiry_idx ON user_tokens (expires_at) WHERE used_at IS NULL;

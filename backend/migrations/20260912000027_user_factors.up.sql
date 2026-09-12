-- Enrolled authentication factors (P3-01).
--
-- One row per factor per user. `P3-02` (TOTP) and `P3-05` (WebAuthn) both land
-- here rather than in tables of their own, because two tables would become two
-- challenge steps, two rate limits and two audit shapes — and the partially
-- authenticated state is the object in this system least worth building twice.
--
-- Specification: MEMORY/specs/P3-01-mfa-framework.md.

CREATE TABLE user_factors (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The tenant, like every other table here. A factor belongs to its user's
    -- organization and the trigger below refuses any other value — the same
    -- agreement `P2-08` found missing on `applications`, where a row could be
    -- filed under the wrong tenant and then hidden by RLS from the one that
    -- owns it.
    org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    type          text NOT NULL,

    -- What the user calls it: "iPhone", "YubiKey". Displayed back to them, so
    -- it is escaped at render and bounded here.
    label         text,

    -- The factor's secret, ENCRYPTED. Null for factor types that hold none —
    -- a WebAuthn credential's public key is not a secret and lives in `data`.
    --
    -- A database read that yields TOTP secrets yields the second factor for
    -- every user at once, which is the difference between a disclosure and a
    -- total compromise of the control.
    secret        bytea,

    -- Per-type material that is not secret: a WebAuthn credential id, a sign
    -- count, an AAGUID. jsonb rather than columns, because the shape differs
    -- per type and this table exists precisely so the types stay one table.
    data          jsonb NOT NULL DEFAULT '{}'::jsonb,

    -- **Null until a code has been verified.**
    --
    -- The field that stops a half-finished enrolment from becoming a factor
    -- nobody can use. An unconfirmed row is invisible to the challenge, does
    -- not satisfy `mfa_required`, and does not prevent removal of the last
    -- working factor. Enrolment is not something that happens when a secret is
    -- generated; it happens when the user proves they can use it.
    confirmed_at  timestamptz,

    last_used_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    -- Only what is implemented. An API that stores "sms" is describing a
    -- capability that does not exist, and a user who enrolled it would have a
    -- factor that can never be challenged — `allowed_login_methods` makes the
    -- same refusal for the same reason.
    CONSTRAINT user_factors_type_valid CHECK (type IN ('totp', 'webauthn')),

    CONSTRAINT user_factors_label_bounded CHECK (label IS NULL OR length(label) <= 64),

    -- A secret that is present must not be empty. An empty bytea would read as
    -- "enrolled" everywhere and verify nothing.
    CONSTRAINT user_factors_secret_not_empty CHECK (secret IS NULL OR length(secret) > 0),

    CONSTRAINT user_factors_data_is_object CHECK (jsonb_typeof(data) = 'object')
);

-- The challenge reads "every confirmed factor for this user", and
-- `mfa_required` reads "does this user have one at all". Both are this index.
CREATE INDEX user_factors_user_idx ON user_factors (user_id, confirmed_at);
CREATE INDEX user_factors_org_idx  ON user_factors (org_id);

-- One TOTP per user.
--
-- Not a limit on factors in general — a user may hold several WebAuthn
-- credentials, one per device, and that is the point of them. TOTP is the
-- authenticator-app secret, and a second one is not a second device: it is an
-- older enrolment nobody removed, which is a live credential the user has
-- forgotten about.
CREATE UNIQUE INDEX user_factors_one_totp
    ON user_factors (user_id)
    WHERE type = 'totp';

COMMENT ON TABLE user_factors IS
  'Enrolled authentication factors. confirmed_at is null until the user has proven they can use it (P3-01).';


-- Row level security, on the same terms as every tenant-scoped table.
ALTER TABLE user_factors ENABLE ROW LEVEL SECURITY;

CREATE POLICY user_factors_tenant_isolation ON user_factors
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON user_factors TO auth_app;


-- A factor's organization is its user's.
--
-- Without this, a factor could be filed under a tenant that does not own the
-- user it belongs to — and then be invisible, under RLS, to the organization
-- that does. `P2-08` found exactly that hole in `applications`, writable since
-- Phase 0 and caught only because somebody went looking.
CREATE OR REPLACE FUNCTION user_factors_org_matches_user()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
DECLARE
    owner_org uuid;
BEGIN
    SELECT u.org_id INTO owner_org FROM users u WHERE u.id = NEW.user_id;

    IF owner_org IS NULL THEN
        RAISE EXCEPTION 'user_factors: user % does not exist', NEW.user_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;

    IF NEW.org_id <> owner_org THEN
        RAISE EXCEPTION 'user_factors: org_id % does not own user % (which belongs to %)',
            NEW.org_id, NEW.user_id, owner_org
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER user_factors_org_matches_user
    BEFORE INSERT OR UPDATE ON user_factors
    FOR EACH ROW EXECUTE FUNCTION user_factors_org_matches_user();

COMMENT ON FUNCTION user_factors_org_matches_user() IS
  'A factor belongs to its user''s organization. Refuses any other value rather than storing a row RLS would then hide from its owner (P3-01).';

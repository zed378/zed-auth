-- P4-10.1: provider subjects identify existing local users (ADR-030).
-- docs/PLAN/04-DATA-MODEL.md § user_identities.
BEGIN;

CREATE TABLE user_identities (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         text NOT NULL,
    provider_subject text COLLATE "C" NOT NULL,
    linked_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT user_identities_provider_valid
        CHECK (provider IN ('google', 'microsoft', 'github')),
    CONSTRAINT user_identities_subject_bounded
        CHECK (octet_length(provider_subject) BETWEEN 1 AND 255),
    CONSTRAINT user_identities_provider_subject_key
        UNIQUE (provider, provider_subject)
);

CREATE INDEX user_identities_user_idx ON user_identities (user_id);

ALTER TABLE user_identities ENABLE ROW LEVEL SECURITY;

-- Ownership follows the referenced user; there is no second tenant column
-- that a writer could disagree with the user's actual organization.
CREATE POLICY user_identities_tenant_isolation ON user_identities
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = user_identities.user_id AND u.org_id = current_org_id()
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = user_identities.user_id AND u.org_id = current_org_id()
    ));

-- Default table privileges include UPDATE. A link must be removed and linked
-- again through P4-11's authenticated, audited flow rather than reassigned.
REVOKE UPDATE ON user_identities FROM auth_app;
GRANT SELECT, INSERT, DELETE ON user_identities TO auth_app;

COMMENT ON TABLE user_identities IS
    'Existing local accounts linked by stable provider subject, never by email. Linking requires an authenticated session (ADR-030, P4-11).';

COMMIT;

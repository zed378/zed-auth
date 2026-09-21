-- P4-08: an AuthnRequest is answered once.
--
-- A SAML service provider sends an AuthnRequest, the user authenticates, and an
-- assertion comes back naming that request in `InResponseTo`. The correlation
-- is what makes an SP-initiated login safe: the service provider knows the
-- assertion answers a login IT started, rather than one an attacker started in
-- the user's browser.
--
-- That guarantee is only worth something if a request is answered ONCE. An
-- AuthnRequest id that can be answered twice is an assertion an attacker can
-- have re-issued, which is the correlation defeated by replay — and the threat
-- review names it as C-3: "InResponseTo must name a stored, single-use
-- AuthnRequest id".
--
-- The row also carries the RelayState, and holding it HERE rather than echoing
-- it through the browser is the point. RelayState is attacker-influenced input
-- that gets echoed back into an HTML form; a value that never leaves the
-- database between the request and the response cannot be tampered with in
-- between, and the size bound is enforced once, on the way in.
--
-- EXPAND/CONTRACT: additive. One new table. A previous-version instance neither
-- reads nor writes it.

BEGIN;

CREATE TABLE saml_authn_requests (
    -- The AuthnRequest's own ID, as the service provider generated it. The
    -- primary key is the single-use guarantee, exactly as it is for
    -- `saml_assertion_ids` (P4-07): two answers to one request collide here
    -- rather than racing in application code.
    id          text PRIMARY KEY,

    org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    sp_id       uuid NOT NULL REFERENCES saml_service_providers (id) ON DELETE CASCADE,

    -- Opaque to this service. Echoed back to the service provider unchanged and
    -- never interpreted — it is the SP's own state, and a service that parsed
    -- it would be parsing something an attacker chose.
    relay_state text,

    -- What the service provider asked for, if it asked.
    --
    -- Stored because it has to SURVIVE the login page. The context check runs
    -- on the way in for a live session, and again on the way back for a user
    -- who had to authenticate — and the second is the one that matters, since
    -- an attacker with no session takes exactly that path. A requirement held
    -- only in memory between the two would be silently dropped, which is the
    -- "never ignored" half of the threat review's C-6.
    requested_authn_context text,

    -- Whether this row remembers an IdP-INITIATED sign-on rather than a real
    -- AuthnRequest (P4-08 F-6).
    --
    -- An IdP-initiated sign-on has no request, and its id is this service's
    -- own bookkeeping: something to put in the login URL so the browser can be
    -- resumed afterwards. The flag is what stops that id being echoed back as
    -- `InResponseTo`, which would tell the service provider the assertion
    -- answers a request it never made — inventing exactly the correlation this
    -- flow does not have.
    idp_initiated boolean NOT NULL DEFAULT false,

    -- Written by the application from one clock, like every other expiring row
    -- here (scripts/check.sh enforces it).
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,

    -- Set when the request is answered. NOT a delete: a consumed request has to
    -- stay visible until it expires, so a second answer can be refused as
    -- "already answered" rather than as "never existed" — an operator reading
    -- the log needs to tell a replay from a typo.
    consumed_at timestamptz,

    CONSTRAINT saml_authn_requests_expiry_after_issue CHECK (expires_at > created_at),

    -- 8 KiB. RelayState is specified as at most 80 BYTES, and every real
    -- service provider stays far inside that; the bound here is generous
    -- because refusing a legitimate login over a specification nobody follows
    -- is a worse failure than storing a few kilobytes. It is still a bound.
    CONSTRAINT saml_authn_requests_relay_state_bounded
        CHECK (relay_state IS NULL OR length(relay_state) <= 8192),

    -- A class reference is a URI. Bounded for the same reason everything else
    -- arriving from a service provider is.
    CONSTRAINT saml_authn_requests_context_bounded
        CHECK (requested_authn_context IS NULL OR length(requested_authn_context) <= 1024)
);

-- Pruning reads this.
CREATE INDEX saml_authn_requests_expires_at_idx ON saml_authn_requests (expires_at);

COMMENT ON TABLE saml_authn_requests IS
    'P4-08: AuthnRequests awaiting an answer. The primary key makes one single-use; consumed_at records that it was answered.';

COMMENT ON COLUMN saml_authn_requests.relay_state IS
    'The service provider''s own opaque state. Echoed unchanged, never interpreted.';

ALTER TABLE saml_authn_requests ENABLE ROW LEVEL SECURITY;

CREATE POLICY saml_authn_requests_tenant_isolation ON saml_authn_requests
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- UPDATE is granted, unlike saml_assertion_ids, because consuming a request is
-- an update of consumed_at. The single-use guarantee does not rest on the grant
-- — it rests on the conditional UPDATE only matching a row whose consumed_at is
-- still null.
GRANT SELECT, INSERT, UPDATE, DELETE ON saml_authn_requests TO auth_app;

COMMIT;

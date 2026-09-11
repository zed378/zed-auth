-- Per-application CORS origins (P1-29, closing PG-17).
--
-- The question this column answers is NOT "where may a code be delivered" —
-- `redirect_uris` answers that. It is "which browser origins may READ a
-- response obtained with this application's tokens". An application can
-- legitimately have one and not the other: a native client has redirect URIs
-- and no origins; a single-page application served from a CDN has both.
--
-- **Per application rather than instance-wide, and that is the whole point.**
-- A single allowlist in configuration would let any tenant's application read
-- any other tenant's user data from a browser, which is the failure PG-17 was
-- opened to prevent.
--
-- Empty by default, so every existing application — and every new one —
-- starts with no cross-origin browser access at all. Additive and
-- backward-compatible: a previous-version instance neither reads nor writes
-- this column, and the default makes its INSERTs valid (docs/PLAN/14,
-- expand/contract).
ALTER TABLE applications
    ADD COLUMN IF NOT EXISTS allowed_origins text[] NOT NULL DEFAULT '{}';

-- The shape is enforced in Go (client.ValidateAllowedOrigin), which can
-- explain itself. This constraint catches only what no validator would ever
-- produce and a direct INSERT might: an origin with a path, a wildcard, or
-- whitespace. Seeding scripts write to this table directly, and PG-26 says
-- they will keep having to.
--
-- Through a function because PostgreSQL refuses a subquery in a CHECK, and
-- there is no array-wide regex operator. The function is IMMUTABLE, which it
-- genuinely is: it reads nothing but its argument.
--
-- The pattern is deliberately loose about the host — anything that is not a
-- slash, query, fragment, wildcard or whitespace — so that `[::1]:3000` and
-- `app.example.com:8443` both pass. Being precise about hostnames here would
-- duplicate the Go validator badly; the job of this constraint is to refuse
-- the shapes that could never be an Origin header at all.
CREATE OR REPLACE FUNCTION origins_are_well_formed(origins text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT coalesce(bool_and(o ~ '^https?://[^/?#*[:space:]]+$'), true)
      FROM unnest(origins) AS o;
$$;

ALTER TABLE applications
    DROP CONSTRAINT IF EXISTS applications_allowed_origins_shape;
ALTER TABLE applications
    ADD CONSTRAINT applications_allowed_origins_shape
        CHECK (origins_are_well_formed(allowed_origins));

COMMENT ON COLUMN applications.allowed_origins IS
    'Browser origins permitted to read responses obtained with this application''s tokens. Exact match against the Origin header; empty means none (P1-29).';

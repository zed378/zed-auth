-- A settings update that names one rule stops discarding the others (P2-14).
--
-- `openapi/openapi.yaml` promises of `PATCH /v1/organizations/{org_id}`:
--
--   > `settings` is merged key by key, so an update naming one setting leaves
--   > the rest as they were.
--
-- `jsonb || jsonb` does not do that. It is a **shallow** merge, so
-- `{"password_policy": {"min_length": 16}}` replaced the entire
-- `password_policy` object and discarded `require_uppercase` and
-- `max_age_days`.
--
-- The failure is not an outage, which is what makes it worth a migration. An
-- administrator raising `min_length` from 12 to 16 — an unambiguous
-- tightening — silently cleared a deliberate `require_uppercase: false` back
-- to the instance default and dropped a deliberate `max_age_days: 0`. Nothing
-- reported it. The policy in force afterwards was one nobody chose, and it
-- looked exactly like the one they did.
--
-- **A general deep merge rather than a special case for `password_policy`.**
-- Hardcoding the one nested key that exists today fixes today's instance and
-- leaves the next nested setting to rediscover it. This removes the class.
--
-- Objects recurse; everything else is replaced. That second half is
-- load-bearing: `allowed_login_methods` is a SET of what is permitted, and a
-- merge that concatenated arrays would make it impossible to ever REMOVE a
-- method — the one direction that matters for a security control.

CREATE OR REPLACE FUNCTION jsonb_deep_merge(base jsonb, patch jsonb)
RETURNS jsonb
LANGUAGE plpgsql
IMMUTABLE
SET search_path = public, pg_temp
AS $$
DECLARE
    merged jsonb;
    key    text;
BEGIN
    IF base IS NULL THEN
        RETURN patch;
    END IF;
    IF patch IS NULL THEN
        RETURN base;
    END IF;

    -- Anything that is not two objects is a replacement, not a merge. This is
    -- the branch that keeps arrays, scalars and nulls behaving the way the
    -- contract says they do.
    IF jsonb_typeof(base) <> 'object' OR jsonb_typeof(patch) <> 'object' THEN
        RETURN patch;
    END IF;

    merged := base;

    FOR key IN SELECT jsonb_object_keys(patch) LOOP
        IF base ? key THEN
            merged := jsonb_set(merged, ARRAY[key],
                                jsonb_deep_merge(base -> key, patch -> key));
        ELSE
            merged := merged || jsonb_build_object(key, patch -> key);
        END IF;
    END LOOP;

    RETURN merged;
END;
$$;

-- plpgsql rather than SQL, so the body is not parsed until first execution.
-- A SQL function cannot reference itself at CREATE time, and this one is
-- recursive by nature.

REVOKE ALL ON FUNCTION jsonb_deep_merge(jsonb, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION jsonb_deep_merge(jsonb, jsonb) TO auth_app;

COMMENT ON FUNCTION jsonb_deep_merge(jsonb, jsonb) IS
  'Recursive merge of two jsonb documents: objects merge key by key, everything else is replaced. Used by the organization settings PATCH so an update naming one rule does not discard its siblings (P2-14).';

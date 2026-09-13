-- Reverses 20260913000032.
--
-- Dropping the function removes reuse DETECTION, not rotation: a rolled-back
-- build still rotates if its code does, and a stolen token then works until it
-- expires. That is the difference worth knowing about before rolling back.

DROP FUNCTION IF EXISTS refresh_token_lineage(text);
DROP INDEX IF EXISTS refresh_tokens_family_idx;

-- Reverses 20260912000026.
--
-- Dropping the function returns the shallow-merge behaviour, which is a
-- rollback to a version that had it. Stored settings are unaffected: this
-- changed how an update combines documents, never what any row holds.

DROP FUNCTION IF EXISTS jsonb_deep_merge(jsonb, jsonb);

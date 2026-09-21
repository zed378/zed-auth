-- Drops the receiving side's name lookup. The grants themselves are untouched;
-- the receiving organization simply sees ids again, as it did before P4-06.

DROP FUNCTION IF EXISTS received_grant_context(uuid[]);

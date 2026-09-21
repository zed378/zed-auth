-- Removes the granting side's read of delegated rows.
--
-- Safe in either order relative to the application rollback: with the policy
-- gone, a delegated row is invisible to the granting tenant again and the
-- readers' join simply finds nothing. Access narrows, never widens.

BEGIN;

DROP POLICY IF EXISTS user_grants_granting_side_read ON user_grants;

COMMIT;

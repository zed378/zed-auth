-- Nothing. Restoring UPDATE and DELETE on the audit log to the application
-- role is not a rollback anybody should be able to perform by running a
-- command; if that is genuinely wanted it is a deliberate GRANT, typed by a
-- person who has said why.
SELECT 1;

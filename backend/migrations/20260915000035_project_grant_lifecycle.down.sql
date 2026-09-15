DROP FUNCTION IF EXISTS granted_organization_names(uuid[]);
DROP FUNCTION IF EXISTS organization_accepts_grants(uuid);
DROP TRIGGER IF EXISTS project_grants_only_narrow ON project_grants;
DROP FUNCTION IF EXISTS project_grants_only_narrow();

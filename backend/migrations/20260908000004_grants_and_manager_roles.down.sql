DROP TABLE IF EXISTS manager_roles;
DROP TRIGGER IF EXISTS user_grants_set_updated_at ON user_grants;
DROP TABLE IF EXISTS user_grants;
DROP TRIGGER IF EXISTS project_grants_set_updated_at ON project_grants;
DROP TABLE IF EXISTS project_grants;

DROP POLICY IF EXISTS project_grants_tenant_isolation ON project_grants;
ALTER TABLE project_grants DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS events_tenant_insert ON events;
DROP POLICY IF EXISTS events_tenant_read ON events;
ALTER TABLE events DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS user_tokens_tenant_isolation ON user_tokens;
ALTER TABLE user_tokens DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS refresh_tokens_tenant_isolation ON refresh_tokens;
ALTER TABLE refresh_tokens DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS sessions_tenant_isolation ON sessions;
ALTER TABLE sessions DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS user_grants_tenant_isolation ON user_grants;
ALTER TABLE user_grants DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS roles_tenant_isolation ON roles;
ALTER TABLE roles DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS applications_tenant_isolation ON applications;
ALTER TABLE applications DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS projects_tenant_isolation ON projects;
ALTER TABLE projects DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS users_tenant_isolation ON users;
ALTER TABLE users DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS organizations_tenant_isolation ON organizations;
ALTER TABLE organizations DISABLE ROW LEVEL SECURITY;

DROP FUNCTION IF EXISTS current_org_id();

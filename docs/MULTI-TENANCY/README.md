# MULTI-TENANCY

This category documents how the Auth Service isolates and organizes tenants: the entity hierarchy from deployment instance down to application, the manager-role hierarchy that decides who administers each level, the cross-organization delegation model (Project Grants), how a request's tenant is actually determined, and the database-level guarantee that keeps tenants apart once it is. It describes the code as built in this repository and cross-references `docs/DATABASE/` for exact schema and SQL, and `docs/PLAN/08-AUTHORIZATION.md` for the design intent it implements — naming every place the two differ and citing the ADR or backlog entry that recorded the decision.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-MULTI-TENANCY-ARCHITECTURE.md`](./00-MULTI-TENANCY-ARCHITECTURE.md) | Overview: hierarchy, isolation model, tenant resolution, cross-org policy | Partially implemented |
| [`01-ORGANIZATION-AND-PROJECT-HIERARCHY.md`](./01-ORGANIZATION-AND-PROJECT-HIERARCHY.md) | Entity hierarchy, manager-role hierarchy, Project Grants | Partially implemented |
| [`02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`](./02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md) | The isolation guarantee, its mechanism, and its verification suite | Partially implemented |
| [`03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md`](./03-CUSTOM-DOMAINS-AND-TENANT-ROUTING.md) | How tenant routing actually works (client- and path-based); custom domains are not built | Partially implemented |

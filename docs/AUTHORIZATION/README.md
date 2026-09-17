# Category: AUTHORIZATION

Multi-tenant Role-Based Access Control (RBAC), Project Grants cross-organization delegation, Attribute-Based Access Control (ABAC), and permission evaluation engine.

## Category Mandate

The `AUTHORIZATION/` directory details **who can do what** in the platform. It defines multi-tenant RBAC, cross-tenant delegation via Project Grants, contextual ABAC evaluation, and server-side authorization enforcement rules.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-AUTHORIZATION-ARCHITECTURE.md` | AuthZ Architecture | Authorization engine architecture, evaluation sequence. |
| `01-MULTI-TENANT-RBAC.md` | Multi-Tenant RBAC | Roles, permissions, org/project scoping rules. |
| `02-PROJECT-GRANTS-DELEGATION.md` | Project Grants Delegation | Cross-organization delegation & `granted_role_keys` subset validation. |
| `03-ATTRIBUTE-BASED-ACCESS-CONTROL.md` | ABAC Specification | Contextual rules (IP, time, device, resource attributes). |
| `04-PERMISSION-EVALUATION-ENGINE.md` | Evaluation Engine | Performance-optimized permission checking pipeline & caching. |

# Authorization

How this service decides who may do what — both the administrative permissions that guard the Management API and the application roles a consumer's product enforces. `docs/PLAN/08-AUTHORIZATION.md` is the design intent and the single source of truth for the model; these documents describe the code that implements it, and record where the two deliberately differ.

Tenant isolation is a separate mechanism that runs underneath all of this: see [`../MULTI-TENANCY/`](../MULTI-TENANCY/). The threat model these controls answer to is [`../SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`](../SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md).

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-AUTHORIZATION-ARCHITECTURE.md`](./00-AUTHORIZATION-ARCHITECTURE.md) | The two systems, where each decision is made, and why they stay separate | Implemented |
| [`01-MULTI-TENANT-RBAC.md`](./01-MULTI-TENANT-RBAC.md) | Roles, permission keys, user grants, the manager-role hierarchy | Implemented |
| [`02-PROJECT-GRANTS-DELEGATION.md`](./02-PROJECT-GRANTS-DELEGATION.md) | Cross-organization delegation: the contract, delegated assignments, grant owners, and how delegated access is decided | Partially implemented |
| [`03-ATTRIBUTE-BASED-ACCESS-CONTROL.md`](./03-ATTRIBUTE-BASED-ACCESS-CONTROL.md) | ABAC, and what has already been decided about it | Draft specification |
| [`04-PERMISSION-EVALUATION-ENGINE.md`](./04-PERMISSION-EVALUATION-ENGINE.md) | Token claims versus `/v1/authz/check`, and the cache | Implemented |
| [`05-ROUTE-PERMISSION-TABLE.md`](./05-ROUTE-PERMISSION-TABLE.md) | Every `/v1` route with its minimum role and scope | Implemented |

## Related

- [`../API/12-ROLE-AND-PERMISSION-API.md`](../API/12-ROLE-AND-PERMISSION-API.md), [`../API/13-PROJECT-GRANT-API.md`](../API/13-PROJECT-GRANT-API.md)
- [`../DATABASE/01-SCHEMA-DEFINITIONS.md`](../DATABASE/01-SCHEMA-DEFINITIONS.md)
- `MEMORY/specs/P4-01-project-grants.md`, `P4-02-delegated-user-grants.md`, `P4-03-project-grant-owner.md`
- `TASKS/BACKLOG.md` PG-30, PG-31, PG-32, PG-34, PG-44

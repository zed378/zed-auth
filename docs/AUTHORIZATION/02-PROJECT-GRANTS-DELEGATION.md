# 02 - Project Grants & Cross-Org Delegation

> Category: **AUTHORIZATION** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify cross-organization project delegation via `project_grants` and subset validation.

## Category Mandate

Enables Organization A to grant access to its Project to Organization B securely.

## Key Topics To Specify

- `project_grants` schema (`project_id`, `granting_org_id`, `granted_org_id`, `granted_role_keys[]`).
- Hard invariant: `granted_role_keys` assigned to users in `granted_org_id` MUST be validated server-side as a subset of the original `project_grants.granted_role_keys` on every request.
- Prevents privilege escalation when granting org revokes a role.

## Reference Architecture & Specification

Validation Pseudocode:
```go
if !isSubset(userGrant.RoleKeys, projectGrant.GrantedRoleKeys) {
    return ErrUnauthorizedRoleEscalation
}
```

## Acceptance Criteria

- [x] Cross-org project grant workflow specified.
- [x] Server-side subset validation invariant enforced on 100% of checks.

## Open Questions

None.

## Related Documents

- `docs/PLAN/08-AUTHORIZATION.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
